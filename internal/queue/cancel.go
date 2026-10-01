package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func (s *Store) notifyCancelled(tasks ...Task) {
	if s == nil || s.OnCancelled == nil {
		return
	}
	for _, t := range tasks {
		t.Status = StatusCancelled
		s.OnCancelled(t)
	}
}

// RegisterRunning ties a cancel func to a claimed task so Cancel can abort the worker.
func (s *Store) RegisterRunning(id int64, cancel context.CancelFunc) {
	if id <= 0 || cancel == nil {
		return
	}
	s.cancels.Store(id, cancel)
}

// UnregisterRunning drops the cancel hook after the worker finishes.
func (s *Store) UnregisterRunning(id int64) {
	s.cancels.Delete(id)
}

func (s *Store) abortRunning(id int64) {
	if v, ok := s.cancels.Load(id); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
}

// Cancel marks pending/running task cancelled with reason manual and aborts a running worker if registered.
func (s *Store) Cancel(id int64) error {
	_, err := s.CancelWithReason(id, CancelReasonManual)
	return err
}

// CancelWithReason marks a pending/running task cancelled with a closed-set reason code.
// Returns the status before cancel (pending or running) so callers can record Activity
// for pending tasks; running cancels are recorded by the worker when the handler returns.
func (s *Store) CancelWithReason(id int64, reason string) (prevStatus string, err error) {
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return "", err
	}
	err = s.DB.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&prevStatus)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("task %d not cancellable", id)
	}
	if err != nil {
		return "", err
	}
	if prevStatus != StatusPending && prevStatus != StatusRunning {
		return prevStatus, fmt.Errorf("task %d not cancellable", id)
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?, progress = NULL
		WHERE id = ? AND status IN (?, ?)
	`, StatusCancelled, finished, message, id, StatusPending, StatusRunning)
	if err != nil {
		return prevStatus, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return prevStatus, fmt.Errorf("task %d not cancellable", id)
	}
	_ = s.persistCommands(id, StatusCancelled, s.taskKind(id))
	_ = s.persistLogs(id, StatusCancelled)
	s.clearLive(id)
	s.abortRunning(id)
	if t, err := s.GetTask(id); err == nil && t != nil {
		s.notifyCancelled(*t)
	}
	return prevStatus, nil
}

// CancelDownloadsForVideo cancels pending and running download, sponsorblock_cut,
// and integrity_check_initial tasks for one video.
// Returns snapshots with pre-cancel Status (pending|running) and the cancel Message applied.
func (s *Store) CancelDownloadsForVideo(videoID int64, reason string) ([]Task, error) {
	if videoID <= 0 {
		return nil, fmt.Errorf("video_id required")
	}
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks
		WHERE kind IN (?, ?, ?) AND video_id = ? AND status IN (?, ?)
	`, KindDownload, KindSponsorblockCut, KindIntegrityCheckInitial, videoID, StatusPending, StatusRunning)
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
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range out {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?, progress = NULL
			WHERE id = ? AND status IN (?, ?)
		`, StatusCancelled, finished, message, t.ID, StatusPending, StatusRunning)
		if err != nil {
			return out, err
		}
		_ = s.persistCommands(t.ID, StatusCancelled, t.Kind)
		_ = s.persistLogs(t.ID, StatusCancelled)
		s.clearLive(t.ID)
		s.abortRunning(t.ID)
	}
	s.notifyCancelled(out...)
	return out, nil
}

// CancelAll cancels all pending tasks with reason manual and returns snapshots for Activity.
func (s *Store) CancelAll() ([]Task, error) {
	message, err := CancelReasonMessage(CancelReasonManual)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE status = ?
	`, StatusPending)
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
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?, progress = NULL
		WHERE status = ?
	`, StatusCancelled, finished, message, StatusPending)
	if err != nil {
		return out, err
	}
	s.notifyCancelled(out...)
	return out, nil
}

// CancelPendingDomain cancels pending tasks for one domain lane and returns snapshots for Activity.
func (s *Store) CancelPendingDomain(domain string) ([]Task, error) {
	return s.cancelDomain(domain, CancelReasonManual, StatusPending)
}

// CancelDomain cancels pending and running tasks for one domain lane (e.g. on deactivate).
// Returns snapshots with pre-cancel Status; callers should write Activity for pending rows
// (running cancels are recorded by the worker when the handler returns).
func (s *Store) CancelDomain(domain, reason string) ([]Task, error) {
	return s.cancelDomain(domain, reason, StatusPending, StatusRunning)
}

func (s *Store) cancelDomain(domain, reason string, statuses ...string) ([]Task, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, 0, 1+len(statuses))
	args = append(args, domain)
	ph := make([]byte, 0, len(statuses)*2)
	for i, st := range statuses {
		if i > 0 {
			ph = append(ph, ',')
		}
		ph = append(ph, '?')
		args = append(args, st)
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE domain = ? AND status IN (`+string(ph)+`)
	`, args...)
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
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range out {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?, progress = NULL
			WHERE id = ? AND status IN (?, ?)
		`, StatusCancelled, finished, message, t.ID, StatusPending, StatusRunning)
		if err != nil {
			return out, err
		}
		_ = s.persistCommands(t.ID, StatusCancelled, t.Kind)
		_ = s.persistLogs(t.ID, StatusCancelled)
		s.clearLive(t.ID)
		s.abortRunning(t.ID)
	}
	s.notifyCancelled(out...)
	return out, nil
}

// CancelPendingScansForSeries cancels all pending scan tasks for a series (full + tip).
// Running scans are left alone. Prefer CancelPendingTipScansForSeries on unmonitor.
func (s *Store) CancelPendingScansForSeries(seriesID int64) (int64, error) {
	if seriesID <= 0 {
		return 0, fmt.Errorf("series_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ? AND series_id = ?`,
		CancelReasonSeriesDeleted,
		KindScan, StatusPending, seriesID,
	)
}

// CancelPendingTipScansForSeries cancels pending tip Scan tasks only.
// Full scans keep running/queued when a series is unmonitored.
func (s *Store) CancelPendingTipScansForSeries(seriesID int64) (int64, error) {
	if seriesID <= 0 {
		return 0, fmt.Errorf("series_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ? AND series_id = ?
		  AND COALESCE(json_extract(payload, '$.mode'), '') = 'scan'`,
		CancelReasonSeriesUnmonitored,
		KindScan, StatusPending, seriesID,
	)
}

// CancelPendingScansForSource cancels pending scan tasks for one source (payload source_id).
// Running scans are left alone. reason must be a closed cancel-reason code.
func (s *Store) CancelPendingScansForSource(sourceID int64, reason string) (int64, error) {
	if sourceID <= 0 {
		return 0, fmt.Errorf("source_id required")
	}
	return s.cancelPendingScans(
		`kind = ? AND status = ?
		  AND CAST(json_extract(payload, '$.source_id') AS INTEGER) = ?`,
		reason,
		KindScan, StatusPending, sourceID,
	)
}

func (s *Store) cancelPendingScans(where, reason string, args ...any) (int64, error) {
	message, err := CancelReasonMessage(reason)
	if err != nil {
		return 0, err
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	var out []Task
	for rows.Next() {
		t, err := s.scanTask(rows)
		if err != nil {
			return 0, err
		}
		ids = append(ids, t.ID)
		t.Message = message
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		_, err := s.DB.SQL.Exec(`
			UPDATE tasks SET status = ?, finished_at = ?, message = ?, progress = NULL
			WHERE id = ? AND status = ?
		`, StatusCancelled, finished, message, id, StatusPending)
		if err != nil {
			return int64(len(out)), err
		}
	}
	s.notifyCancelled(out...)
	return int64(len(out)), nil
}
