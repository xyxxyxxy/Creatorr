package web_test

import (
	"fmt"
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

func TestVideoDetailDownloadHiddenWhenPresent(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "dl-hidden.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	roots, err := lib.ListRoots()
	if err != nil || len(roots) == 0 {
		t.Fatalf("roots: %v len=%d", err, len(roots))
	}
	prof, err := lib.CreateProfile("p-dl", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: roots[0].ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, source_url, acquired_via)
		VALUES (?, 'imp', 'Imported', 'downloaded', 'https://example.com/watch?v=imp', 'import')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, source_url)
		VALUES (?, 'w1', 'Wanted', 'wanted', 'https://example.com/watch?v=w1')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var importedID, wantedID int64
	if err := d.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'imp'`).Scan(&importedID); err != nil {
		t.Fatal(err)
	}
	if err := d.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'w1'`).Scan(&wantedID); err != nil {
		t.Fatal(err)
	}
	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)
	get := func(id int64) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/series/%d/videos/%d", ser.ID, id), nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rec.Code, truncate(rec.Body.String(), 400))
		}
		return rec.Body.String()
	}
	body := get(importedID)
	if strings.Contains(body, "Download now") {
		t.Fatal("imported downloaded video should hide Download now")
	}
	if strings.Contains(body, "File integrity") {
		t.Fatal("video detail must not show File integrity row (file-level now)")
	}
	if !strings.Contains(get(wantedID), "Download now") {
		t.Fatal("wanted video should show Download now")
	}
}
