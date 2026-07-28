package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

const (
	flareSessionTTLMinutes = 15
	flareCacheMinTTL       = 2 * time.Minute
	flareCacheMaxTTL       = 30 * time.Minute
	flareSolveTimeout      = 60 * time.Second
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
	Cmd               string `json:"cmd"`
	URL               string `json:"url,omitempty"`
	MaxTimeout        int    `json:"maxTimeout,omitempty"`
	Session           string `json:"session,omitempty"`
	SessionTTLMinutes int    `json:"session_ttl_minutes,omitempty"`
}

type flareResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		UserAgent string        `json:"userAgent"`
		Cookies   []flareCookie `json:"cookies"`
	} `json:"solution"`
	Sessions []string `json:"sessions"`
}

type flareCacheEntry struct {
	cookies   []flareCookie
	userAgent string
	expiresAt time.Time
}

type flareSessionEntry struct {
	id       string
	flareURL string
	inflight int
}

var (
	flareMu       sync.Mutex
	flareSessions = map[string]*flareSessionEntry{} // host -> session
	flareCache    = map[string]flareCacheEntry{}    // host -> cookies
)

// HasFlareSession reports whether Creatorr currently holds an open FlareSolverr
// browser session for host (not cookie-cache-only).
func HasFlareSession(host string) bool {
	host = normalizeFlareHost(host)
	if host == "" {
		return false
	}
	flareMu.Lock()
	defer flareMu.Unlock()
	_, ok := flareSessions[host]
	return ok
}

// InvalidateFlareHost drops cookie cache and destroys any open session for host.
// Call on CookieInvalid / auth failure so the next solve recreates a clean browser.
func InvalidateFlareHost(ctx context.Context, host string) {
	host = normalizeFlareHost(host)
	if host == "" {
		return
	}
	flareMu.Lock()
	delete(flareCache, host)
	ent := flareSessions[host]
	delete(flareSessions, host)
	flareMu.Unlock()
	if ent == nil {
		return
	}
	_ = destroyFlareSessionAPI(ctx, ent.flareURL, ent.id)
}

// ReleaseFlareSession destroys the host session when idle (no in-flight solve).
// Call after a domain lane drains (no pending/running tasks).
func ReleaseFlareSession(ctx context.Context, host string) {
	host = normalizeFlareHost(host)
	if host == "" {
		return
	}
	flareMu.Lock()
	ent := flareSessions[host]
	if ent == nil || ent.inflight > 0 {
		flareMu.Unlock()
		return
	}
	id, flareURL := ent.id, ent.flareURL
	delete(flareSessions, host)
	flareMu.Unlock()
	_ = destroyFlareSessionAPI(ctx, flareURL, id)
}

// DestroyAllFlareSessions best-effort closes every Creatorr-tracked session (shutdown).
func DestroyAllFlareSessions(ctx context.Context) {
	flareMu.Lock()
	snapshot := make([]*flareSessionEntry, 0, len(flareSessions))
	for host, ent := range flareSessions {
		snapshot = append(snapshot, ent)
		delete(flareSessions, host)
	}
	flareMu.Unlock()
	for _, ent := range snapshot {
		if ent == nil {
			continue
		}
		_ = destroyFlareSessionAPI(ctx, ent.flareURL, ent.id)
	}
}

func normalizeFlareHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.TrimPrefix(host, "www.")
}

func hostFromTargetURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return normalizeFlareHost(u.Hostname())
}

func flareSessionID(host string) string {
	return "creatorr-" + host
}

func flareEndpoint(flareURL string) string {
	return strings.TrimRight(strings.TrimSpace(flareURL), "/") + "/v1"
}

func postFlare(ctx context.Context, flareURL string, body flareRequest, timeout time.Duration) (flareResponse, error) {
	var zero flareResponse
	reqBody, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, flareEndpoint(flareURL), bytes.NewReader(reqBody))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return zero, fmt.Errorf("FlareSolverr unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, fmt.Errorf("FlareSolverr returned %s", resp.Status)
	}
	var answer flareResponse
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return zero, fmt.Errorf("decode FlareSolverr response: %w", err)
	}
	if answer.Status != "ok" && body.Cmd != "sessions.list" {
		if body.Cmd == "sessions.destroy" {
			return answer, nil
		}
		msg := answer.Message
		if msg == "" {
			msg = "unknown error"
		}
		return zero, fmt.Errorf("FlareSolverr failed: %s", msg)
	}
	return answer, nil
}

func ensureFlareSession(ctx context.Context, flareURL, host string) (sessionID string, release func(), err error) {
	host = normalizeFlareHost(host)
	if host == "" {
		return "", func() {}, fmt.Errorf("FlareSolverr host required")
	}
	id := flareSessionID(host)
	flareMu.Lock()
	if ent, ok := flareSessions[host]; ok && ent != nil {
		ent.inflight++
		ent.flareURL = flareURL
		flareMu.Unlock()
		return ent.id, func() { endFlareInflight(host) }, nil
	}
	flareMu.Unlock()

	_, err = postFlare(ctx, flareURL, flareRequest{Cmd: "sessions.create", Session: id}, 30*time.Second)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already") {
			return "", func() {}, err
		}
	}

	flareMu.Lock()
	if ent, ok := flareSessions[host]; ok && ent != nil {
		ent.inflight++
		ent.flareURL = flareURL
		flareMu.Unlock()
		return ent.id, func() { endFlareInflight(host) }, nil
	}
	flareSessions[host] = &flareSessionEntry{id: id, flareURL: flareURL, inflight: 1}
	flareMu.Unlock()
	return id, func() { endFlareInflight(host) }, nil
}

func endFlareInflight(host string) {
	flareMu.Lock()
	defer flareMu.Unlock()
	if ent := flareSessions[host]; ent != nil && ent.inflight > 0 {
		ent.inflight--
	}
}

func destroyFlareSessionAPI(ctx context.Context, flareURL, sessionID string) error {
	if strings.TrimSpace(flareURL) == "" || sessionID == "" {
		return nil
	}
	_, err := postFlare(ctx, flareURL, flareRequest{Cmd: "sessions.destroy", Session: sessionID}, 15*time.Second)
	return err
}

// solveFlareChallenge asks FlareSolverr to visit targetURL with a real browser
// (reusing a per-host session) and returns cookies plus the user agent it used.
func solveFlareChallenge(ctx context.Context, flareURL, targetURL, sessionID string, timeout time.Duration) ([]flareCookie, string, error) {
	answer, err := postFlare(ctx, flareURL, flareRequest{
		Cmd:               "request.get",
		URL:               targetURL,
		MaxTimeout:        int(timeout / time.Millisecond),
		Session:           sessionID,
		SessionTTLMinutes: flareSessionTTLMinutes,
	}, timeout+15*time.Second)
	if err != nil {
		return nil, "", err
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

func flareCacheGet(host string) (flareCacheEntry, bool) {
	flareMu.Lock()
	defer flareMu.Unlock()
	ent, ok := flareCache[host]
	if !ok || time.Now().After(ent.expiresAt) {
		if ok {
			delete(flareCache, host)
		}
		return flareCacheEntry{}, false
	}
	return ent, true
}

func flareCachePut(host string, cookies []flareCookie, ua string) {
	flareMu.Lock()
	defer flareMu.Unlock()
	flareCache[host] = flareCacheEntry{
		cookies:   cookies,
		userAgent: ua,
		expiresAt: cookieCacheExpiry(cookies, time.Now()),
	}
}

func cookieCacheExpiry(cookies []flareCookie, now time.Time) time.Time {
	earliest := now.Add(flareCacheMaxTTL)
	found := false
	for _, c := range cookies {
		if c.Expires <= 0 {
			continue
		}
		name := strings.ToLower(c.Name)
		if name != "cf_clearance" && !strings.Contains(name, "clearance") && !strings.HasPrefix(name, "cf_") {
			continue
		}
		exp := time.Unix(int64(c.Expires), 0)
		if exp.Before(earliest) {
			earliest = exp
			found = true
		}
	}
	if !found {
		return now.Add(flareCacheMaxTTL)
	}
	minAt := now.Add(flareCacheMinTTL)
	maxAt := now.Add(flareCacheMaxTTL)
	if earliest.Before(minAt) {
		return minAt
	}
	if earliest.After(maxAt) {
		return maxAt
	}
	return earliest
}

func writeMergedJar(existing []byte, cookies []flareCookie) (path string, cleanup func(), err error) {
	merged := mergeNetscapeJar(existing, cookies)
	tmp, err := os.CreateTemp("", "ytdlp-flare-cookies-*.txt")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := tmp.Write(merged); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", func() {}, err
	}
	path = tmp.Name()
	return path, func() { _ = os.Remove(path) }, nil
}

// resolveCookies returns the cookies path and user agent yt-dlp should use.
// When flareURL is empty it passes existingCookiesPath through unchanged.
// Otherwise it uses a per-host FlareSolverr session (and short cookie cache),
// merges cookies on top of any existing jar into a temp file; the returned
// cleanup must be called (deferred) once the caller is done using the jar.
func resolveCookies(ctx context.Context, flareURL, targetURL, existingCookiesPath string) (jarPath string, userAgent string, cleanup func(), err error) {
	if strings.TrimSpace(flareURL) == "" {
		return existingCookiesPath, "", func() {}, nil
	}
	host := hostFromTargetURL(targetURL)
	if host == "" {
		return "", "", func() {}, appErr(apperrors.CodeResolveFailed, "FlareSolverr challenge failed", "target URL has no host")
	}

	var existing []byte
	if existingCookiesPath != "" {
		existing, _ = os.ReadFile(existingCookiesPath)
	}

	if cached, ok := flareCacheGet(host); ok {
		path, clean, werr := writeMergedJar(existing, cached.cookies)
		if werr != nil {
			return "", "", func() {}, werr
		}
		return path, cached.userAgent, clean, nil
	}

	sessionID, release, err := ensureFlareSession(ctx, flareURL, host)
	if err != nil {
		return "", "", func() {}, appErr(apperrors.CodeResolveFailed, "FlareSolverr challenge failed", err.Error())
	}

	cookies, ua, err := solveFlareChallenge(ctx, flareURL, targetURL, sessionID, flareSolveTimeout)
	release()
	if err != nil {
		InvalidateFlareHost(ctx, host)
		return "", "", func() {}, appErr(apperrors.CodeResolveFailed, "FlareSolverr challenge failed", err.Error())
	}
	flareCachePut(host, cookies, ua)

	path, clean, werr := writeMergedJar(existing, cookies)
	if werr != nil {
		return "", "", func() {}, werr
	}
	return path, ua, clean, nil
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
