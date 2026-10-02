package web_test

import (
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

func TestSourcesTableView(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	_, err = lib.CreateSeries(library.CreateSeriesParams{
		Title: "T", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest("GET", "/explorer/browse?type=sources&at=browser&view=table", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-list-table`) {
		t.Fatalf("missing data-list-table: %s", body[:min(800, len(body))])
	}
	if !strings.Contains(body, `data-col="url"`) || !strings.Contains(body, `data-col="domain"`) || !strings.Contains(body, `data-col="name"`) || !strings.Contains(body, `data-col="kind"`) || !strings.Contains(body, `data-col="discovered"`) {
		t.Fatalf("table missing name/url/domain/kind/discovered cols: %s", body[:min(1200, len(body))])
	}
	tbody := body
	if i := strings.Index(body, `id="sources-list-rows"`); i >= 0 {
		tbody = body[i:]
	}
	kindTD := strings.Index(tbody, `data-col="kind"`)
	discoveredTD := strings.Index(tbody, `data-col="discovered"`)
	nameTD := strings.Index(tbody, `data-col="name"`)
	if kindTD < 0 || discoveredTD < 0 || nameTD < 0 || kindTD >= discoveredTD || discoveredTD >= nameTD {
		t.Fatalf("col order kind→discovered→name: kind=%d discovered=%d name=%d", kindTD, discoveredTD, nameTD)
	}
	nameSlice := tbody[nameTD:min(nameTD+400, len(tbody))]
	if strings.Contains(nameSlice, `data-lucide="list-`) || strings.Contains(nameSlice, `data-lucide="film"`) || strings.Contains(nameSlice, `data-lucide="eye`) {
		t.Fatalf("kind/discovered icons must not sit in name column: %s", nameSlice)
	}
	kindSlice := tbody[kindTD:min(kindTD+350, len(tbody))]
	if !strings.Contains(kindSlice, `data-lucide="list-video"`) && !strings.Contains(kindSlice, `data-lucide="film"`) {
		t.Fatalf("kind column missing feed/single icon: %s", kindSlice)
	}
	if strings.Contains(kindSlice, `data-lucide="list-minus"`) || strings.Contains(kindSlice, `data-lucide="eye`) {
		t.Fatalf("kind column must be kind-only: %s", kindSlice)
	}
	discoveredSlice := tbody[discoveredTD:min(discoveredTD+350, len(tbody))]
	if !strings.Contains(discoveredSlice, "Wanted") && !strings.Contains(discoveredSlice, "Ignored") {
		t.Fatalf("discovered column missing Wanted/Ignored: %s", discoveredSlice)
	}
	if strings.Contains(body, "URL / Name") {
		t.Fatalf("table should not merge URL/Name: %s", body[:min(800, len(body))])
	}
	if !strings.Contains(body, `name="q_field"`) || !strings.Contains(body, `value="url"`) || !strings.Contains(body, `value="label"`) {
		t.Fatalf("sources toolbar missing q_field select: %s", body[:min(1200, len(body))])
	}
	qfIdx := strings.Index(body, `name="q_field"`)
	qIdx := strings.Index(body, `name="q"`)
	searchIconIdx := strings.Index(body, `data-lucide="search"`)
	if qfIdx < 0 || qIdx < 0 || qfIdx >= qIdx {
		t.Fatalf("q_field should sit left of search input: q_field=%d q=%d", qfIdx, qIdx)
	}
	if searchIconIdx < 0 || searchIconIdx >= qfIdx {
		t.Fatalf("search icon should live in q_field select, not the input: icon=%d q_field=%d", searchIconIdx, qfIdx)
	}
	labelIdx := strings.Index(body, `value="label"`)
	urlIdx := strings.Index(body, `value="url"`)
	seriesIdx := strings.Index(body, `value="series"`)
	if labelIdx < 0 || urlIdx < 0 || seriesIdx < 0 || labelIdx >= urlIdx || urlIdx >= seriesIdx {
		t.Fatalf("browser sources q_field order want Name, URL, Series: label=%d url=%d series=%d body=%s",
			labelIdx, urlIdx, seriesIdx, body[:min(1200, len(body))])
	}
	if !strings.Contains(body, `>Series</span>`) || !strings.Contains(body, "series=") {
		t.Fatalf("browser sources Filter missing Series select: %s", body[:min(1600, len(body))])
	}
	if !strings.Contains(body, `<table`) {
		t.Fatalf("missing table element")
	}
	if strings.Contains(body, `id="sources-list-infinite"`) {
		t.Fatalf("table view should not have infinite sentinel")
	}
	if !strings.Contains(body, `/explorer/browse`) {
		t.Fatalf("table pager should use explorer browse URLs")
	}
}

func TestSourcesTableViewBrowserShell(t *testing.T) {
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
		Title: "T", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest("GET", "/browser?type=sources&view=table", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-list-table`) {
		t.Fatalf("browser sources table missing data-list-table")
	}

	req = httptest.NewRequest("GET", "/explorer/browse?type=sources&at=series-detail&series_id="+itoa(ser.ID)+"&view=table", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("series sources status %d", rec.Code)
	}
	scoped := rec.Body.String()
	if strings.Contains(scoped, `aria-label="Source filters"`) || strings.Contains(scoped, `data-list-table`) {
		t.Fatalf("series-detail Sources should be plain list without Filter bar/table: %s", scoped[:min(800, len(scoped))])
	}
	if !strings.Contains(scoped, `id="sources-list-rows"`) {
		t.Fatalf("series-detail Sources missing list rows: %s", scoped[:min(400, len(scoped))])
	}
}

func TestSourcesViewLinkUsesExplorerBrowse(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	_, err = lib.CreateSeries(library.CreateSeriesParams{
		Title: "T", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/c",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest("GET", "/explorer/browse?type=sources&at=browser", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `hx-get="/explorer/browse?`) || !strings.Contains(body, `view=table`) {
		t.Fatalf("table view link should hx-get explorer browse fragment, got: %s", body[:min(1200, len(body))])
	}
}
