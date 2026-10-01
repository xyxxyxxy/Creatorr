package library

import (
	"fmt"
)

// MarkDeleted clears file rows, sets status deleted, writes history.
// Used for intentional removals (retention, user delete) - not for transient missing paths.
func (s *Store) MarkDeleted(videoID int64, reason string, taskID int64) error {
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ?`, videoID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE videos SET status = 'deleted' WHERE id = ?
	`, videoID); err != nil {
		return err
	}
	detail := fmt.Sprintf(`{"reason":%q}`, reason)
	if taskID <= 0 {
		return fmt.Errorf("%w: task_id required for file_deleted history", ErrInvalid)
	}
	if _, err := tx.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'file_deleted', ?, ?, ?)
	`, videoID, nowRFC3339(), "Files removed ("+reason+")", detail, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkMissing sets status missing but keeps file rows (path preserved for restore).
// taskID links history to the sync_files task when > 0.
func (s *Store) MarkMissing(videoID, taskID int64) error {
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'missing' WHERE id = ?`, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "file_missing", "Media file missing on disk", map[string]any{
		"reason": "sync_files",
	}, taskID)
}

// RestoreDownloaded sets status downloaded when a previously missing file is back.
// taskID links history to the sync_files task when > 0.
func (s *Store) RestoreDownloaded(videoID, taskID int64) error {
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "file_restored", "Media file found again", map[string]any{
		"reason": "sync_files",
	}, taskID)
}

// MarkExternallyChanged sets status integrity_check_failed when packed media size no longer
// matches files.size_bytes. Updates size_bytes to the on-disk size (idempotent for
// later syncs). Does not enqueue download or media_verify.
func (s *Store) MarkExternallyChanged(videoID, taskID, oldSize, newSize int64) error {
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'integrity_check_failed' WHERE id = ?`, videoID); err != nil {
		return err
	}
	if _, err := s.DB.SQL.Exec(`
		UPDATE files SET size_bytes = ? WHERE video_id = ? AND kind = 'video'
	`, newSize, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "file_externally_changed", "Media file size changed on disk", map[string]any{
		"reason":   "sync_files",
		"old_size": oldSize,
		"new_size": newSize,
	}, taskID)
}
