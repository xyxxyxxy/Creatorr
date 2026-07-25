package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueClearPlaybackCache queues a wipe of all progressive stream caches.
func (s *Store) EnqueueClearPlaybackCache() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindClearPlaybackCache,
		Domain:  queue.SystemDomain,
		Message: "Clear progressive stream cache",
	})
}

// ClearPlaybackCachePass removes {CacheDir}/playback-cache and zeros progressive columns.
func (s *Store) ClearPlaybackCachePass(ctx context.Context, _ *queue.Task, progress func(msg string, pct *float64)) (cleared int, err error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	p20, p100 := 0.2, 1.0
	if progress != nil {
		progress("Clearing progressive stream cache…", &p20)
	}

	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("var", "cache")
	}
	dir := filepath.Join(root, "playback-cache")
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}

	res, err := s.DB.SQL.Exec(`
		UPDATE videos SET
		  stream_playback_cached_seconds = 0,
		  stream_playback_cache_complete = 0,
		  stream_playback_cache_written_at = NULL,
		  stream_playback_cache_last_access = NULL
		WHERE stream_playback_cached_seconds != 0
		   OR stream_playback_cache_complete != 0
		   OR stream_playback_cache_written_at IS NOT NULL
		   OR stream_playback_cache_last_access IS NOT NULL
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	cleared = int(n)

	if progress != nil {
		progress(ClearPlaybackCacheMessage(cleared), &p100)
	}
	return cleared, nil
}

// ClearPlaybackCacheMessage formats the finish message.
func ClearPlaybackCacheMessage(cleared int) string {
	return fmt.Sprintf("Cleared progressive stream cache (%d video(s))", cleared)
}

// RecordClearPlaybackCacheActivity stores the outcome on the task.
func (s *Store) RecordClearPlaybackCacheActivity(taskID int64, cleared int) {
	msg := ClearPlaybackCacheMessage(cleared)
	if s.Queue != nil {
		p := 1.0
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
	detail, _ := json.Marshal(map[string]any{"cleared": cleared})
	if s.Queue != nil {
		_ = s.Queue.SetDetail(taskID, string(detail))
	}
}
