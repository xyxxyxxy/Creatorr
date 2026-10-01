package library

import (
	"errors"
	"fmt"
	"strings"

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
	if short := shortHistoryError(message); short != "" {
		histMsg = histMsg + ": " + short
	}
	_ = s.AddVideoHistory(videoID, "download_failed", histMsg, detail, taskID)
	return nil
}

func shortHistoryError(message string) string {
	s := strings.TrimSpace(message)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	const max = 120
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
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

// ClearVideoDownloadError sets wanted_download_error → wanted. Does not enqueue.
// Refuses other statuses (including wanted_archive).
func (s *Store) ClearVideoDownloadError(videoID int64) error {
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	if cur.Status != StatusWantedDownloadError {
		return fmt.Errorf("%w: clear download error only from wanted_download_error (got %s)", ErrInvalid, cur.Status)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusWanted, videoID)
	return err
}

// SeriesHasDownloadErrors is true when ClearSeriesDownloadErrors would change any video.
func (s *Store) SeriesHasDownloadErrors(seriesID int64) (bool, error) {
	n, err := s.CountSeriesDownloadErrors(seriesID)
	return n > 0, err
}

// CountSeriesDownloadErrors returns how many videos on the series are wanted_download_error.
func (s *Store) CountSeriesDownloadErrors(seriesID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos
		WHERE series_id = ? AND status = ?
	`, seriesID, StatusWantedDownloadError).Scan(&n)
	return n, err
}

// ClearSeriesDownloadErrors sets all wanted_download_error videos on the series to wanted.
// Includes import rows (null source_id). Leaves wanted_archive alone. Does not enqueue.
func (s *Store) ClearSeriesDownloadErrors(seriesID int64) (int, error) {
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return 0, err
	}
	res, err := s.DB.SQL.Exec(`
		UPDATE videos SET status = ?
		WHERE series_id = ? AND status = ?
	`, StatusWanted, seriesID, StatusWantedDownloadError)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ClearVideoDownloadErrorsBulk clears wanted_download_error → wanted for each id.
// Skips wrong status / missing. Does not enqueue.
func (s *Store) ClearVideoDownloadErrorsBulk(ids []int64) (updated, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if err := s.ClearVideoDownloadError(id); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
				skipped++
				continue
			}
			return updated, skipped, err
		}
		updated++
	}
	return updated, skipped, nil
}
