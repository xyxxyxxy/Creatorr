package web_test

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
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
	if !strings.Contains(body, `class="footer footer-center`) || !strings.Contains(body, "GitHub") {
		t.Fatalf("missing supportability footer: %s", truncate(body, 400))
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
	if !strings.Contains(body, "best") {
		t.Fatalf("audio series should show best quality, got: %s", truncate(body, 800))
	}
	if strings.Contains(body, "0 sources") {
		t.Fatalf("series line2 must not show source count: %s", truncate(body, 800))
	}
	if strings.Contains(body, library.Profile480Name+" ·") {
		t.Fatalf("audio series must not show assigned profile name: %s", truncate(body, 800))
	}
	if !strings.Contains(body, "js-series-select") || !strings.Contains(body, "data-series-bulk-bar") {
		t.Fatalf("missing series bulk select/action bar: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "data-series-bulk-mode") || !strings.Contains(body, "list-checks") {
		t.Fatalf("missing series multi-select toggle: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "data-series-select-wrap") {
		t.Fatalf("missing series select wrap: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "modal-bulk-edit-series") || !strings.Contains(body, "modal-bulk-edit-series-metadata") {
		t.Fatalf("missing bulk edit modals: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "data-series-bulk-edit") || !strings.Contains(body, "data-series-bulk-metadata") {
		t.Fatalf("missing series bulk Edit / Edit metadata (btn_labeled): %s", truncate(body, 500))
	}
	if !strings.Contains(body, "data-series-bulk-monitor") || !strings.Contains(body, "data-series-bulk-unmonitor") {
		t.Fatalf("missing series bulk Monitor/Unmonitor actions: %s", truncate(body, 500))
	}
	if !strings.Contains(body, `action="/actions/bulk-set-series-monitored"`) {
		t.Fatalf("missing bulk set-series-monitored forms: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "monitor-toggle-root") || !strings.Contains(body, "data-series-monitor-wrap") {
		t.Fatalf("series list missing monitor AsButton wrap: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `data-tip="Monitor"`) && !strings.Contains(body, `data-tip="Unmonitor"`) {
		t.Fatalf("series list missing monitor tip: %s", truncate(body, 400))
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
	if !strings.Contains(body, "modal-add-series") || !strings.Contains(body, `src="/static/import.js"`) {
		t.Fatalf("expected add-series modal + import.js script with no series: %s", truncate(body, 400))
	}
	// Inline create join markup lives in the import picker script (bundled from ui/src/js/import.js).
	jsRec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(jsRec, httptest.NewRequest(http.MethodGet, "/static/import.js", nil))
	if jsRec.Code != 200 || !strings.Contains(jsRec.Body.String(), "data-import-allow-create") {
		t.Fatalf("import.js missing inline create join: status %d", jsRec.Code)
	}
	if !strings.Contains(body, "modal-add-video") || !strings.Contains(body, "js-add-video-form") {
		t.Fatalf("expected add-video modal with no series: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `class="space-y-3 js-add-video-form" data-add-video-series-id="" novalidate`) {
		t.Fatalf("expected add-video form novalidate: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `type="text" name="fetch_url" id="add-video-url"`) {
		t.Fatalf("expected add-video URL type=text: %s", truncate(body, 400))
	}
	if strings.Contains(body, `id="add-video-remote-id"`) || strings.Contains(body, "add-video-remote-display") {
		t.Fatalf("add-video modal must not show Remote ID controls: %s", truncate(body, 400))
	}
	if strings.Contains(body, `id="btn-scan"`) {
		t.Fatalf("scan button should be removed: %s", truncate(body, 400))
	}

	root, err := lib.CreateRoot("archive", t.TempDir(), "", nil)
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
	if strings.Contains(body2, `id="import-full-scan-note"`) || strings.Contains(body2, "Not all videos may be indexed yet") {
		t.Fatalf("full-scan import note should be removed: %s", truncate(body2, 400))
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
	if !strings.Contains(body2, "modal-add-series") || !strings.Contains(body2, `src="/static/import.js"`) {
		t.Fatalf("expected add-series modal + import.js script when series exist: %s", truncate(body2, 400))
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
	if !strings.Contains(body, `id="notify-badge"`) || !strings.Contains(body, `badge-info`) {
		t.Fatalf("empty notify badge should render info chrome: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `id="notify-badge" class="badge badge-xs absolute top-1 right-1 hidden badge-info"`) {
		t.Fatalf("zero unread notify badge must stay hidden info: %s", truncate(body, 400))
	}
	if _, err := notify.InsertNotification(d, notify.EventCookieInvalid, "cookie", "bad", 0, false, ""); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != 200 {
		t.Fatalf("status %d: %s", rec2.Code, rec2.Body.String())
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, `id="notify-badge" class="badge badge-xs absolute top-1 right-1 badge-error"`) ||
		!strings.Contains(body2, `>1</span>`) {
		t.Fatalf("unread alert must SSR badge-error count: %s", truncate(body2, 500))
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
	if !strings.Contains(body, "Downloaded / indexed") {
		t.Fatalf("videos stat must show downloaded / indexed: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Most wanted") {
		t.Fatalf("missing most wanted section: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Recent additions") {
		t.Fatalf("missing recent additions section: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Recent tasks") || !strings.Contains(body, "No tasks.") {
		t.Fatalf("recent tasks section must show empty state: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `id="tasks-list-live"`) || !strings.Contains(body, `data-list-mode="fixed"`) {
		t.Fatalf("overview recent tasks must use locked tasks Explorer: %s", truncate(body, 600))
	}
	if !overviewBrowseHrefHas(body, map[string][]string{
		"type": {"series"},
		"sort": {"wanted"},
		"view": {"gallery"},
	}) {
		t.Fatalf("most wanted Browse must open Browser Series wanted gallery: %s", truncate(body, 800))
	}
	if !overviewBrowseHrefHas(body, map[string][]string{
		"type":   {"videos"},
		"status": {"downloaded"},
		"sort":   {"acquired"},
		"view":   {"gallery"},
	}) {
		t.Fatalf("recent additions Browse must open Browser Videos downloaded/acquired gallery: %s", truncate(body, 800))
	}
	if !overviewBrowseHrefHas(body, map[string][]string{
		"type":   {"tasks"},
		"status": {queue.StatusDone, queue.StatusFailed, queue.StatusCancelled},
		"sort":   {queue.TaskSortCreated},
	}) {
		t.Fatalf("empty overview Recent tasks Browse must open finished Browser Tasks filter: %s", truncate(body, 800))
	}
	if strings.Contains(body, `id="overview-recent-list"`) {
		t.Fatalf("overview Recent must stay gallery, not list: %s", truncate(body, 400))
	}
	if strings.Contains(body, `id="overview-recent-gallery"`) &&
		!strings.Contains(body, `grid-cols-2 sm:grid-cols-3 lg:grid-cols-4`) {
		t.Fatalf("overview Recent gallery must match Browser Videos gallery grid: %s", truncate(body, 400))
	}
	if strings.Contains(body, `id="overview-recent-gallery"`) &&
		!strings.Contains(body, `[&>:nth-child(n+5)]:hidden`) {
		t.Fatalf("overview Recent gallery must hide overflow cards by breakpoint: %s", truncate(body, 500))
	}
	// Empty library: Most wanted stays empty (no gallery grid to assert).

	idxRecent := strings.Index(body, "Recent additions")
	idxWanted := strings.Index(body, "Most wanted")
	idxTasks := strings.Index(body, "Recent tasks")
	if idxRecent < 0 || idxWanted < 0 || idxTasks < 0 || idxRecent >= idxWanted || idxWanted >= idxTasks {
		t.Fatalf("overview order must be Recent additions, Most wanted, Recent tasks")
	}
}

func TestOverviewWantedGalleryMatchesSeriesGalleryGrid(t *testing.T) {
	b, err := os.ReadFile("templates/overview.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	start := strings.Index(body, `id="overview-wanted-gallery"`)
	if start < 0 {
		t.Fatal("overview-wanted-gallery missing")
	}
	tag := body[:start]
	if i := strings.LastIndex(tag, "<ul"); i >= 0 {
		tag = tag[i:]
	}
	want := `grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7`
	if !strings.Contains(tag, want) {
		t.Fatalf("Most wanted must use Browser Series gallery grid %q, got %q", want, tag)
	}
	if !strings.Contains(tag, `[&>:nth-child(n+4)]:hidden`) || !strings.Contains(tag, `xl:[&>:nth-child(n+7)]:block`) {
		t.Fatalf("Most wanted must hide overflow cards by breakpoint, got %q", tag)
	}
	if web.OverviewGalleryRow != 7 {
		t.Fatalf("OverviewGalleryRow=%d want 7 (max xl series-gallery row)", web.OverviewGalleryRow)
	}
}

func TestOverviewShowsRecentTasks(t *testing.T) {
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

	pendingID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind:   queue.KindScan, Domain: "example.com", Message: "queued only",
	})
	if err != nil {
		t.Fatal(err)
	}
	runningID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind:   queue.KindDownload, Domain: "cdn.example", Message: "Fetching",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, queue.StatusRunning, runningID); err != nil {
		t.Fatal(err)
	}

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
	if !strings.Contains(body, "Recent tasks") {
		t.Fatalf("missing recent tasks section: %s", truncate(body, 400))
	}
	if !overviewBrowseHrefHas(body, map[string][]string{
		"type":   {"tasks"},
		"status": {queue.StatusPending, queue.StatusRunning},
		"sort":   {queue.TaskSortQueue},
	}) {
		t.Fatalf("open overview Recent tasks Browse must open pending+running Browser Tasks: %s", truncate(body, 800))
	}
	if !strings.Contains(body, `id="task-row-`+strconv.FormatInt(runningID, 10)+`"`) {
		t.Fatalf("missing running task row: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `id="task-row-`+strconv.FormatInt(pendingID, 10)+`"`) {
		t.Fatalf("queued task must appear in overview recent tasks: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "download") || !strings.Contains(body, "cdn.example") {
		t.Fatalf("missing kind/domain on running row: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `href="/task/`+strconv.FormatInt(runningID, 10)+`"`) {
		t.Fatalf("running row must link to task detail: %s", truncate(body, 600))
	}
	if strings.Contains(body, "/actions/cancel-task") {
		t.Fatalf("overview recent tasks must not offer cancel")
	}
	if strings.Contains(body, "list_view_toolbar") || strings.Contains(body, `aria-label="Task filters"`) {
		t.Fatalf("overview recent tasks must omit Filter toolbar: %s", truncate(body, 600))
	}
	idxRecent := strings.Index(body, "Recent additions")
	idxWanted := strings.Index(body, "Most wanted")
	idxTasks := strings.Index(body, "Recent tasks")
	if idxRecent < 0 || idxWanted < 0 || idxTasks < 0 || idxRecent >= idxWanted || idxWanted >= idxTasks {
		t.Fatalf("overview order must be Recent additions, Most wanted, Recent tasks")
	}

	// Cap at 4 open tasks.
	for i := 0; i < 5; i++ {
		if _, err := q.Enqueue(queue.EnqueueParams{
			Origin: queue.OriginManual,
			Kind:   queue.KindScan, Domain: "example.com", Message: "extra " + strconv.Itoa(i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d after extras: %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if got := strings.Count(body, `id="task-row-`); got != web.OverviewTasksFixed {
		t.Fatalf("overview recent tasks want %d rows, got %d", web.OverviewTasksFixed, got)
	}

	// Idle queues: fall back to newest finished.
	if _, err := d.SQL.Exec(`UPDATE tasks SET status = ?`, queue.StatusDone); err != nil {
		t.Fatal(err)
	}
	doneID, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind:   queue.KindSyncFiles, Domain: "system", Message: "finished glance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, queue.StatusDone, doneID); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d when idle: %s", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if !strings.Contains(body, `id="task-row-`+strconv.FormatInt(doneID, 10)+`"`) {
		t.Fatalf("idle overview must show finished task: %s", truncate(body, 600))
	}
	if strings.Count(body, `id="task-row-`) > web.OverviewTasksFixed {
		t.Fatalf("idle overview must still cap at %d rows", web.OverviewTasksFixed)
	}
	if !overviewBrowseHrefHas(body, map[string][]string{
		"type":   {"tasks"},
		"status": {queue.StatusDone, queue.StatusFailed, queue.StatusCancelled},
		"sort":   {queue.TaskSortCreated},
	}) {
		t.Fatalf("idle overview Recent tasks Browse must open finished Browser Tasks filter: %s", truncate(body, 800))
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
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
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

func TestActionMaintenanceRunQueuesMultiple(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "maint-run.db"))
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

	form := strings.NewReader("actions=apply_episode_naming&actions=regenerate_nfos&actions=integrity_check")
	req := httptest.NewRequest(http.MethodPost, "/actions/maintenance-run", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "ok=maintenance-run") || !strings.Contains(loc, "actions=") {
		t.Fatalf("location=%q", loc)
	}
	for _, kind := range []string{queue.KindRenameEpisodes, queue.KindRegenerateNFO, queue.KindIntegrityCheck} {
		busy, err := q.HasPendingOrRunningKind(kind, queue.SystemDomain)
		if err != nil || !busy {
			t.Fatalf("expected %s queued, busy=%v err=%v", kind, busy, err)
		}
	}

	formEmpty := strings.NewReader("")
	req2 := httptest.NewRequest(http.MethodPost, "/actions/maintenance-run", formEmpty)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("empty status %d", rec2.Code)
	}
	if !strings.Contains(rec2.Header().Get("Location"), "err=") {
		t.Fatalf("empty location=%q", rec2.Header().Get("Location"))
	}
}

func TestActionPreviewApplyEpisodeNaming(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "preview-rename.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	rootPath := t.TempDir()
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: rootPath})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/actions/preview-apply-episode-naming", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No packed episodes") && !strings.Contains(body, "would rename") {
		t.Fatalf("unexpected preview body: %s", body)
	}
}

func TestActionMaintenanceConfirmSummary(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "maint-summary.db"))
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

	form := strings.NewReader("actions=apply_episode_naming&actions=refresh_sidecars")
	req := httptest.NewRequest(http.MethodPost, "/actions/maintenance-confirm-summary", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		PackedVideos     int  `json:"packed_videos"`
		ContactsExternal bool `json:"contacts_external"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.PackedVideos != 0 {
		t.Fatalf("packed_videos=%d", payload.PackedVideos)
	}
	if !payload.ContactsExternal {
		t.Fatal("expected contacts_external for refresh_sidecars")
	}

	formLocal := strings.NewReader("actions=regenerate_nfos")
	req2 := httptest.NewRequest(http.MethodPost, "/actions/maintenance-confirm-summary", formLocal)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("local status %d", rec2.Code)
	}
	var local struct {
		ContactsExternal bool `json:"contacts_external"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&local); err != nil {
		t.Fatal(err)
	}
	if local.ContactsExternal {
		t.Fatal("regenerate_nfos must not contact external")
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
		if path == "/history" {
			if rec.Code != http.StatusFound {
				t.Fatalf("/history want 302, got %d: %s", rec.Code, rec.Body.String())
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "type=tasks") {
				t.Fatalf("/history redirect=%q", loc)
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
			if !strings.Contains(body, ">plugins<") {
				t.Fatalf("%s missing plugins subsection", path)
			}
			if strings.Contains(body, ">EJS<") {
				t.Fatalf("%s still has removed EJS subsection", path)
			}
			if strings.Contains(body, ">PO token</") || strings.Contains(body, "already integrated") {
				t.Fatalf("%s still has removed PO token section", path)
			}
			if !strings.Contains(body, "yt-dlp-ejs") || !strings.Contains(body, "https://github.com/yt-dlp/ejs") {
				t.Fatalf("%s missing bundled yt-dlp-ejs row", path)
			}
			if !strings.Contains(body, ">Notes<") || !strings.Contains(body, "https://github.com/yt-dlp/yt-dlp#strongly-recommended") ||
				!strings.Contains(body, "already installed on PATH") {
				t.Fatalf("%s missing plugins Notes / Deno link", path)
			}
			if !strings.Contains(body, ">bundled<") {
				t.Fatalf("%s missing bundled source for yt-dlp-ejs", path)
			}
			if !strings.Contains(body, "/yt-dlp-plugins/") || !strings.Contains(body, "yt_dlp_plugins") {
				t.Fatalf("%s missing further plugins mount hint", path)
			}
			if !strings.Contains(body, "https://github.com/yt-dlp/yt-dlp#plugins") {
				t.Fatalf("%s missing yt-dlp plugins docs link", path)
			}
			if !strings.Contains(body, `id="ytdlp-plugins-present"`) {
				t.Fatalf("%s missing present plugins list", path)
			}
			if !strings.Contains(body, "youtube_player_client") || !strings.Contains(body, ">player_client<") {
				t.Fatalf("%s missing youtube player client setting", path)
			}
			if !strings.Contains(body, ">arguments<") {
				t.Fatalf("%s missing arguments section header", path)
			}
			if !strings.Contains(body, "tv,default") {
				t.Fatalf("%s missing default youtube player client value", path)
			}
			if !strings.Contains(body, "https://github.com/yt-dlp/yt-dlp#extractor-arguments") {
				t.Fatalf("%s missing extractor-arguments docs link", path)
			}
			if !strings.Contains(body, "External services") || !strings.Contains(body, "FlareSolverr URL") || !strings.Contains(body, "PO token provider URL") {
				t.Fatalf("%s missing external service URL joins", path)
			}
			if !strings.Contains(body, "CREATORR_FLARESOLVERR_URL") || !strings.Contains(body, "CREATORR_POT_PROVIDER_URL") {
				t.Fatalf("%s missing env hints", path)
			}
			if !strings.Contains(body, "&#39;fetch_pot&#39; is under &#39;plugins&#39;") &&
				!strings.Contains(body, "'fetch_pot' is under 'plugins'") {
				t.Fatalf("%s missing fetch_pot location hint", path)
			}
			if !strings.Contains(body, "fetch_pot (disabled)") || !strings.Contains(body, `value="never"`) {
				t.Fatalf("%s pot_fetch should be disabled/never when provider URL unset", path)
			}
			if !strings.Contains(body, `id="setting-pot_fetch"`) {
				t.Fatalf("%s pot_fetch should live under plugins", path)
			}
			if !strings.Contains(body, `href="https://github.com/yt-dlp/yt-dlp#extractor-arguments"`) ||
				!strings.Contains(body, ">extractor-arguments<") ||
				!strings.Contains(body, "fetch_pot") {
				t.Fatalf("%s missing fetch_pot extractor-arguments link", path)
			}
			if !strings.Contains(body, "connect-pot-service-health") || !strings.Contains(body, `hx-get="/settings/connect/external-services/pot"`) {
				t.Fatalf("%s missing async PO token health load", path)
			}
			if !strings.Contains(body, "connect-flare-service-health") || !strings.Contains(body, `hx-get="/settings/connect/external-services/flare"`) {
				t.Fatalf("%s missing async Flare health load", path)
			}
			if !strings.Contains(body, `id="ytdlp-connect-installed-version"`) {
				t.Fatalf("%s missing installed version field", path)
			}
			if strings.Contains(body, `hx-get="/settings/connect/ytdlp-installed-version"`) {
				t.Fatalf("%s should render installed version with the page shell (not hx Pending)", path)
			}
			if strings.Contains(body, `placeholder="loading"`) {
				t.Fatalf("%s should not show loading placeholder for installed version on shell", path)
			}
			if !strings.Contains(body, `id="ytdlp-connect-last-checked"`) || !strings.Contains(body, "Last checked") {
				t.Fatalf("%s missing yt-dlp last checked field", path)
			}
			if !strings.Contains(body, "ytdlp_update_channel") {
				t.Fatalf("%s missing yt-dlp update channel on page shell", path)
			}
			if !strings.Contains(body, "Configure the update schedule under &#39;Settings → Scheduler&#39;") &&
				!strings.Contains(body, "Configure the update schedule under 'Settings → Scheduler'") {
				t.Fatalf("%s missing yt-dlp schedule hint under update channel", path)
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
			continue
		}
		if path == "/tasks" {
			body := rec.Body.String()
			if !strings.Contains(body, "interactive") || !strings.Contains(body, "Pausing a domain") {
				t.Fatalf("/tasks missing interactive/pause note")
			}
			if !strings.Contains(body, `id="tasks-list-live"`) {
				t.Fatalf("/tasks missing tasks-list-live")
			}
			if !strings.Contains(body, `data-scheduled-task`) || !strings.Contains(body, "download_wanted") || !strings.Contains(body, queue.KindSyncFiles) {
				t.Fatalf("/tasks missing scheduled task rows")
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
			if !strings.Contains(body, "Use FlareSolverr") || !strings.Contains(body, "CREATORR_FLARESOLVERR_URL") {
				t.Fatalf("%s Use FlareSolverr should be disabled when Flare URL unset", path)
			}
			if strings.Contains(body, "Use FlareSolverr (disabled)") {
				t.Fatalf("%s Use FlareSolverr label must not append (disabled)", path)
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
			if strings.Contains(body, "Scan root folders") {
				t.Fatalf("%s still has Scan root folders", path)
			}
			if !strings.Contains(body, "Apply episode format") {
				t.Fatalf("%s missing apply episode format", path)
			}
			if !strings.Contains(body, "Select actions") || !strings.Contains(body, "Select scope") {
				t.Fatalf("%s missing maintenance sections", path)
			}
			if strings.Contains(body, "maintenance-steps") || strings.Contains(body, "steps-vertical") {
				t.Fatalf("%s still has steps UI", path)
			}
			if !strings.Contains(body, `action="/actions/maintenance-run"`) {
				t.Fatalf("%s missing maintenance-run form", path)
			}
			if !strings.Contains(body, `id="maintenance-run-submit"`) || !strings.Contains(body, "Run selected") {
				t.Fatalf("%s missing Run selected", path)
			}
			if strings.Contains(body, `id="maintenance-preview-rename"`) || strings.Contains(body, "Preview renames") {
				t.Fatalf("%s still has standalone Preview renames", path)
			}
			if !strings.Contains(body, `id="modal-maintenance-rename-preview"`) || !strings.Contains(body, `id="maintenance-rename-preview-continue"`) {
				t.Fatalf("%s missing rename preview Continue step", path)
			}
			if !strings.Contains(body, `id="maintenance-confirm-affected"`) || !strings.Contains(body, `id="maintenance-confirm-external"`) {
				t.Fatalf("%s missing confirm affected/external chrome", path)
			}
			if !strings.Contains(body, `name="actions"`) || !strings.Contains(body, `value="apply_episode_naming"`) {
				t.Fatalf("%s missing action checkboxes", path)
			}
			if !strings.Contains(body, "Integrity check") {
				t.Fatalf("%s missing verify all media", path)
			}
			if !strings.Contains(body, "Refresh sidecars") {
				t.Fatalf("%s missing refresh sidecars", path)
			}
			if !strings.Contains(body, "Danger zone") || !strings.Contains(body, `value="reset_metadata_from_info"`) {
				t.Fatalf("%s missing danger zone reset metadata", path)
			}
			if !strings.Contains(body, `id="maintenance-confirm-reset-meta"`) {
				t.Fatalf("%s missing reset metadata confirm alert", path)
			}
			if !strings.Contains(body, "File sync") || !strings.Contains(body, `value="sync_files"`) {
				t.Fatalf("%s missing file sync", path)
			}
			if !strings.Contains(body, "list-panel") {
				t.Fatalf("%s missing list-panel", path)
			}
			if !strings.Contains(body, "maintenance-scope-chips") || !strings.Contains(body, "js-maintenance-series-dd") {
				t.Fatalf("%s missing maintenance series scope picker", path)
			}
			if !strings.Contains(body, "js-maintenance-scope-all") || !strings.Contains(body, "All series") {
				t.Fatalf("%s missing All series scope checkbox", path)
			}
			if strings.Contains(body, "modal-library-picker") || strings.Contains(body, "maintenance-scope-choose") {
				t.Fatalf("%s still has removed library picker / Choose scope", path)
			}
			if !strings.Contains(body, "modal-maintenance-confirm") {
				t.Fatalf("%s missing maintenance confirm modal", path)
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
		Origin: queue.OriginManual,
		Kind:   queue.KindScan, Domain: "system", Message: "scan",
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
	if !strings.Contains(body, "Stages") {
		t.Fatalf("missing Stages: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "enqueued") {
		t.Fatalf("missing enqueued stage: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "Listing example.com") {
		t.Fatalf("missing log lines on detail: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `hx-trigger="load"`) {
		t.Fatalf("expected one-shot logs refresh on open: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `for="modal-cancel-task"`) || !strings.Contains(body, "data-task-progress") {
		t.Fatalf("running task should show Cancel + progress: %s", truncate(body, 400))
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
	if !strings.Contains(body, "enqueued") {
		t.Fatalf("missing enqueued stage: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "done") && !strings.Contains(body, "failed") {
		t.Fatalf("missing terminal stage: %s", truncate(body, 400))
	}
	if strings.Contains(body, "id=\"task-logs\"") {
		t.Fatalf("logs section should be hidden when finished: %s", truncate(body, 400))
	}
	if strings.Contains(body, `for="modal-cancel-task"`) || strings.Contains(body, "data-task-progress") {
		t.Fatalf("finished task must not show Cancel + progress: %s", truncate(body, 400))
	}
}

func TestTaskDetailFailedShowsError(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fail-ui.db"))
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
		Origin: queue.OriginManual,
		Kind:   queue.KindDownload, Domain: "example.com", Message: "download",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.MergeDetailJSON(tid, map[string]any{"keep": "yes"}); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.ClaimNext()
	if err != nil || claimed == nil || claimed.ID != tid {
		t.Fatalf("claim: err=%v task=%v", err, claimed)
	}
	q.Logs.Append(tid, "Downloading video")
	q.Logs.Append(tid, "ERROR: unable to download")
	if err := q.Finish(tid, queue.StatusFailed, "yt-dlp download failed", "DownloadFailed", "ERROR: unable to download"); err != nil {
		t.Fatal(err)
	}
	if err := q.MergeDetailJSON(tid, map[string]any{"error": "ERROR: unable to download"}); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest(http.MethodGet, "/task/"+strconv.FormatInt(tid, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Error code") || !strings.Contains(body, "DownloadFailed") {
		t.Fatalf("missing error code: %s", truncate(body, 600))
	}
	if !strings.Contains(body, ">Error<") && !strings.Contains(body, ">Error</th>") {
		t.Fatalf("missing Error label: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "ERROR: unable to download") {
		t.Fatalf("missing error text: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `id="task-logs"`) || !strings.Contains(body, "Downloading video") {
		t.Fatalf("failed task should show persisted Logs: %s", truncate(body, 600))
	}
	if strings.Contains(body, `hx-trigger="load"`) || strings.Contains(body, `hx-target="#task-logs"`) {
		t.Fatalf("failed Logs must not offer Refresh: %s", truncate(body, 600))
	}
	if strings.Contains(body, ">raw</th>") {
		t.Fatalf("should not show unlabeled raw: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "keep") {
		t.Fatalf("other detail keys should remain: %s", truncate(body, 600))
	}
	if strings.Contains(body, `for="modal-cancel-task"`) || strings.Contains(body, "data-task-progress") {
		t.Fatalf("failed task must not show Cancel + progress: %s", truncate(body, 600))
	}
}

func TestTaskDetailRenameEpisodesList(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	seedHandler(t, d)
	rootPath := t.TempDir()
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: rootPath})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "RenameTD", SourceURL: "https://www.example.com/@renametd", RootID: root.ID,
		QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "rtd1", Title: "Ep One", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind:   queue.KindRenameEpisodes, Domain: queue.SystemDomain, Message: "Rename",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Finish(tid, queue.StatusDone, "Renamed 1, skipped busy 0, failed 0", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := lib.AddVideoHistory(res.VideoID, "renamed", "Episode files renamed", map[string]any{
		"previous":      "oldstem",
		"new":           "newstem",
		"previous_path": filepath.Join(rootPath, "old", "oldstem"),
		"new_path":      filepath.Join(rootPath, "new", "newstem"),
	}, tid); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest(http.MethodGet, "/task/"+strconv.FormatInt(tid, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Renames") {
		t.Fatalf("missing Renames section: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "1 renamed") {
		t.Fatalf("missing rename count: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "Ep One") || !strings.Contains(body, "RenameTD") {
		t.Fatalf("missing titles: %s", truncate(body, 600))
	}
	if !strings.Contains(body, "oldstem") || !strings.Contains(body, "newstem") {
		t.Fatalf("missing paths: %s", truncate(body, 600))
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
		Origin: queue.OriginManual,
		Kind:   queue.KindScan, Domain: "example.com", Message: "Scan: indexed 1 videos",
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

func TestSourceDetailCookieSmartStatus(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui-smart.db"))
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
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetCookies(d, "example.com", "# Netscape\n.example.com\tTRUE\t/\tFALSE\t0\ta\tb\n"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetSmartCookies(d, "example.com", true); err != nil {
		t.Fatal(err)
	}
	if err := domains.SaveCookieSmart(d, src.ID, domains.CookieSmartState{
		Prefer: true,
		Ring:   []string{domains.CookieOutcomeFallback, domains.CookieOutcomeFallback, domains.CookieOutcomeFallback, domains.CookieOutcomeFallback},
		N:      3,
	}); err != nil {
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
	if !strings.Contains(body, "Cookie policy") || !strings.Contains(body, "prefer cookies") {
		t.Fatalf("missing cookie policy: %s", truncate(body, 800))
	}
	if !strings.Contains(body, "Cookie learning") || !strings.Contains(body, "4 fallback") {
		t.Fatalf("missing cookie learning: %s", truncate(body, 800))
	}
}

func TestStaticCSS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()
	web.StaticHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	css := rec.Body.Bytes()
	if !bytes.Contains(css, []byte("--p")) && len(css) < 1000 {
		t.Fatalf("css too small: %d", len(css))
	}
	// Filter/Sort/View menus sit in .input.join-item; open state must overlay
	// (not grow the join) when keep-open uses .dropdown-open after HTMX.
	for _, pin := range []string{
		"js-list-toolbar-dd.dropdown-open",
		"position:absolute!important",
		".btn.btn-soft.btn-accent:is([type=checkbox],[type=radio]):checked",
		".btn.btn-accent:not(.btn-soft):is([type=checkbox],[type=radio]):checked",
		".js-list-toolbar-dd.input.is-bulk-on",
	} {
		if !bytes.Contains(css, []byte(pin)) {
			t.Fatalf("app.css missing toolbar dropdown overlay pin %q", pin)
		}
	}
}

func TestSetSeriesMonitoredHTMX(t *testing.T) {
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

	body := strings.NewReader("series_id=" + itoa(ser.ID) + "&monitored=0&redirect=/series")
	req := httptest.NewRequest(http.MethodPost, "/actions/set-series-monitored", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "/series" {
		t.Fatalf("expected HX-Redirect /series, got %q body=%s", loc, truncate(rec.Body.String(), 200))
	}
	got, err := lib.GetSeries(ser.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Monitored {
		t.Fatal("expected series unmonitored")
	}
}

func TestSeriesDetailHasMonitoredActionNotOnEditForm(t *testing.T) {
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
	if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Ep One",
		UploadDate: "2024-01-02T00:00:00Z",
	}); err != nil {
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
		t.Fatalf("series detail missing monitor action: %s", truncate(body, 400))
	}
	// Detail BtnClass path: outline bookmark only (no green fill).
	if idx := strings.Index(body, `data-tip="Unmonitor"`); idx >= 0 {
		snip := body[idx:]
		if len(snip) > 180 {
			snip = snip[:180]
		}
		if strings.Contains(snip, "text-success") || strings.Contains(snip, "fill-current") {
			t.Fatalf("series detail monitor btn must not use filled success icon: %s", snip)
		}
	}
	if i := strings.Index(body, `id="modal-edit-series"`); i >= 0 {
		edit := body[i:]
		if j := strings.Index(edit, `id="modal-edit-series-metadata"`); j > 0 {
			edit = edit[:j]
		}
		if strings.Contains(edit, `name="monitored"`) {
			t.Fatalf("series detail edit form must not include monitored: %s", truncate(edit, 400))
		}
	}
	if strings.Contains(body, "delivery-mode-join") {
		t.Fatalf("series detail should use delivery select, not radio join: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `name="delivery_mode"`) {
		t.Fatalf("series detail missing delivery select: %s", truncate(body, 400))
	}
	if strings.Contains(body, "data-video-bulk-mode") || strings.Contains(body, "modal-bulk-edit-videos-metadata") {
		t.Fatalf("series detail Videos glance must omit multi-select bulk: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "Browse all") || !strings.Contains(body, `data-view-mode="gallery"`) {
		t.Fatalf("series detail Videos glance missing Browse all / locked gallery: %s", truncate(body, 500))
	}
}

func TestSeriesDetailImportRowFiltersVideos(t *testing.T) {
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
	if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Imported",
		UploadDate: "2024-01-02T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	// url.Values.Encode sorts keys; html/template escapes & in attrs.
	wantHref := `/browser?series=` + itoa(ser.ID) + `&amp;sort=acquired&amp;source=import&amp;type=videos`
	importIdx := strings.Index(body, `aria-label="Show imported videos"`)
	if importIdx < 0 {
		t.Fatalf("Import row missing: %s", truncate(body, 600))
	}
	// Anchor opens before aria-label; grab a window that includes href=.
	start := importIdx - 200
	if start < 0 {
		start = 0
	}
	chunk := body[start:min(importIdx+80, len(body))]
	if !strings.Contains(chunk, `href="`+wantHref+`"`) {
		t.Fatalf("Import row missing Browse href: %s", truncate(chunk, 400))
	}
	if strings.Contains(chunk, `hx-get=`) || strings.Contains(chunk, `hx-target=`) {
		t.Fatalf("Import row must be a plain Browser link: %s", truncate(chunk, 400))
	}
	liveIdx := strings.Index(body, `id="sources-list-live"`)
	if liveIdx < 0 || importIdx < liveIdx {
		t.Fatalf("Import row should sit after Sources Explorer (live=%d import=%d)", liveIdx, importIdx)
	}
}

func TestSeriesVideosGlanceOneGalleryRow(t *testing.T) {
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
	for i := 0; i < web.SeriesVideoGlanceSize+1; i++ {
		if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
			SeriesID:   ser.ID,
			Title:      "V" + itoa(int64(i)),
			UploadDate: "2024-01-02T00:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID)+"?q=test&view=list&page=2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	liveStart := strings.Index(body, `id="series-videos-live"`)
	if liveStart < 0 {
		t.Fatal("series Videos live missing")
	}
	// Next major section after Videos (Notes or Files); avoid matching data-*-id=.
	liveEndRel := strings.Index(body[liveStart:], `id="files-list-live"`)
	if liveEndRel < 0 {
		liveEndRel = strings.Index(body[liveStart:], `>Notes<`)
	}
	if liveEndRel < 0 {
		liveEndRel = len(body) - liveStart
	}
	live := body[liveStart : liveStart+liveEndRel]
	if strings.Contains(live, `aria-label="Video filters"`) || strings.Contains(live, `aria-label="Active filters"`) {
		t.Fatalf("series Videos glance must omit filter bar: %s", truncate(live, 800))
	}
	if strings.Contains(live, `data-lucide="chevron-first"`) || strings.Contains(live, `data-list-mode="paginated"`) {
		t.Fatalf("series Videos glance must not paginate: %s", truncate(live, 600))
	}
	if !strings.Contains(live, `data-view-mode="gallery"`) || !strings.Contains(live, `data-list-mode="fixed"`) {
		t.Fatalf("series Videos glance must be fixed gallery: %s", truncate(live, 400))
	}
	if !strings.Contains(live, `grid-cols-2 sm:grid-cols-3 lg:grid-cols-4`) {
		t.Fatalf("series Videos glance missing Browser Videos gallery grid: %s", truncate(live, 400))
	}
	browse := `/browser?series=` + itoa(ser.ID) + `&amp;sort=acquired&amp;type=videos&amp;view=gallery`
	if !strings.Contains(live, `Browse all`) || !strings.Contains(live, `href="`+browse+`"`) {
		t.Fatalf("series Videos missing Browse all: %s", truncate(live, 600))
	}
	rowsStart := strings.Index(live, `id="series-videos-rows"`)
	if rowsStart < 0 {
		t.Fatal("series Videos rows missing")
	}
	rowsEnd := strings.Index(live[rowsStart:], `</ul>`)
	if rowsEnd < 0 {
		t.Fatal("series Videos rows unclosed")
	}
	rowsChunk := live[rowsStart : rowsStart+rowsEnd]
	vidPrefix := `/series/` + itoa(ser.ID) + `/videos/`
	if got := strings.Count(rowsChunk, vidPrefix); got != web.SeriesVideoGlanceSize {
		t.Fatalf("series Videos glance cards=%d want %d", got, web.SeriesVideoGlanceSize)
	}
}

func TestListLoadModesInfiniteAndPaginated(t *testing.T) {
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
	videoTotal := web.InfiniteChunkSize + 5
	for i := 0; i < videoTotal; i++ {
		if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
			SeriesID:   ser.ID,
			Title:      "Ep " + itoa(int64(i+1)),
			UploadDate: "2024-01-02T00:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	// Infinite list on Browser Videos: first chunk + sentinel.
	req := httptest.NewRequest(http.MethodGet, "/browser?type=videos&view=list", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-list-mode="infinite"`) {
		t.Fatalf("missing infinite mode: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `id="videos-list-infinite"`) {
		t.Fatalf("missing sentinel: %s", truncate(body, 400))
	}
	if strings.Contains(body, `data-lucide="chevron-first"`) {
		t.Fatalf("pager should not show for infinite list")
	}
	if got := strings.Count(body, `data-video-id="`); got != web.InfiniteChunkSize {
		t.Fatalf("first paint rows=%d want %d", got, web.InfiniteChunkSize)
	}
	if got := strings.Count(body, `class="skeleton`); got < 5 {
		t.Fatalf("expected next-chunk skeletons in sentinel, got %d: %s", got, truncate(body, 400))
	}

	// Append chunk page=2 via Explorer browse.
	req = httptest.NewRequest(http.MethodGet, "/explorer/browse?type=videos&at=browser&view=list&page=2", nil)
	req.Header.Set("HX-Target", "videos-list-infinite")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("append status %d", rec.Code)
	}
	chunk := rec.Body.String()
	if !strings.Contains(chunk, `data-infinite-chunk`) {
		t.Fatalf("missing chunk wrapper: %s", truncate(chunk, 300))
	}
	if got := strings.Count(chunk, `data-video-id="`); got != 5 {
		// videoTotal, first InfiniteChunkSize, page 2 has the remainder
		t.Fatalf("append rows=%d want 5: %s", got, truncate(chunk, 300))
	}

	// through=2 full live returns all when total fits in two chunks.
	req = httptest.NewRequest(http.MethodGet, "/browser?type=videos&view=list&through=2", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if got := strings.Count(body, `data-video-id="`); got != videoTotal {
		t.Fatalf("through=2 rows=%d want %d", got, videoTotal)
	}

	// Refresh clamp: through=20 → max 100, but we only have 25.
	req = httptest.NewRequest(http.MethodGet, "/browser?type=videos&view=list&through=20", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, `data-through-clamped="1"`) {
		t.Fatalf("expected clamp flag: %s", truncate(body, 400))
	}

	// Table: paginated, pager when enough pages, no sentinel.
	req = httptest.NewRequest(http.MethodGet, "/browser?type=videos&view=table", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, `data-list-table`) {
		t.Fatalf("table missing: %s", truncate(body, 300))
	}
	if strings.Contains(body, `id="videos-list-infinite"`) {
		t.Fatalf("table must not have infinite sentinel")
	}
	if got := strings.Count(body, `data-video-id="`); got != web.VideoPageSize {
		t.Fatalf("table page rows=%d want %d", got, web.VideoPageSize)
	}
	if !strings.Contains(body, `data-lucide="chevron-first"`) {
		t.Fatalf("table missing pager")
	}

	// Series detail videos: locked paginated glance (covered by TestSeriesVideosGlanceLockedList).
	req = httptest.NewRequest(http.MethodGet, "/series/"+itoa(ser.ID), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if strings.Contains(body, `id="series-videos`) && strings.Contains(body, `-infinite"`) {
		t.Fatalf("series detail must not infinite-scroll videos")
	}

	// Series list infinite.
	req = httptest.NewRequest(http.MethodGet, "/series?view=list", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, `data-list-mode="infinite"`) {
		t.Fatalf("series list missing infinite: %s", truncate(body, 300))
	}

	// Overview fixed.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, `data-list-mode="fixed"`) {
		t.Fatalf("overview recent missing fixed mode")
	}
}

func TestListInfiniteJSDuplicateIDPin(t *testing.T) {
	b, err := os.ReadFile("ui/src/js/list_infinite.js")
	if err != nil {
		// Test cwd may be package dir.
		b, err = os.ReadFile("internal/web/ui/src/js/list_infinite.js")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "duplicate data-video-id") {
		t.Fatal("missing duplicate-id string-guard pin in list_infinite.js")
	}
}

func TestSeriesAndVideosTableView(t *testing.T) {
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
	if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Ep One",
		UploadDate: "2024-01-02T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	for _, path := range []string{"/series?view=table", "/browser?type=videos&view=table"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, truncate(rec.Body.String(), 300))
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-list-table`) || !strings.Contains(body, `data-list-table-cols`) {
			t.Fatalf("%s missing table chrome: %s", path, truncate(body, 400))
		}
		wantSummary := "1 series"
		if strings.Contains(path, "videos") {
			wantSummary = "1 video"
		}
		if !strings.Contains(body, wantSummary) {
			t.Fatalf("%s missing table summary %q: %s", path, wantSummary, truncate(body, 400))
		}
		if strings.Contains(path, "type=videos") {
			if !strings.Contains(body, `list-table-sticky-end`) {
				t.Fatalf("%s missing sticky Actions column: %s", path, truncate(body, 400))
			}
		}
		if !strings.Contains(body, `data-table-col="title"`) {
			t.Fatalf("%s missing title column picker: %s", path, truncate(body, 400))
		}
	}
}

func TestSeriesTableHeaderSortDownloaded(t *testing.T) {
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
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Demo", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://example.com/c",
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/series?view=table&sort=downloaded", nil)
	req.AddCookie(&http.Cookie{Name: "creatorr_cols_series", Value: "title,downloaded,monitored"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-col="downloaded"`) {
		t.Fatalf("missing downloaded column: %s", truncate(body, 500))
	}
	if !strings.Contains(body, `aria-sort="descending"`) {
		t.Fatalf("active downloaded sort should set aria-sort: %s", truncate(body, 800))
	}
	if !strings.Contains(body, `class="list-table-sort-link"`) || !strings.Contains(body, `sort=downloaded`) {
		t.Fatalf("want whole-cell sort link for downloaded: %s", truncate(body, 800))
	}
	if !strings.Contains(body, `data-lucide="arrow-down"`) {
		t.Fatalf("active downloaded sort should show down arrow: %s", truncate(body, 800))
	}
	if !strings.Contains(body, `data-lucide="arrow-up-down"`) {
		t.Fatalf("inactive sortable headers should show up-down cue: %s", truncate(body, 800))
	}
	// Monitored has no SortOpt: plain th label, no sort link on that col.
	monIdx := strings.Index(body, `data-col="monitored"`)
	if monIdx < 0 {
		t.Fatal("missing monitored column")
	}
	monChunk := body[monIdx:min(monIdx+200, len(body))]
	if strings.Contains(monChunk, `list-table-sort-link`) {
		t.Fatalf("monitored header must stay plain: %s", monChunk)
	}
}

func TestVideosPageHasBulkSelect(t *testing.T) {
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
	if _, err := lib.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Ep One",
		UploadDate: "2024-01-02T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	legacy := httptest.NewRecorder()
	r.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/videos?view=gallery", nil))
	if legacy.Code != http.StatusMovedPermanently {
		t.Fatalf("/videos status %d want 301", legacy.Code)
	}
	if loc := legacy.Header().Get("Location"); loc != "/browser?type=videos&view=gallery" {
		t.Fatalf("/videos Location=%q", loc)
	}

	req := httptest.NewRequest(http.MethodGet, "/browser?type=videos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `href="/videos"`) {
		t.Fatalf("nav must not link to /videos: %s", truncate(body, 400))
	}
	if !strings.Contains(body, "data-video-bulk-mode") {
		t.Fatalf("Browser Videos missing video multi-select toggle: %s", truncate(body, 500))
	}
	if !strings.Contains(body, "data-video-bulk-refresh") || !strings.Contains(body, "data-video-bulk-metadata") {
		t.Fatalf("Browser Videos missing Refresh/Edit metadata (btn_labeled): %s", truncate(body, 500))
	}
	if !strings.Contains(body, "modal-bulk-edit-videos-metadata") || !strings.Contains(body, "modal-bulk-delete-videos") {
		t.Fatalf("Browser Videos missing video bulk modals: %s", truncate(body, 500))
	}
	if !strings.Contains(body, `action="/actions/bulk-want-videos"`) {
		t.Fatalf("Browser Videos missing bulk want form: %s", truncate(body, 400))
	}
	if strings.Contains(body, `name="series_id"`) && strings.Contains(body, `id="form-bulk-want-videos"`) {
		// Library-wide bulk forms omit series_id; series detail still includes it.
		wantFormStart := strings.Index(body, `id="form-bulk-want-videos"`)
		wantFormEnd := strings.Index(body[wantFormStart:], "</form>")
		if wantFormStart >= 0 && wantFormEnd > 0 {
			chunk := body[wantFormStart : wantFormStart+wantFormEnd]
			if strings.Contains(chunk, `name="series_id"`) {
				t.Fatalf("Browser Videos bulk want form should omit series_id: %s", truncate(chunk, 300))
			}
		}
	}

	idsReq := httptest.NewRequest(http.MethodGet, "/videos/ids", nil)
	idsRec := httptest.NewRecorder()
	r.ServeHTTP(idsRec, idsReq)
	if idsRec.Code != 200 {
		t.Fatalf("videos/ids status %d: %s", idsRec.Code, idsRec.Body.String())
	}
	if !strings.Contains(idsRec.Body.String(), `"ids"`) {
		t.Fatalf("videos/ids missing ids: %s", truncate(idsRec.Body.String(), 200))
	}
}

func TestSeriesSourceScanButtons(t *testing.T) {
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
	srcs, err := lib.ListSources(ser.ID)
	if err != nil || len(srcs) != 1 {
		t.Fatalf("sources: %v len %d", err, len(srcs))
	}
	src := srcs[0]
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	seriesPath := "/series/" + itoa(ser.ID)
	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, truncate(rec.Body.String(), 400))
		}
		return rec.Body.String()
	}
	assertScanQueued := func(body string) {
		t.Helper()
		if !strings.Contains(body, `aria-label="Scan for new videos" aria-disabled="true"`) {
			t.Fatalf("scan button should stay disabled while a scan is queued: %s", truncate(body, 600))
		}
		if strings.Contains(body, `aria-label="Start full scan"`) || strings.Contains(body, `/actions/full-rescan-source`) {
			t.Fatalf("list quick actions must not offer Full scan: %s", truncate(body, 600))
		}
	}
	assertScanQueued(get(seriesPath))

	if _, err := d.SQL.Exec(`DELETE FROM tasks WHERE kind = 'scan'`); err != nil {
		t.Fatal(err)
	}
	incomplete := get(seriesPath)
	// html/template escapes apostrophes in attributes (&#39;).
	if !strings.Contains(incomplete, `data-tip="Finish Full scan on the source page first"`) ||
		!strings.Contains(incomplete, `aria-label="Scan for new videos" aria-disabled="true"`) {
		t.Fatalf("tip Scan should stay visible and disabled until full scan finishes: %s", truncate(incomplete, 600))
	}
	if strings.Contains(incomplete, `aria-label="Start full scan"`) || strings.Contains(incomplete, `/actions/full-rescan-source`) {
		t.Fatalf("list quick actions must not offer Full scan: %s", truncate(incomplete, 600))
	}
	if err := lib.MarkFullScanDone(src.ID); err != nil {
		t.Fatal(err)
	}
	idle := get(seriesPath)
	wantScan := `aria-label="Scan for new videos"`
	wantBtn := `class="btn btn-xs btn-square join-item tooltip tooltip-top"`
	if !strings.Contains(idle, wantBtn) || strings.Contains(idle, `aria-label="Scan for new videos" aria-disabled="true"`) || !strings.Contains(idle, wantScan) {
		t.Fatalf("idle tip Scan should be a joined tip host and enabled: %s", truncate(idle, 600))
	}
	wantEdit := `class="btn btn-xs btn-square join-item tooltip tooltip-top" data-tip="Edit" aria-label="Edit"`
	if !strings.Contains(idle, wantEdit) {
		t.Fatalf("edit button should match Files join tip style: %s", truncate(idle, 800))
	}

	if _, err := lib.FullRescanSource(src.ID); err != nil {
		t.Fatal(err)
	}
	assertScanQueued(get(seriesPath))
	oob := get(seriesPath + "/task-indicators")
	if strings.Contains(oob, "can't evaluate field") || !strings.Contains(oob, "source-scan-actions-") {
		t.Fatalf("task-indicators OOB must render source_scan_actions: %s", truncate(oob, 600))
	}
	if strings.Contains(oob, `aria-label="Start full scan"`) || strings.Contains(oob, `/actions/full-rescan-source`) {
		t.Fatalf("OOB source_scan_actions must not offer Full scan: %s", truncate(oob, 600))
	}
	detail := get(seriesPath + "/sources/" + itoa(src.ID))
	if strings.Count(detail, `class="btn" disabled`) < 2 || !strings.Contains(detail, `for="modal-edit-source" class="btn"`) {
		t.Fatalf("source detail should keep Scan and Full scan disabled: %s", truncate(detail, 800))
	}
	if !strings.Contains(detail, `/actions/full-rescan-source`) {
		t.Fatalf("source detail must keep Full scan action: %s", truncate(detail, 800))
	}
	settingsAt := strings.Index(detail, ">Settings</span>")
	statusAt := strings.Index(detail, ">Status</span>")
	urlAt := strings.Index(detail, ">URL</th>")
	domainAt := strings.Index(detail, ">Domain</th>")
	if settingsAt < 0 || statusAt < settingsAt || urlAt < settingsAt || urlAt > statusAt || domainAt < statusAt || strings.Contains(detail, "Source settings") || !strings.Contains(detail, "md:grid-cols-2") {
		t.Fatalf("source detail should split Settings and Status: settings=%d status=%d url=%d domain=%d", settingsAt, statusAt, urlAt, domainAt)
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

// overviewBrowseHrefHas reports whether body contains an href="/browser?…" whose
// query includes every want key/value (order-insensitive; multi status ok).
func overviewBrowseHrefHas(body string, want map[string][]string) bool {
	const prefix = `href="/browser?`
	for {
		i := strings.Index(body, prefix)
		if i < 0 {
			return false
		}
		start := i + len(`href="`)
		end := strings.IndexByte(body[start:], '"')
		if end < 0 {
			return false
		}
		raw := html.UnescapeString(body[start : start+end])
		u, err := url.Parse(raw)
		if err != nil {
			body = body[start+1:]
			continue
		}
		q := u.Query()
		ok := true
		for k, vals := range want {
			got := q[k]
			have := map[string]int{}
			for _, g := range got {
				have[g]++
			}
			for _, v := range vals {
				if have[v] == 0 {
					ok = false
					break
				}
				have[v]--
			}
			if !ok {
				break
			}
		}
		if ok {
			return true
		}
		body = body[start+1:]
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
