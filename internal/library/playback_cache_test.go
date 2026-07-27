package library_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

func TestCoalesceDependentMPEGTSSegs(t *testing.T) {
	dir := t.TempDir()
	// One good TS from ffmpeg; second file is garbage (not independently decodable).
	good := filepath.Join(dir, "seg00000.ts")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-nostdin", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=25",
		"-f", "lavfi", "-i", "sine=f=440",
		"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-f", "mpegts", good)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg unavailable: %v (%s)", err, out)
	}
	bad := filepath.Join(dir, "seg00001.ts")
	if err := os.WriteFile(bad, []byte("not a transport stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-INDEPENDENT-SEGMENTS
#EXTINF:1.000000,
seg00000.ts
#EXTINF:0.500000,
seg00001.ts
#EXT-X-ENDLIST
`), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := library.CoalesceDependentMPEGTSSegs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected coalesce")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("bad seg should be removed")
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if strings.Contains(s, "seg00001.ts") {
		t.Fatalf("merged uri still listed: %s", s)
	}
	if strings.Contains(s, "INDEPENDENT-SEGMENTS") {
		t.Fatalf("must strip independent flag: %s", s)
	}
	if !strings.Contains(s, "#EXTINF:1.500000,") && !strings.Contains(s, "#EXTINF:1.5") {
		t.Fatalf("expected summed duration: %s", s)
	}
	if !strings.Contains(s, "#EXT-X-ENDLIST") {
		t.Fatalf("preserve ENDLIST: %s", s)
	}
}

// TestRematerializeVideo1Live repairs repo var/cache/playback-cache/1 when asked.
// Run: CREATORR_REMAT_V1=1 go test ./internal/library -run RematerializeVideo1Live -count=1
func TestRematerializeVideo1Live(t *testing.T) {
	if os.Getenv("CREATORR_REMAT_V1") != "1" {
		t.Skip("set CREATORR_REMAT_V1=1 to rematerialize var/cache/playback-cache/1")
	}
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// library tests often run with cwd = module root; allow override.
	if v := os.Getenv("CREATORR_ROOT"); v != "" {
		root = v
	}
	s := openLib(t)
	s.CacheDir = filepath.Join(root, "var", "cache")
	dir := s.PlaybackCacheDir(1)
	if _, err := os.Stat(filepath.Join(dir, "index.m3u8")); err != nil {
		t.Fatalf("cache missing under %s: %v", dir, err)
	}
	if err := s.RematerializeCompletePlayback(1); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "#EXT-X-DISCONTINUITY") {
		t.Fatalf("discontinuity remains: %s", body)
	}
	t.Logf("rematerialized ok:\n%s", body)
}

func TestRematerializeCompletePlayback(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	_ = settings.Set(s.DB, settings.KeyStreamPlaybackCache, "true")
	vid := seedStreamVideo(t, s, "pc-remat")

	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"seg00000.ts", "seg00001.ts"} {
		out := filepath.Join(dir, name)
		cmd := exec.Command("ffmpeg", "-hide_banner", "-nostdin", "-y",
			"-f", "lavfi", "-i", "testsrc=size=64x64:rate=25",
			"-f", "lavfi", "-i", "sine=f=440",
			"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
			"-f", "mpegts", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("ffmpeg unavailable: %v (%s)", err, b)
		}
		_ = i
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:1.000000,
seg00000.ts
#EXT-X-DISCONTINUITY
#EXTINF:1.000000,
seg00001.ts
#EXT-X-ENDLIST
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 2, HandoffSourceSeconds: 2, Complete: true,
		WrittenAt: "t", LastAccess: "t", NextSeg: 2, LiveSegsCopied: 1,
	})

	if err := s.RematerializeCompletePlayback(vid); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("discontinuity must be gone: %s", text)
	}
	if !strings.Contains(text, "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", text)
	}
	m, ok := s.LoadPlaybackMeta(vid)
	if !ok || !m.Complete || m.CachedSeconds <= 0 {
		t.Fatalf("meta: ok=%v %+v", ok, m)
	}
	if m.LiveSegsCopied != 0 {
		t.Fatalf("live_segs_copied reset want 0 got %d", m.LiveSegsCopied)
	}
	entries := 0
	for _, line := range strings.Split(text, "\n") {
		u := strings.TrimSpace(line)
		if u == "" || strings.HasPrefix(u, "#") {
			continue
		}
		entries++
		path := filepath.Join(dir, filepath.Base(u))
		cmd := exec.Command("ffmpeg", "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-i", path, "-f", "null", "-")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("seg %s not independently decodable: %v (%s)", u, err, out)
		}
	}
	if entries < 1 {
		t.Fatal("expected rematerialized segs")
	}
}

func TestFinalizePlaybackCacheIfNearDuration(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	_ = settings.Set(s.DB, settings.KeyStreamPlaybackCache, "true")
	vid := seedStreamVideo(t, s, "pc-near-dur")

	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fake TS bytes: rematerialize may fail; complete flag must still stick.
	if err := os.WriteFile(filepath.Join(dir, "seg00000.ts"), []byte("ts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(`#EXTM3U
#EXT-X-PLAYLIST-TYPE:EVENT
#EXTINF:90.500000,
seg00000.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 90.5, HandoffSourceSeconds: 90.5, Complete: false,
		WrittenAt: "t", LastAccess: "t", NextSeg: 1,
	})

	if err := s.FinalizePlaybackCacheIfNearDuration(vid, 92); err != nil {
		t.Fatal(err)
	}
	m, ok := s.LoadPlaybackMeta(vid)
	if !ok || !m.Complete {
		t.Fatalf("expected complete within 2s slack: ok=%v meta=%+v", ok, m)
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", body)
	}
}

func TestFinalizePlaybackCacheIfNearDurationTooShort(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	_ = settings.Set(s.DB, settings.KeyStreamPlaybackCache, "true")
	vid := seedStreamVideo(t, s, "pc-far-dur")

	dir := s.PlaybackCacheDir(vid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seg00000.ts"), []byte("ts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:80.0,\nseg00000.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 80, Complete: false, WrittenAt: "t", LastAccess: "t", NextSeg: 1,
	})
	if err := s.FinalizePlaybackCacheIfNearDuration(vid, 92); err != nil {
		t.Fatal(err)
	}
	m, ok := s.LoadPlaybackMeta(vid)
	if !ok || m.Complete {
		t.Fatalf("must not complete when short: ok=%v meta=%+v", ok, m)
	}
}

func TestPromoteMarksCompleteOnLiveEndlist(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	_ = settings.Set(s.DB, settings.KeyStreamPlaybackCache, "true")
	vid := seedStreamVideo(t, s, "pc-endlist")

	beginDir := s.BeginningDir(vid)
	_ = os.MkdirAll(beginDir, 0o755)
	_ = os.WriteFile(filepath.Join(beginDir, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:10.0,\nseg00000.ts\n"), 0o644)
	_ = os.WriteFile(filepath.Join(beginDir, "seg00000.ts"), []byte("begin"), 0o644)
	if err := s.WriteBeginningMeta(vid, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsurePlaybackCacheSeeded(vid); err != nil {
		t.Fatal(err)
	}

	live := t.TempDir()
	_ = os.WriteFile(filepath.Join(live, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:4.0,
seg00000.ts
#EXT-X-ENDLIST
`), 0o644)
	_ = os.WriteFile(filepath.Join(live, "seg00000.ts"), []byte("live"), 0o644)

	// Declared duration 92; mux only reached ~14s - ENDLIST still completes.
	if err := s.PromoteLiveSegmentsToPlayback(vid, live, 92); err != nil {
		t.Fatal(err)
	}
	m, ok := s.LoadPlaybackMeta(vid)
	if !ok || !m.Complete {
		t.Fatalf("expected complete after live ENDLIST: ok=%v meta=%+v", ok, m)
	}
	body, err := os.ReadFile(filepath.Join(s.PlaybackCacheDir(vid), "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", body)
	}
	if !strings.Contains(string(body), "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Fatalf("expected VOD: %s", body)
	}
}

func TestPromoteSkipsCompleteCache(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	_ = settings.Set(s.DB, settings.KeyStreamPlaybackCache, "true")
	vid := seedStreamVideo(t, s, "pc-complete-skip")

	dir := s.PlaybackCacheDir(vid)
	_ = os.MkdirAll(dir, 0o755)
	playlist := "#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:10.0,\nseg00000.ts\n#EXT-X-ENDLIST\n"
	_ = os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(playlist), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "seg00000.ts"), []byte("vod"), 0o644)
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 10, HandoffSourceSeconds: 10, Complete: true,
		WrittenAt: "t", LastAccess: "t", NextSeg: 1, LiveSegsCopied: 0,
	})

	live := t.TempDir()
	_ = os.WriteFile(filepath.Join(live, "index.m3u8"), []byte("#EXTM3U\n#EXTINF:4.0,\nseg00000.ts\n#EXT-X-ENDLIST\n"), 0o644)
	_ = os.WriteFile(filepath.Join(live, "seg00000.ts"), []byte("live-again"), 0o644)

	if err := s.PromoteLiveSegmentsToPlayback(vid, live, 10); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != playlist {
		t.Fatalf("complete cache mutated:\n%s", body)
	}
	m, ok := s.LoadPlaybackMeta(vid)
	if !ok || !m.Complete || m.CachedSeconds != 10 {
		t.Fatalf("meta changed: ok=%v %+v", ok, m)
	}
}

func TestEnsurePlaybackHandoffDiscontinuity(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	vid := seedStreamVideo(t, s, "pc-disc")

	beginDir := s.BeginningDir(vid)
	_ = os.MkdirAll(beginDir, 0o755)
	_ = os.WriteFile(filepath.Join(beginDir, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:5.0,
seg00000.ts
#EXTINF:5.0,
seg00001.ts
`), 0o644)
	if err := s.WriteBeginningMeta(vid, 10); err != nil {
		t.Fatal(err)
	}

	playDir := s.PlaybackCacheDir(vid)
	_ = os.MkdirAll(playDir, 0o755)
	// Seeded beginning (2 segs) + promoted live (1 seg) without DISCONTINUITY.
	_ = os.WriteFile(filepath.Join(playDir, "index.m3u8"), []byte(`#EXTM3U
#EXTINF:5.0,
seg00000.ts
#EXTINF:5.0,
seg00001.ts
#EXTINF:4.0,
seg00002.ts
`), 0o644)
	writePlaybackMeta(t, s, vid, library.PlaybackMeta{
		CachedSeconds: 14, HandoffSourceSeconds: 14, WrittenAt: "t", LastAccess: "t",
		NextSeg: 3, LiveSegsCopied: 1,
	})

	_, _, _, ok := s.DurableStreamPrefix(vid)
	if !ok {
		t.Fatal("expected playback prefix")
	}
	body, err := os.ReadFile(filepath.Join(playDir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "seg00001.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:4.0,\nseg00002.ts") {
		t.Fatalf("expected discontinuity before first live seg: %s", body)
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
	completeJSON := "false"
	if m.Complete {
		completeJSON = "true"
	}
	path := filepath.Join(dir, "meta.json")
	raw := []byte(`{"cached_seconds":` + formatFloat(m.CachedSeconds) +
		`,"handoff_source_seconds":` + formatFloat(m.HandoffSourceSeconds) +
		`,"complete":` + completeJSON + `,"written_at":"` + m.WrittenAt + `","last_access":"` + m.LastAccess +
		`","next_seg":` + strconv.Itoa(m.NextSeg) +
		`,"live_segs_copied":` + strconv.Itoa(m.LiveSegsCopied) + `}`)
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
