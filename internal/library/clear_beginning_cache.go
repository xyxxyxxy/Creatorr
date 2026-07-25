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

// EnqueueClearBeginningCache queues a wipe of all download-beginning caches.
func (s *Store) EnqueueClearBeginningCache() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindClearBeginningCache,
		Domain:  queue.SystemDomain,
		Message: "Clear beginning of stream cache",
	})
}

// ClearBeginningCachePass removes {CacheDir}/download-beginnings, clears
// stream_beginning_cached, and cancels pending cache_beginning tasks.
func (s *Store) ClearBeginningCachePass(ctx context.Context, _ *queue.Task, progress func(msg string, pct *float64)) (cleared int, err error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	p20, p100 := 0.2, 1.0
	if progress != nil {
		progress("Clearing beginning cache…", &p20)
	}

	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("var", "cache")
	}
	dir := filepath.Join(root, "download-beginnings")
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}

	res, err := s.DB.SQL.Exec(`UPDATE videos SET stream_beginning_cached = 0 WHERE stream_beginning_cached != 0`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	cleared = int(n)

	_, _ = s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, message = ?, finished_at = datetime('now')
		WHERE kind = ? AND status = ?
	`, queue.StatusCancelled, "Beginning cache cleared", queue.KindCacheBeginning, queue.StatusPending)

	if progress != nil {
		progress(ClearBeginningCacheMessage(cleared), &p100)
	}
	return cleared, nil
}

// ClearBeginningCacheMessage formats the finish message.
func ClearBeginningCacheMessage(cleared int) string {
	return fmt.Sprintf("Cleared beginning cache (%d video flag(s))", cleared)
}

// RecordClearBeginningCacheActivity stores the outcome on the task.
func (s *Store) RecordClearBeginningCacheActivity(taskID int64, cleared int) {
	msg := ClearBeginningCacheMessage(cleared)
	if s.Queue != nil {
		p := 1.0
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
	detail, _ := json.Marshal(map[string]any{"cleared": cleared})
	if s.Queue != nil {
		_ = s.Queue.SetDetail(taskID, string(detail))
	}
}
