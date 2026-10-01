package queue

import (
	"database/sql"
)

// ListActive returns running + pending tasks with queue positions per domain.
func (s *Store) ListActive() ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE status IN (?, ?)
		ORDER BY domain ASC, CASE status WHEN ? THEN 0 ELSE 1 END, queue_seq ASC, id ASC
	`, StatusRunning, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	pos := map[string]int{}
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		if t.Status == StatusPending {
			pos[t.Domain]++
			t.QueuePos = pos[t.Domain]
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveFileDelete returns pending/running delete_files tasks (system lane).
func (s *Store) ListActiveFileDelete() ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind = ? AND status IN (?, ?)
		ORDER BY id ASC
	`, KindDeleteFiles, StatusRunning, StatusPending)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveForSeries returns pending/running tasks tied to a series.
func (s *Store) ListActiveForSeries(seriesID int64) ([]Task, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE series_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id ASC
	`, seriesID, StatusRunning, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ActiveTaskForVideo returns the best pending/running task for a video (running preferred).
// Also matches system-lane delete_files payloads that list video_id or its series.
func (s *Store) ActiveTaskForVideo(videoID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE video_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, videoID, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil && t != nil {
		return t, nil
	}
	return s.activeFileDeleteTaskForVideo(videoID)
}

// ActiveIntegrityTaskForVideo returns pending/running integrity_check(_initial) for a video.
func (s *Store) ActiveIntegrityTaskForVideo(videoID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE video_id = ? AND kind IN (?, ?) AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, videoID, KindIntegrityCheck, KindIntegrityCheckInitial, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// ActiveLibraryIntegrityCheckCovering returns pending/running bulk integrity_check that
// includes videoID (whole library, or series_ids / video_ids scope). Nil when none or out of scope.
func (s *Store) ActiveLibraryIntegrityCheckCovering(videoID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind = ? AND domain = ? AND status IN (?, ?)
		  AND (video_id IS NULL OR video_id = 0)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, KindIntegrityCheck, SystemDomain, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil || t == nil {
		return t, err
	}
	seriesIDs, videoIDs := FileDeleteIDsFromPayload(t.Payload)
	if len(seriesIDs) == 0 && len(videoIDs) == 0 {
		return t, nil
	}
	for _, id := range videoIDs {
		if id == videoID {
			return t, nil
		}
	}
	if len(seriesIDs) == 0 {
		return nil, nil
	}
	var seriesID int64
	if err := s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, videoID).Scan(&seriesID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	for _, id := range seriesIDs {
		if id == seriesID {
			return t, nil
		}
	}
	return nil, nil
}

// IntegrityTaskLinkForVideo prefers a video-scoped integrity task; else a covering library integrity_check.
func (s *Store) IntegrityTaskLinkForVideo(videoID int64) (*Task, error) {
	t, err := s.ActiveIntegrityTaskForVideo(videoID)
	if err != nil || t != nil {
		return t, err
	}
	return s.ActiveLibraryIntegrityCheckCovering(videoID)
}

// ActiveNonIntegrityTaskForVideo returns the best pending/running non-integrity task for a video
// (download, SponsorBlock cut, sidecars, …). Falls back to delete_files covering this video.
func (s *Store) ActiveNonIntegrityTaskForVideo(videoID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE video_id = ? AND kind NOT IN (?, ?) AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, videoID, KindIntegrityCheck, KindIntegrityCheckInitial, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil && t != nil {
		return t, nil
	}
	return s.activeFileDeleteTaskForVideo(videoID)
}

func (s *Store) activeFileDeleteTaskForVideo(videoID int64) (*Task, error) {
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, videoID).Scan(&seriesID)
	tasks, err := s.ListActiveFileDelete()
	if err != nil {
		return nil, err
	}
	var best *Task
	for i := range tasks {
		t := &tasks[i]
		sids, vids := FileDeleteIDsFromPayload(t.Payload)
		hit := false
		for _, vid := range vids {
			if vid == videoID {
				hit = true
				break
			}
		}
		if !hit && seriesID > 0 {
			for _, sid := range sids {
				if sid == seriesID {
					hit = true
					break
				}
			}
		}
		if !hit {
			continue
		}
		if t.Status == StatusRunning {
			return t, nil
		}
		if best == nil {
			best = t
		}
	}
	return best, nil
}

// ActiveScanForSeries returns the running or pending scan task for a series, if any.
func (s *Store) ActiveScanForSeries(seriesID int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind = ? AND series_id = ? AND status IN (?, ?)
		ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, KindScan, seriesID, StatusRunning, StatusPending, StatusRunning)
	t, err := s.scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
