package domains

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// CookieJar is Netscape jar text on a host domains override (domains.cookies).
type CookieJar struct {
	Domain    string `json:"domain"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

// ListCookieJars returns host overrides with non-empty domains.cookies.
func ListCookieJars(database *db.DB) ([]CookieJar, error) {
	rows, err := database.SQL.Query(`
		SELECT domain, cookies, updated_at FROM domains
		WHERE domain != ?
		  AND cookies IS NOT NULL AND TRIM(cookies) != ''
		ORDER BY domain
	`, settings.DomainDefault)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CookieJar
	for rows.Next() {
		var c CookieJar
		if err := rows.Scan(&c.Domain, &c.Content, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCookies returns Netscape jar text for a host override, or empty if none.
func GetCookies(database *db.DB, domain string) (string, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == settings.DomainDefault {
		return "", nil
	}
	content, err := getCookiesExact(database, domain)
	if err != nil || content != "" {
		return content, err
	}
	return getCookiesExact(database, "www."+domain)
}

func getCookiesExact(database *db.DB, domain string) (string, error) {
	var content sql.NullString
	err := database.SQL.QueryRow(`SELECT cookies FROM domains WHERE domain = ?`, domain).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !content.Valid {
		return "", nil
	}
	return content.String, nil
}

// SetCookies stores Netscape jar text on a host domains override.
// Rejects domain=default (Access jars are host-only). Empty content clears the jar.
func SetCookies(database *db.DB, domain, content string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	if domain == settings.DomainDefault {
		return fmt.Errorf("cookies are host override only")
	}
	if strings.TrimSpace(content) == "" {
		return ClearCookies(database, domain)
	}
	if err := EnsureHost(database, domain); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		UPDATE domains SET cookies = ?, updated_at = ? WHERE domain = ?
	`, content, now, domain)
	if err != nil {
		return err
	}
	_, _ = database.SQL.Exec(`UPDATE domains SET cookies = NULL, updated_at = ? WHERE domain = ?`, now, "www."+domain)
	return nil
}

// ClearCookies clears domains.cookies on a host (and legacy www. key).
// Also clears domain=default for legacy cleanup. Does not delete the domains row.
func ClearCookies(database *db.DB, domain string) error {
	domain = settings.NormalizeDomain(domain)
	if domain == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.SQL.Exec(`
		UPDATE domains SET cookies = NULL, updated_at = ?
		WHERE domain = ? OR domain = ?
	`, now, domain, "www."+domain)
	return err
}

// CookiesApply reports whether a cookie jar would be passed for this hostname.
func CookiesApply(database *db.DB, domain string) (ok bool, tip string, err error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return false, "", nil
	}
	content, err := ResolveCookies(database, domain)
	if err != nil || content == "" {
		return false, "", err
	}
	return true, "Cookies", nil
}

// ResolveCookies returns Netscape jar text for a hostname (exact host, then suffix match on host jars).
func ResolveCookies(database *db.DB, domain string) (string, error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == settings.DomainDefault {
		return "", nil
	}
	content, err := GetCookies(database, domain)
	if err != nil {
		return "", err
	}
	if content != "" {
		return content, nil
	}
	list, err := ListCookieJars(database)
	if err != nil {
		return "", err
	}
	for _, c := range list {
		d := settings.NormalizeDomain(c.Domain)
		if d == "" || d == settings.DomainDefault {
			continue
		}
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return c.Content, nil
		}
	}
	return "", nil
}
