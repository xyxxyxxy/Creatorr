package library

import (
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// SourceHasRetryableVideos is true when Retry would change any video status.
func (s *Store) SourceHasRetryableVideos(sourceID int64) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos
		WHERE source_id = ? AND status IN ('wanted_download_error', 'wanted_archive')
	`, sourceID).Scan(&n)
	return n > 0, err
}

// MarkDownloadFailed sets video status to wanted_download_error and appends history.
func (s *Store) MarkDownloadFailed(videoID, taskID int64, code, message string) error {
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted_download_error' WHERE id = ?`, videoID)
	if err != nil {
		return err
	}
	stage := apperrors.DownloadFailStage(code)
	detail := map[string]any{
		"previous_status": cur.Status,
		"code":            code,
		"stage":           stage,
	}
	if message != "" {
		detail["error"] = message
	}
	histMsg := "Download failed"
	switch stage {
	case "remux":
		histMsg = "Remux failed"
	case "pack":
		histMsg = "Pack failed"
	}
	_ = s.AddVideoHistory(videoID, "download_failed", histMsg, detail, taskID)
	return nil
}

// RetrySourceErrors sets wanted_download_error / wanted_archive back to wanted for videos on this source.
// Cancels pending/running archive.org-lane downloads for those videos. Does not enqueue downloads.
func (s *Store) RetrySourceErrors(sourceID int64) (int, error) {
	if _, err := s.GetSourceByID(sourceID); err != nil {
		return 0, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM videos
		WHERE source_id = ? AND status IN ('wanted_download_error', 'wanted_archive')
	`, sourceID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	for _, id := range ids {
		_ = s.CancelArchiveDownloadsForVideo(id)
		_, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, id)
		if err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
