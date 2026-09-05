package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func seedHandler(t *testing.T, d *db.DB) {
	t.Helper()
}

func TestSeriesListRenders(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/series", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Series") || !strings.Contains(body, "Creatorr") {
		t.Fatalf("unexpected body: %s", truncate(body, 200))
	}
	if !strings.Contains(body, "list-panel") || !strings.Contains(body, "Add series") {
		t.Fatalf("missing list chrome: %s", truncate(body, 300))
	}
	if !strings.Contains(body, `id="series-error-badge"`) {
		t.Fatalf("missing series error nav badge: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `id="series-list-live"`) {
		t.Fatalf("missing series list live: %s", truncate(body, 400))
	}
	// Empty library: big centered Add CTA (no filter bar / header Add).
	if !strings.Contains(body, `for="modal-add-series" class="btn btn-primary grow`) {
		t.Fatalf("missing empty-state Add series CTA: %s", truncate(body, 400))
	}
	if strings.Contains(body, "js-list-filters") {
		t.Fatalf("empty series list should hide filters: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "modal-add-series") || !strings.Contains(body, `name="title"`) {
		t.Fatalf("missing add-series modal or title input: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-add-series-choice`) || !strings.Contains(body, "From channel / playlist URL") || !strings.Contains(body, "Create manually") {
		t.Fatalf("missing add-series path choice: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `js-add-series-pick`) || !strings.Contains(body, ">OR<") {
		t.Fatalf("missing add-series OR pick buttons: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-add-series-steps`) || !strings.Contains(body, `data-add-series-step="fetching"`) {
		t.Fatalf("missing add-series steps / fetching: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `alert alert-error`) || !strings.Contains(body, `js-add-series-fetch-err`) {
		t.Fatalf("missing add-series fetch error alert: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `name="source_url"`) || !strings.Contains(body, `name="scan_cron"`) {
		t.Fatalf("missing add-series URL path fields: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `supportedsites.md`) || !strings.Contains(body, "channel/playlist URL of a") {
		t.Fatalf("missing yt-dlp supported sites hint: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `name="source_label"`) || !strings.Contains(body, `data-add-series-step="series"`) {
		t.Fatalf("missing add-series series step: %s", truncate(body, 400))
	}
	if strings.Contains(body, "add-series-url-preview") || strings.Contains(body, "data-add-series-url-entry") {
		t.Fatalf("old URL-entry/preview still present: %s", truncate(body, 400))
	}

	req2 := httptest.NewRequest(http.MethodGet, "/series/error-count.json", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("error-count status %d: %s", rec2.Code, rec2.Body.String())
	}
	var countPayload struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&countPayload); err != nil {
		t.Fatal(err)
	}
	if countPayload.Count != 0 {
		t.Fatalf("empty library error count=%d", countPayload.Count)
	}
}

func TestSeriesListAudioQualityShowsBest(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	var rootID, profileID int64
	if err := d.SQL.QueryRow(`SELECT id FROM root_folders LIMIT 1`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT id FROM quality_profiles WHERE name = ?`, library.Profile480Name).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title:            "Audio Show",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        false,
		DeliveryMode:     library.DeliveryAudio,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/series", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "best · 0 sources") {
		t.Fatalf("audio series should show best quality, got: %s", truncate(body, 800))
	}
	if strings.Contains(body, library.Profile480Name+" ·") {
		t.Fatalf("audio series must not show assigned profile name: %s", truncate(body, 800))
	}
}

func TestImportPageWithoutSeries(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/import", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Create a series first") {
		t.Fatalf("empty-series gate should be gone: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `id="btn-import"`) || !strings.Contains(body, "File matching") {
		t.Fatalf("import UI should render with no series: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "modal-add-series") || !strings.Contains(body, "Create new series") {
		t.Fatalf("expected add-series modal + Match create row with no series: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "modal-add-video") || !strings.Contains(body, "js-add-video-form") {
		t.Fatalf("expected add-video modal with no series: %s", truncate(body, 400))
	}
	if strings.Contains(body, `id="btn-scan"`) {
		t.Fatalf("scan button should be removed: %s", truncate(body, 400))
	}

	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", SourceURL: "https://example.com/show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/import", nil))
	if rec2.Code != 200 {
		t.Fatalf("with series status %d", rec2.Code)
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, `id="import-full-scan-note"`) || !strings.Contains(body2, "Not all videos may be indexed yet") {
		t.Fatalf("expected full-scan import note after create series: %s", truncate(body2, 400))
	}
	if strings.Contains(body2, `id="import-full-scan-note" role="alert" class="alert alert-warning text-sm hidden"`) {
		t.Fatalf("full-scan note should be visible while full_scan_done=0: %s", truncate(body2, 400))
	}
	if !strings.Contains(body2, `alert alert-warning`) || !strings.Contains(body2, "incomplete on one or more sources") {
		t.Fatalf("expected warning full-scan import note: %s", truncate(body2, 400))
	}
	if !strings.Contains(body2, `id="btn-import"`) || !strings.Contains(body2, "File matching") {
		t.Fatalf("expected import UI with series: %s", truncate(body2, 400))
	}
	if strings.Contains(body2, `id="btn-scan"`) {
		t.Fatalf("scan button should be removed: %s", truncate(body2, 400))
	}
	if strings.Contains(body2, "Create a series first") {
		t.Fatalf("empty-series gate should be gone: %s", truncate(body2, 400))
	}
	if !strings.Contains(body2, "modal-add-series") || !strings.Contains(body2, "Create new series") {
		t.Fatalf("expected add-series modal + Match create row when series exist: %s", truncate(body2, 400))
	}
	if !strings.Contains(body2, "modal-add-video") || !strings.Contains(body2, "js-add-video-form") {
		t.Fatalf("expected add-video modal when series exist: %s", truncate(body2, 400))
	}
	if strings.Contains(body2, `id="import-match-create"`) {
		t.Fatalf("inline create panel should be removed: %s", truncate(body2, 400))
	}
}

func TestOverviewRenders(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/"`) || !strings.Contains(body, "Creatorr") {
		t.Fatalf("missing brand link to overview: %s", truncate(body, 400))
	}
	if strings.Contains(body, ">Overview</a>") {
		t.Fatalf("overview should not be a nav menu item: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Overview") || !strings.Contains(body, `class="stats`) {
		t.Fatalf("missing overview stats: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "modal-add-series") || !strings.Contains(body, "Add series") {
		t.Fatalf("missing add-series on overview: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-add-series-choice`) || !strings.Contains(body, "From channel / playlist URL") {
		t.Fatalf("missing add-series path choice on overview: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "stat-title") || !strings.Contains(body, "On disk") {
		t.Fatalf("missing stat blocks: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Recent additions") {
		t.Fatalf("missing recent additions section: %s", truncate(body, 400))
	}
}

func TestActionRunScheduledQueuesSyncFiles(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "run-sched.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'v1', 'One', 'wanted')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	form := strings.NewReader("key=" + settings.KeySyncFilesCron + "&redirect=/tasks")
	req := httptest.NewRequest(http.MethodPost, "/actions/run-scheduled", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "ok=sync-files-queued") {
		t.Fatalf("location=%q", loc)
	}
	busy, err := q.HasPendingOrRunningKind(queue.KindSyncFiles, queue.SystemDomain)
	if err != nil || !busy {
		t.Fatalf("expected sync_files queued, busy=%v err=%v", busy, err)
	}
}

func TestTasksShowsSoftPausedHostWithoutDomainsRow(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	if err := domains.SetPaused(d, "tylerraw.com", true); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tylerraw.com") {
		t.Fatalf("soft-paused host missing from tasks page")
	}
	if !strings.Contains(body, `data-tip="Resume"`) {
		t.Fatalf("expected Resume control for soft-paused lane")
	}
}

func TestSettingsAndTasksUseListPanel(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	for _, path := range []string{"/settings/general", "/settings/connect", "/settings/library", "/settings/maintenance", "/settings/scheduler", "/settings/queue", "/settings/domains", "/tasks", "/history", "/stats"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if path == "/settings/domains" {
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("%s want redirect, got %d", path, rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/settings/queue" {
				t.Fatalf("%s redirect=%q", path, loc)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, rec.Body.String())
		}
		if strings.HasPrefix(path, "/settings/") && strings.Contains(rec.Body.String(), "<details open") {
			t.Fatalf("%s settings nav submenu should close after navigation", path)
		}
		if path == "/settings/general" {
			body := rec.Body.String()
			if !strings.Contains(body, "Authentication") || !strings.Contains(body, "Appearance") || !strings.Contains(body, `name="theme-picker"`) || !strings.Contains(body, `value="cyberpunk"`) {
				t.Fatalf("%s missing auth/theme", path)
			}
			if !strings.Contains(body, "modal-change-credentials") || !strings.Contains(body, "Change username / password") {
				t.Fatalf("%s missing credentials modal", path)
			}
			if !strings.Contains(body, "js-change-credentials-form") || !strings.Contains(body, "js-auth-username") {
				t.Fatalf("%s missing credentials validator hooks", path)
			}
			if !strings.Contains(body, "validator-hint") || !strings.Contains(body, "Username is required") {
				t.Fatalf("%s missing credentials validator hints", path)
			}
			if strings.Contains(body, "External services") || strings.Contains(body, "FlareSolverr URL") || strings.Contains(body, "modal-add-notify-channel") {
				t.Fatalf("%s still has Connect content", path)
			}
			if strings.Contains(body, "fieldset-legend") && strings.Contains(body, "yt-dlp") {
				t.Fatalf("%s still has yt-dlp settings", path)
			}
			if strings.Contains(body, `id="theme-menu"`) || strings.Contains(body, `data-theme-toggle`) {
				t.Fatalf("%s still has navbar theme controls", path)
			}
			continue
		}
		if path == "/settings/connect" {
			body := rec.Body.String()
			if !strings.Contains(body, "fieldset-legend") || !strings.Contains(body, "yt-dlp") || !strings.Contains(body, "ytdlp_update_channel") {
				t.Fatalf("%s missing yt-dlp settings", path)
			}
			if !strings.Contains(body, "External services") || !strings.Contains(body, "FlareSolverr URL") || !strings.Contains(body, "PO token provider URL") {
				t.Fatalf("%s missing external service URL joins", path)
			}
			if !strings.Contains(body, "CREATORR_FLARESOLVERR_URL") || !strings.Contains(body, "CREATORR_POT_PROVIDER_URL") {
				t.Fatalf("%s missing env hints", path)
			}
			if !strings.Contains(body, "Enable &#39;PO token fetch&#39; below") {
				t.Fatalf("%s missing PO token enable hint", path)
			}
			if !strings.Contains(body, "PO token fetch (disabled)") || !strings.Contains(body, `value="never"`) {
				t.Fatalf("%s pot_fetch should be disabled/never when provider URL unset", path)
			}
			if !strings.Contains(body, "connect-pot-service-health") || !strings.Contains(body, `hx-get="/settings/connect/external-services/pot"`) {
				t.Fatalf("%s missing async PO token health load", path)
			}
			if !strings.Contains(body, "connect-flare-service-health") || !strings.Contains(body, `hx-get="/settings/connect/external-services/flare"`) {
				t.Fatalf("%s missing async Flare health load", path)
			}
			if !strings.Contains(body, `id="ytdlp-connect-installed-version"`) || !strings.Contains(body, `hx-get="/settings/connect/ytdlp-installed-version"`) {
				t.Fatalf("%s missing async yt-dlp installed version load", path)
			}
			if !strings.Contains(body, `placeholder="loading"`) {
				t.Fatalf("%s missing yt-dlp installed version loading placeholder", path)
			}
			if !strings.Contains(body, `id="ytdlp-connect-last-checked"`) || !strings.Contains(body, "Last checked") {
				t.Fatalf("%s missing yt-dlp last checked field", path)
			}
			if !strings.Contains(body, "ytdlp_update_channel") {
				t.Fatalf("%s missing yt-dlp update channel on page shell", path)
			}
			if !strings.Contains(body, "Checking") || !strings.Contains(body, "loading-spinner") {
				t.Fatalf("%s missing pending health spinner", path)
			}
			if !strings.Contains(body, "Notifications") {
				t.Fatalf("%s missing Notifications", path)
			}
			if strings.Contains(body, `name="flare_solverr_url"`) {
				t.Fatalf("%s still posts flare_solverr_url", path)
			}
			if strings.Contains(body, "Appearance") || strings.Contains(body, "Authentication") {
				t.Fatalf("%s still has General content", path)
			}
			continue
		}
		if path == "/history" {
			body := rec.Body.String()
			if !strings.Contains(body, `id="notifications"`) || !strings.Contains(body, "Finished tasks") {
				t.Fatalf("/history missing notification/task sections")
			}
			if strings.Contains(body, `class="tooltip tooltip-top join-item"`) {
				t.Fatalf("/history range clear must not wrap join-item around the button")
			}
			if !strings.Contains(body, `data-tip="Clear time range"`) || !strings.Contains(body, `input join-item tooltip tooltip-top`) {
				t.Fatalf("/history range clear missing join-item tip on the control")
			}
		}
		if path == "/tasks" {
			body := rec.Body.String()
			if !strings.Contains(body, "interactive") || !strings.Contains(body, "Pausing a domain") {
				t.Fatalf("/tasks missing interactive/pause note")
			}
			if !strings.Contains(body, `data-scheduled-task`) || !strings.Contains(body, "download_wanted") || !strings.Contains(body, queue.KindSyncFiles) {
				t.Fatalf("/tasks missing scheduled task rows on system lane")
			}
			if !strings.Contains(body, `action="/actions/run-scheduled"`) || !strings.Contains(body, `data-tip="Queue now"`) {
				t.Fatalf("/tasks missing queue-now on scheduled rows")
			}
			if strings.Contains(body, "data-download-schedule") {
				t.Fatalf("/tasks still has header download schedule chip")
			}
		}
		if path == "/settings/library" || path == "/settings/queue" || path == "/settings/maintenance" {
			body := rec.Body.String()
			if !strings.Contains(body, "/settings/general") || !strings.Contains(body, "/settings/connect") || !strings.Contains(body, "/settings/queue") || !strings.Contains(body, "/settings/scheduler") || !strings.Contains(body, "/settings/maintenance") {
				t.Fatalf("%s missing settings sub-nav in navbar", path)
			}
			if strings.Contains(body, `href="/settings/domains"`) {
				t.Fatalf("%s still has Domains nav link", path)
			}
		}
		if path == "/settings/scheduler" {
			body := rec.Body.String()
			if !strings.Contains(body, "/settings/scheduler") || !strings.Contains(body, "download_wanted_cron") || !strings.Contains(body, "sync_files_cron") || !strings.Contains(body, "retention_delete_cron") || !strings.Contains(body, "ytdlp_update_cron") {
				t.Fatalf("%s missing scheduler form", path)
			}
			if strings.Contains(body, "download_new_on_scan") {
				t.Fatalf("%s still has download_new_on_scan", path)
			}
			if strings.Contains(body, "download_wanted_order") {
				t.Fatalf("%s still has queue settings", path)
			}
			continue
		}
		if path == "/settings/queue" {
			body := rec.Body.String()
			if strings.Contains(body, "download_wanted_order") {
				t.Fatalf("%s still has download_wanted_order", path)
			}
			if !strings.Contains(body, "max_download_queue") || !strings.Contains(body, "max_parallel_tasks") {
				t.Fatalf("%s missing queue form fields", path)
			}
			if strings.Contains(body, "download_new_on_scan") {
				t.Fatalf("%s should not have download_new_on_scan", path)
			}
			if !strings.Contains(body, ">Defaults</h2>") || !strings.Contains(body, "Domain overrides") || !strings.Contains(body, "modal-add-domain-override") {
				t.Fatalf("%s missing domain defaults/overrides", path)
			}
			if !strings.Contains(body, `id="domain-defaults-table-row"`) || !strings.Contains(body, `>default</span>`) || strings.Contains(body, `modal-edit-domain-default`) {
				t.Fatalf("%s missing fixed default row or has edit modal for default", path)
			}
			if !strings.Contains(body, "FlareSolverr, cookies, and membership credentials are set on a 'Domain override' per domain") {
				t.Fatalf("%s missing Access info-only blurb", path)
			}
			if strings.Contains(body, `name="use_flaresolverr"`) && strings.Contains(body, `action="/actions/save-domain-default"`) {
				// Flare checkbox must not appear on Domain defaults save form (override modal OK).
				defaultsIdx := strings.Index(body, `action="/actions/save-domain-default"`)
				overridesIdx := strings.Index(body, "Domain overrides")
				if defaultsIdx >= 0 && overridesIdx > defaultsIdx {
					chunk := body[defaultsIdx:overridesIdx]
					if strings.Contains(chunk, `name="use_flaresolverr"`) || strings.Contains(chunk, `name="cookies"`) || strings.Contains(chunk, `name="username"`) {
						t.Fatalf("%s Domain defaults still has Access fields", path)
					}
				}
			}
			if !strings.Contains(body, "How to export cookies") || !strings.Contains(body, "Site membership login") {
				t.Fatalf("%s missing Access guides in override modal", path)
			}
			if !strings.Contains(body, "Use FlareSolverr") || !strings.Contains(body, `name="use_flaresolverr"`) {
				t.Fatalf("%s missing FlareSolverr control in override modal", path)
			}
			if !strings.Contains(body, "Use FlareSolverr (disabled)") || !strings.Contains(body, "CREATORR_FLARESOLVERR_URL") {
				t.Fatalf("%s Use FlareSolverr should be disabled when Flare URL unset", path)
			}
			if !strings.Contains(body, "list-panel") {
				t.Fatalf("%s missing list-panel", path)
			}
			continue
		}
		if path == "/settings/library" {
			body := rec.Body.String()
			if strings.Contains(body, "Scan root folders") {
				t.Fatalf("%s still has Maintenance actions", path)
			}
			if !strings.Contains(body, "list-panel") {
				t.Fatalf("%s missing list-panel", path)
			}
			if !strings.Contains(body, "Changing library settings does not update already downloaded videos") {
				t.Fatalf("%s missing library settings scope alert", path)
			}
			if strings.Contains(body, "Saving does not") {
				t.Fatalf("%s still has per-field Saving does not hints", path)
			}
			continue
		}
		if path == "/settings/maintenance" {
			body := rec.Body.String()
			if !strings.Contains(body, "Scan root folders") {
				t.Fatalf("%s missing maintenance actions", path)
			}
			if !strings.Contains(body, "Apply episode format") {
				t.Fatalf("%s missing apply episode format", path)
			}
			if !strings.Contains(body, "list-panel") {
				t.Fatalf("%s missing list-panel", path)
			}
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, "list-panel") {
			t.Fatalf("%s missing list-panel", path)
		}
		if !strings.Contains(body, "list-header") {
			t.Fatalf("%s missing list-header", path)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/settings want redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/settings/general") {
		t.Fatalf("/settings redirect=%q", loc)
	}
}

func TestTaskDetailPage(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)

	tid, err := q.Enqueue(queue.EnqueueParams{
		Kind: queue.KindScan, Domain: "system", Message: "scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := q.ClaimNext()
	if err != nil || claimed == nil || claimed.ID != tid {
		t.Fatalf("claim: err=%v task=%v", err, claimed)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	// Running task detail + logs.
	q.Logs.Append(tid, "Running")
	q.Logs.Append(tid, "Listing example.com")
	req := httptest.NewRequest(http.MethodGet, "/task/"+strconv.FormatInt(tid, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("running status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Live task status") {
		t.Fatalf("missing live help: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Listing example.com") {
		t.Fatalf("missing log lines on detail: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `hx-trigger="load"`) {
		t.Fatalf("expected one-shot logs refresh on open: %s", truncate(body, 400))
	}

	req = httptest.NewRequest(http.MethodGet, "/task/"+strconv.FormatInt(tid, 10)+"/logs", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("logs status %d: %s", rec.Code, rec.Body.String())
	}
	logsBody := rec.Body.String()
	if !strings.Contains(logsBody, "Listing example.com") {
		t.Fatalf("missing log snapshot: %s", truncate(logsBody, 400))
	}
	if strings.Contains(logsBody, `hx-trigger="load"`) {
		t.Fatalf("/logs swap must not re-trigger load: %s", truncate(logsBody, 400))
	}

	if err := q.Finish(tid, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
	detail := `{"missing_ids":[1],"retention_ids":[],"video_ids":[1]}`
	if err := q.SetDetail(tid, detail); err != nil {
		t.Fatal(err)
	}
	if len(q.Logs.Snapshot(tid)) != 0 {
		t.Fatal("expected logs cleared after Finish")
	}

	req = httptest.NewRequest(http.MethodGet, "/task/"+strconv.FormatInt(tid, 10), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("finished status %d: %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if !strings.Contains(body, "scan") && !strings.Contains(body, "Done") {
		t.Fatalf("missing task message: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `class="breadcrumbs`) {
		t.Fatalf("missing breadcrumbs: %s", truncate(body, 400))
	}
	if !strings.Contains(body, strconv.FormatInt(tid, 10)) {
		t.Fatalf("missing task id: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "missing_ids") {
		t.Fatalf("missing detail: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "video_ids") {
		t.Fatalf("missing video_ids field: %s", truncate(body, 400))
	}
	if strings.Contains(body, "Videos in detail") {
		t.Fatalf("videos list panel should be gone: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "#1") {
		t.Fatalf("expected annotated video id: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Finished task outcome") {
		t.Fatalf("missing finished help: %s", truncate(body, 400))
	}
	if strings.Contains(body, "id=\"task-logs\"") {
		t.Fatalf("logs section should be hidden when finished: %s", truncate(body, 400))
	}
}

func TestSourceDetailPage(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Demo", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	src := ser.Sources[0]
	_, _ = q.CancelAll()
	tid, err := q.Enqueue(queue.EnqueueParams{
		Kind: queue.KindScan, Domain: "example.com", Message: "Scan: indexed 1 videos",
		SeriesID: ser.ID,
		Payload:  map[string]any{"source_id": src.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := q.ClaimNext()
	if err != nil || claimed == nil || claimed.ID != tid {
		t.Fatalf("claim: err=%v task=%v", err, claimed)
	}
	if err := q.Finish(tid, queue.StatusDone, "Scan: indexed 1 videos", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := q.SetDetail(tid, `{"source_id":`+strconv.FormatInt(src.ID, 10)+`,"video_ids":[]}`); err != nil {
		t.Fatal(err)
	}
	if err := lib.AddSourceHistory(src.ID, library.SourceHistScanned, "Scan: indexed 1 videos", map[string]any{
		"mode": library.SourceHistModeScan, "created": 1, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	path := "/series/" + strconv.FormatInt(ser.ID, 10) + "/sources/" + strconv.FormatInt(src.ID, 10)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "History") || !strings.Contains(body, "Scan: indexed") {
		t.Fatalf("missing history: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "example.com/c") {
		t.Fatalf("missing source url: %s", truncate(body, 400))
	}
	if strings.Contains(body, "Indexed videos that belong to this source") {
		t.Fatalf("source detail should not list videos: %s", truncate(body, 400))
	}
}

func TestStaticCSS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("--p")) && rec.Body.Len() < 1000 {
		t.Fatalf("css too small: %d", rec.Body.Len())
	}
}

func TestMonitorToggleHTMX(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Demo", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	body := strings.NewReader("series_id=" + itoa(ser.ID) + "&monitored=0")
	req := httptest.NewRequest(http.MethodPost, "/actions/set-series-monitored", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("unexpected redirect %s", loc)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "monitor-toggle-root") || !strings.Contains(out, "hx-post") {
		t.Fatalf("expected toggle partial: %s", truncate(out, 300))
	}
}

func TestSeriesDetailHasMonitorToggle(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Demo", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "monitor-toggle-root") || !strings.Contains(body, `action="/actions/set-series-monitored"`) {
		t.Fatalf("series detail missing monitor toggle: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-tip="Unmonitor"`) {
		t.Fatalf("series detail missing Unmonitor tip: %s", truncate(body, 400))
	}
}

func TestSaveDomainDefaultHTMX(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	body := strings.NewReader("max_download_queue=9&max_parallel_tasks=2&task_cooldown_seconds=15&download_rate_limit_value=5&download_rate_limit_unit=M&sleep_requests=3&redirect=/settings/queue")
	req := httptest.NewRequest(http.MethodPost, "/actions/save-domain-default", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, `id="domain-defaults-table-row"`) || !strings.Contains(out, `hx-swap-oob="outerHTML:#domain-defaults-table-row"`) {
		t.Fatalf("expected OOB default row: %s", truncate(out, 400))
	}
	if !strings.Contains(out, "<td>9</td>") || !strings.Contains(out, "<td>15</td>") {
		t.Fatalf("expected saved limits in row: %s", truncate(out, 400))
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestActionAddSeriesManual(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	body := strings.NewReader("title=Manual+Show&root_id=1&quality_profile_id=1&delivery_mode=download&monitored=1")
	req := httptest.NewRequest(http.MethodPost, "/actions/add-series", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/series/") || strings.Contains(loc, "ok=created") {
		t.Fatalf("location=%s", loc)
	}
	list, err := lib.ListSeries()
	if err != nil || len(list) != 1 || list[0].Title != "Manual Show" {
		t.Fatalf("series=%v err=%v", list, err)
	}
	if list[0].SourceCount != 0 {
		t.Fatalf("want no sources, got %d", list[0].SourceCount)
	}
}

func TestActionAddSeriesManualJSON(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	body := strings.NewReader("title=JSON+Show&root_id=1&quality_profile_id=1&delivery_mode=download&response=json")
	req := httptest.NewRequest(http.MethodPost, "/actions/add-series", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ID < 1 || out.Title != "JSON Show" {
		t.Fatalf("out=%+v", out)
	}
}

func TestActionAddSeriesURLRequiresTitleWithoutDraft(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	body := strings.NewReader("source_url=https://example.com/c&root_id=1&quality_profile_id=1&delivery_mode=download&monitored=1&scan_cron=@weekly")
	req := httptest.NewRequest(http.MethodPost, "/actions/add-series", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") || !strings.Contains(loc, "add=1") {
		t.Fatalf("location=%s", loc)
	}
	list, _ := lib.ListSeries()
	if len(list) != 0 {
		t.Fatalf("should not create series without title/draft, got %d", len(list))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
