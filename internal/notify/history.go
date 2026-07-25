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

// Unread reports whether this alert/warning notification is still unread.
func (n Notification) Unread() bool {
	return IsUnreadEvent(n.Event) && !n.ReadAt.Valid
}

// ListFilter selects notification rows.
type ListFilter struct {
	Event      string
	From       string // inclusive UTC RFC3339Nano on created_at
	To         string // inclusive UTC RFC3339Nano on created_at
	UnreadOnly bool
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

// MarkExternalOK sets external_ok=1 and optionally read_at when readAt non-empty.
func MarkExternalOK(database *db.DB, id int64, readAt string) error {
	if database == nil || id <= 0 {
		return nil
	}
	if strings.TrimSpace(readAt) != "" {
		_, err := database.SQL.Exec(`
			UPDATE notifications SET external_ok = 1, read_at = COALESCE(read_at, ?) WHERE id = ?
		`, readAt, id)
		return err
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
	q := `
		SELECT id, created_at, event, title, body, task_id, external_ok, read_at
		FROM notifications` + where + `
		ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := database.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

// CountUnread returns unread alert notifications.
func CountUnread(database *db.DB) (int, error) {
	return CountNotifications(database, ListFilter{UnreadOnly: true})
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

// MarkAllRead marks all unread alert/warning notifications as read.
func MarkAllRead(database *db.DB) (int64, error) {
	if database == nil {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	evs := UnreadEvents()
	ph := strings.Repeat("?,", len(evs))
	ph = ph[:len(ph)-1]
	args := make([]any, 0, 1+len(evs))
	args = append(args, now)
	for _, e := range evs {
		args = append(args, e)
	}
	res, err := database.SQL.Exec(`
		UPDATE notifications SET read_at = ?
		WHERE read_at IS NULL AND event IN (`+ph+`)
	`, args...)
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
	if from := strings.TrimSpace(f.From); from != "" {
		parts = append(parts, `datetime(created_at) >= datetime(?)`)
		args = append(args, from)
	}
	if to := strings.TrimSpace(f.To); to != "" {
		parts = append(parts, `datetime(created_at) <= datetime(?)`)
		args = append(args, to)
	}
	if f.UnreadOnly {
		evs := UnreadEvents()
		ph := strings.Repeat("?,", len(evs))
		ph = ph[:len(ph)-1]
		parts = append(parts, `read_at IS NULL AND event IN (`+ph+`)`)
		for _, e := range evs {
			args = append(args, e)
		}
	}
	if len(parts) == 0 {
		return "", args
	}
	return ` WHERE ` + strings.Join(parts, ` AND `), args
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
