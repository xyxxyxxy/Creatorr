package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func testSettingsRouter(t *testing.T) http.Handler {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "settings-autosave.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	h := &web.Handler{Library: library.NewStore(d, q), Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

func TestActionSaveSettingsHTMXSuccess(t *testing.T) {
	r := testSettingsRouter(t)
	form := url.Values{}
	form.Set("redirect", "/settings/queue")
	form.Set(settings.KeyDownloadWantedOrder, settings.DownloadWantedOrderNewest)
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Settings saved.") || !strings.Contains(body, "hx-swap-oob") {
		t.Fatalf("body=%q", body)
	}
}

func TestActionSaveSettingsHTMXCronErrorKeepsValue(t *testing.T) {
	r := testSettingsRouter(t)
	form := url.Values{}
	form.Set("redirect", "/settings/scheduler")
	form.Set(settings.KeyDownloadWantedCron, "not a cron")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Settings-Cron-Key") != settings.KeyDownloadWantedCron {
		t.Fatalf("cron key header=%q", rec.Header().Get("X-Settings-Cron-Key"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="setting-cron-wrap-download_wanted_cron"`) {
		t.Fatalf("missing cron wrap: %q", body)
	}
	if !strings.Contains(body, "not a cron") {
		t.Fatalf("posted value not preserved: %q", body)
	}
	if !strings.Contains(body, "validator-hint") {
		t.Fatalf("missing inline error: %q", body)
	}
}

func TestActionSaveSettingsHTMXCronErrorTargetsInvalidField(t *testing.T) {
	r := testSettingsRouter(t)
	form := url.Values{}
	form.Set("redirect", "/settings/scheduler")
	form.Set(settings.KeyDownloadWantedCron, "@hourly")
	form.Set(settings.KeySyncFilesCron, "not a cron")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Settings-Cron-Key") != settings.KeySyncFilesCron {
		t.Fatalf("cron key header=%q", rec.Header().Get("X-Settings-Cron-Key"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="setting-cron-wrap-sync_files_cron"`) {
		t.Fatalf("wrong field in response: %q", body)
	}
	if !strings.Contains(body, "not a cron") {
		t.Fatalf("invalid value not preserved: %q", body)
	}
}

func TestActionSaveSettingsHTMXCronPersists(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "cron-persist.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	if err := library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	h := &web.Handler{Library: library.NewStore(d, q), Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	form := url.Values{}
	form.Set("redirect", "/settings/scheduler")
	form.Set(settings.KeyDownloadWantedCron, "@daily")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got, err := settings.Get(d, settings.KeyDownloadWantedCron)
	if err != nil {
		t.Fatal(err)
	}
	if got != "@daily" {
		t.Fatalf("stored %q want @daily", got)
	}
}

func TestActionSaveSettingsErrorRedirectsToSourceTab(t *testing.T) {
	r := testSettingsRouter(t)
	form := url.Values{}
	form.Set("redirect", "/settings/scheduler")
	form.Set(settings.KeyDownloadWantedCron, "not a cron")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/settings/scheduler?err=") {
		t.Fatalf("location %q", loc)
	}
}

func TestSettingsConnectExternalServiceHealthSkipped(t *testing.T) {
	r := testSettingsRouter(t)
	for _, svc := range []string{"pot", "flare"} {
		t.Run(svc, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/settings/connect/external-services/"+svc, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "Not configured") {
				t.Fatalf("body=%q", body)
			}
		})
	}
}

func TestSettingsConnectExternalServiceHealthInvalidService(t *testing.T) {
	r := testSettingsRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/connect/external-services/unknown", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSettingsConnectYtDlpLiveFragment(t *testing.T) {
	r := testSettingsRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/connect", nil)
	req.Header.Set("HX-Target", "ytdlp-connect-live")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="ytdlp-connect-live"`) {
		t.Fatalf("missing live root: %q", body)
	}
	if !strings.Contains(body, "Last checked") || !strings.Contains(body, "Installed version") {
		t.Fatalf("missing yt-dlp status fields: %q", body)
	}
	if !strings.Contains(body, "ytdlp_update_channel") {
		t.Fatalf("missing update channel: %q", body)
	}
	if strings.Contains(body, "fieldset-legend") {
		t.Fatalf("full page leaked into live fragment: %q", body)
	}
}
