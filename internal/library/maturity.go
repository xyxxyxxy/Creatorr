package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// Maturity batch cap per download-wanted cron tick (shared across media + sidecar).
const maturityEnqueueBatchCap = 32

// EnqueueMaturityDue enqueues due media and sidecar maturity tasks.
// Delays come from each series' quality profile (0 = that pass off).
// Media maturity is preferred when both are due for the same video.
func (s *Store) EnqueueMaturityDue() (mediaN, sidecarN int, err error) {
	if s.Queue == nil {
		return 0, 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	remaining := maturityEnqueueBatchCap
	mediaN, err = s.enqueueMaturityMedia(remaining)
	if err != nil {
		return mediaN, 0, err
	}
	remaining -= mediaN
	if remaining > 0 {
		sidecarN, err = s.enqueueMaturitySidecars(remaining)
		if err != nil {
			return mediaN, sidecarN, err
		}
	}
	return mediaN, sidecarN, nil
}

func (s *Store) enqueueMaturityMedia(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, v.status, COALESCE(v.source_url,''), COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN quality_profiles qp ON qp.id = ser.quality_profile_id
		LEFT JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1
		  AND qp.maturity_redownload_hours > 0
		  AND v.status IN ('downloaded', 'streamable')
		  AND v.upload_date IS NOT NULL AND TRIM(v.upload_date) != ''
		  AND v.acquired_at IS NOT NULL AND TRIM(v.acquired_at) != ''
		  AND datetime('now') >= datetime(v.upload_date, '+' || qp.maturity_redownload_hours || ' hours')
		  AND datetime(v.acquired_at) < datetime(v.upload_date, '+' || qp.maturity_redownload_hours || ' hours')
		ORDER BY v.upload_date ASC, v.id ASC
		LIMIT ?
	`, limit*4) // over-fetch; skip busy / inactive below
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		if n >= limit {
			break
		}
		var id, seriesID int64
		var status, url, sourceURL string
		if err := rows.Scan(&id, &seriesID, &status, &url, &sourceURL); err != nil {
			return n, err
		}
		domain := queueDomain(url)
		if domain == "unknown" {
			domain = queueDomain(sourceURL)
		}
		ok, err := domains.IsActive(s.DB, domain)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		switch status {
		case "downloaded":
			busy, err := s.hasPendingDownload(id)
			if err != nil {
				return n, err
			}
			if busy {
				continue
			}
			_, err = s.Queue.Enqueue(queue.EnqueueParams{
				Kind:     queue.KindDownload,
				Domain:   domain,
				SeriesID: seriesID,
				VideoID:  id,
				Message:  "Maturity re-download",
				Payload:  map[string]any{"video_id": id, "maturity": true},
			})
			if err != nil {
				if errors.Is(err, queue.ErrQueueFull) {
					return n, nil
				}
				if errors.Is(err, queue.ErrDuplicate) {
					continue
				}
				return n, err
			}
			n++
		case "streamable":
			busy, err := s.hasPendingPackStream(id)
			if err != nil {
				return n, err
			}
			if busy {
				continue
			}
			_, err = s.Queue.Enqueue(queue.EnqueueParams{
				Kind:     queue.KindPackStream,
				Domain:   domain,
				SeriesID: seriesID,
				VideoID:  id,
				Message:  "Maturity stream re-pack",
				Payload:  map[string]any{"video_id": id, "maturity": true},
			})
			if err != nil {
				if errors.Is(err, queue.ErrDuplicate) {
					continue
				}
				return n, err
			}
			n++
		}
	}
	return n, rows.Err()
}

func (s *Store) enqueueMaturitySidecars(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, COALESCE(v.source_url,''), COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN quality_profiles qp ON qp.id = ser.quality_profile_id
		LEFT JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1
		  AND qp.maturity_sidecar_hours > 0
		  AND v.status IN ('downloaded', 'streamable')
		  AND v.upload_date IS NOT NULL AND TRIM(v.upload_date) != ''
		  AND v.acquired_at IS NOT NULL AND TRIM(v.acquired_at) != ''
		  AND datetime('now') >= datetime(v.upload_date, '+' || qp.maturity_sidecar_hours || ' hours')
		  AND datetime(v.acquired_at) < datetime(v.upload_date, '+' || qp.maturity_sidecar_hours || ' hours')
		  AND (v.sidecars_acquired_at IS NULL OR TRIM(v.sidecars_acquired_at) = ''
		       OR datetime(v.sidecars_acquired_at) < datetime(v.upload_date, '+' || qp.maturity_sidecar_hours || ' hours'))
		  AND NOT EXISTS (
		    SELECT 1 FROM tasks t
		    WHERE t.video_id = v.id AND t.status IN (?, ?)
		      AND t.kind IN (?, ?, ?, ?, ?)
		  )
		ORDER BY v.upload_date ASC, v.id ASC
		LIMIT ?
	`,
		queue.StatusPending, queue.StatusRunning,
		queue.KindDownload, queue.KindPackStream, queue.KindRefreshSidecars, queue.KindSponsorblockCut, queue.KindMediaVerify,
		limit*4)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		if n >= limit {
			break
		}
		var id, seriesID int64
		var url, sourceURL string
		if err := rows.Scan(&id, &seriesID, &url, &sourceURL); err != nil {
			return n, err
		}
		if _, ok, err := s.HasPackAnchor(id); err != nil {
			return n, err
		} else if !ok {
			continue
		}
		domain := queueDomain(url)
		if domain == "unknown" {
			domain = queueDomain(sourceURL)
		}
		ok, err := domains.IsActive(s.DB, domain)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		_, err = s.Queue.Enqueue(queue.EnqueueParams{
			Kind:     queue.KindRefreshSidecars,
			Domain:   domain,
			SeriesID: seriesID,
			VideoID:  id,
			Message:  "Maturity sidecar refresh",
			Payload:  map[string]any{"video_id": id, "maturity": true},
		})
		if err != nil {
			if errors.Is(err, queue.ErrDuplicate) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func (s *Store) hasPendingPackStream(videoID int64) (bool, error) {
	var id sql.NullInt64
	err := s.DB.SQL.QueryRow(`
		SELECT id FROM tasks
		WHERE kind = ? AND video_id = ? AND status IN (?, ?)
		LIMIT 1
	`, queue.KindPackStream, videoID, queue.StatusPending, queue.StatusRunning).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return id.Valid, nil
}

// MarkSidecarsAcquired sets videos.sidecars_acquired_at to now (maturity sidecar success).
func (s *Store) MarkSidecarsAcquired(videoID int64) error {
	_, err := s.DB.SQL.Exec(`UPDATE videos SET sidecars_acquired_at = ? WHERE id = ?`, nowRFC3339(), videoID)
	return err
}

// TaskPayloadMaturity reports whether a task payload requests maturity media replace.
func TaskPayloadMaturity(payload string) bool {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" {
		return false
	}
	return strings.Contains(payload, `"maturity":true`) || strings.Contains(payload, `"maturity": true`)
}
