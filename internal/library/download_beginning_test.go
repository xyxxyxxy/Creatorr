package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestBeginningStoreRoundTrip(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()

	const vid int64 = 42
	if s.HasBeginning(vid) {
		t.Fatal("expected no beginning")
	}
	dir := s.BeginningDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:1.0,\na.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBeginningMeta(vid, 20); err != nil {
		t.Fatal(err)
	}
	if !s.HasBeginning(vid) {
		t.Fatal("expected beginning ready")
	}
	meta, ok := s.LoadBeginningMeta(vid)
	if !ok || meta.DurationSeconds != 20 {
		t.Fatalf("meta=%v ok=%v", meta, ok)
	}
	if err := s.ClearBeginning(vid); err != nil {
		t.Fatal(err)
	}
	if s.HasBeginning(vid) {
		t.Fatal("expected cleared")
	}
}

func TestEnqueueCacheBeginningDisabled(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	if err := settings.Set(s.DB, settings.KeyCacheBeginningSeconds, "0"); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueCacheBeginning(1)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected no enqueue, got %d", id)
	}
}

func TestFileSyncBeginningLostAndRestored(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	s.PublicBaseURL = "http://example.com:8787"
	if err := settings.Set(s.DB, settings.KeyCacheBeginningSeconds, "20"); err != nil {
		t.Fatal(err)
	}

	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@s",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "T", WebpageURL: "https://www.example.com/watch?v=vid1",
SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable', stream_urls_kind = 'pipe' WHERE id = ?`, vid); err != nil {
		t.Fatal(err)
	}

	dir := s.BeginningDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:1.0,\na.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteBeginningMeta(vid, 20); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if !v.StreamBeginningCached {
		t.Fatal("expected stream_beginning_cached after write")
	}

	// Lose cache without ClearBeginning (simulates disk wipe).
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	resSync, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if resSync.Total() < 1 {
		t.Fatalf("expected file sync change, got %d", resSync.Total())
	}
	v, err = s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "streamable" {
		t.Fatalf("status=%s want streamable", v.Status)
	}
	if v.StreamBeginningCached {
		t.Fatal("expected stream_beginning_cached cleared")
	}
	var beginTasks int
	if err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks WHERE kind = 'cache_beginning' AND video_id = ? AND status IN ('pending', 'running')
	`, vid).Scan(&beginTasks); err != nil {
		t.Fatal(err)
	}
	if beginTasks != 1 {
		t.Fatalf("expected cache_beginning requeue, got %d", beginTasks)
	}

	// Restore files on disk; sync should re-flag (cancel pending beginning first so restore path is clean).
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE video_id = ? AND kind = 'cache_beginning'`, vid); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:1.0,\na.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"duration_seconds":20,"written_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	resSync, err = s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if resSync.Total() < 1 {
		t.Fatalf("expected restore change, got %d", resSync.Total())
	}
	v, err = s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if !v.StreamBeginningCached {
		t.Fatal("expected stream_beginning_cached after restore")
	}
}

func TestFileSyncBeginningLostNoRequeueWhenDisabled(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	s.PublicBaseURL = "http://example.com:8787"
	if err := settings.Set(s.DB, settings.KeyCacheBeginningSeconds, "0"); err != nil {
		t.Fatal(err)
	}

	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@s2",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid2", Title: "T", WebpageURL: "https://www.example.com/watch?v=vid2",
SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable', stream_urls_kind = 'pipe', stream_beginning_cached = 1 WHERE id = ?`, vid); err != nil {
		t.Fatal(err)
	}

	resSync, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if resSync.Total() < 1 {
		t.Fatalf("expected beginning_missing change, got %d", resSync.Total())
	}
	var beginTasks int
	if err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks WHERE kind = 'cache_beginning' AND video_id = ?
	`, vid).Scan(&beginTasks); err != nil {
		t.Fatal(err)
	}
	if beginTasks != 0 {
		t.Fatalf("expected no requeue when setting 0, got %d", beginTasks)
	}
}

func TestStreamNeedsBeginningAndCDNDirect(t *testing.T) {
	cases := []struct {
		kind      string
		needBegin bool
		cdnDirect bool
	}{
		{"", true, false},
		{"pipe", true, false},
		{"hls", false, true},
		{"progressive", false, true},
	}
	for _, tc := range cases {
		if got := library.StreamNeedsBeginning(tc.kind); got != tc.needBegin {
			t.Fatalf("NeedsBeginning(%s)=%v want %v", tc.kind, got, tc.needBegin)
		}
		if got := library.StreamCDNDirect(tc.kind); got != tc.cdnDirect {
			t.Fatalf("CDNDirect(%s)=%v want %v", tc.kind, got, tc.cdnDirect)
		}
	}
}
