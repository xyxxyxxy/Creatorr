package library

import (
	"fmt"
	"strconv"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// SourceDownloadErrorThreshold returns how many wanted_download_error videos on a
// source trigger holding other wanted videos as wanted_source_error.
func (s *Store) SourceDownloadErrorThreshold() int {
	raw, err := settings.Get(s.DB, settings.KeySourceDownloadErrorThreshold)
	if err != nil || raw == "" {
		return 2
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return settings.DefaultSourceDownloadErrorThreshold
	}
	return n
}

// CountDownloadErrors returns videos with status wanted_download_error for a source.
func (s *Store) CountDownloadErrors(sourceID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos WHERE source_id = ? AND status = 'wanted_download_error'
	`, sourceID).Scan(&n)
	return n, err
}

// SourceShouldHoldWanted reports whether new/wanted videos on this source
// should be held as wanted_source_error.
func (s *Store) SourceShouldHoldWanted(sourceID int64) (bool, error) {
	if sourceID <= 0 {
		return false, nil
	}
	n, err := s.CountDownloadErrors(sourceID)
	if err != nil {
		return false, err
	}
	return n >= s.SourceDownloadErrorThreshold(), nil
}

// SourceHasRetryableVideos is true when Retry would change any video status.
func (s *Store) SourceHasRetryableVideos(sourceID int64) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos
		WHERE source_id = ? AND status IN ('wanted_download_error', 'wanted_source_error')
	`, sourceID).Scan(&n)
	return n > 0, err
}

// MarkDownloadFailed sets video status to wanted_download_error, appends history, and
// may hold sibling wanted videos as wanted_source_error when the threshold is met.
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
	if cur.SourceID.Valid {
		return s.applySourceErrorHold(cur.SourceID.Int64, taskID)
	}
	return nil
}

// HoldSourceOnYtDlpError immediately holds wanted videos as wanted_source_error
// after a whole-source yt-dlp failure (scan/list/resolve). Does not require the
// download-error threshold. Idempotent when already held.
func (s *Store) HoldSourceOnYtDlpError(sourceID, taskID int64) error {
	if sourceID <= 0 {
		return nil
	}
	return s.forceSourceErrorHold(sourceID, taskID, "Held: source yt-dlp / site failure",
		map[string]any{"source_id": sourceID, "reason": "ytdlp"})
}

// applySourceErrorHold flips remaining wanted → wanted_source_error and cancels
// their pending downloads when the source has enough wanted_download_error videos.
// taskID is the download task that tipped the threshold (0 if unknown).
func (s *Store) applySourceErrorHold(sourceID, taskID int64) error {
	hold, err := s.SourceShouldHoldWanted(sourceID)
	if err != nil || !hold {
		return err
	}
	thresh := s.SourceDownloadErrorThreshold()
	return s.forceSourceErrorHold(sourceID, taskID,
		fmt.Sprintf("Held: source has %d+ download errors", thresh),
		map[string]any{"source_id": sourceID, "threshold": thresh})
}

func (s *Store) forceSourceErrorHold(sourceID, taskID int64, histMsg string, detail map[string]any) error {
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM videos WHERE source_id = ? AND status = 'wanted'
	`, sourceID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if len(ids) == 0 {
		return nil
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET status = 'wanted_source_error'
		WHERE source_id = ? AND status = 'wanted'
	`, sourceID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = s.AddVideoHistory(id, "source_failed", histMsg, detail, taskID)
		if s.Queue != nil {
			_, _ = s.Queue.CancelDownloadsForVideo(id, "Cancelled (source download errors)")
		}
	}
	return nil
}

// RetrySourceErrors sets wanted_download_error and wanted_source_error back to wanted
// for videos on this source. Does not enqueue downloads.
func (s *Store) RetrySourceErrors(sourceID int64) (int, error) {
	if _, err := s.GetSourceByID(sourceID); err != nil {
		return 0, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM videos
		WHERE source_id = ? AND status IN ('wanted_download_error', 'wanted_source_error')
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
		_, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, id)
		if err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
