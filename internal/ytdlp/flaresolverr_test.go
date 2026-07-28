package ytdlp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func resetFlareState(t *testing.T) {
	t.Helper()
	flareMu.Lock()
	defer flareMu.Unlock()
	flareSessions = map[string]*flareSessionEntry{}
	flareCache = map[string]flareCacheEntry{}
}

func TestMergeNetscapeJarAppendsAndOverrides(t *testing.T) {
	existing := []byte("# Netscape HTTP Cookie File\n" +
		"example.com\tFALSE\t/\tFALSE\t0\tsession\tOLD\n" +
		"example.com\tFALSE\t/\tFALSE\t0\tkeepme\tSTILLHERE\n")
	fresh := []flareCookie{
		{Name: "session", Value: "NEW", Domain: "example.com", Path: "/"},
		{Name: "cf_clearance", Value: "abc", Domain: ".example.com", Path: "/", Secure: true, Expires: 1999999999},
	}
	merged := string(mergeNetscapeJar(existing, fresh))

	if !strings.Contains(merged, "session\tNEW") {
		t.Fatalf("expected updated session cookie, got:\n%s", merged)
	}
	if strings.Contains(merged, "session\tOLD") {
		t.Fatalf("stale session cookie value should have been replaced:\n%s", merged)
	}
	if !strings.Contains(merged, "keepme\tSTILLHERE") {
		t.Fatalf("unrelated existing cookie should be preserved:\n%s", merged)
	}
	if !strings.Contains(merged, "cf_clearance\tabc") {
		t.Fatalf("expected new cf_clearance cookie, got:\n%s", merged)
	}
	if !strings.Contains(merged, ".example.com\tTRUE\t/\tTRUE\t1999999999\tcf_clearance\tabc") {
		t.Fatalf("expected well-formed Netscape line for cf_clearance, got:\n%s", merged)
	}
}

func TestMergeNetscapeJarEmptyExisting(t *testing.T) {
	fresh := []flareCookie{{Name: "a", Value: "1", Domain: "example.com"}}
	merged := string(mergeNetscapeJar(nil, fresh))
	if !strings.Contains(merged, "a\t1") {
		t.Fatalf("expected fresh cookie in merged jar, got:\n%s", merged)
	}
}

func TestMergeNetscapeJarSkipsUnnamedCookies(t *testing.T) {
	fresh := []flareCookie{{Name: "", Value: "1", Domain: "example.com"}}
	merged := string(mergeNetscapeJar(nil, fresh))
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(merged), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines++
		}
	}
	if lines != 0 {
		t.Fatalf("expected no cookie lines, got:\n%s", merged)
	}
}

func TestCookieCacheExpiryClamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got := cookieCacheExpiry([]flareCookie{{Name: "cf_clearance", Expires: float64(now.Add(time.Hour).Unix())}}, now)
	if !got.Equal(now.Add(flareCacheMaxTTL)) {
		t.Fatalf("max clamp: got %v want %v", got, now.Add(flareCacheMaxTTL))
	}
	got = cookieCacheExpiry([]flareCookie{{Name: "cf_clearance", Expires: float64(now.Add(30 * time.Second).Unix())}}, now)
	if !got.Equal(now.Add(flareCacheMinTTL)) {
		t.Fatalf("min clamp: got %v want %v", got, now.Add(flareCacheMinTTL))
	}
	got = cookieCacheExpiry([]flareCookie{{Name: "other", Expires: float64(now.Add(time.Hour).Unix())}}, now)
	if !got.Equal(now.Add(flareCacheMaxTTL)) {
		t.Fatalf("default max: got %v", got)
	}
}

func TestResolveCookiesSessionReuseAndCache(t *testing.T) {
	resetFlareState(t)
	var creates atomic.Int32
	var gets atomic.Int32
	var destroys atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req flareRequest
		_ = json.Unmarshal(body, &req)
		switch req.Cmd {
		case "sessions.create":
			creates.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "Session created", "session": req.Session})
		case "sessions.destroy":
			destroys.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "Session destroyed"})
		case "request.get":
			gets.Add(1)
			if req.Session == "" {
				t.Errorf("request.get missing session")
			}
			if req.SessionTTLMinutes != flareSessionTTLMinutes {
				t.Errorf("session_ttl_minutes=%d", req.SessionTTLMinutes)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"solution": map[string]any{
					"userAgent": "TestAgent/1",
					"cookies": []map[string]any{
						{"name": "cf_clearance", "value": "tok", "domain": ".example.com", "path": "/", "expires": float64(time.Now().Add(10 * time.Minute).Unix()), "secure": true},
					},
				},
			})
		default:
			t.Errorf("unexpected cmd %q", req.Cmd)
			http.Error(w, "bad", 500)
		}
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	path1, ua1, clean1, err := resolveCookies(ctx, srv.URL, "https://www.example.com/watch?v=1", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clean1)
	if ua1 != "TestAgent/1" {
		t.Fatalf("ua=%q", ua1)
	}
	raw, _ := os.ReadFile(path1)
	if !strings.Contains(string(raw), "cf_clearance\ttok") {
		t.Fatalf("jar missing clearance:\n%s", raw)
	}
	if !HasFlareSession("example.com") {
		t.Fatal("expected warm session after first solve")
	}
	if creates.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("after first: creates=%d gets=%d", creates.Load(), gets.Load())
	}

	path2, ua2, clean2, err := resolveCookies(ctx, srv.URL, "https://example.com/watch?v=2", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clean2)
	if ua2 != "TestAgent/1" {
		t.Fatalf("cached ua=%q", ua2)
	}
	_ = path2
	if creates.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("cache should skip Flare HTTP: creates=%d gets=%d", creates.Load(), gets.Load())
	}

	ReleaseFlareSession(ctx, "example.com")
	if HasFlareSession("example.com") {
		t.Fatal("expected session gone after release")
	}
	if destroys.Load() != 1 {
		t.Fatalf("destroys=%d", destroys.Load())
	}

	// Cache still valid: no create/get.
	_, _, clean3, err := resolveCookies(ctx, srv.URL, "https://example.com/watch?v=3", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clean3)
	if creates.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("cache after release: creates=%d gets=%d", creates.Load(), gets.Load())
	}
	if HasFlareSession("example.com") {
		t.Fatal("cache hit must not open session")
	}
}

func TestResolveCookiesInvalidateOnFailure(t *testing.T) {
	resetFlareState(t)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req flareRequest
		_ = json.Unmarshal(body, &req)
		switch req.Cmd {
		case "sessions.create":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "sessions.destroy":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "request.get":
			if n.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": "challenge failed"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"solution": map[string]any{
					"userAgent": "UA",
					"cookies":   []map[string]any{{"name": "cf_clearance", "value": "ok", "domain": ".example.com", "path": "/", "expires": float64(time.Now().Add(10 * time.Minute).Unix())}},
				},
			})
		}
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	_, _, _, err := resolveCookies(ctx, srv.URL, "https://example.com/a", "")
	if err == nil {
		t.Fatal("expected failure")
	}
	if HasFlareSession("example.com") {
		t.Fatal("session should be cleared after failure")
	}

	path, _, clean, err := resolveCookies(ctx, srv.URL, "https://example.com/b", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clean)
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "cf_clearance\tok") {
		t.Fatalf("got:\n%s", raw)
	}
	if !HasFlareSession("example.com") {
		t.Fatal("expected session after recovery")
	}
}

func TestHasFlareSessionFalseInitially(t *testing.T) {
	resetFlareState(t)
	if HasFlareSession("example.com") {
		t.Fatal("expected no session")
	}
}
