package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/auth"
	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestSetupAndLoginFlow(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	h := &Handler{Library: library.NewStore(d, queue.NewStore(d)), Queue: queue.NewStore(d)}
	r := chi.NewRouter()
	h.Mount(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Create account") {
		t.Fatalf("setup get: %d", rec.Code)
	}

	form := url.Values{
		"username":         {"op"},
		"password":         {"pass"},
		"password_confirm": {"pass"},
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("setup post: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie after setup")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("setup after done: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Sign in") {
		t.Fatalf("login get: %d", rec.Code)
	}

	bad := url.Values{"username": {"op"}, "password": {"wrong"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(bad.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("bad login: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	ok := url.Values{"username": {"op"}, "password": {"pass"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(ok.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("good login: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("logout: %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected cleared session cookie")
	}
}

func TestSaveAuthAndRegenerateAPIKey(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	hash, _ := auth.HashPassword("pass")
	if _, err := settings.CompleteSetup(d, "admin", hash); err != nil {
		t.Fatal(err)
	}
	oldKey, _ := settings.APIKey(d)
	h := &Handler{Library: library.NewStore(d, queue.NewStore(d)), Queue: queue.NewStore(d)}
	r := chi.NewRouter()
	h.Mount(r)

	form := url.Values{
		"auth_settings":         {"1"},
		"auth_username":         {"admin2"},
		"auth_password":         {"pass2"},
		"auth_password_confirm": {"pass2"},
		"redirect":              {"/settings/general"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save auth: %d", rec.Code)
	}
	u, _ := settings.AuthUsername(d)
	if u != "admin2" {
		t.Fatalf("username=%q", u)
	}
	if !auth.CheckPassword(mustHash(t, d), "pass2") {
		t.Fatal("password not updated")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/actions/regenerate-api-key", strings.NewReader("redirect=/settings/general"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("regen: %d", rec.Code)
	}
	neu, _ := settings.APIKey(d)
	if neu == "" || neu == oldKey {
		t.Fatalf("api key not regenerated: old=%q new=%q", oldKey, neu)
	}
}

func mustHash(t *testing.T, d *db.DB) string {
	t.Helper()
	h, err := settings.AuthPasswordHash(d)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
