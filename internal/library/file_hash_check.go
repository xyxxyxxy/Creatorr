package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// RunSingleFileIntegrityCheck runs hash fill/compare (or NFO match) for one files row.
// For kind=video with no hash, null-decodes first. On success/failure updates stamps.
// Returns mismatch detail when failed (and stamps attempted).
func (s *Store) RunSingleFileIntegrityCheck(ctx context.Context, videoID, fileID int64, progress func(msg string, pct *float64)) (ok bool, detail string, err error) {
	f, err := s.GetVideoFile(videoID, fileID)
	if err != nil {
		return false, "", err
	}
	if _, serr := os.Stat(f.Path); serr != nil {
		return false, "", fmt.Errorf("%w: file missing on disk", ErrInvalid)
	}
	if f.Kind == "nfo" {
		match, _, nerr := s.nfoDiskMatchesVideo(videoID)
		if nerr != nil {
			_ = s.MarkFileHashAttempted(f.ID)
			return false, integrityFailDetail(nerr), nerr
		}
		if !match {
			_ = s.MarkFileHashAttempted(f.ID)
			_ = s.AddVideoHistory(videoID, "sidecar_externally_changed", "NFO does not match expected metadata", map[string]any{
				"reason":  "file_hash_check",
				"kind":    "nfo",
				"path":    f.Path,
				"file_id": f.ID,
			}, 0)
			return false, "NFO does not match expected metadata", nil
		}
		_ = s.MarkFileHashOK(f.ID)
		return true, "", nil
	}
	if f.Kind == "video" {
		hasHash := f.ContentHash.Valid && strings.TrimSpace(f.ContentHash.String) != ""
		if !hasHash {
			if err := VerifyDownloadedMedia(ctx, f.Path, progress); err != nil {
				_ = s.MarkFileHashAttempted(f.ID)
				return false, integrityFailDetail(err), err
			}
		}
	}
	if progress != nil {
		progress("Checking file hash…", nil)
	}
	result, mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path)
	if herr != nil {
		return false, integrityFailDetail(herr), herr
	}
	if result == IntegrityResultFailed {
		stored, _, _ := s.FileContentHash(f.ID)
		disk, _ := integrity.SHA256File(f.Path)
		ev := "file_externally_changed"
		msg := "Media integrity check failed"
		if f.Kind != "video" {
			ev = "sidecar_externally_changed"
			msg = "Sidecar integrity check failed"
		}
		_ = s.AddVideoHistory(videoID, ev, msg, map[string]any{
			"reason":   "file_hash_check",
			"kind":     f.Kind,
			"path":     f.Path,
			"file_id":  f.ID,
			"detail":   mismatch,
			"old_hash": stored,
			"new_hash": disk,
		}, 0)
		return false, mismatch, nil
	}
	return true, "", nil
}

// ApplyFileIntegrityVideoStatus sets downloaded_integrity_failed when any file is
// derived-failed; otherwise restores downloaded. recovered is true when status
// changed from downloaded_integrity_failed to downloaded.
func (s *Store) ApplyFileIntegrityVideoStatus(videoID, taskID int64) (recovered bool, err error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, err
	}
	failed, err := s.VideoHasFailedIntegrityFile(videoID)
	if err != nil {
		return false, err
	}
	wasFailed := v.Status == "downloaded_integrity_failed"
	if failed {
		if !wasFailed {
			if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded_integrity_failed' WHERE id = ?`, videoID); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if wasFailed {
		if err := s.MarkVerified(videoID, taskID, nil); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// EnqueueFileHashCheck queues system-lane file_hash_check for one files row.
func (s *Store) EnqueueFileHashCheck(videoID, fileID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	busy, taskID, err := s.FileHashCheckBusy(videoID, fileID)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: integrity check already queued (task %d)", ErrConflict, taskID)
	}
	f, err := s.GetVideoFile(videoID, fileID)
	if err != nil {
		return 0, err
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	if _, serr := os.Stat(f.Path); serr != nil {
		return 0, fmt.Errorf("%w: file missing on disk", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:   queue.OriginManual,
		Kind:     queue.KindFileHashCheck,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Message:  "Check file hash",
		Payload: map[string]any{
			"video_id": videoID,
			"file_id":  fileID,
			"path":     f.Path,
			"kind":     f.Kind,
		},
	})
}

// FileHashCheckBusy reports an open file_hash_check for fileID or covering video integrity task.
func (s *Store) FileHashCheckBusy(videoID, fileID int64) (busy bool, taskID int64, err error) {
	if s.Queue == nil {
		return false, 0, nil
	}
	var id int64
	err = s.DB.SQL.QueryRow(`
		SELECT id FROM tasks
		WHERE kind = ? AND status IN (?, ?)
		  AND CAST(COALESCE(json_extract(payload, '$.file_id'), 0) AS INTEGER) = ?
		ORDER BY id DESC LIMIT 1
	`, queue.KindFileHashCheck, queue.StatusPending, queue.StatusRunning, fileID).Scan(&id)
	if err == nil {
		return true, id, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, 0, err
	}
	t, err := s.Queue.IntegrityTaskLinkForVideo(videoID)
	if err != nil {
		return false, 0, err
	}
	if t != nil {
		return true, t.ID, nil
	}
	return false, 0, nil
}
