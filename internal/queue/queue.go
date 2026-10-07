package queue

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Task is a queued unit of work.
type Task struct {
	ID           int64
	Kind         string
	Status       string
	SeriesID     sql.NullInt64
	VideoID      sql.NullInt64
	Payload      string
	ErrorCode    string
	ErrorMessage string
	Message      string
	Detail       string   // structured outcome JSON for History
	Commands     []string // shell-formatted external argv lines (yt-dlp/ffmpeg/…)
	Logs         []string // progress lines (live buffer or persisted on failed)
	Progress       sql.NullFloat64
	InterruptCount int // restart requeues; 0 when never interrupted
	Domain         string
	QueueSeq       int64
	Origin         string
	ParentTaskID   sql.NullInt64
	CreatedAt      string
	StartedAt      sql.NullString
	FinishedAt     sql.NullString
	QueuePos       int // pending only: 1 = next to claim in domain; 0 when running
}

// Store wraps queue operations.
type Store struct {
	DB *db.DB

	mu       sync.Mutex
	cooldown map[string]time.Time // domain -> earliest next claim
	cancels  sync.Map             // taskID (int64) -> context.CancelFunc for running tasks

	// Logs holds in-memory progress lines for running tasks; flushed to tasks.logs on failed Finish.
	Logs *TaskLogs
	// Live holds latest message + progress fraction for running tasks (not persisted).
	Live *LiveState
	// Commands holds yt-dlp/ffmpeg argv lines while running (deduped); flushed to SQLite on Finish/Cancel when policy allows.
	Commands *TaskCommands

	// OnCancelled is invoked after a task is marked cancelled (pending or running).
	// Optional; library.NewStore wires source/video history recording.
	OnCancelled func(Task)
}

func NewStore(database *db.DB) *Store {
	return &Store{
		DB:       database,
		cooldown: make(map[string]time.Time),
		Logs:     newTaskLogs(),
		Live:     newLiveState(),
		Commands: newTaskCommands(),
	}
}

// DomainFromURL extracts hostname for queue lane (lowercased, www. stripped).
func DomainFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if strings.Contains(raw, ".") && !strings.Contains(raw, "/") {
			if d := settings.NormalizeDomain(raw); d != "" {
				return d
			}
		}
		return "unknown"
	}
	host := settings.NormalizeDomain(u.Hostname())
	if host == "" {
		return "unknown"
	}
	return host
}

// ListByParentTaskID returns tasks spawned by parentID, oldest first.
func (s *Store) ListByParentTaskID(parentID int64) ([]Task, error) {
	if parentID <= 0 {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id
		FROM tasks WHERE parent_task_id = ?
		ORDER BY created_at ASC, id ASC
	`, parentID)
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

// GetTask returns a task by id (any status), or nil if missing.
func (s *Store) GetTask(id int64) (*Task, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, kind, status, series_id, video_id, payload,
		       COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
		       COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
		       origin, parent_task_id,
		       COALESCE(NULLIF(commands, ''), '[]'),
		       COALESCE(NULLIF(logs, ''), '[]')
		FROM tasks WHERE id = ?
	`, id)
	t, err := s.scanTaskWithExtras(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// TaskStatus returns the current status string for a task id (empty if missing).
func (s *Store) TaskStatus(id int64) (string, error) {
	var st string
	err := s.DB.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&st)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return st, err
}

func (s *Store) scanTask(scanner interface {
	Scan(dest ...any) error
}) (*Task, error) {
	var t Task
	err := scanner.Scan(
		&t.ID, &t.Kind, &t.Status, &t.SeriesID, &t.VideoID, &t.Payload,
		&t.ErrorCode, &t.ErrorMessage, &t.Message, &t.Detail,
		&t.Progress, &t.Domain, &t.QueueSeq, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
		&t.Origin, &t.ParentTaskID,
	)
	if err != nil {
		return nil, err
	}
	s.applyLive(&t)
	return &t, nil
}

func (s *Store) scanTaskWithExtras(scanner interface {
	Scan(dest ...any) error
}) (*Task, error) {
	var t Task
	var commandsJSON, logsJSON string
	err := scanner.Scan(
		&t.ID, &t.Kind, &t.Status, &t.SeriesID, &t.VideoID, &t.Payload,
		&t.ErrorCode, &t.ErrorMessage, &t.Message, &t.Detail,
		&t.Progress, &t.Domain, &t.QueueSeq, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
		&t.Origin, &t.ParentTaskID,
		&commandsJSON, &logsJSON,
	)
	if err != nil {
		return nil, err
	}
	t.Commands = parseCommandsJSON(commandsJSON)
	t.Logs = parseCommandsJSON(logsJSON)
	s.applyLive(&t)
	s.applyCommands(&t)
	s.applyLogs(&t)
	return &t, nil
}

func parseCommandsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// applyLogs overlays in-memory progress lines onto a running/pending task row.
func (s *Store) applyLogs(t *Task) {
	if s == nil || t == nil || (t.Status != StatusPending && t.Status != StatusRunning) {
		return
	}
	if lines := s.Logs.Snapshot(t.ID); len(lines) > 0 {
		t.Logs = lines
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
