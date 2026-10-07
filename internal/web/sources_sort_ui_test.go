package web_test

import (
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/web"
)

func TestSourcesLastScannedSortDefaultDirNewestFirst(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	z, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Zulu", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/z",
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Alpha", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.CreateSeries(library.CreateSeriesParams{
		Title: "Middle", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/m",
	})
	if err != nil {
		t.Fatal(err)
	}
	var tid int64
	if err := d.SQL.QueryRow(`SELECT id FROM tasks LIMIT 1`).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	newest := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if _, err := d.SQL.Exec(`INSERT INTO source_history (source_id, created_at, event, message, detail, task_id) VALUES (?, ?, 'scanned', '', '{}', ?)`, a.Sources[0].ID, newest, tid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`INSERT INTO source_history (source_id, created_at, event, message, detail, task_id) VALUES (?, ?, 'scanned', '', '{}', ?)`, z.Sources[0].ID, old, tid); err != nil {
		t.Fatal(err)
	}

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	// No dir= → DefaultSortDir(last_scanned)=desc → newest first.
	req := httptest.NewRequest("GET", "/explorer/browse?type=sources&at=browser&view=table&sort=last_scanned", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	re := regexp.MustCompile(`href="/series/\d+">([^<]+)</a>`)
	var titles []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		titles = append(titles, m[1])
	}
	if len(titles) < 3 || titles[0] != "Alpha" || titles[1] != "Zulu" || titles[2] != "Middle" {
		t.Fatalf("default last_scanned want Alpha,Zulu,Middle got %v", titles)
	}
	if !regexp.MustCompile(`aria-label="Sort: Last scanned descending"`).MatchString(body) {
		t.Fatalf("toolbar should show Last scanned descending: %s", body[:min(1200, len(body))])
	}
}
