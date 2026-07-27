package settings

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// ResolvedCredentials are effective yt-dlp --username/--password for a hostname.
type ResolvedCredentials struct {
	Username string
	Password string
	FromHost bool // true when host row username is non-NULL (override or explicit empty)
}

// CredentialsForURL resolves membership credentials for a source URL host.
// Parses host locally (no cookies/queue import) to avoid settings↔cookies cycles.
func CredentialsForURL(database *db.DB, rawURL string) (ResolvedCredentials, error) {
	host := ""
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		host = u.Hostname()
	}
	return CredentialsForDomain(database, NormalizeDomain(host))
}

// CredentialsForDomain resolves host override credentials only (no Domain defaults).
// Host NULL username or missing row = none. Host empty username = explicitly none.
func CredentialsForDomain(database *db.DB, domain string) (ResolvedCredentials, error) {
	domain = NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" || domain == DomainDefault {
		return ResolvedCredentials{}, nil
	}
	var user, pass sql.NullString
	err := database.SQL.QueryRow(`
		SELECT username, password FROM domains WHERE domain = ?
	`, domain).Scan(&user, &pass)
	if err == sql.ErrNoRows {
		return ResolvedCredentials{}, nil
	}
	if err != nil {
		return ResolvedCredentials{}, err
	}
	if !user.Valid {
		return ResolvedCredentials{}, nil
	}
	u := strings.TrimSpace(user.String)
	if u == "" {
		return ResolvedCredentials{FromHost: true}, nil
	}
	p := ""
	if pass.Valid {
		p = pass.String
	}
	return ResolvedCredentials{Username: u, Password: p, FromHost: true}, nil
}

func defaultCredentialPair(database *db.DB) (username, password string, err error) {
	if err := EnsureDefaultDomain(database); err != nil {
		return "", "", err
	}
	var user, pass sql.NullString
	err = database.SQL.QueryRow(`
		SELECT username, password FROM domains WHERE domain = ?
	`, DomainDefault).Scan(&user, &pass)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if user.Valid {
		username = strings.TrimSpace(user.String)
	}
	if pass.Valid {
		password = pass.String
	}
	return username, password, nil
}

// DefaultCredentials returns Domain defaults username/password for Settings forms.
func DefaultCredentials(database *db.DB) (username, password string, err error) {
	return defaultCredentialPair(database)
}

// SaveDefaultCredentials stores credentials on domains row domain=default.
// Blank password keeps the existing password when username stays set.
func SaveDefaultCredentials(database *db.DB, username, password string, keepPassword bool) error {
	if err := EnsureDefaultDomain(database); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = '', password = '', updated_at = datetime('now')
			WHERE domain = ?
		`, DomainDefault)
		return err
	}
	if password != "" {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = ?, password = ?, updated_at = datetime('now')
			WHERE domain = ?
		`, username, password, DomainDefault)
		return err
	}
	if keepPassword {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = ?, updated_at = datetime('now')
			WHERE domain = ?
		`, username, DomainDefault)
		return err
	}
	return fmt.Errorf("password required for new credentials")
}

// SaveHostCredentials stores host override credentials. inherit=true clears to NULL (inherit).
// Blank password with keepPassword retains the stored password when username is set.
func SaveHostCredentials(database *db.DB, domain, username, password string, inherit, keepPassword bool) error {
	if err := ValidateOverrideDomain(domain); err != nil {
		return err
	}
	domain = NormalizeDomain(domain)
	if inherit {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = NULL, password = NULL, updated_at = datetime('now')
			WHERE domain = ?
		`, domain)
		return err
	}
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = '', password = '', updated_at = datetime('now')
			WHERE domain = ?
		`, domain)
		return err
	}
	if password != "" {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = ?, password = ?, updated_at = datetime('now')
			WHERE domain = ?
		`, username, password, domain)
		return err
	}
	if keepPassword {
		_, err := database.SQL.Exec(`
			UPDATE domains SET username = ?, updated_at = datetime('now')
			WHERE domain = ?
		`, username, domain)
		return err
	}
	return fmt.Errorf("password required for new credentials")
}

// HostHasStoredPassword reports whether the host row has a non-empty password.
func HostHasStoredPassword(database *db.DB, domain string) (bool, error) {
	domain = NormalizeDomain(domain)
	var pass sql.NullString
	err := database.SQL.QueryRow(`SELECT password FROM domains WHERE domain = ?`, domain).Scan(&pass)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return pass.Valid && strings.TrimSpace(pass.String) != "", nil
}
