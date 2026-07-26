package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// RefreshListed updates an existing indexed video from a listing.
// Does not create videos; returns videoID=0 when remote_id is unknown.
func (s *Store) RefreshListed(seriesID int64, li ListedVideo, taskID int64) (videoID int64, updated bool, err error) {
	if li.RemoteID == "" {
		return 0, false, fmt.Errorf("%w: remote_id required", ErrInvalid)
	}
	upload := NormalizeUploadTime(li.UploadDate)

	var existingID int64
	var prevUpload sql.NullString
	err = s.DB.SQL.QueryRow(`
		SELECT id, upload_date FROM videos
		WHERE series_id = ? AND remote_id = ?
	`, seriesID, li.RemoteID).Scan(&existingID, &prevUpload)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	var uploadVal, src any
	if upload != "" {
		uploadVal = upload
	}
	if li.SourceID > 0 {
		src = li.SourceID
	}
	// Soft-fill title/description/thumbnail_url only when empty (never clobber first-seen / operator).
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = COALESCE(NULLIF(title, ''), ?),
		  source_url = COALESCE(NULLIF(?, ''), source_url),
		  description = COALESCE(NULLIF(description, ''), ?),
		  thumbnail_url = COALESCE(NULLIF(thumbnail_url, ''), ?),
		  upload_date = COALESCE(?, upload_date),
		  media_type = CASE WHEN ? != '' THEN ? ELSE media_type END,
		  source_id = COALESCE(source_id, ?)
		WHERE id = ?
	`, li.Title, li.WebpageURL, li.Description, li.ThumbnailURL, uploadVal,
		NormalizeMediaType(li.MediaType), NormalizeMediaType(li.MediaType),
		src, existingID)
	if err != nil {
		return 0, false, err
	}
	if li.DurationSeconds > 0 {
		_ = s.SetDurationSecondsIfEmpty(existingID, int(li.DurationSeconds+0.5))
	}

	prevDay := ""
	if prevUpload.Valid {
		prevDay = UploadCalendarDate(prevUpload.String)
	}
	newDay := UploadCalendarDate(upload)
	if newDay != "" {
		changed, rerr := s.ReindexSeriesUTCDay(seriesID, newDay)
		if rerr != nil {
			return 0, false, rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, taskID)
	}
	if prevDay != "" && prevDay != newDay {
		changed, rerr := s.ReindexSeriesUTCDay(seriesID, prevDay)
		if rerr != nil {
			return 0, false, rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, taskID)
	}

	return existingID, true, nil
}

// SoftFillVideoFromEntry soft-fills empty Creatorr-owned columns from a yt-dlp resolve entry.
// Never clobbers non-empty title/plot/thumb/source_url/upload_date (media_type updates when extract non-empty).
// When upload_date is filled for the first time, reindexes that UTC day and renames packed peers.
func (s *Store) SoftFillVideoFromEntry(videoID int64, e ytdlp.Entry, taskID int64) error {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	li := EntryFromYtDlp(e, 0)
	upload := NormalizeUploadTime(li.UploadDate)
	if upload == "" {
		upload = sidecarUploadTime(li.UploadDate)
	}

	var prevUpload sql.NullString
	if err := s.DB.SQL.QueryRow(`SELECT upload_date FROM videos WHERE id = ?`, videoID).Scan(&prevUpload); err != nil {
		return err
	}

	var uploadVal any
	if upload != "" {
		uploadVal = upload
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = COALESCE(NULLIF(title, ''), ?),
		  source_url = COALESCE(NULLIF(source_url, ''), NULLIF(?, '')),
		  description = COALESCE(NULLIF(description, ''), ?),
		  thumbnail_url = COALESCE(NULLIF(thumbnail_url, ''), ?),
		  upload_date = COALESCE(upload_date, ?),
		  media_type = CASE WHEN ? != '' THEN ? ELSE media_type END
		WHERE id = ?
	`, strings.TrimSpace(li.Title), strings.TrimSpace(li.WebpageURL), li.Description, strings.TrimSpace(li.ThumbnailURL),
		uploadVal, NormalizeMediaType(li.MediaType), NormalizeMediaType(li.MediaType), videoID)
	if err != nil {
		return err
	}
	if li.DurationSeconds > 0 {
		_ = s.SetDurationSecondsIfEmpty(videoID, int(li.DurationSeconds+0.5))
	}
	_, _ = s.SoftFillVideoGenresFromCategories(videoID, e.Categories)

	prevDay := ""
	if prevUpload.Valid {
		prevDay = UploadCalendarDate(prevUpload.String)
	}
	// Soft-fill never overwrites an existing date; only reindex when we actually filled one.
	if prevDay == "" && upload != "" {
		newDay := UploadCalendarDate(upload)
		if newDay != "" {
			changed, rerr := s.ReindexSeriesUTCDay(v.SeriesID, newDay)
			if rerr != nil {
				return rerr
			}
			_ = s.repackEpisodeNumberChanges(changed, taskID)
		}
	}
	return nil
}

// EnqueueMetadataRescanSeries queues a series-scoped metadata rescan (existing videos only).
func (s *Store) EnqueueMetadataRescanSeries(seriesID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return 0, err
	}
	sources, err := s.MonitoredSources(seriesID)
	if err != nil {
		return 0, err
	}
	if len(sources) == 0 {
		return 0, fmt.Errorf("%w: no sources", ErrInvalid)
	}
	if busy, err := s.hasPendingMetadataRescan(seriesID, 0); err != nil {
		return 0, err
	} else if busy {
		return 0, fmt.Errorf("%w: metadata rescan already queued", ErrConflict)
	}
	domain := queueDomain(sources[0].URL)
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRescanMetadata,
		Domain:   domain,
		SeriesID: seriesID,
		Message:  "Metadata rescan",
		Payload:  map[string]any{"series_id": seriesID},
	})
}

// EnqueueMetadataRescanVideo queues a single-video metadata refresh.
func (s *Store) EnqueueMetadataRescanVideo(videoID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	if busy, err := s.hasPendingMetadataRescan(v.SeriesID, videoID); err != nil {
		return 0, err
	} else if busy {
		return 0, fmt.Errorf("%w: metadata rescan already queued", ErrConflict)
	}
	domain := "unknown"
	if v.SourceURL.Valid {
		domain = queueDomain(v.SourceURL.String)
	} else if v.SourceID.Valid {
		var url string
		_ = s.DB.SQL.QueryRow(`SELECT url FROM sources WHERE id = ?`, v.SourceID.Int64).Scan(&url)
		domain = queueDomain(url)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRescanMetadata,
		Domain:   domain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Message:  "Metadata rescan",
		Payload:  map[string]any{"video_id": videoID, "series_id": v.SeriesID},
	})
}

func (s *Store) hasPendingMetadataRescan(seriesID, videoID int64) (bool, error) {
	var id sql.NullInt64
	var err error
	if videoID > 0 {
		err = s.DB.SQL.QueryRow(`
			SELECT id FROM tasks
			WHERE kind = ? AND video_id = ? AND status IN (?, ?)
			LIMIT 1
		`, queue.KindRescanMetadata, videoID, queue.StatusPending, queue.StatusRunning).Scan(&id)
	} else {
		err = s.DB.SQL.QueryRow(`
			SELECT id FROM tasks
			WHERE kind = ? AND series_id = ? AND video_id IS NULL AND status IN (?, ?)
			LIMIT 1
		`, queue.KindRescanMetadata, seriesID, queue.StatusPending, queue.StatusRunning).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return id.Valid, nil
}

// EnqueueRefreshSidecarsVideo queues NFO/thumb/subs refresh for one packed video (never media or info.json).
// Operator action - does not set sidecars_acquired_at (maturity cron uses payload maturity: true).
func (s *Store) EnqueueRefreshSidecarsVideo(videoID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	if _, ok, err := s.HasPackAnchor(videoID); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("%w: no packed video or .strm on disk", ErrInvalid)
	}
	domain := "unknown"
	if v.SourceURL.Valid {
		domain = queueDomain(v.SourceURL.String)
	} else if v.SourceID.Valid {
		var url string
		_ = s.DB.SQL.QueryRow(`SELECT url FROM sources WHERE id = ?`, v.SourceID.Int64).Scan(&url)
		domain = queueDomain(url)
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRefreshSidecars,
		Domain:   domain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Message:  "Refresh sidecars",
		Payload:  map[string]any{"video_id": videoID, "series_id": v.SeriesID},
	})
	if errors.Is(err, queue.ErrDuplicate) {
		return 0, fmt.Errorf("%w: sidecar refresh already queued", ErrConflict)
	}
	return id, err
}
