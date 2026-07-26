package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

const playbackMetaFile = "meta.json"

// PlaybackMeta describes a progressive on-play stream cache for one video.
type PlaybackMeta struct {
	CachedSeconds        float64 `json:"cached_seconds"`
	HandoffSourceSeconds float64 `json:"handoff_source_seconds,omitempty"`
	Complete             bool    `json:"complete"`
	WrittenAt            string  `json:"written_at"`
	LastAccess           string  `json:"last_access"`
	NextSeg              int     `json:"next_seg"`       // next seg index to append
	LiveSegsCopied       int     `json:"live_segs_copied"` // how many live playlist entries already ingested
}

var playbackPromoteMu sync.Mutex

// PlaybackCacheDir returns {CacheDir}/playback-cache/{videoID}/.
func (s *Store) PlaybackCacheDir(videoID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("var", "cache")
	}
	return filepath.Join(root, "playback-cache", strconv.FormatInt(videoID, 10))
}

// HasPlaybackCache reports whether a usable progressive cache exists.
func (s *Store) HasPlaybackCache(videoID int64) bool {
	_, ok := s.LoadPlaybackMeta(videoID)
	return ok
}

// LoadPlaybackMeta reads meta.json when present with cached_seconds > 0 and an index playlist.
func (s *Store) LoadPlaybackMeta(videoID int64) (PlaybackMeta, bool) {
	dir := s.PlaybackCacheDir(videoID)
	data, err := os.ReadFile(filepath.Join(dir, playbackMetaFile))
	if err != nil {
		return PlaybackMeta{}, false
	}
	var m PlaybackMeta
	if err := json.Unmarshal(data, &m); err != nil || m.CachedSeconds <= 0 {
		return PlaybackMeta{}, false
	}
	if st, err := os.Stat(filepath.Join(dir, "index.m3u8")); err != nil || st.Size() == 0 {
		return PlaybackMeta{}, false
	}
	return m, true
}

// ClearPlaybackCache removes progressive cache for one video and zeros DB columns.
func (s *Store) ClearPlaybackCache(videoID int64) error {
	dir := s.PlaybackCacheDir(videoID)
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.ClearPlaybackCacheColumns(videoID)
}

// ClearPlaybackCacheColumns zeros progressive cache columns without touching disk.
func (s *Store) ClearPlaybackCacheColumns(videoID int64) error {
	_, err := s.DB.SQL.Exec(`
		UPDATE videos SET
		  stream_playback_cached_seconds = 0,
		  stream_playback_cache_complete = 0,
		  stream_playback_cache_written_at = NULL,
		  stream_playback_cache_last_access = NULL
		WHERE id = ?
	`, videoID)
	return err
}

// TouchPlaybackCacheAccess bumps last_access on disk + DB for LRU.
func (s *Store) TouchPlaybackCacheAccess(videoID int64) {
	m, ok := s.LoadPlaybackMeta(videoID)
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m.LastAccess = now
	_ = s.writePlaybackMetaFile(videoID, m)
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET stream_playback_cache_last_access = ? WHERE id = ?`, now, videoID)
}

func (s *Store) writePlaybackMetaFile(videoID int64, m PlaybackMeta) error {
	dir := s.PlaybackCacheDir(videoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, playbackMetaFile), b, 0o644)
}

func (s *Store) syncPlaybackCacheColumns(videoID int64, m PlaybackMeta) error {
	complete := 0
	if m.Complete {
		complete = 1
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE videos SET
		  stream_playback_cached_seconds = ?,
		  stream_playback_cache_complete = ?,
		  stream_playback_cache_written_at = ?,
		  stream_playback_cache_last_access = ?
		WHERE id = ?
	`, m.CachedSeconds, complete, nullIfEmpty(m.WrittenAt), nullIfEmpty(m.LastAccess), videoID)
	return err
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// DurableStreamPrefix returns the preferred durable HLS prefix dir for handoff
// (playback-cache if present, else beginning). ok false when neither usable.
func (s *Store) DurableStreamPrefix(videoID int64) (dir string, handoffSource float64, cachedSec float64, ok bool) {
	if m, pok := s.LoadPlaybackMeta(videoID); pok {
		_ = s.ensurePlaybackHandoffDiscontinuity(videoID)
		h := m.HandoffSourceSeconds
		if h <= 0 {
			h = m.CachedSeconds
		}
		return s.PlaybackCacheDir(videoID), h, m.CachedSeconds, true
	}
	if m, bok := s.LoadBeginningMeta(videoID); bok {
		h := m.HandoffSourceSeconds
		if h <= 0 {
			h = m.DurationSeconds
		}
		return s.BeginningDir(videoID), h, m.DurationSeconds, true
	}
	return "", 0, 0, false
}

// PlaybackCacheEnabled reads the global setting.
func (s *Store) PlaybackCacheEnabled() bool {
	ok, err := settings.StreamPlaybackCacheEnabled(s.DB)
	return err == nil && ok
}

// EnsurePlaybackCacheSeeded copies beginning into playback-cache when progressive is empty
// and a beginning exists. Returns current meta (possibly empty).
func (s *Store) EnsurePlaybackCacheSeeded(videoID int64) (PlaybackMeta, error) {
	if m, ok := s.LoadPlaybackMeta(videoID); ok {
		return m, nil
	}
	begin, ok := s.LoadBeginningMeta(videoID)
	if !ok {
		return PlaybackMeta{}, nil
	}
	src := s.BeginningDir(videoID)
	dest := s.PlaybackCacheDir(videoID)
	if err := os.RemoveAll(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return PlaybackMeta{}, err
	}
	if err := copyDirFiles(src, dest); err != nil {
		return PlaybackMeta{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	next := countPlaybackSegs(dest)
	m := PlaybackMeta{
		CachedSeconds:        begin.DurationSeconds,
		HandoffSourceSeconds: begin.HandoffSourceSeconds,
		Complete:             false,
		WrittenAt:            now,
		LastAccess:           now,
		NextSeg:              next,
	}
	if m.HandoffSourceSeconds <= 0 {
		m.HandoffSourceSeconds = m.CachedSeconds
	}
	if err := s.writePlaybackMetaFile(videoID, m); err != nil {
		return PlaybackMeta{}, err
	}
	_ = s.syncPlaybackCacheColumns(videoID, m)
	_, _ = CoalesceDependentMPEGTSSegs(dest)
	if m2, ok := s.LoadPlaybackMeta(videoID); ok {
		return m2, nil
	}
	return m, nil
}

func countPlaybackSegs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "seg") && (strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".m4s")) {
			n++
		}
	}
	return n
}

func copyDirFiles(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		in, err := os.Open(filepath.Join(src, name))
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(dest, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		_ = in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// PromoteLiveSegmentsToPlayback copies new live HLS segments into the progressive cache.
// protectVideoID is skipped during budget eviction (active play).
func (s *Store) PromoteLiveSegmentsToPlayback(videoID int64, liveDir string, durationSec float64) error {
	if !s.PlaybackCacheEnabled() {
		return nil
	}
	playbackPromoteMu.Lock()
	defer playbackPromoteMu.Unlock()

	m, err := s.EnsurePlaybackCacheSeeded(videoID)
	if err != nil {
		return err
	}
	dest := s.PlaybackCacheDir(videoID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if m.WrittenAt == "" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		m.WrittenAt = now
		m.LastAccess = now
	}
	next := m.NextSeg
	if next < 0 {
		next = countPlaybackSegs(dest)
	}

	liveEntries := parsePlaybackMediaEntries(filepath.Join(liveDir, "index.m3u8"))
	if m.LiveSegsCopied > len(liveEntries) {
		m.LiveSegsCopied = len(liveEntries)
	}
	toCopy := liveEntries[m.LiveSegsCopied:]
	liveEnded := playbackPlaylistHasEndlist(filepath.Join(liveDir, "index.m3u8"))
	if len(toCopy) == 0 {
		_ = s.ensurePlaybackHandoffDiscontinuity(videoID)
		return s.finalizePlaybackCacheComplete(videoID, m, liveEnded, durationSec)
	}

	added := 0
	var addSec float64
	var playlist strings.Builder
	existing := filepath.Join(dest, "index.m3u8")
	if data, err := os.ReadFile(existing); err == nil && len(data) > 0 {
		playlist.Write(stripPlaybackEndlist(data))
		if !strings.HasSuffix(playlist.String(), "\n") {
			playlist.WriteByte('\n')
		}
	} else {
		playlist.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}

	// First live segs after a beginning seed need DISCONTINUITY: live mux PTS restart at handoff.
	if m.LiveSegsCopied == 0 && playbackPlaylistHasMedia(playlist.String()) && !strings.Contains(playlist.String(), "#EXT-X-DISCONTINUITY") {
		playlist.WriteString("#EXT-X-DISCONTINUITY\n")
	}

	for _, e := range toCopy {
		src := filepath.Join(liveDir, e.uri)
		if st, err := os.Stat(src); err != nil || st.Size() == 0 {
			break // wait until sequential next seg exists
		}
		ext := filepath.Ext(e.uri)
		if ext == "" {
			ext = ".ts"
		}
		name := fmt.Sprintf("seg%05d%s", next, ext)
		dst := filepath.Join(dest, name)
		b, err := os.ReadFile(src)
		if err != nil {
			break
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		playlist.WriteString("#EXTINF:")
		playlist.WriteString(e.extinf)
		if !strings.Contains(e.extinf, ",") {
			playlist.WriteByte(',')
		}
		playlist.WriteByte('\n')
		playlist.WriteString(name)
		playlist.WriteByte('\n')
		next++
		added++
		addSec += playbackExtinfSeconds(e.extinf)
	}
	if added == 0 {
		return s.finalizePlaybackCacheComplete(videoID, m, liveEnded, durationSec)
	}
	m.CachedSeconds += addSec
	m.NextSeg = next
	m.LiveSegsCopied += added
	if m.HandoffSourceSeconds <= 0 {
		m.HandoffSourceSeconds = m.CachedSeconds
	} else {
		m.HandoffSourceSeconds += addSec
	}
	m.LastAccess = time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(existing, []byte(playlist.String()), 0o644); err != nil {
		return err
	}
	if err := s.writePlaybackMetaFile(videoID, m); err != nil {
		return err
	}
	_ = s.syncPlaybackCacheColumns(videoID, m)
	_, _ = CoalesceDependentMPEGTSSegs(dest)
	if m2, ok := s.LoadPlaybackMeta(videoID); ok {
		m = m2
	}
	if err := s.finalizePlaybackCacheComplete(videoID, m, liveEnded, durationSec); err != nil {
		return err
	}
	return s.EnforcePlaybackCacheBudget(videoID)
}

// FinalizePlaybackCacheIfNearDuration marks a partial progressive cache complete when
// cached seconds are within slack of declared duration (live dir may already be gone).
func (s *Store) FinalizePlaybackCacheIfNearDuration(videoID int64, durationSec float64) error {
	m, ok := s.LoadPlaybackMeta(videoID)
	if !ok {
		return nil
	}
	return s.finalizePlaybackCacheComplete(videoID, m, false, durationSec)
}

// finalizePlaybackCacheComplete marks progressive cache complete when live mux ended
// or cached seconds are within a small slack of declared duration (mux length can be
// slightly short of NFO duration_seconds).
func (s *Store) finalizePlaybackCacheComplete(videoID int64, m PlaybackMeta, liveEnded bool, durationSec float64) error {
	if m.Complete {
		return nil
	}
	nearDuration := durationSec > 0 && m.CachedSeconds+2.0 >= durationSec
	if !liveEnded && !nearDuration {
		return nil
	}
	index := filepath.Join(s.PlaybackCacheDir(videoID), "index.m3u8")
	data, err := os.ReadFile(index)
	if err != nil || len(data) == 0 {
		return nil
	}
	body := string(stripPlaybackEndlist(data))
	if strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		body = strings.Replace(body, "#EXT-X-PLAYLIST-TYPE:EVENT", "#EXT-X-PLAYLIST-TYPE:VOD", 1)
	} else if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:") {
		if strings.Contains(body, "#EXT-X-MEDIA-SEQUENCE:") {
			body = strings.Replace(body, "#EXT-X-MEDIA-SEQUENCE:0\n", "#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n", 1)
		} else if strings.HasPrefix(body, "#EXTM3U\n") {
			body = strings.Replace(body, "#EXTM3U\n", "#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n", 1)
		} else {
			body = "#EXT-X-PLAYLIST-TYPE:VOD\n" + body
		}
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "#EXT-X-ENDLIST\n"
	if err := os.WriteFile(index, []byte(body), 0o644); err != nil {
		return err
	}
	m.Complete = true
	m.LastAccess = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.writePlaybackMetaFile(videoID, m); err != nil {
		return err
	}
	if err := s.syncPlaybackCacheColumns(videoID, m); err != nil {
		return err
	}
	// Collapse handoff discontinuity + mid-GOP fragments into continuous VOD HLS.
	if err := s.rematerializeCompletePlayback(videoID); err != nil {
		slog.Warn("playback cache", "msg", "rematerialize complete failed; keeping coalesced VOD", "video_id", videoID, "err", err)
	}
	return nil
}

// RematerializeCompletePlayback rebuilds a progressive cache as continuous VOD HLS
// (concat all segs, re-cut on keyframes). Safe to call on an already-complete cache.
func (s *Store) RematerializeCompletePlayback(videoID int64) error {
	return s.rematerializeCompletePlayback(videoID)
}

// rematerializeCompletePlayback concatenates playlist segments into one MPEG-TS, then
// re-segments to VOD HLS with independent_segments so Emby remux does not hit handoff
// discontinuity or mid-GOP fragments.
func (s *Store) rematerializeCompletePlayback(videoID int64) error {
	dir := s.PlaybackCacheDir(videoID)
	index := filepath.Join(dir, "index.m3u8")
	entries := parsePlaybackMediaEntries(index)
	if len(entries) == 0 {
		return fmt.Errorf("playback cache empty")
	}
	for _, e := range entries {
		st, err := os.Stat(filepath.Join(dir, e.uri))
		if err != nil || st.Size() == 0 {
			return fmt.Errorf("missing segment %s", e.uri)
		}
	}

	work, err := os.MkdirTemp("", "creatorr-playback-remat-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	listPath := filepath.Join(work, "concat.txt")
	var list strings.Builder
	for _, e := range entries {
		abs, err := filepath.Abs(filepath.Join(dir, e.uri))
		if err != nil {
			return err
		}
		list.WriteString(ffmpegConcatFileLine(abs))
	}
	if err := os.WriteFile(listPath, []byte(list.String()), 0o644); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	continuous := filepath.Join(work, "continuous.ts")
	concatArgs := []string{"-hide_banner", "-nostdin", "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", continuous}
	concatCmd := exec.CommandContext(ctx, "ffmpeg", concatArgs...)
	exectrace.Record(ctx, "ffmpeg", concatArgs...)
	if out, err := concatCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("concat: %w: %s", err, truncateBytes(out, 400))
	}

	hlsDir := filepath.Join(work, "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return err
	}
	hlsIndex := filepath.Join(hlsDir, "index.m3u8")
	segPattern := filepath.Join(hlsDir, "seg%05d.ts")
	hlsArgs := []string{
		"-hide_banner", "-nostdin", "-y", "-i", continuous,
		"-c", "copy",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_list_size", "0",
		"-hls_segment_filename", segPattern,
		hlsIndex,
	}
	hlsCmd := exec.CommandContext(ctx, "ffmpeg", hlsArgs...)
	exectrace.Record(ctx, "ffmpeg", hlsArgs...)
	if out, err := hlsCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("re-hls: %w: %s", err, truncateBytes(out, 400))
	}

	newEntries := parsePlaybackMediaEntries(hlsIndex)
	if len(newEntries) == 0 {
		return fmt.Errorf("re-hls produced no segments")
	}
	var sum float64
	for _, e := range newEntries {
		sum += playbackExtinfSeconds(e.extinf)
	}
	if sum <= 0 {
		return fmt.Errorf("re-hls duration sum empty")
	}

	// Ensure VOD + ENDLIST on the new playlist (ffmpeg usually writes both).
	hlsBody, err := os.ReadFile(hlsIndex)
	if err != nil {
		return err
	}
	hlsText := string(stripPlaybackEndlist(hlsBody))
	if !strings.Contains(hlsText, "#EXT-X-PLAYLIST-TYPE:") {
		hlsText = strings.Replace(hlsText, "#EXTM3U\n", "#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n", 1)
	} else if strings.Contains(hlsText, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		hlsText = strings.Replace(hlsText, "#EXT-X-PLAYLIST-TYPE:EVENT", "#EXT-X-PLAYLIST-TYPE:VOD", 1)
	}
	if !strings.HasSuffix(hlsText, "\n") {
		hlsText += "\n"
	}
	hlsText += "#EXT-X-ENDLIST\n"
	if err := os.WriteFile(hlsIndex, []byte(hlsText), 0o644); err != nil {
		return err
	}

	// Swap: remove old media from dest, copy new HLS in.
	oldEntries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range oldEntries {
		name := e.Name()
		if name == playbackMetaFile {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, name))
	}
	newFiles, err := os.ReadDir(hlsDir)
	if err != nil {
		return err
	}
	for _, e := range newFiles {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(hlsDir, e.Name())
		dst := filepath.Join(dir, e.Name())
		if err := copyFilePath(src, dst); err != nil {
			return err
		}
	}

	// ffmpeg independent_segments still occasionally emits short non-IDR cuts; coalesce.
	if _, err := CoalesceDependentMPEGTSSegs(dir); err != nil {
		return fmt.Errorf("coalesce after rematerialize: %w", err)
	}

	sum = 0
	for _, e := range parsePlaybackMediaEntries(filepath.Join(dir, "index.m3u8")) {
		sum += playbackExtinfSeconds(e.extinf)
	}
	if sum <= 0 {
		return fmt.Errorf("playlist duration empty after coalesce")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	m := PlaybackMeta{
		CachedSeconds:        sum,
		HandoffSourceSeconds: sum,
		Complete:             true,
		WrittenAt:            now,
		LastAccess:           now,
		NextSeg:              countPlaybackSegs(dir),
		LiveSegsCopied:       0,
	}
	if prev, ok := s.LoadPlaybackMeta(videoID); ok && prev.WrittenAt != "" {
		m.WrittenAt = prev.WrittenAt
	}
	if err := s.writePlaybackMetaFile(videoID, m); err != nil {
		return err
	}
	return s.syncPlaybackCacheColumns(videoID, m)
}

func copyFilePath(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// ffmpegConcatFileLine formats one concat demuxer entry with single-quoted path.
func ffmpegConcatFileLine(abs string) string {
	esc := strings.ReplaceAll(filepath.ToSlash(abs), "'", `'\''`)
	return "file '" + esc + "'\n"
}

func playbackPlaylistHasEndlist(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
			return true
		}
	}
	return false
}

func playbackPlaylistHasMedia(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#EXTINF:") {
			return true
		}
	}
	return false
}

// ensurePlaybackHandoffDiscontinuity inserts #EXT-X-DISCONTINUITY between beginning-seeded
// segs and promoted live segs when missing (live mux PTS restart at handoff).
func (s *Store) ensurePlaybackHandoffDiscontinuity(videoID int64) error {
	beginEntries := parsePlaybackMediaEntries(filepath.Join(s.BeginningDir(videoID), "index.m3u8"))
	seedN := len(beginEntries)
	if seedN == 0 {
		return nil
	}
	index := filepath.Join(s.PlaybackCacheDir(videoID), "index.m3u8")
	data, err := os.ReadFile(index)
	if err != nil || len(data) == 0 {
		return nil
	}
	if strings.Contains(string(data), "#EXT-X-DISCONTINUITY") {
		return nil
	}
	playEntries := parsePlaybackMediaEntries(index)
	if len(playEntries) <= seedN {
		return nil
	}
	// Confirm boundary URI matches beginning's last seg name pattern (seed count).
	lines := strings.Split(string(data), "\n")
	var out []string
	mediaSeen := 0
	injected := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#EXTINF:") {
			if mediaSeen == seedN && !injected {
				out = append(out, "#EXT-X-DISCONTINUITY")
				injected = true
			}
			mediaSeen++
		}
		out = append(out, lines[i])
	}
	if !injected {
		return nil
	}
	body := strings.Join(out, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return os.WriteFile(index, []byte(body), 0o644)
}

func stripPlaybackEndlist(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

type playbackMediaEntry struct {
	extinf        string
	uri           string
	discontinuity bool
}

func parsePlaybackMediaEntries(path string) []playbackMediaEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []playbackMediaEntry
	pendingDisc := false
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "#EXT-X-DISCONTINUITY" {
			pendingDisc = true
			continue
		}
		if !strings.HasPrefix(trimmed, "#EXTINF:") {
			continue
		}
		extinf := strings.TrimPrefix(trimmed, "#EXTINF:")
		uri := ""
		for j := i + 1; j < len(lines); j++ {
			u := strings.TrimSpace(lines[j])
			if u == "" || strings.HasPrefix(u, "#") {
				continue
			}
			uri = filepath.Base(u)
			i = j
			break
		}
		if uri != "" && uri != "." && uri != ".." {
			out = append(out, playbackMediaEntry{extinf: extinf, uri: uri, discontinuity: pendingDisc})
			pendingDisc = false
		}
	}
	return out
}

// CoalesceDependentMPEGTSSegs appends MPEG-TS segments that are not independently
// decodable into the previous segment (never across #EXT-X-DISCONTINUITY). Emby remux
// opens each .ts cold; non-IDR fragments with no SPS/PPS hang mid-play.
// Rewrites index.m3u8 in place. Returns whether any merge happened.
func CoalesceDependentMPEGTSSegs(dir string) (bool, error) {
	index := filepath.Join(dir, "index.m3u8")
	entries := parsePlaybackMediaEntries(index)
	if len(entries) < 2 {
		return false, nil
	}
	changed := false
	i := 1
	for i < len(entries) {
		if entries[i].discontinuity {
			i++
			continue
		}
		path := filepath.Join(dir, entries[i].uri)
		if tsSegmentIndependentlyDecodable(path) {
			i++
			continue
		}
		prevPath := filepath.Join(dir, entries[i-1].uri)
		if err := appendFileBytes(prevPath, path); err != nil {
			return changed, err
		}
		sum := playbackExtinfSeconds(entries[i-1].extinf) + playbackExtinfSeconds(entries[i].extinf)
		entries[i-1].extinf = formatPlaybackExtinf(sum)
		_ = os.Remove(path)
		entries = append(entries[:i], entries[i+1:]...)
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, rewritePlaybackPlaylistEntries(index, entries)
}

func tsSegmentIndependentlyDecodable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-i", path, "-f", "null", "-")
	return cmd.Run() == nil
}

func appendFileBytes(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func formatPlaybackExtinf(sec float64) string {
	return strconv.FormatFloat(sec, 'f', 6, 64) + ","
}

func rewritePlaybackPlaylistEntries(index string, entries []playbackMediaEntry) error {
	data, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	hadEndlist := strings.Contains(string(data), "#EXT-X-ENDLIST")
	var header []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "#EXT-X-ENDLIST" || trimmed == "#EXT-X-DISCONTINUITY" ||
			strings.HasPrefix(trimmed, "#EXTINF:") {
			break
		}
		if trimmed == "" {
			continue
		}
		if trimmed == "#EXT-X-INDEPENDENT-SEGMENTS" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		header = append(header, line)
	}
	var b strings.Builder
	for _, h := range header {
		b.WriteString(h)
		b.WriteByte('\n')
	}
	for _, e := range entries {
		if e.discontinuity {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		b.WriteString("#EXTINF:")
		b.WriteString(e.extinf)
		if !strings.Contains(e.extinf, ",") {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
		b.WriteString(e.uri)
		b.WriteByte('\n')
	}
	if hadEndlist {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return os.WriteFile(index, []byte(b.String()), 0o644)
}

func playbackExtinfSeconds(extinf string) float64 {
	rest := strings.TrimSpace(extinf)
	if i := strings.IndexByte(rest, ','); i >= 0 {
		rest = rest[:i]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

// EnforcePlaybackCacheBudget deletes least-recently-accessed whole-video progressive
// caches until SUM(cached_seconds) <= max_hours*3600. protectVideoID is never evicted.
func (s *Store) EnforcePlaybackCacheBudget(protectVideoID int64) error {
	hours, err := settings.StreamPlaybackCacheMaxHours(s.DB)
	if err != nil || hours <= 0 {
		return err
	}
	budget := float64(hours) * 3600
	for {
		var total float64
		err := s.DB.SQL.QueryRow(`SELECT COALESCE(SUM(stream_playback_cached_seconds), 0) FROM videos`).Scan(&total)
		if err != nil {
			return err
		}
		if total <= budget+0.01 {
			return nil
		}
		var victim int64
		err = s.DB.SQL.QueryRow(`
			SELECT id FROM videos
			WHERE stream_playback_cached_seconds > 0
			  AND id != ?
			ORDER BY
			  CASE WHEN stream_playback_cache_last_access IS NULL OR stream_playback_cache_last_access = '' THEN 0 ELSE 1 END,
			  stream_playback_cache_last_access ASC,
			  stream_playback_cache_written_at ASC,
			  id ASC
			LIMIT 1
		`, protectVideoID).Scan(&victim)
		if err != nil {
			// Only over budget because of the protected video; stop.
			return nil
		}
		if err := s.ClearPlaybackCache(victim); err != nil {
			return err
		}
	}
}

// EffectiveStreamCacheSeconds returns max(beginning, progressive) for UI %.
func (s *Store) EffectiveStreamCacheSeconds(videoID int64, beginningFlag bool, progressiveSec float64, beginningSettingSec int) float64 {
	eff := progressiveSec
	if m, ok := s.LoadPlaybackMeta(videoID); ok && m.CachedSeconds > eff {
		eff = m.CachedSeconds
	}
	if beginningFlag {
		if m, ok := s.LoadBeginningMeta(videoID); ok && m.DurationSeconds > eff {
			eff = m.DurationSeconds
		} else if beginningSettingSec > 0 && float64(beginningSettingSec) > eff {
			eff = float64(beginningSettingSec)
		}
	}
	return eff
}

// StreamCachePercent returns 0–100 for UI; -1 when duration unknown (no %).
func StreamCachePercent(effective, duration float64, complete bool) int {
	if complete || (duration > 0 && effective+0.5 >= duration) {
		return 100
	}
	if duration <= 0 {
		return -1
	}
	p := int(effective/duration*100 + 0.5)
	if p > 100 {
		return 100
	}
	if p < 0 {
		return 0
	}
	return p
}
