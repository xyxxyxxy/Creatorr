package library

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// YtArchiveURL builds the yt-dlp web.archive:youtube URL for a remote id.
func YtArchiveURL(remoteID string) string {
	id := strings.TrimSpace(remoteID)
	if id == "" {
		return ""
	}
	return "ytarchive:" + id
}

// IsYouTubeSourceURL reports whether rawURL is a YouTube watch host (fallback trigger gate).
func IsYouTubeSourceURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com",
		"youtu.be", "www.youtu.be":
		return true
	default:
		return strings.HasSuffix(host, ".youtube.com")
	}
}

// TaskPayloadArchive reports whether a download task payload requests archive-lane fetch.
func TaskPayloadArchive(payload string) bool {
	p := strings.ToLower(strings.TrimSpace(payload))
	return strings.Contains(p, `"archive_fallback":true`) ||
		strings.Contains(p, `"archive_fallback": true`)
}

// IsArchiveDownloadTask reports archive.org lane download (domain and/or payload).
func IsArchiveDownloadTask(domain, payload string) bool {
	if strings.EqualFold(strings.TrimSpace(domain), ArchiveOrgDomain) {
		return true
	}
	return TaskPayloadArchive(payload)
}

// MarkWantedArchive sets status wanted_archive (live unavailable; archive retry pending).
func (s *Store) MarkWantedArchive(videoID, taskID int64, detail string) error {
	_, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusWantedArchive, videoID)
	if err != nil {
		return err
	}
	msg := "Live source unavailable; Web Archive retry queued"
	if strings.TrimSpace(detail) != "" {
		msg = detail
	}
	return s.AddVideoHistory(videoID, "archive_fallback_queued", msg, map[string]any{
		"detail": strings.TrimSpace(detail),
	}, taskID)
}

// EnqueueArchiveDownload queues a download on the archive.org lane for ytarchive:{remote_id}.
// Does not EnsureHost (no Domains override row). Honors active / soft-pause / max queue.
// Returns (taskID, true, nil) when enqueued; (0, false, nil) when skipped (busy/paused/full/inactive/setting).
func (s *Store) EnqueueArchiveDownload(videoID int64) (int64, bool, error) {
	if s.Queue == nil {
		return 0, false, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	on, err := settings.ArchiveFallbackEnabled(s.DB)
	if err != nil {
		return 0, false, err
	}
	if !on {
		return 0, false, nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, false, err
	}
	if strings.TrimSpace(v.RemoteID) == "" {
		return 0, false, fmt.Errorf("%w: remote_id required for archive download", ErrInvalid)
	}
	busy, err := s.hasPendingDownload(videoID)
	if err != nil {
		return 0, false, err
	}
	if busy {
		return 0, false, nil
	}
	domain := ArchiveOrgDomain
	ok, err := domains.IsActive(s.DB, domain)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	paused, err := domains.IsPaused(s.DB, domain)
	if err != nil {
		return 0, false, err
	}
	if paused {
		return 0, false, nil
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusWantedArchive, videoID)
	if err != nil {
		return 0, false, err
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindDownload,
		Domain:   domain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Message:  "Web Archive download",
		Payload:  map[string]any{"video_id": videoID, "archive_fallback": true},
	})
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) || errors.Is(err, queue.ErrQueueFull) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

// QueueArchiveFallbackAfterUnavailable marks wanted_archive and tries immediate archive enqueue.
// Returns true when fallback path was taken (status set); enqueued may still be false if lane full/paused.
func (s *Store) QueueArchiveFallbackAfterUnavailable(videoID, liveTaskID int64, detail string) (taken bool, err error) {
	on, err := settings.ArchiveFallbackEnabled(s.DB)
	if err != nil || !on {
		return false, err
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, err
	}
	src := ""
	if v.SourceURL.Valid {
		src = v.SourceURL.String
	}
	if !IsYouTubeSourceURL(src) {
		return false, nil
	}
	if err := s.MarkWantedArchive(videoID, liveTaskID, detail); err != nil {
		return false, err
	}
	_, _, err = s.EnqueueArchiveDownload(videoID)
	return true, err
}

// EnqueueWantedArchiveBackfill enqueues archive.org downloads for wanted_archive rows lacking a task.
func (s *Store) EnqueueWantedArchiveBackfill(limit int) (int, error) {
	if limit <= 0 || s.Queue == nil {
		return 0, nil
	}
	on, err := settings.ArchiveFallbackEnabled(s.DB)
	if err != nil || !on {
		return 0, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM videos WHERE status = ? ORDER BY id ASC LIMIT ?
	`, StatusWantedArchive, limit*4)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		if n >= limit {
			break
		}
		var id int64
		if err := rows.Scan(&id); err != nil {
			return n, err
		}
		_, ok, err := s.EnqueueArchiveDownload(id)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, rows.Err()
}

// CancelArchiveDownloadsForVideo cancels pending/running archive.org-lane downloads for videoID.
func (s *Store) CancelArchiveDownloadsForVideo(videoID int64) error {
	if s.Queue == nil {
		return nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM tasks
		WHERE kind = ? AND video_id = ? AND domain = ?
		  AND status IN (?, ?)
	`, queue.KindDownload, videoID, ArchiveOrgDomain, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tid int64
		if err := rows.Scan(&tid); err != nil {
			return err
		}
		_ = s.Queue.Cancel(tid)
	}
	return rows.Err()
}
