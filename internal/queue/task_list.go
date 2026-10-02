package queue

import (
	"fmt"
	"strings"
)

// Task list sort keys for the unified Tasks Explorer.
const (
	TaskSortCreated    = "created"
	TaskSortDuration   = "duration"
	TaskSortInterrupted = "interrupted"
	TaskSortProgress   = "progress"
	TaskSortQueue      = "queue"
	TaskSortKind       = "kind"
	TaskSortDomain     = "domain"
	TaskSortStatus     = "status"
	TaskSortParent     = "parent"
)

// AllTaskStatuses is every tasks.status value the Explorer Status filter can select.
var AllTaskStatuses = []string{StatusPending, StatusRunning, StatusDone, StatusFailed, StatusCancelled}

const taskListSelectCols = `id, kind, status, series_id, video_id, payload,
	COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
	COALESCE(detail,''), progress, domain, queue_seq, created_at, started_at, finished_at,
	origin, parent_task_id, COALESCE(interrupt_count, 0)`

// TaskListFilter selects open and/or finished tasks for the unified Tasks Explorer.
// Empty Statuses = all AllTaskStatuses. From/To are inclusive UTC bounds on created_at.
type TaskListFilter struct {
	Statuses []string
	Domain   string
	Kind     string
	Origin   string
	From     string
	To       string
	Sort     string
	SortDir  string // asc|desc; empty uses per-sort default
}

// ListTasks returns tasks matching the filter (paginated).
func (s *Store) ListTasks(f TaskListFilter, limit, offset int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where, args := taskListFilterSQL(f)
	order := taskListOrderSQL(f)
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(`
		SELECT `+taskListSelectCols+`
		FROM tasks
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out, err := s.scanTaskListRows(rows)
	if err != nil {
		return nil, err
	}
	if err := s.assignQueuePositions(out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountTasks returns how many tasks match the filter.
func (s *Store) CountTasks(f TaskListFilter) (int, error) {
	where, args := taskListFilterSQL(f)
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE `+where, args...).Scan(&n)
	return n, err
}

// DistinctTaskDomains returns distinct non-empty domains among all tasks.
func (s *Store) DistinctTaskDomains() ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT domain FROM tasks
		WHERE domain != ''
		ORDER BY CASE WHEN domain = ? THEN 0 ELSE 1 END, domain ASC
	`, SystemDomain)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DistinctTaskKinds returns distinct kinds among all tasks (sorted).
func (s *Store) DistinctTaskKinds() ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT kind FROM tasks
		ORDER BY kind ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) scanTaskListRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]Task, error) {
	var out []Task
	for rows.Next() {
		var t Task
		err := rows.Scan(
			&t.ID, &t.Kind, &t.Status, &t.SeriesID, &t.VideoID, &t.Payload,
			&t.ErrorCode, &t.ErrorMessage, &t.Message, &t.Detail,
			&t.Progress, &t.Domain, &t.QueueSeq, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
			&t.Origin, &t.ParentTaskID, &t.InterruptCount,
		)
		if err != nil {
			return nil, err
		}
		s.applyLive(&t)
		out = append(out, t)
	}
	return out, rows.Err()
}

// assignQueuePositions paints per-domain pending #n onto pending rows in the page.
func (s *Store) assignQueuePositions(tasks []Task) error {
	domains := map[string]struct{}{}
	for _, t := range tasks {
		if t.Status == StatusPending && t.Domain != "" {
			domains[t.Domain] = struct{}{}
		}
	}
	if len(domains) == 0 {
		return nil
	}
	posByID := map[int64]int{}
	for d := range domains {
		rows, err := s.DB.SQL.Query(`
			SELECT id FROM tasks
			WHERE domain = ? AND status = ?
			ORDER BY queue_seq ASC, id ASC
		`, d, StatusPending)
		if err != nil {
			return err
		}
		pos := 0
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			pos++
			posByID[id] = pos
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
	}
	for i := range tasks {
		if p, ok := posByID[tasks[i].ID]; ok {
			tasks[i].QueuePos = p
		}
	}
	return nil
}

func taskListFilterSQL(f TaskListFilter) (string, []any) {
	statuses := normalizeTaskListStatuses(f.Statuses)
	if len(statuses) == 0 {
		statuses = append([]string{}, AllTaskStatuses...)
	}
	parts := []string{"status IN (" + sqlPlaceholders(len(statuses)) + ")"}
	args := make([]any, 0, len(statuses)+6)
	for _, st := range statuses {
		args = append(args, st)
	}
	if d := strings.TrimSpace(f.Domain); d != "" {
		parts = append(parts, "domain = ?")
		args = append(args, d)
	}
	if k := strings.TrimSpace(f.Kind); k != "" {
		parts = append(parts, "kind = ?")
		args = append(args, k)
	}
	if o := strings.TrimSpace(f.Origin); o != "" && ValidOrigin(o) {
		parts = append(parts, "origin = ?")
		args = append(args, o)
	}
	if from := strings.TrimSpace(f.From); from != "" {
		parts = append(parts, "datetime(created_at) >= datetime(?)")
		args = append(args, from)
	}
	if to := strings.TrimSpace(f.To); to != "" {
		parts = append(parts, "datetime(created_at) <= datetime(?)")
		args = append(args, to)
	}
	return strings.Join(parts, " AND "), args
}

func taskListOrderSQL(f TaskListFilter) string {
	sort := NormalizeTaskSort(f.Sort)
	dir := normalizeTaskSortDir(sort, f.SortDir)
	asc := strings.EqualFold(dir, "asc")
	dirSQL := "DESC"
	if asc {
		dirSQL = "ASC"
	}
	nullsLast := func(expr string) string {
		// SQLite: NULLs sort first in ASC; push them last for both dirs when wanted.
		if asc {
			return fmt.Sprintf("(%s) IS NULL, (%s) ASC", expr, expr)
		}
		return fmt.Sprintf("(%s) IS NULL, (%s) DESC", expr, expr)
	}
	switch sort {
	case TaskSortDuration:
		// Finished: finished-started; running: now-started; queued: NULL (last).
		expr := `CASE
			WHEN status IN ('done','failed','cancelled') THEN
				(julianday(COALESCE(finished_at, created_at)) - julianday(COALESCE(started_at, created_at)))
			WHEN status = 'running' AND started_at IS NOT NULL THEN
				(julianday('now') - julianday(started_at))
			ELSE NULL END`
		return nullsLast(expr) + ", id " + dirSQL
	case TaskSortInterrupted:
		return "interrupt_count " + dirSQL + ", id " + dirSQL
	case TaskSortProgress:
		return nullsLast("progress") + ", id " + dirSQL
	case TaskSortQueue:
		// Running first, then pending by claim order; finished last.
		if asc {
			return `CASE status WHEN 'running' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END ASC,
				queue_seq ASC, id ASC`
		}
		return `CASE status WHEN 'running' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END ASC,
			queue_seq DESC, id DESC`
	case TaskSortKind:
		return "kind " + dirSQL + ", id " + dirSQL
	case TaskSortDomain:
		return "domain " + dirSQL + ", id " + dirSQL
	case TaskSortStatus:
		return "status " + dirSQL + ", id " + dirSQL
	case TaskSortParent:
		return nullsLast("parent_task_id") + ", id " + dirSQL
	default: // created
		return "created_at " + dirSQL + ", id " + dirSQL
	}
}

// NormalizeTaskSort returns a known sort key or TaskSortCreated.
func NormalizeTaskSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TaskSortCreated, TaskSortDuration, TaskSortInterrupted, TaskSortProgress,
		TaskSortQueue, TaskSortKind, TaskSortDomain, TaskSortStatus, TaskSortParent:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return TaskSortCreated
	}
}

func normalizeTaskSortDir(sort, dir string) string {
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "asc" || dir == "desc" {
		return dir
	}
	// Defaults: created/duration/interrupted/progress desc; kind/domain/status/parent/queue asc-ish.
	switch sort {
	case TaskSortKind, TaskSortDomain, TaskSortStatus, TaskSortParent:
		return "asc"
	default:
		return "desc"
	}
}

func normalizeTaskListStatuses(statuses []string) []string {
	allowed := map[string]struct{}{
		StatusPending: {}, StatusRunning: {},
		StatusDone: {}, StatusFailed: {}, StatusCancelled: {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(statuses))
	for _, st := range statuses {
		st = strings.TrimSpace(st)
		if st == "" {
			continue
		}
		if _, ok := allowed[st]; !ok {
			continue
		}
		if _, ok := seen[st]; ok {
			continue
		}
		seen[st] = struct{}{}
		out = append(out, st)
	}
	return out
}
