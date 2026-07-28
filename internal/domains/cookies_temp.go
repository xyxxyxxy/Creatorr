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
// Returns empty path when nothing is stored (omit --cookies).
func TempJarForURL(database *db.DB, dir, rawURL string) (string, error) {
	host := queue.DomainFromURL(rawURL)
	content, err := ResolveCookies(database, host)
	if err != nil || content == "" {
		return "", err
	}
	return WriteTempJar(dir, host, content)
}
