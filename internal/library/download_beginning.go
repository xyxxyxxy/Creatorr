package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

const beginningMetaFile = "meta.json"

// BeginningMeta describes a persisted download-beginning cache for one video.
type BeginningMeta struct {
	DurationSeconds      float64 `json:"duration_seconds"`
	HandoffSourceSeconds float64 `json:"handoff_source_seconds,omitempty"` // source seek for live handoff; 0 = use DurationSeconds
	WrittenAt            string  `json:"written_at"`
}

// BeginningDir returns {CacheDir}/download-beginnings/{videoID}/.
func (s *Store) BeginningDir(videoID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("var", "cache")
	}
	return filepath.Join(root, "download-beginnings", strconv.FormatInt(videoID, 10))
}

// HasBeginning reports whether a usable beginning cache exists for the video.
func (s *Store) HasBeginning(videoID int64) bool {
	_, ok := s.LoadBeginningMeta(videoID)
	return ok
}

// LoadBeginningMeta reads meta.json when present and duration > 0 with an index playlist.
func (s *Store) LoadBeginningMeta(videoID int64) (BeginningMeta, bool) {
	dir := s.BeginningDir(videoID)
	data, err := os.ReadFile(filepath.Join(dir, beginningMetaFile))
	if err != nil {
		return BeginningMeta{}, false
	}
	var m BeginningMeta
	if err := json.Unmarshal(data, &m); err != nil || m.DurationSeconds <= 0 {
		return BeginningMeta{}, false
	}
	if st, err := os.Stat(filepath.Join(dir, "index.m3u8")); err != nil || st.Size() == 0 {
		return BeginningMeta{}, false
	}
	return m, true
}

// ClearBeginning removes the download-beginning cache for a video (best-effort).
func (s *Store) ClearBeginning(videoID int64) error {
	dir := s.BeginningDir(videoID)
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = s.SetStreamBeginningCached(videoID, false)
	return nil
}

// WriteBeginningMeta writes meta.json into an existing beginning directory.
func (s *Store) WriteBeginningMeta(videoID int64, durationSeconds float64) error {
	return s.WriteBeginningMetaHandoff(videoID, durationSeconds, 0)
}

// WriteBeginningMetaHandoff writes beginning duration and optional source handoff offset.
func (s *Store) WriteBeginningMetaHandoff(videoID int64, durationSeconds, handoffSource float64) error {
	if durationSeconds <= 0 {
		return fmt.Errorf("%w: beginning duration must be > 0", ErrInvalid)
	}
	dir := s.BeginningDir(videoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m := BeginningMeta{
		DurationSeconds:      durationSeconds,
		HandoffSourceSeconds: handoffSource,
		WrittenAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, beginningMetaFile), b, 0o644); err != nil {
		return err
	}
	_ = s.SetStreamBeginningCached(videoID, true)
	return nil
}

// EnqueueCacheBeginning queues a cache_beginning task after stream pack.
// No-op (returns 0, nil) when setting is 0. Soft-skips inactive domain / non-streamable.
func (s *Store) EnqueueCacheBeginning(videoID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	sec, err := settings.CacheBeginningSeconds(s.DB)
	if err != nil {
		return 0, err
	}
	if sec <= 0 {
		return 0, nil
	}
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	if cur.Status != "streamable" {
		return 0, fmt.Errorf("%w: video not streamable", ErrInvalid)
	}
	if !StreamNeedsBeginning(cur.StreamKind()) {
		// CDN HLS/progressive - beginning cache not useful.
		return 0, nil
	}
	ser, err := s.GetSeries(cur.SeriesID, false)
	if err != nil {
		return 0, err
	}
	if !ser.IsStream() {
		return 0, fmt.Errorf("%w: series not stream delivery", ErrInvalid)
	}
	domain := "unknown"
	if cur.SourceURL.Valid && strings.TrimSpace(cur.SourceURL.String) != "" {
		domain = queueDomain(cur.SourceURL.String)
	} else if cur.SourceID.Valid {
		var url string
		_ = s.DB.SQL.QueryRow(`SELECT url FROM sources WHERE id = ?`, cur.SourceID.Int64).Scan(&url)
		domain = queueDomain(url)
	}
	ok, err := domains.IsActive(s.DB, domain)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("%w: domain inactive", ErrInvalid)
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindCacheBeginning,
		Domain:   domain,
		SeriesID: cur.SeriesID,
		VideoID:  videoID,
		Message:  "",
		Payload:  map[string]any{"video_id": videoID},
	})
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, fmt.Errorf("%w: download beginning already queued", ErrConflict)
		}
		if errors.Is(err, queue.ErrQueueFull) {
			return 0, fmt.Errorf("%w: %v", ErrConflict, err)
		}
		return 0, err
	}
	return id, nil
}
