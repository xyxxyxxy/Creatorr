package cookies

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// WriteTempJar writes Netscape cookie text to a temp file under dir for a handler invoke.
// Creatorr passes that path as --cookies to domain handlers (list/resolve/download/sidecars).
// Caller must remove the file (or the parent dir) after the handler exits.
func WriteTempJar(dir, domain, content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("empty cookie content")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := domain
	if name == "" {
		name = "cookies"
	}
	name = strings.ReplaceAll(name, "/", "_")
	path := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// TempJarForURL resolves Settings cookie jars for rawURL and materializes a temp Netscape file
// so workers can pass --cookies to external handlers during scan/download/metadata work.
// Lookup order: exact host → suffix match on stored host jars → default jar.
// Returns empty path when nothing is stored (omit --cookies).
func TempJarForURL(database *db.DB, dir, rawURL string) (string, error) {
	host := queue.DomainFromURL(rawURL)
	content, err := ResolveContent(database, host)
	if err != nil || content == "" {
		return "", err
	}
	return WriteTempJar(dir, host, content)
}

// Applies reports whether a cookie jar would be passed for this hostname
// (host jar, suffix match, or default). tip is a short UI label when ok.
func Applies(database *db.DB, domain string) (ok bool, tip string, err error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" {
		return false, "", nil
	}
	content, src, err := resolveContent(database, domain)
	if err != nil || content == "" {
		return false, "", err
	}
	switch src {
	case settings.DomainDefault:
		return true, "Default cookies", nil
	default:
		return true, "Cookies", nil
	}
}

// ResolveContent returns Netscape jar text for a hostname (same order as TempJarForURL).
func ResolveContent(database *db.DB, domain string) (string, error) {
	content, _, err := resolveContent(database, domain)
	return content, err
}

func resolveContent(database *db.DB, domain string) (content, source string, err error) {
	domain = settings.NormalizeDomain(domain)
	if domain == "" || domain == "unknown" || domain == "system" {
		return "", "", nil
	}
	content, err = Get(database, domain)
	if err != nil {
		return "", "", err
	}
	if content != "" {
		return content, domain, nil
	}
	list, err := List(database)
	if err != nil {
		return "", "", err
	}
	for _, c := range list {
		d := settings.NormalizeDomain(c.Domain)
		if d == "" || d == settings.DomainDefault {
			continue
		}
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return c.Content, d, nil
		}
	}
	content, err = Get(database, settings.DomainDefault)
	if err != nil || content == "" {
		return "", "", err
	}
	return content, settings.DomainDefault, nil
}

// DomainOfURL returns hostname for cookie lookup (www. stripped).
func DomainOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return queue.DomainFromURL(raw)
	}
	return settings.NormalizeDomain(u.Hostname())
}
