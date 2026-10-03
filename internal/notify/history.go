package notify

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// Notification is one in-app notification log row.
type Notification struct {
	ID         int64
	CreatedAt  string
	Event      string
	Title      string
	Body       string
	TaskID     sql.NullInt64
	ExternalOK bool
	ReadAt     sql.NullString
}

// Unread reports whether read_at is unset (any level; info can be marked unread).
func (n Notification) Unread() bool {
	return !n.ReadAt.Valid
}

// ListFilter selects notification rows.
type ListFilter struct {
	Event      string
	Level      string // info | warning | alert; empty = all
	From       string // inclusive UTC RFC3339Nano on created_at
	To         string // inclusive UTC RFC3339Nano on created_at
	UnreadOnly bool   // read_at IS NULL (any event)
	ReadOnly   bool   // read_at IS NOT NULL; mutually exclusive with UnreadOnly
	Sort       string // created | level; empty = created; legacy when accepted
	SortDir    string // asc|desc
}

// InsertNotification writes a notification row. taskID <= 0 stores NULL.
// readAt empty means unread (alert/warning events); non-empty marks read at insert.
func InsertNotification(database *db.DB, event, title, body string, taskID int64, externalOK bool, readAt string) (int64, error) {
	if database == nil {
		return 0, fmt.Errorf("database required")
	}
	event = AliasEvent(strings.TrimSpace(event))
	if !validEvent(event) {
		return 0, fmt.Errorf("unknown notify event %q", event)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var task any
	if taskID > 0 {
		task = taskID
	}
	var read any
	if strings.TrimSpace(readAt) != "" {
		read = readAt
	}
	ext := 0
	if externalOK {
		ext = 1
	}
	res, err := database.SQL.Exec(`
		INSERT INTO notifications (created_at, event, title, body, task_id, external_ok, read_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, now, event, title, body, task, ext, read)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// MarkExternalOK sets external_ok=1. Does not change read_at; in-app ack only.
func MarkExternalOK(database *db.DB, id int64) error {
	if database == nil || id <= 0 {
		return nil
	}
	_, err := database.SQL.Exec(`UPDATE notifications SET external_ok = 1 WHERE id = ?`, id)
	return err
}

// GetNotification returns one notification by id.
func GetNotification(database *db.DB, id int64) (Notification, error) {
	var n Notification
	if database == nil || id <= 0 {
		return n, fmt.Errorf("notification not found")
	}
	row := database.SQL.QueryRow(`
		SELECT id, created_at, event, title, body, task_id, external_ok, read_at
		FROM notifications WHERE id = ?
	`, id)
	n, err := scanNotification(row)
	if err == sql.ErrNoRows {
		return n, fmt.Errorf("notification not found")
	}
	return n, err
}

// ListNotifications returns newest-first page.
func ListNotifications(database *db.DB, f ListFilter, limit, offset int) ([]Notification, error) {
	if database == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where, args := notificationWhere(f)
	order := notificationOrderSQL(f)
	q := `
		SELECT id, created_at, event, title, body, task_id, external_ok, read_at
		FROM notifications` + where + `
		ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := database.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountNotifications returns matching row count.
func CountNotifications(database *db.DB, f ListFilter) (int, error) {
	if database == nil {
		return 0, nil
	}
	where, args := notificationWhere(f)
	var n int
	err := database.SQL.QueryRow(`SELECT COUNT(*) FROM notifications`+where, args...).Scan(&n)
	return n, err
}

// ListNotificationIDs returns notification ids matching filter (list order).
// toggleableOnly is kept for callers; all in-app rows are toggleable (incl. info).
func ListNotificationIDs(database *db.DB, f ListFilter, toggleableOnly bool) ([]int64, error) {
	if database == nil {
		return nil, nil
	}
	_ = toggleableOnly
	where, args := notificationWhere(f)
	q := `SELECT id FROM notifications` + where + ` ORDER BY ` + notificationOrderSQL(f)
	rows, err := database.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CountUnread returns unread alert/warning count for the nav badge (info ignored).
func CountUnread(database *db.DB) (int, error) {
	if database == nil {
		return 0, nil
	}
	evs := UnreadEvents()
	ph := strings.Repeat("?,", len(evs))
	ph = ph[:len(ph)-1]
	args := make([]any, 0, len(evs))
	for _, e := range evs {
		args = append(args, e)
	}
	var n int
	err := database.SQL.QueryRow(
		`SELECT COUNT(*) FROM notifications WHERE read_at IS NULL AND event IN (`+ph+`)`,
		args...,
	).Scan(&n)
	return n, err
}

// MarkRead sets read_at on one notification (no-op if already read).
func MarkRead(database *db.DB, id int64) error {
	if database == nil || id <= 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		UPDATE notifications SET read_at = ? WHERE id = ? AND read_at IS NULL
	`, now, id)
	if err == nil {
		publishRead(database, id)
	}
	return err
}

// MarkUnread clears read_at on one notification (no-op if already unread).
func MarkUnread(database *db.DB, id int64) error {
	if database == nil || id <= 0 {
		return nil
	}
	_, err := database.SQL.Exec(`
		UPDATE notifications SET read_at = NULL WHERE id = ? AND read_at IS NOT NULL
	`, id)
	if err == nil {
		publishRead(database, id)
	}
	return err
}

// MarkReadMany sets read_at on selected ids (one SSE publish).
func MarkReadMany(database *db.DB, ids []int64) (int64, error) {
	return markReadStateMany(database, ids, true)
}

// MarkUnreadMany clears read_at on selected ids (one SSE publish).
func MarkUnreadMany(database *db.DB, ids []int64) (int64, error) {
	return markReadStateMany(database, ids, false)
}

func markReadStateMany(database *db.DB, ids []int64, wantRead bool) (int64, error) {
	if database == nil || len(ids) == 0 {
		return 0, nil
	}
	clean := make([]int64, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, nil
	}
	idPh := strings.Repeat("?,", len(clean))
	idPh = idPh[:len(idPh)-1]
	args := make([]any, 0, 1+len(clean))
	var q string
	if wantRead {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		args = append(args, now)
		q = `UPDATE notifications SET read_at = ? WHERE id IN (` + idPh + `) AND read_at IS NULL`
	} else {
		q = `UPDATE notifications SET read_at = NULL WHERE id IN (` + idPh + `) AND read_at IS NOT NULL`
	}
	for _, id := range clean {
		args = append(args, id)
	}
	res, err := database.SQL.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		publishRead(database, 0)
	}
	return n, nil
}

// MarkAllRead marks every notification with null read_at as read (any level).
func MarkAllRead(database *db.DB) (int64, error) {
	if database == nil {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := database.SQL.Exec(`
		UPDATE notifications SET read_at = ?
		WHERE read_at IS NULL
	`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		publishRead(database, 0)
	}
	return n, nil
}

func notificationWhere(f ListFilter) (string, []any) {
	var parts []string
	var args []any
	if ev := strings.TrimSpace(f.Event); ev != "" {
		parts = append(parts, `event = ?`)
		args = append(args, AliasEvent(ev))
	}
	if evs := EventsForLevel(f.Level); len(evs) > 0 {
		ph := strings.Repeat("?,", len(evs))
		ph = ph[:len(ph)-1]
		parts = append(parts, `event IN (`+ph+`)`)
		for _, e := range evs {
			args = append(args, e)
		}
	}
	if from := strings.TrimSpace(f.From); from != "" {
		parts = append(parts, `datetime(created_at) >= datetime(?)`)
		args = append(args, from)
	}
	if to := strings.TrimSpace(f.To); to != "" {
		parts = append(parts, `datetime(created_at) <= datetime(?)`)
		args = append(args, to)
	}
	if f.UnreadOnly {
		parts = append(parts, `read_at IS NULL`)
	} else if f.ReadOnly {
		parts = append(parts, `read_at IS NOT NULL`)
	}
	if len(parts) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(parts, ` AND `), args
}

func notificationOrderSQL(f ListFilter) string {
	dir := strings.ToLower(strings.TrimSpace(f.SortDir))
	if dir != "asc" && dir != "desc" {
		dir = "desc"
	}
	dirSQL := "DESC"
	if dir == "asc" {
		dirSQL = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(f.Sort)) {
	case "level":
		return notificationLevelOrderExpr() + ` ` + dirSQL + `, id ` + dirSQL
	default: // created (and legacy when)
		return `created_at ` + dirSQL + `, id ` + dirSQL
	}
}

func levelEventPlaceholders(level string) string {
	evs := EventsForLevel(level)
	if len(evs) == 0 {
		return "''"
	}
	parts := make([]string, len(evs))
	for i, e := range evs {
		parts[i] = "'" + strings.ReplaceAll(e, "'", "''") + "'"
	}
	return strings.Join(parts, ",")
}

func notificationLevelOrderExpr() string {
	return `CASE
		WHEN event IN (` + levelEventPlaceholders("alert") + `) THEN 0
		WHEN event IN (` + levelEventPlaceholders("warning") + `) THEN 1
		ELSE 2 END`
}

func scanNotification(row rowScanner) (Notification, error) {
	var n Notification
	var ext int
	if err := row.Scan(&n.ID, &n.CreatedAt, &n.Event, &n.Title, &n.Body, &n.TaskID, &ext, &n.ReadAt); err != nil {
		return n, err
	}
	n.ExternalOK = ext != 0
	return n, nil
}
