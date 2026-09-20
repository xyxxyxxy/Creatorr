package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestVideoDetailIntegrityIndicatorEligible(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "integrity-ind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	rootPath := t.TempDir()
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: rootPath})
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)

	roots, err := lib.ListRoots()
	if err != nil || len(roots) == 0 {
		t.Fatalf("roots: %v len=%d", err, len(roots))
	}
	root := roots[0]
	prof, err := lib.CreateProfile("p-integrity", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := lib.UpdateProfileParams(prof.ID, library.UpdateProfileParams{VerifyMedia: &on}); err != nil {
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
		VALUES (?, 'v1', 'One', 'downloaded')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := d.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'v1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "one.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lib.RegisterFileKind(videoID, media, "video"); err != nil {
		t.Fatal(err)
	}
	_ = settings.Set(d, settings.KeyIntegrityCheckCron, "@quarterly")

	h := &web.Handler{Library: lib, Queue: q}
	r := chi.NewRouter()
	h.Mount(r)

	path := fmt.Sprintf("/series/%d/videos/%d", ser.ID, videoID)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, truncate(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-lucide="badge-help"`) {
		t.Fatalf("expected eligible badge-help: %s", truncate(body, 600))
	}
	// html/template escapes quotes in attributes (&#39;).
	if !strings.Contains(body, "File integrity") || !strings.Contains(body, "no hash yet") {
		t.Fatalf("expected eligible tip: %s", truncate(body, 600))
	}
}
