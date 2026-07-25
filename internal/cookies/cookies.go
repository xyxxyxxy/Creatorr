package cookies

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Cookie is Netscape jar text for one domain.
type Cookie struct {
	Domain    string `json:"domain"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

// List returns all stored cookies (content included).
func List(database *db.DB) ([]Cookie, error) {
	rows, err := database.SQL.Query(`SELECT domain, content, updated_at FROM cookies ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cookie
	for rows.Next() {
		var c Cookie
		if err := rows.Scan(&c.Domain, &c.Content, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns cookie content for domain, or empty if missing.
// Looks up the normalized host, then a legacy www. key if present.
// Pass settings.DomainDefault for the global fallback jar.
func Get(database *db.DB, domain string) (string, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return "", nil
	}
	content, err := getExact(database, domain)
	if err != nil || content != "" || domain == settings.DomainDefault {
		return content, err
	}
	return getExact(database, "www."+domain)
}

func getExact(database *db.DB, domain string) (string, error) {
	var content string
	err := database.SQL.QueryRow(`SELECT content FROM cookies WHERE domain = ?`, domain).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return content, err
}

// Upsert stores Netscape jar text for a domain (www. stripped).
// Domain may be settings.DomainDefault ("default") for the global fallback jar.
func Upsert(database *db.DB, domain, content string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	_, err := database.SQL.Exec(`
		INSERT INTO cookies (domain, content, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at
	`, domain, content, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if domain != settings.DomainDefault {
		_, _ = database.SQL.Exec(`DELETE FROM cookies WHERE domain = ?`, "www."+domain)
	}
	return nil
}

// Delete removes cookies for a domain (and legacy www. key).
func Delete(database *db.DB, domain string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return nil
	}
	_, err := database.SQL.Exec(`DELETE FROM cookies WHERE domain = ? OR domain = ?`, domain, "www."+domain)
	return err
}
