package notify

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	apprise "github.com/unraid/apprise-go"
)

// Channel is one notification target with subscribed events.
// Apprise channels are rows in notification_channels; the Creatorr in-app channel is virtual.
type Channel struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// List returns the fixed in-app channel first, then Apprise channels by id.
func List(database *db.DB) ([]Channel, error) {
	out := []Channel{InAppChannel()}
	if database == nil {
		return out, nil
	}
	rows, err := database.SQL.Query(`
		SELECT id, name, url, events, created_at, updated_at
		FROM notification_channels ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		if IsInAppChannel(c) {
			continue // ignore stray DB rows with the reserved URL
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one channel by id.
func Get(database *db.DB, id int64) (Channel, error) {
	var c Channel
	if database == nil || id <= 0 {
		return c, fmt.Errorf("notify channel not found")
	}
	row := database.SQL.QueryRow(`
		SELECT id, name, url, events, created_at, updated_at
		FROM notification_channels WHERE id = ?
	`, id)
	c, err := scanChannel(row)
	if err == sql.ErrNoRows {
		return c, fmt.Errorf("notify channel not found")
	}
	return c, err
}

// ListForEvent returns channels subscribed to event (in-app first when subscribed).
func ListForEvent(database *db.DB, event string) ([]Channel, error) {
	all, err := List(database)
	if err != nil {
		return nil, err
	}
	event = AliasEvent(strings.TrimSpace(event))
	var out []Channel
	for _, c := range all {
		if slicesContains(c.Events, event) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Upsert inserts (id<=0) or updates an Apprise channel. Validates Apprise URL and events.
// The Creatorr in-app channel cannot be created or updated.
func Upsert(database *db.DB, id int64, name, rawURL string, events []string) (int64, error) {
	if database == nil {
		return 0, fmt.Errorf("database required")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, fmt.Errorf("Apprise URL required")
	}
	if IsInAppURL(rawURL) {
		return 0, ErrInAppChannelReadOnly
	}
	if id > 0 {
		existing, err := Get(database, id)
		if err != nil {
			return 0, err
		}
		if IsInAppChannel(existing) {
			return 0, ErrInAppChannelReadOnly
		}
	}
	if err := ValidateURL(rawURL); err != nil {
		return 0, err
	}
	ev, err := NormalizeEvents(events)
	if err != nil {
		return 0, err
	}
	evJSON, err := json.Marshal(ev)
	if err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if id > 0 {
		res, err := database.SQL.Exec(`
			UPDATE notification_channels SET name = ?, url = ?, events = ?, updated_at = ?
			WHERE id = ?
		`, name, rawURL, string(evJSON), now, id)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return 0, fmt.Errorf("notify channel not found")
		}
		return id, nil
	}
	res, err := database.SQL.Exec(`
		INSERT INTO notification_channels (name, url, events, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, name, rawURL, string(evJSON), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Delete removes an Apprise channel by id. The virtual in-app channel (id 0) cannot
// be deleted; a stray DB row with the reserved URL may be removed.
func Delete(database *db.DB, id int64) error {
	if database == nil {
		return nil
	}
	if id <= 0 {
		return ErrInAppChannelReadOnly
	}
	_, err := database.SQL.Exec(`DELETE FROM notification_channels WHERE id = ?`, id)
	return err
}

// ValidateURL checks that Apprise accepts the URL scheme.
func ValidateURL(rawURL string) error {
	client := apprise.New()
	if err := client.Add(rawURL); err != nil {
		return fmt.Errorf("invalid Apprise URL: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanChannel(row rowScanner) (Channel, error) {
	var c Channel
	var evJSON string
	if err := row.Scan(&c.ID, &c.Name, &c.URL, &evJSON, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(evJSON), &c.Events); err != nil {
		return c, fmt.Errorf("notify channel %d events: %w", c.ID, err)
	}
	if c.Events == nil {
		c.Events = []string{}
	}
	// Read-time aliases for legacy channel event ids.
	aliased, err := NormalizeEvents(c.Events)
	if err == nil {
		c.Events = aliased
	} else {
		for i, id := range c.Events {
			c.Events[i] = AliasEvent(id)
		}
	}
	return c, nil
}

func slicesContains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
