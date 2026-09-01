package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func openAuthDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestValidatePassword(t *testing.T) {
	if err := auth.ValidatePassword("ab", "ab"); err == nil {
		t.Fatal("expected min length error")
	}
	if err := auth.ValidatePassword("abcd", "abce"); err == nil {
		t.Fatal("expected mismatch")
	}
	if err := auth.ValidatePassword("abcd", "abcd"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSignVerifyAndEpoch(t *testing.T) {
	secret := "test-secret-32-bytes-long!!!!!!"
	exp := time.Now().UTC().Add(time.Hour)
	val, err := auth.SignSession(secret, "admin", 1, exp)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := auth.VerifySession(secret, val, "admin", 1, time.Now().UTC())
	if !ok || u != "admin" {
		t.Fatalf("verify failed: ok=%v u=%q", ok, u)
	}
	if _, ok := auth.VerifySession(secret, val, "admin", 2, time.Now().UTC()); ok {
		t.Fatal("epoch mismatch should fail")
	}
	if _, ok := auth.VerifySession(secret, val, "other", 1, time.Now().UTC()); ok {
		t.Fatal("user mismatch should fail")
	}
}

func TestAPIKeyMatches(t *testing.T) {
	if !auth.APIKeyMatches("abcdef", "abcdef") {
		t.Fatal("expected match")
	}
	if auth.APIKeyMatches("abcdef", "abcdeg") {
		t.Fatal("expected mismatch")
	}
	if auth.APIKeyMatches("abc", "abcd") {
		t.Fatal("length mismatch")
	}
}

func TestMiddlewareSetupRequired(t *testing.T) {
	d := openAuthDB(t)
	r := chi.NewRouter()
	r.Use(auth.Middleware(d))
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/api/tasks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("home"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != 200 {
		t.Fatalf("health: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	key, _ := settings.APIKey(d)
	req.Header.Set(auth.HeaderAPIKey, key)
	r.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("api before setup: %d", rec.Code)
	}
	var er struct{ Code string }
	_ = json.NewDecoder(rec.Body).Decode(&er)
	if er.Code != "SetupRequired" {
		t.Fatalf("code=%q", er.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("html setup redirect: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("HX-Redirect") != "/setup" {
		t.Fatalf("htmx setup: %d hx=%q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestMiddlewareAfterSetup(t *testing.T) {
	d := openAuthDB(t)
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.CompleteSetup(d, "admin", hash); err != nil {
		t.Fatal(err)
	}
	key, _ := settings.APIKey(d)

	r := chi.NewRouter()
	r.Use(auth.Middleware(d))
	r.Get("/api/tasks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code != 401 {
		t.Fatalf("no creds: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set(auth.HeaderAPIKey, key)
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("api key: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Fatalf("login redirect: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	w := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	if err := auth.IssueSession(w, req, d, "admin"); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cookie: %d", rec.Code)
	}
	// Sliding cookie re-issued.
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected sliding Set-Cookie")
	}

	// Password change bumps epoch; old cookie dies.
	newHash, _ := auth.HashPassword("newer")
	if _, err := settings.UpdateAuthCredentials(d, "admin", newHash, true); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("stale cookie after pw change: %d", rec.Code)
	}
}

func TestCompleteSetupRace(t *testing.T) {
	d := openAuthDB(t)
	hash1, _ := auth.HashPassword("pass1")
	hash2, _ := auth.HashPassword("pass2")
	res1, err := settings.CompleteSetup(d, "alice", hash1)
	if err != nil || res1.AlreadySetup {
		t.Fatalf("first setup: %+v %v", res1, err)
	}
	res2, err := settings.CompleteSetup(d, "bob", hash2)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.AlreadySetup {
		t.Fatal("second setup should lose")
	}
	u, _ := settings.AuthUsername(d)
	if u != "alice" {
		t.Fatalf("username=%q", u)
	}
}

func TestCheckPassword(t *testing.T) {
	hash, err := auth.HashPassword("abcd")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckPassword(hash, "abcd") {
		t.Fatal("check failed")
	}
	if auth.CheckPassword(hash, "abce") {
		t.Fatal("bad password accepted")
	}
	if !auth.CheckLogin("admin", "admin", hash, "abcd") {
		t.Fatal("CheckLogin should succeed")
	}
	if auth.CheckLogin("admin", "nope", hash, "abcd") {
		t.Fatal("wrong user should fail")
	}
	if auth.CheckLogin("admin", "admin", hash, "wrong") {
		t.Fatal("wrong password should fail")
	}
}

func TestSafeNextPath(t *testing.T) {
	cases := map[string]string{
		"":              "/",
		"/":             "/",
		"/series":       "/series",
		"/settings/general": "/settings/general",
		"//evil.com":    "/",
		"/\\evil.com":   "/",
		"/@evil":        "/",
		"https://x":     "/",
		"/ok?x=1&y=2":   "/ok?x=1&y=2",
	}
	for in, want := range cases {
		if got := auth.SafeNextPath(in); got != want {
			t.Fatalf("SafeNextPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRequestIsHTTPSTrustProxy(t *testing.T) {
	auth.SetTrustForwardedProto(false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if auth.RequestIsHTTPS(req) {
		t.Fatal("untrusted forwarded proto should be ignored")
	}
	auth.SetTrustForwardedProto(true)
	if !auth.RequestIsHTTPS(req) {
		t.Fatal("trusted forwarded proto should count")
	}
	auth.SetTrustForwardedProto(false)
}

func TestLoginLimiter(t *testing.T) {
	lim := auth.NewLoginLimiter()
	ip := "203.0.113.9"
	for i := 0; i < 5; i++ {
		if !lim.Allow(ip) {
			t.Fatalf("fail %d should still allow", i)
		}
		lim.Fail(ip)
	}
	if lim.Allow(ip) {
		t.Fatal("expected lock after 5 fails")
	}
	lim.Success(ip)
	if !lim.Allow(ip) {
		t.Fatal("success should clear lock")
	}
}

func TestRegenerateAPIKey(t *testing.T) {
	d := openAuthDB(t)
	old, _ := settings.APIKey(d)
	neu, err := settings.RegenerateAPIKey(d)
	if err != nil {
		t.Fatal(err)
	}
	if neu == "" || neu == old {
		t.Fatalf("old=%q new=%q", old, neu)
	}
	if len(neu) != 32 {
		t.Fatalf("want 32 hex chars, got len=%d %q", len(neu), neu)
	}
}
