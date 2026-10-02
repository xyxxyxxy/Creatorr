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
	if !strings.Contains(body, `data-col="url"`) || !strings.Contains(body, `data-col="domain"`) || !strings.Contains(body, `data-col="name"`) {
		t.Fatalf("table missing name/url/domain cols: %s", body[:min(1200, len(body))])
	}
	if strings.Contains(body, "URL / Name") {
		t.Fatalf("table should not merge URL/Name: %s", body[:min(800, len(body))])
	}
	if !strings.Contains(body, `name="q_field"`) || !strings.Contains(body, `value="url"`) || !strings.Contains(body, `value="label"`) {
		t.Fatalf("sources toolbar missing q_field select: %s", body[:min(1200, len(body))])
	}
	if !strings.Contains(body, `value="series"`) {
		t.Fatalf("browser sources q_field missing series option: %s", body[:min(800, len(body))])
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
