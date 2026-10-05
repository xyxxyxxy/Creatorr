package web_test

import (
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
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func TestExplorerBrowseAndBrowserShell(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Explorer Ser", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@explorer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO files (series_id, video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, NULL, ?, 'poster', datetime('now'), 3)
	`, ser.ID, filepath.Join(t.TempDir(), "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	for _, path := range []string{
		"/explorer/browse?type=series&at=series",
		"/explorer/browse?type=videos&at=browser",
		"/explorer/browse?type=sources&at=browser",
		"/explorer/browse?type=files&at=browser",
	} {
		rec := get(path)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, truncate(rec.Body.String(), 300))
		}
	}
	filesLiveRec := get("/explorer/browse?type=files&at=browser")
	filesLive := filesLiveRec.Body.String()
	if !strings.Contains(filesLive, `id="files-list-live"`) {
		t.Fatalf("files explorer missing live root: %s", truncate(filesLive, 400))
	}
	if strings.Contains(filesLive, `value="cards"`) || strings.Contains(filesLive, `value="gallery"`) {
		t.Fatalf("files explorer must not offer cards/gallery: %s", truncate(filesLive, 400))
	}
	// Default sort is Acquired (server-side); selected toggle href may omit sort= when URL has none.
	if !strings.Contains(filesLive, "Sort: Acquired") || !strings.Contains(filesLive, `name="sort" value="acquired"`) {
		t.Fatalf("browser files default sort should be Acquired: %s", truncate(filesLive, 600))
	}
	gotSortCookie := false
	for _, c := range filesLiveRec.Result().Cookies() {
		if c.Name == "creatorr_sort_files" && strings.HasPrefix(c.Value, "acquired") {
			gotSortCookie = true
			break
		}
	}
	if !gotSortCookie {
		t.Fatalf("browser files should remember Acquired sort cookie: %v", filesLiveRec.Result().Cookies())
	}
	srcLive := get("/explorer/browse?type=sources&at=browser").Body.String()
	if !strings.Contains(srcLive, `id="sources-list-live"`) {
		t.Fatalf("sources explorer missing live root: %s", truncate(srcLive, 400))
	}
	if strings.Contains(srcLive, `value="cards"`) || strings.Contains(srcLive, `value="gallery"`) {
		t.Fatalf("sources explorer must not offer cards/gallery: %s", truncate(srcLive, 400))
	}
	if !strings.Contains(srcLive, `list-filter-presence--solo`) || !strings.Contains(srcLive, ">Has</a>") {
		t.Fatalf("sources scan error should use Has/No presence: %s", truncate(srcLive, 600))
	}
	if !strings.Contains(srcLive, ">Domain<") || !strings.Contains(srcLive, ">Full scan<") ||
		!strings.Contains(srcLive, ">Schedule<") || !strings.Contains(srcLive, ">Series monitored<") ||
		!strings.Contains(srcLive, ">Discovered<") ||
		!strings.Contains(srcLive, `>Series</span>`) || !strings.Contains(srcLive, "series=") {
		t.Fatalf("sources missing new filters: %s", truncate(srcLive, 800))
	}
	if !strings.Contains(srcLive, "sort=last_scanned") || !strings.Contains(srcLive, "sort=domain") {
		t.Fatalf("sources sort should offer domain and last scanned: %s", truncate(srcLive, 800))
	}
	if strings.Contains(srcLive, `name="error"`) || strings.Contains(srcLive, "Has error") {
		t.Fatalf("sources must not use error= select options: %s", truncate(srcLive, 600))
	}
	hasErr := get("/explorer/browse?type=sources&at=browser&not_empty=scan_error")
	if hasErr.Code != 200 {
		t.Fatalf("not_empty=scan_error status %d", hasErr.Code)
	}
	if !strings.Contains(hasErr.Body.String(), `aria-label="Has scan error" aria-pressed="true"`) {
		t.Fatalf("Has scan error should be pressed: %s", truncate(hasErr.Body.String(), 600))
	}

	scoped := get("/explorer/browse?type=sources&at=series-detail&series_id=" + itoa(ser.ID))
	if scoped.Code != 200 {
		t.Fatalf("scoped sources status %d: %s", scoped.Code, truncate(scoped.Body.String(), 300))
	}
	scopedBody := scoped.Body.String()
	if strings.Contains(scopedBody, `aria-label="Source filters"`) || strings.Contains(scopedBody, `data-list-view-toolbar`) {
		t.Fatalf("series-detail Sources should omit Filter bar: %s", truncate(scopedBody, 600))
	}
	if strings.Contains(scopedBody, `data-list-table`) || strings.Contains(scopedBody, `data-view-mode="table"`) {
		t.Fatalf("series-detail Sources should stay list view: %s", truncate(scopedBody, 600))
	}
	if !strings.Contains(scopedBody, `id="sources-list-rows"`) {
		t.Fatalf("series-detail Sources missing list rows: %s", truncate(scopedBody, 400))
	}
	if strings.Contains(scopedBody, `data-sources-bulk-mode`) || strings.Contains(scopedBody, `js-source-select`) {
		t.Fatalf("series-detail Sources must not offer multi-select: %s", truncate(scopedBody, 600))
	}

	tableForced := get("/explorer/browse?type=sources&at=series-detail&series_id=" + itoa(ser.ID) + "&view=table")
	if tableForced.Code != 200 {
		t.Fatalf("view=table scoped status %d", tableForced.Code)
	}
	if strings.Contains(tableForced.Body.String(), `data-list-table`) || !strings.Contains(tableForced.Body.String(), `id="sources-list-rows"`) {
		t.Fatalf("series-detail Sources must ignore view=table: %s", truncate(tableForced.Body.String(), 600))
	}

	for _, old := range []string{"/series/list-live", "/videos/live"} {
		rec := get(old)
		if rec.Code != http.StatusPermanentRedirect {
			t.Fatalf("%s want 308 got %d", old, rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/explorer/browse?") {
			t.Fatalf("%s redirect Location=%q", old, loc)
		}
	}

	browser := get("/browser")
	if browser.Code != 200 {
		t.Fatalf("/browser status %d: %s", browser.Code, truncate(browser.Body.String(), 300))
	}
	body := browser.Body.String()
	if !strings.Contains(body, `aria-label="Explorer type"`) {
		t.Fatalf("browser missing type join: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `href="/browser?type=series" class="btn join-item btn-accent"`) ||
		!strings.Contains(body, `aria-current="page"`) {
		t.Fatalf("browser should mark active type: %s", truncate(body, 400))
	}
	if !strings.Contains(body, `id="series-list-live"`) {
		t.Fatalf("browser default type should be series: %s", truncate(body, 400))
	}
	foundTypeCookie := false
	for _, c := range browser.Result().Cookies() {
		if c.Name == "creatorr_browser_type" && c.Value == "series" {
			foundTypeCookie = true
		}
	}
	if !foundTypeCookie {
		t.Fatalf("browser should set type cookie to series")
	}

	wide := get("/browser?type=sources&wide=1")
	if wide.Code != http.StatusSeeOther {
		t.Fatalf("wide toggle want 303 got %d", wide.Code)
	}
	foundWide := false
	for _, c := range wide.Result().Cookies() {
		if c.Name == "creatorr_browser_wide" && c.Value == "1" {
			foundWide = true
		}
	}
	if !foundWide {
		t.Fatalf("wide=1 should set wide cookie")
	}

	widePage := httptest.NewRequest(http.MethodGet, "/browser?type=sources", nil)
	widePage.AddCookie(&http.Cookie{Name: "creatorr_browser_wide", Value: "1"})
	wideRec := httptest.NewRecorder()
	r.ServeHTTP(wideRec, widePage)
	if wideRec.Code != 200 {
		t.Fatalf("wide browser status %d", wideRec.Code)
	}
	wideBody := wideRec.Body.String()
	if !strings.Contains(wideBody, `class="list-panel card bg-base-100 border border-base-300 mb-4 browser-wide"`) {
		t.Fatalf("wide should expand list-panel only: %s", truncate(wideBody, 500))
	}
	if !strings.Contains(wideBody, `app-shell flex flex-col min-h-screen w-full mx-auto max-w-5xl`) {
		t.Fatalf("wide must keep shell max-w-5xl for nav/footer")
	}
	if strings.Contains(wideBody, `max-w-none`) {
		t.Fatalf("wide must not drop shell max-width")
	}

	srcPage := get("/browser?type=sources")
	if srcPage.Code != 200 {
		t.Fatalf("browser sources status %d", srcPage.Code)
	}
	if !strings.Contains(srcPage.Body.String(), `id="sources-list-live"`) {
		t.Fatalf("browser sources missing explorer: %s", truncate(srcPage.Body.String(), 400))
	}

	detail := get("/series/" + itoa(ser.ID))
	if detail.Code != 200 {
		t.Fatalf("series detail status %d: %s", detail.Code, truncate(detail.Body.String(), 300))
	}
	dbBody := detail.Body.String()
	if !strings.Contains(dbBody, `id="sources-list-live"`) {
		t.Fatalf("series detail sources should use Explorer: %s", truncate(dbBody, 400))
	}
	if strings.Contains(dbBody, "sources_page=") {
		t.Fatalf("series detail should not use sources_page pager")
	}
}

func TestFilesExplorerBulkModeAndIDs(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Bulk Files", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@bulkfiles",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO files (series_id, video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, NULL, ?, 'poster', datetime('now'), 3)
	`, ser.ID, filepath.Join(t.TempDir(), "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/explorer/browse?type=files&at=browser", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-files-bulk-mode`) || !strings.Contains(body, `js-file-select`) {
		t.Fatalf("files explorer missing multi-select: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-files-bulk-bar`) {
		t.Fatalf("files explorer missing bulk bar: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-files-bulk-check`) {
		t.Fatalf("files explorer missing Check integrity (btn_labeled): %s", truncate(body, 600))
	}
	if !strings.Contains(body, ">Status</span>") || !strings.Contains(body, "status=failed") ||
		!strings.Contains(body, "status=ok") || !strings.Contains(body, "status=unchecked") ||
		!strings.Contains(body, "status=inactive") || !strings.Contains(body, "status=na") {
		t.Fatalf("files explorer missing Status filter: %s", truncate(body, 800))
	}
	if strings.Contains(body, "missing=1") || strings.Contains(body, "integrity=failed") {
		t.Fatalf("legacy Missing/Integrity filters must be gone: %s", truncate(body, 800))
	}
	idsRec := httptest.NewRecorder()
	r.ServeHTTP(idsRec, httptest.NewRequest(http.MethodGet, "/files/ids?type=files&at=browser", nil))
	if idsRec.Code != 200 {
		t.Fatalf("ids status %d: %s", idsRec.Code, idsRec.Body.String())
	}
	if !strings.Contains(idsRec.Body.String(), `"ids"`) {
		t.Fatalf("ids json: %s", idsRec.Body.String())
	}
}

func TestFilesExplorerCheckIntegrityDisabledWhenQueued(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Busy Check", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@busycheck",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "bc1", Title: "Clip", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clip.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lib.RegisterFileKind(res.VideoID, path, "json"); err != nil {
		t.Fatal(err)
	}
	var fileID int64
	if err := d.SQL.QueryRow(`SELECT id FROM files WHERE video_id = ? AND kind = 'json'`, res.VideoID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.EnqueueFileHashCheck(res.VideoID, fileID); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=files&at=browser", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-tip="Integrity check already queued"`) {
		t.Fatalf("queued file should disable Check integrity: %s", truncate(body, 800))
	}
	if strings.Contains(body, `action="/actions/check-file-hash"`) {
		t.Fatal("Check integrity form must not render while queued")
	}
}

func TestSourcesExplorerBulkModeAndIDs(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Bulk Sources", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@bulksources",
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/explorer/browse?type=sources&at=browser", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-sources-bulk-mode`) || !strings.Contains(body, `js-source-select`) {
		t.Fatalf("sources explorer missing multi-select: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-sources-bulk-bar`) {
		t.Fatalf("sources explorer missing bulk bar: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-sources-bulk-scan`) {
		t.Fatalf("sources explorer missing Scan (btn_labeled): %s", truncate(body, 600))
	}
	if !strings.Contains(body, `action="/actions/bulk-scan-sources"`) ||
		!strings.Contains(body, `action="/actions/bulk-delete-sources"`) {
		t.Fatalf("sources explorer missing bulk actions: %s", truncate(body, 600))
	}
	idsRec := httptest.NewRecorder()
	r.ServeHTTP(idsRec, httptest.NewRequest(http.MethodGet, "/sources/ids?type=sources&at=browser", nil))
	if idsRec.Code != 200 {
		t.Fatalf("ids status %d: %s", idsRec.Code, idsRec.Body.String())
	}
	if !strings.Contains(idsRec.Body.String(), `"ids"`) ||
		!strings.Contains(idsRec.Body.String(), strconv.FormatInt(srcID, 10)) {
		t.Fatalf("ids json: %s", idsRec.Body.String())
	}
	impactRec := httptest.NewRecorder()
	r.ServeHTTP(impactRec, httptest.NewRequest(http.MethodGet,
		"/sources/delete-impact?source_id="+strconv.FormatInt(srcID, 10), nil))
	if impactRec.Code != 200 {
		t.Fatalf("delete-impact status %d: %s", impactRec.Code, impactRec.Body.String())
	}
	if !strings.Contains(impactRec.Body.String(), `"indexed"`) ||
		!strings.Contains(impactRec.Body.String(), `"downloaded"`) {
		t.Fatalf("delete-impact json: %s", impactRec.Body.String())
	}

	delRec := httptest.NewRecorder()
	form := url.Values{}
	form.Set("source_id", strconv.FormatInt(srcID, 10))
	form.Set("confirm_delete", "1")
	form.Set("redirect", "/browser?type=sources")
	req := httptest.NewRequest(http.MethodPost, "/actions/bulk-delete-sources", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(delRec, req)
	if delRec.Code != http.StatusSeeOther {
		t.Fatalf("bulk delete status %d: %s", delRec.Code, delRec.Body.String())
	}
	loc := delRec.Header().Get("Location")
	if !strings.Contains(loc, "ok=bulk_sources_deleted") {
		t.Fatalf("bulk delete redirect: %s", loc)
	}
	if _, err := lib.GetSourceByID(srcID); err == nil {
		t.Fatal("source should be gone after bulk delete")
	}
}

func TestNotificationsExplorerBulkModeAndIDs(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	if _, err := notify.InsertNotification(d, notify.EventVerifyFailed, "bulk notify", "x", 0, false, ""); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/explorer/browse?type=notifications&at=browser", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-notifications-bulk-mode`) || !strings.Contains(body, `js-notification-select`) {
		t.Fatalf("notifications missing multi-select: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-notifications-bulk-bar`) {
		t.Fatalf("notifications missing bulk bar: %s", truncate(body, 600))
	}
	if !strings.Contains(body, `data-notifications-bulk-read`) ||
		!strings.Contains(body, `data-notifications-bulk-unread`) {
		t.Fatalf("notifications missing Mark read/unread (btn_labeled): %s", truncate(body, 600))
	}
	idsRec := httptest.NewRecorder()
	r.ServeHTTP(idsRec, httptest.NewRequest(http.MethodGet, "/notifications/ids?type=notifications&at=browser", nil))
	if idsRec.Code != 200 {
		t.Fatalf("ids status %d: %s", idsRec.Code, idsRec.Body.String())
	}
	if !strings.Contains(idsRec.Body.String(), `"ids"`) {
		t.Fatalf("ids json: %s", idsRec.Body.String())
	}
}

func TestVideoSeriesFilterBadgesUseTitlesForFilterIDs(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	onPage, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "On Page Ser", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@onpage",
	})
	if err != nil {
		t.Fatal(err)
	}
	offPage, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Off Page Ser", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@offpage",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only On Page has a matching video; Off Page is filter-only (no row on this page).
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'r1', 'Ep', 'wanted')
	`, onPage.ID); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	path := "/browser?type=videos&series=" + strconv.FormatInt(onPage.ID, 10) +
		"&series=" + strconv.FormatInt(offPage.ID, 10) + "&status=wanted"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `aria-label="On Page Ser"`) || !strings.Contains(body, `aria-label="Off Page Ser"`) {
		t.Fatalf("Series chips must use titles for filter IDs not on the page: %s", truncate(body, 1200))
	}
	if strings.Contains(body, `aria-label="#`+strconv.FormatInt(offPage.ID, 10)+`"`) {
		t.Fatalf("Off Page Ser must not fall back to #id chip: %s", truncate(body, 800))
	}
}

