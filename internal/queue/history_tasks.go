package queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Finish marks a task done, failed, or cancelled.
// Domain cooldown is started on ClaimNext only (not here); interactive finishes never cool down.
func (s *Store) Finish(id int64, status, message, errCode, errMsg string) error {
	if status != StatusDone && status != StatusFailed && status != StatusCancelled {
		return fmt.Errorf("invalid finish status %q", status)
	}

	if err := s.persistCommands(id, status, s.taskKind(id)); err != nil {
		return err
	}
	if err := s.persistLogs(id, status); err != nil {
		return err
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, finished_at = ?, message = ?, error_code = NULLIF(?, ''), error_message = NULLIF(?, '')
		WHERE id = ? AND status = ?
	`, status, finished, message, errCode, errMsg, id, StatusRunning)
	if err != nil {
		return err
	}
	s.clearLive(id)
	return nil
}

// UpdateProgress sets in-memory message/progress for a running task (not written to SQLite).
// When progress is nil, live progress is cleared (message-only / spinner UI).
// Final message is persisted on Finish/Cancel. Restart requeues running tasks anyway.
func (s *Store) UpdateProgress(id int64, message string, progress *float64) error {
	if s == nil || id <= 0 {
		return nil
	}
	s.Live.Set(id, message, progress)
	return nil
}

// UpdatePayload replaces the JSON payload on a running or pending task (cursor resume).
func (s *Store) UpdatePayload(id int64, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET payload = ? WHERE id = ? AND status IN (?, ?)`, string(b), id, StatusPending, StatusRunning)
	return err
}

// SetDetail stores structured outcome JSON on a task (History detail).
func (s *Store) SetDetail(id int64, detail string) error {
	_, err := s.DB.SQL.Exec(`UPDATE tasks SET detail = NULLIF(?, '') WHERE id = ?`, detail, id)
	return err
}

// MergeDetailJSON merges patch keys into tasks.detail (JSON object).
// Non-JSON existing detail is preserved under key "error".
func (s *Store) MergeDetailJSON(id int64, patch map[string]any) error {
	if s == nil || id <= 0 || len(patch) == 0 {
		return nil
	}
	var cur string
	err := s.DB.SQL.QueryRow(`SELECT COALESCE(detail, '') FROM tasks WHERE id = ?`, id).Scan(&cur)
	if err != nil {
		return err
	}
	m := map[string]any{}
	if strings.TrimSpace(cur) != "" {
		if json.Unmarshal([]byte(cur), &m) != nil || m == nil {
			m = map[string]any{"error": cur}
		}
	}
	for k, v := range patch {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.SetDetail(id, string(b))
}

// AppendCommand appends a shell-formatted external command line in memory.
// bin selects policy (yt-dlp / ffmpeg / ffprobe). Flushed to tasks.commands on Finish/Cancel when allowed.
func (s *Store) AppendCommand(id int64, bin, line string) error {
	if s == nil || id <= 0 {
		return nil
	}
	s.Commands.Append(id, bin, line)
	return nil
}

// persistCommands writes buffered command lines to SQLite once (History), or clears them
// when PersistCommandsOnStatus says not to keep this kind/status.
func (s *Store) persistCommands(id int64, status, kind string) error {
	if s == nil || id <= 0 {
		return nil
	}
	if !PersistCommandsOnStatus(kind, status) {
		s.Commands.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET commands = '[]' WHERE id = ?`, id)
		return err
	}
	lines := s.Commands.Snapshot(id)
	if len(lines) == 0 {
		s.Commands.Clear(id)
		return nil
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET commands = ? WHERE id = ?`, string(b), id)
	if err != nil {
		return err
	}
	s.Commands.Clear(id)
	return nil
}

// PersistLogsOnStatus reports whether progress log lines should be written to SQLite.
// Only failed tasks keep the ring (debugging yt-dlp / PO / remux); done and cancelled drop it.
func PersistLogsOnStatus(status string) bool {
	return status == StatusFailed
}

// persistLogs writes buffered progress lines to tasks.logs on failure, else clears the column.
func (s *Store) persistLogs(id int64, status string) error {
	if s == nil || id <= 0 {
		return nil
	}
	if !PersistLogsOnStatus(status) {
		s.Logs.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET logs = '[]' WHERE id = ?`, id)
		return err
	}
	lines := s.Logs.Snapshot(id)
	if len(lines) == 0 {
		s.Logs.Clear(id)
		_, err := s.DB.SQL.Exec(`UPDATE tasks SET logs = '[]' WHERE id = ?`, id)
		return err
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET logs = ? WHERE id = ?`, string(b), id)
	if err != nil {
		return err
	}
	s.Logs.Clear(id)
	return nil
}

func (s *Store) taskKind(id int64) string {
	var kind string
	_ = s.DB.SQL.QueryRow(`SELECT kind FROM tasks WHERE id = ?`, id).Scan(&kind)
	return kind
}

// RequeueStaleRunning marks interrupted running tasks as pending after process restart.
func (s *Store) RequeueStaleRunning() (int64, error) {
	res, err := s.DB.SQL.Exec(`
		UPDATE tasks SET status = ?, started_at = NULL, message = 'Requeued after restart', progress = NULL
		WHERE status = ?
	`, StatusPending, StatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
