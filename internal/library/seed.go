package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	DefaultProfileName = "best"
	DefaultFormat      = "bv*+ba"

	Profile1080Name   = "1080p"
	Profile1080Format = "bv*[height<=1080]+ba/b[height<=1080]/bv*+ba/b"
	Profile720Name    = "720p"
	Profile720Format  = "bv*[height<=720]+ba/b[height<=720]/bv*+ba/b"
)

// SeedDefaults inserts the shipped root folder and quality profiles when tables are empty.
// Root path comes from cfg.LibraryRoot (/media/library in container; var/media/library local),
// stored as an absolute path. Root name is the last path segment (same as empty Name on create).
// Profiles: best (strict bv*+ba, no fallback), 1080p, 720p (soft unrestricted tails). Insert order keeps best as lowest id.
// Remux is always MKV (library.RemuxContainer; not a Setting).
// Bare yt-dlp "best" is intentionally avoided: on DASH sites it often picks a soft progressive file.
func SeedDefaults(database *db.DB, cfg config.Config) error {
	path := cfg.LibraryRoot
	if path == "" {
		path = filepath.Join("var", "media", "library")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve library root %q: %w", path, err)
	}
	path = abs
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("mkdir root %s: %w", path, err)
	}

	var roots int
	if err := database.SQL.QueryRow(`SELECT COUNT(*) FROM root_folders`).Scan(&roots); err != nil {
		return err
	}
	if roots == 0 {
		name, err := resolveRootName("", path)
		if err != nil {
			return fmt.Errorf("seed root name: %w", err)
		}
		_, err = database.SQL.Exec(`
			INSERT INTO root_folders (name, path, retention_ttl_seconds) VALUES (?, ?, NULL)
		`, name, path)
		if err != nil {
			return fmt.Errorf("seed root: %w", err)
		}
	}

	var profiles int
	if err := database.SQL.QueryRow(`SELECT COUNT(*) FROM quality_profiles`).Scan(&profiles); err != nil {
		return err
	}
	if profiles == 0 {
		seeds := []struct{ name, format string }{
			{DefaultProfileName, DefaultFormat},
			{Profile1080Name, Profile1080Format},
			{Profile720Name, Profile720Format},
		}
		for _, s := range seeds {
			_, err := database.SQL.Exec(`
				INSERT INTO quality_profiles (name, format_selector) VALUES (?, ?)
			`, s.name, s.format)
			if err != nil {
				return fmt.Errorf("seed quality profile %q: %w", s.name, err)
			}
		}
	}
	return nil
}
