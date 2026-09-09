package domains

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// WriteTempJar writes Netscape cookie text to a temp file under dir for a yt-dlp invoke.
// Caller must remove the file (or the parent dir) after the invoke exits.
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

// TempJarForURL resolves the host override jar for rawURL and materializes a temp Netscape file.
// Returns empty path when nothing is stored or allowStored is false (omit --cookies).
// allowStored is false when cookies_after_fail is on for non-retry yt-dlp invokes.
func TempJarForURL(database *db.DB, dir, rawURL string, allowStored bool) (string, error) {
	if !allowStored {
		return "", nil
	}
	host := queue.DomainFromURL(rawURL)
	content, err := ResolveCookies(database, host)
	if err != nil || content == "" {
		return "", err
	}
	return WriteTempJar(dir, host, content)
}

// TempJarForNonDownload materializes the stored jar unless cookies_after_fail is on
// (scan / meta / sidecars never use the account jar in that mode).
func TempJarForNonDownload(database *db.DB, dir, rawURL string) (string, error) {
	afterFail, err := CookiesAfterFailForURL(database, rawURL)
	if err != nil {
		return "", err
	}
	return TempJarForURL(database, dir, rawURL, AllowStoredJar(afterFail, false))
}

// StoredJarForURL returns whether a stored jar exists and a temp path when allowStored.
// hadStored is true when ResolveCookies finds content (independent of allowStored).
func StoredJarForURL(database *db.DB, dir, rawURL string, allowStored bool) (path string, hadStored bool, err error) {
	host := queue.DomainFromURL(rawURL)
	content, err := ResolveCookies(database, host)
	if err != nil {
		return "", false, err
	}
	if content == "" {
		return "", false, nil
	}
	if !allowStored {
		return "", true, nil
	}
	path, err = WriteTempJar(dir, host, content)
	return path, true, err
}
