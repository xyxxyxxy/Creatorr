package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// flareCookie is one cookie as returned by FlareSolverr's solution.cookies.
type flareCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

type flareRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type flareResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		UserAgent string        `json:"userAgent"`
		Cookies   []flareCookie `json:"cookies"`
	} `json:"solution"`
}

// solveFlareChallenge asks FlareSolverr to visit targetURL with a real browser
// and returns the resulting cookie jar plus the user agent it used.
func solveFlareChallenge(ctx context.Context, flareURL, targetURL string, timeout time.Duration) ([]flareCookie, string, error) {
	reqBody, err := json.Marshal(flareRequest{
		Cmd: "request.get", URL: targetURL, MaxTimeout: int(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, "", err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(flareURL), "/") + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout + 15*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("FlareSolverr unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("FlareSolverr returned %s", resp.Status)
	}
	var answer flareResponse
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return nil, "", fmt.Errorf("decode FlareSolverr response: %w", err)
	}
	if answer.Status != "ok" {
		msg := answer.Message
		if msg == "" {
			msg = "unknown error"
		}
		return nil, "", fmt.Errorf("FlareSolverr failed: %s", msg)
	}
	return answer.Solution.Cookies, answer.Solution.UserAgent, nil
}

// resolveCookies returns the cookies path and user agent yt-dlp should use.
// When flareURL is empty it passes existingCookiesPath through unchanged.
// Otherwise it solves the Cloudflare challenge and merges the resulting
// cookies on top of any existing jar into a temp file; the returned cleanup
// must be called (deferred) once the caller is done using the jar.
func resolveCookies(ctx context.Context, flareURL, targetURL, existingCookiesPath string) (jarPath string, userAgent string, cleanup func(), err error) {
	if strings.TrimSpace(flareURL) == "" {
		return existingCookiesPath, "", func() {}, nil
	}
	cookies, ua, err := solveFlareChallenge(ctx, flareURL, targetURL, 60*time.Second)
	if err != nil {
		return "", "", func() {}, appErr(apperrors.CodeResolveFailed, "FlareSolverr challenge failed", err.Error())
	}
	var existing []byte
	if existingCookiesPath != "" {
		existing, _ = os.ReadFile(existingCookiesPath)
	}
	merged := mergeNetscapeJar(existing, cookies)
	tmp, err := os.CreateTemp("", "ytdlp-flare-cookies-*.txt")
	if err != nil {
		return "", "", func() {}, err
	}
	if _, err := tmp.Write(merged); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", "", func() {}, err
	}
	path := tmp.Name()
	return path, ua, func() { _ = os.Remove(path) }, nil
}

// mergeNetscapeJar rewrites existing cookie lines and appends/overrides them
// with fresh ones from FlareSolverr, keyed by domain+name so the newest value
// for a given cookie always wins.
func mergeNetscapeJar(existing []byte, fresh []flareCookie) []byte {
	type row struct{ domain, name, line string }
	order := []string{}
	rows := map[string]row{}

	keyOf := func(domain, name string) string {
		return strings.ToLower(strings.TrimPrefix(domain, ".")) + "\t" + name
	}
	add := func(domain, name, line string) {
		k := keyOf(domain, name)
		if _, ok := rows[k]; !ok {
			order = append(order, k)
		}
		rows[k] = row{domain: domain, name: name, line: line}
	}

	for _, raw := range strings.Split(string(existing), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimPrefix(line, "#HttpOnly_")
		if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			continue
		}
		fields := strings.Split(trimmed, "\t")
		if len(fields) < 7 {
			continue
		}
		add(fields[0], fields[5], line)
	}

	for _, c := range fresh {
		if c.Name == "" {
			continue
		}
		domain := c.Domain
		if domain == "" {
			continue
		}
		includeSub := "FALSE"
		if strings.HasPrefix(domain, ".") {
			includeSub = "TRUE"
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}
		expiry := "0"
		if c.Expires > 0 {
			expiry = strconv.FormatInt(int64(c.Expires), 10)
		}
		line := strings.Join([]string{domain, includeSub, path, secure, expiry, c.Name, c.Value}, "\t")
		if c.HTTPOnly {
			line = "#HttpOnly_" + line
		}
		add(domain, c.Name, line)
	}

	var buf bytes.Buffer
	buf.WriteString("# Netscape HTTP Cookie File\n")
	for _, k := range order {
		buf.WriteString(rows[k].line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
