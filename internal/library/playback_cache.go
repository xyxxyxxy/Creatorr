package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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
	if len(toCopy) == 0 {
		return nil
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
		playlist.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-PLAYLIST-TYPE:EVENT\n")
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
		return nil
	}
	if err := os.WriteFile(existing, []byte(playlist.String()), 0o644); err != nil {
		return err
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
	body := []byte(playlist.String())
	if durationSec > 0 && m.CachedSeconds+0.5 >= durationSec {
		m.Complete = true
		body = append(body, []byte("#EXT-X-ENDLIST\n")...)
	}
	if err := os.WriteFile(existing, body, 0o644); err != nil {
		return err
	}
	if err := s.writePlaybackMetaFile(videoID, m); err != nil {
		return err
	}
	_ = s.syncPlaybackCacheColumns(videoID, m)
	return s.EnforcePlaybackCacheBudget(videoID)
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
	extinf string
	uri    string
}

func parsePlaybackMediaEntries(path string) []playbackMediaEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []playbackMediaEntry
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
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
			out = append(out, playbackMediaEntry{extinf: extinf, uri: uri})
		}
	}
	return out
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
