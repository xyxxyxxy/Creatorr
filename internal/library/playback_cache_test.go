package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestPlaybackCacheDirAndMeta(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	vid := seedStreamVideo(t, s, "pc-meta")

	if s.HasPlaybackCache(vid) {
		t.Fatal("expected no playback cache")
	}
	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:4.0,\nseg00000.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := library.PlaybackMeta{CachedSeconds: 4, WrittenAt: "2026-01-01T00:00:00Z", LastAccess: "2026-01-01T00:00:00Z", NextSeg: 1}
	writePlaybackMeta(t, s, vid, m)
	got, ok := s.LoadPlaybackMeta(vid)
	if !ok || got.CachedSeconds != 4 {
		t.Fatalf("meta=%v ok=%v", got, ok)
	}
	if !s.HasPlaybackCache(vid) {
		t.Fatal("expected HasPlaybackCache")
	}
	if err := s.ClearPlaybackCache(vid); err != nil {
		t.Fatal(err)
	}
	if s.HasPlaybackCache(vid) {
		t.Fatal("expected cleared")
	}
}

func TestEnforcePlaybackCacheBudgetLRU(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	if err := settings.Set(s.DB, settings.KeyStreamPlaybackCacheMaxHours, "10"); err != nil {
		t.Fatal(err)
	}

	s.PublicBaseURL = "http://example.com:8787"
	_ = settings.Set(s.DB, settings.KeyExternalBaseURL, "http://example.com:8787")
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "PC-LRU", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@lru"})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(remote string) int64 {
		t.Helper()
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: remote, Title: "T", WebpageURL: "https://www.example.com/watch?v=" + remote,
			SourceID: src.ID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable', stream_urls_kind = 'pipe' WHERE id = ?`, res.VideoID)
		return res.VideoID
	}
	v1 := mk("lru1")
	v2 := mk("lru2")
	v3 := mk("lru3")

	writePlaybackSecs(t, s, v1, 15000, "2026-01-01T00:00:00Z")
	writePlaybackSecs(t, s, v2, 15000, "2026-01-02T00:00:00Z")
	writePlaybackSecs(t, s, v3, 15000, "2026-01-03T00:00:00Z")
	// Budget 10h=36000s; total 45000 → evict oldest (v1) → 30000 under budget.

	if err := s.EnforcePlaybackCacheBudget(v3); err != nil {
		t.Fatal(err)
	}
	if s.HasPlaybackCache(v1) {
		t.Fatal("expected v1 LRU evicted")
	}
	if !s.HasPlaybackCache(v2) {
		t.Fatal("expected v2 kept")
	}
	if !s.HasPlaybackCache(v3) {
		t.Fatal("expected protected v3 kept")
	}
}

func TestDurableStreamPrefixPrefersPlayback(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	vid := seedStreamVideo(t, s, "pc-prefix")

	beginDir := s.BeginningDir(vid)
	_ = os.MkdirAll(beginDir, 0o755)
	_ = os.WriteFile(filepath.Join(beginDir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:10.0,\nseg00000.ts\n"), 0o644)
	if err := s.WriteBeginningMeta(vid, 10); err != nil {
		t.Fatal(err)
	}

	playDir := s.PlaybackCacheDir(vid)
	_ = os.MkdirAll(playDir, 0o755)
	_ = os.WriteFile(filepath.Join(playDir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:40.0,\nseg00000.ts\n"), 0o644)
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 40, HandoffSourceSeconds: 42, WrittenAt: "t", LastAccess: "t", NextSeg: 1,
	})

	dir, handoff, cached, ok := s.DurableStreamPrefix(vid)
	if !ok || cached != 40 || handoff != 42 {
		t.Fatalf("dir=%s handoff=%v cached=%v ok=%v", dir, handoff, cached, ok)
	}
	if dir != playDir {
		t.Fatalf("want playback dir, got %s", dir)
	}
}

func TestStreamCachePercent(t *testing.T) {
	if library.StreamCachePercent(50, 100, false) != 50 {
		t.Fatal()
	}
	if library.StreamCachePercent(50, 100, true) != 100 {
		t.Fatal()
	}
	if library.StreamCachePercent(50, 0, false) != -1 {
		t.Fatal()
	}
}

func TestClearPlaybackCachePass(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	vid := seedStreamVideo(t, s, "pc-clear")
	writePlaybackSecs(t, s, vid, 100, "2026-01-01T00:00:00Z")
	tid, err := s.EnqueueClearPlaybackCache()
	if err != nil || tid < 1 {
		t.Fatalf("enqueue: id=%d err=%v", tid, err)
	}
	n, err := s.ClearPlaybackCachePass(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("cleared=%d", n)
	}
	if s.HasPlaybackCache(vid) {
		t.Fatal("expected wiped")
	}
}

func seedStreamVideo(t *testing.T, s *library.Store, remote string) int64 {
	t.Helper()
	s.PublicBaseURL = "http://example.com:8787"
	_ = settings.Set(s.DB, settings.KeyExternalBaseURL, "http://example.com:8787")
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "PC-" + remote, RootID: rootID, QualityProfileID: profileID, Monitored: true,
		DeliveryMode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@" + remote,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: remote, Title: "T", WebpageURL: "https://www.example.com/watch?v=" + remote,
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable', stream_urls_kind = 'pipe' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	return res.VideoID
}

func writePlaybackMeta(t *testing.T, s *library.Store, vid int64, m library.PlaybackMeta) {
	t.Helper()
	// Use public API path via disk + column sync by Clear/write helpers.
	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil || len(b) == 0 {
		_ = os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:1.0,\nseg00000.ts\n"), 0o644)
	}
	// Write meta via promote path isn't available; use JSON file like production.
	path := filepath.Join(dir, "meta.json")
	raw := []byte(`{"cached_seconds":` + formatFloat(m.CachedSeconds) +
		`,"handoff_source_seconds":` + formatFloat(m.HandoffSourceSeconds) +
		`,"complete":false,"written_at":"` + m.WrittenAt + `","last_access":"` + m.LastAccess +
		`","next_seg":` + strconv.Itoa(m.NextSeg) + `}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	complete := 0
	if m.Complete {
		complete = 1
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET stream_playback_cached_seconds = ?, stream_playback_cache_complete = ?,
		stream_playback_cache_written_at = ?, stream_playback_cache_last_access = ? WHERE id = ?
	`, m.CachedSeconds, complete, m.WrittenAt, m.LastAccess, vid)
	if err != nil {
		t.Fatal(err)
	}
}

func writePlaybackSecs(t *testing.T, s *library.Store, vid int64, sec float64, access string) {
	t.Helper()
	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:1.0,\nseg00000.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: sec, WrittenAt: access, LastAccess: access, NextSeg: 1,
	})
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
