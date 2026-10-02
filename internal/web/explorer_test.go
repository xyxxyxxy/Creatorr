package web_test

import (
	"net/http"
	"net/http/httptest"
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
		"/explorer/browse?type=videos&at=videos",
		"/explorer/browse?type=sources&at=browser",
	} {
		rec := get(path)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", path, rec.Code, truncate(rec.Body.String(), 300))
		}
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
		!strings.Contains(srcLive, ">Schedule<") || !strings.Contains(srcLive, ">Series monitored<") {
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
	if !strings.Contains(body, `href="/browser?type=series" class="btn join-item btn-primary"`) ||
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
