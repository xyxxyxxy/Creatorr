package worker_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
)

func TestApplyPackStreamAutoIgnoreSkipsBeginning(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "pack-ai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.Set(d, settings.KeyCacheBeginningSeconds, "20")
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)

	lib := &library.Store{DB: d, Queue: queue.NewStore(d)}
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Stream AI", SourceURL: "https://example.com/s", RootID: root.ID, QualityProfileID: prof.ID,
		Monitored: true, AutoIgnoreMediaTypes: []string{"short"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, source_url, status)
		VALUES (?, 'vid-short', 'Short', 'https://example.com/v/s', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	videoID, _ := res.LastInsertId()

	ignored, err := worker.ApplyPackStreamAutoIgnore(lib, ser.ID, videoID, 0, "short")
	if err != nil {
		t.Fatal(err)
	}
	if !ignored {
		t.Fatal("expected auto-ignore")
	}
	v, err := lib.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "ignored" {
		t.Fatalf("status=%q want ignored", v.Status)
	}
	if v.MediaType != "short" {
		t.Fatalf("media_type=%q", v.MediaType)
	}

	// Beginning must not enqueue for non-streamable (ignored) videos.
	id, err := lib.EnqueueCacheBeginning(videoID)
	if err == nil && id != 0 {
		t.Fatalf("beginning enqueued id=%d for ignored video", id)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = ? AND video_id = ?`, queue.KindCacheBeginning, videoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("cache_beginning tasks=%d want 0", n)
	}

	// Empty media_type never ignores.
	res2, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, source_url, status)
		VALUES (?, 'vid-empty', 'Empty', 'https://example.com/v/e', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid2, _ := res2.LastInsertId()
	ignored, err = worker.ApplyPackStreamAutoIgnore(lib, ser.ID, vid2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if ignored {
		t.Fatal("empty media_type must not auto-ignore")
	}
}
