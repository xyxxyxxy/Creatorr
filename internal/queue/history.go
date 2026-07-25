package queue

import (
	"database/sql"
	"strings"
)

// HistoryStatuses are terminal task statuses shown on the History page.
var HistoryStatuses = []string{StatusDone, StatusFailed, StatusCancelled}

const historySelectCols = `id, kind, status, series_id, video_id, payload,
	COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(message,''),
	COALESCE(detail,''), progress, domain, priority, created_at, started_at, finished_at`

// HistoryFilter selects finished tasks for the History list / API.
// Empty Statuses = all HistoryStatuses; empty Domain/Kind/From/To = no filter on that column.
// From/To are inclusive UTC RFC3339Nano bounds on COALESCE(finished_at, created_at).
type HistoryFilter struct {
	Statuses []string
	Domain  string
	Kind    string
	From    string
	To      string
}

// ListHistory returns finished tasks (newest finished first).
func (s *Store) ListHistory(f HistoryFilter, limit, offset int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where, args := historyFilterSQL(f)
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(`
		SELECT `+historySelectCols+`
		FROM tasks
		WHERE `+where+`
		ORDER BY COALESCE(finished_at, created_at) DESC, id DESC
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// CountHistory returns how many finished tasks match the filter.
func (s *Store) CountHistory(f HistoryFilter) (int, error) {
	where, args := historyFilterSQL(f)
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE `+where, args...).Scan(&n)
	return n, err
}

// ListHistoryByKind returns finished tasks of one kind.
func (s *Store) ListHistoryByKind(kind string, limit, offset int) ([]Task, error) {
	return s.ListHistory(HistoryFilter{Kind: kind}, limit, offset)
}

// CountHistoryByKind counts finished tasks of one kind.
func (s *Store) CountHistoryByKind(kind string) (int, error) {
	return s.CountHistory(HistoryFilter{Kind: kind})
}

// DistinctHistoryDomains returns distinct non-empty domains among finished tasks.
// system is listed first; other domains are alphabetical.
func (s *Store) DistinctHistoryDomains() ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT domain FROM tasks
		WHERE status IN (?, ?, ?) AND domain != ''
		ORDER BY CASE WHEN domain = ? THEN 0 ELSE 1 END, domain ASC
	`, StatusDone, StatusFailed, StatusCancelled, SystemDomain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

// DistinctHistoryKinds returns distinct kinds among finished tasks (sorted).
func (s *Store) DistinctHistoryKinds() ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT kind FROM tasks
		WHERE status IN (?, ?, ?)
		ORDER BY kind ASC
	`, StatusDone, StatusFailed, StatusCancelled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func historyFilterSQL(f HistoryFilter) (string, []any) {
	statuses := normalizeHistoryStatuses(f.Statuses)
	if len(statuses) == 0 {
		statuses = append([]string{}, HistoryStatuses...)
	}
	parts := []string{"status IN (" + sqlPlaceholders(len(statuses)) + ")"}
	args := make([]any, 0, len(statuses)+2)
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
	if from := strings.TrimSpace(f.From); from != "" {
		parts = append(parts, "datetime(COALESCE(finished_at, created_at)) >= datetime(?)")
		args = append(args, from)
	}
	if to := strings.TrimSpace(f.To); to != "" {
		parts = append(parts, "datetime(COALESCE(finished_at, created_at)) <= datetime(?)")
		args = append(args, to)
	}
	return strings.Join(parts, " AND "), args
}

const sourceHistoryWhere = `
	(json_valid(detail) AND CAST(json_extract(detail, '$.source_id') AS INTEGER) = ?)
	OR (json_valid(payload) AND CAST(json_extract(payload, '$.source_id') AS INTEGER) = ?)
	OR video_id IN (SELECT id FROM videos WHERE source_id = ?)
`

// ListHistoryForSource returns finished tasks tied to a source.
func (s *Store) ListHistoryForSource(sourceID int64, limit, offset int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT `+historySelectCols+`
		FROM tasks
		WHERE status IN (?, ?, ?) AND (`+sourceHistoryWhere+`)
		ORDER BY COALESCE(finished_at, created_at) DESC, id DESC
		LIMIT ? OFFSET ?
	`, StatusDone, StatusFailed, StatusCancelled, sourceID, sourceID, sourceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// CountHistoryForSource counts finished tasks for a source.
func (s *Store) CountHistoryForSource(sourceID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(DISTINCT id) FROM tasks
		WHERE status IN (?, ?, ?) AND (`+sourceHistoryWhere+`)
	`, StatusDone, StatusFailed, StatusCancelled, sourceID, sourceID, sourceID).Scan(&n)
	return n, err
}

// LatestHistoryIDForSource returns the newest finished task id for a source, or 0.
func (s *Store) LatestHistoryIDForSource(sourceID int64) (int64, error) {
	var id sql.NullInt64
	err := s.DB.SQL.QueryRow(`
		SELECT id FROM tasks
		WHERE status IN (?, ?, ?) AND (`+sourceHistoryWhere+`)
		ORDER BY COALESCE(finished_at, created_at) DESC, id DESC
		LIMIT 1
	`, StatusDone, StatusFailed, StatusCancelled, sourceID, sourceID, sourceID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

func scanTaskRows(rows *sql.Rows) ([]Task, error) {
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func normalizeHistoryStatuses(statuses []string) []string {
	allowed := map[string]struct{}{
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

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
