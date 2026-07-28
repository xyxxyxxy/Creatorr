package domains

import (
	"database/sql"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// IsPaused reports whether the hostname lane is soft-paused (no new claims).
// Missing domain_runtime row = not paused. Never consults domains.active.
func IsPaused(database *db.DB, domain string) (bool, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return false, nil
	}
	var paused int
	err := database.SQL.QueryRow(`SELECT paused FROM domain_runtime WHERE domain = ?`, domain).Scan(&paused)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return paused != 0, nil
}

// SetPaused soft-pauses or resumes a hostname lane.
// Pause upserts domain_runtime; resume deletes the row.
// Never creates or updates domains rows. Does not cancel tasks.
func SetPaused(database *db.DB, domain string, paused bool) error {
	if err := settings.ValidateOverrideDomain(domain); err != nil {
		return err
	}
	domain = settings.NormalizeDomain(domain)
	if !paused {
		_, err := database.SQL.Exec(`DELETE FROM domain_runtime WHERE domain = ?`, domain)
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		INSERT INTO domain_runtime (domain, paused, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT(domain) DO UPDATE SET paused = 1, updated_at = excluded.updated_at
	`, domain, now)
	return err
}

// ListPaused returns hostnames currently soft-paused, sorted.
func ListPaused(database *db.DB) ([]string, error) {
	rows, err := database.SQL.Query(`
		SELECT domain FROM domain_runtime WHERE paused != 0 ORDER BY domain
	`)
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
