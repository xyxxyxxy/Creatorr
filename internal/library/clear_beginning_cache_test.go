package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestClearBeginningCachePass(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BeginClear", SourceURL: "https://www.example.com/@bc",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "bc1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=bc1",
		UploadDate: "2024-06-01T12:00:00Z", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBeginningMeta(res.VideoID, 20); err != nil {
		t.Fatal(err)
	}
	// WriteBeginningMeta needs index.m3u8 for HasBeginning; Clear only needs dir + flag.
	dir := s.BeginningDir(res.VideoID)
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStreamBeginningCached(res.VideoID, true); err != nil {
		t.Fatal(err)
	}

	pendingID, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind: queue.KindCacheBeginning, Domain: "example.com",
		SeriesID: ser.ID, VideoID: res.VideoID, Message: "Download beginning",
		Payload: map[string]any{"video_id": res.VideoID},
	})
	if err != nil {
		t.Fatal(err)
	}

	tid, err := s.EnqueueClearBeginningCache()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := s.ClearBeginningCachePass(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 1 {
		t.Fatalf("cleared=%d want 1", cleared)
	}
	if _, err := os.Stat(filepath.Join(s.CacheDir, "download-beginnings")); !os.IsNotExist(err) {
		t.Fatalf("expected beginning root gone, err=%v", err)
	}
	var flag int
	_ = s.DB.SQL.QueryRow(`SELECT stream_beginning_cached FROM videos WHERE id = ?`, res.VideoID).Scan(&flag)
	if flag != 0 {
		t.Fatalf("flag=%d want 0", flag)
	}
	pt, err := s.Queue.GetTask(pendingID)
	if err != nil {
		t.Fatal(err)
	}
	if pt.Status != queue.StatusCancelled {
		t.Fatalf("pending beginning status=%s want cancelled", pt.Status)
	}
}
