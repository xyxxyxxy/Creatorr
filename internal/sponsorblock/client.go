package sponsorblock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIBase = "https://sponsor.ajay.app"
	minInterval    = time.Second
)

// Segment is one SponsorBlock segment from the API.
type Segment struct {
	UUID         string
	Category     string
	ActionType   string
	Start        float64
	End          float64
	VideoDuration float64
}

type apiSegment struct {
	Category   string          `json:"category"`
	ActionType string          `json:"actionType"`
	UUID       string          `json:"UUID"`
	Segment    json.RawMessage `json:"segment"`
	VideoDuration float64      `json:"videoDuration"`
}

// Client talks to the SponsorBlock API with a process-wide rate gate.
type Client struct {
	HTTP    *http.Client
	BaseURL string

	mu       sync.Mutex
	lastStart time.Time
}

// DefaultClient is the shared rate-gated client.
var DefaultClient = &Client{}

func (c *Client) base() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPIBase
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) waitGate(ctx context.Context) error {
	if c == nil {
		c = DefaultClient
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := minInterval - time.Since(c.lastStart)
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	c.lastStart = time.Now()
	return nil
}

// FetchSegments loads skip/mark segments for a YouTube video id.
// Soft-fails: returns (nil, warn) on HTTP/API problems rather than hard error
// when soft is desired by caller - here we return error for hard cases and
// ErrSoft for rate/unavailable so callers can warn and continue.
func (c *Client) FetchSegments(ctx context.Context, videoID string, categories []string) ([]Segment, error) {
	if c == nil {
		c = DefaultClient
	}
	videoID = strings.TrimSpace(videoID)
	categories = NormalizeCategoryList(categories)
	if videoID == "" || len(categories) == 0 {
		return nil, nil
	}
	if err := c.waitGate(ctx); err != nil {
		return nil, err
	}

	u, err := url.Parse(c.base() + "/api/skipSegments")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("videoID", videoID)
	for _, cat := range categories {
		q.Add("category", cat)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Creatorr/sponsorblock")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, SoftError{Msg: "SponsorBlock request failed: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		return parseSegmentsJSON(body)
	case http.StatusNotFound:
		return nil, nil // no segments
	case http.StatusTooManyRequests:
		retry := parseRetryAfter(resp.Header.Get("Retry-After"))
		if retry > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retry):
			}
			if err := c.waitGate(ctx); err != nil {
				return nil, err
			}
			req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			req2.Header.Set("Accept", "application/json")
			req2.Header.Set("User-Agent", "Creatorr/sponsorblock")
			resp2, err2 := c.httpClient().Do(req2)
			if err2 != nil {
				return nil, SoftError{Msg: "SponsorBlock retry failed: " + err2.Error()}
			}
			defer func() { _ = resp2.Body.Close() }()
			body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4<<20))
			if resp2.StatusCode == http.StatusOK {
				return parseSegmentsJSON(body2)
			}
			if resp2.StatusCode == http.StatusNotFound {
				return nil, nil
			}
			return nil, SoftError{Msg: fmt.Sprintf("SponsorBlock HTTP %d after retry", resp2.StatusCode)}
		}
		return nil, SoftError{Msg: "SponsorBlock rate limited (429)"}
	default:
		return nil, SoftError{Msg: fmt.Sprintf("SponsorBlock HTTP %d", resp.StatusCode)}
	}
}

// SoftError is a non-fatal SponsorBlock failure (warn + continue).
type SoftError struct{ Msg string }

func (e SoftError) Error() string { return e.Msg }

func IsSoft(err error) bool {
	_, ok := err.(SoftError)
	return ok
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 2 * time.Second
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d > 0 && d < time.Minute {
			return d
		}
	}
	return 2 * time.Second
}

func parseSegmentsJSON(body []byte) ([]Segment, error) {
	body = bytesTrimSpace(body)
	if len(body) == 0 || string(body) == "[]" {
		return nil, nil
	}
	var raw []apiSegment
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, SoftError{Msg: "SponsorBlock JSON: " + err.Error()}
	}
	out := make([]Segment, 0, len(raw))
	for _, a := range raw {
		start, end, ok := parseSegmentPair(a.Segment)
		if !ok {
			continue
		}
		out = append(out, Segment{
			UUID:          strings.TrimSpace(a.UUID),
			Category:      strings.ToLower(strings.TrimSpace(a.Category)),
			ActionType:    strings.TrimSpace(a.ActionType),
			Start:         start,
			End:           end,
			VideoDuration: a.VideoDuration,
		})
	}
	return out, nil
}

func parseSegmentPair(raw json.RawMessage) (start, end float64, ok bool) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 {
		return 0, 0, false
	}
	var pair []float64
	if err := json.Unmarshal(raw, &pair); err == nil && len(pair) >= 2 {
		return pair[0], pair[1], true
	}
	var obj struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Start, obj.End, true
	}
	return 0, 0, false
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
