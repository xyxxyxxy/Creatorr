package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is process bootstrap from environment. Runtime knobs live in Settings (SQLite).
// Paths are fixed layouts (not env): container absolute dirs when /data exists, else var/.
type Config struct {
	Host            string
	Port            int
	DBPath          string
	FlareSolverrURL string // filled from Settings after seed (optional CREATORR_FLARESOLVERR_URL one-shot)
	LibraryRoot     string // seed path for empty root_folders
	ImportRoot      string
	// YtDlpBin is the yt-dlp executable. Empty after Load; main sets via ytdlp.ResolveBin
	// (Docker image: /usr/local/bin/yt-dlp; local: PATH fallback).
	YtDlpBin string
	// YtDlpPluginsDir is always passed as --plugin-dirs (operator mounts).
	YtDlpPluginsDir string
	// YtDlpSystemPluginsDir is the baked POT provider plugin path (also --plugin-dirs).
	YtDlpSystemPluginsDir string
	// PotProviderURL is CREATORR_POT_PROVIDER_URL (bgutil HTTP base URL). Empty disables POT.
	PotProviderURL string
	// CacheDir holds Creatorr-managed accelerating artifacts (e.g. download beginnings).
	CacheDir string
	// PublicBaseURL is deprecated bootstrap only; runtime uses Settings external_base_url
	// (one-shot migrate from CREATORR_PUBLIC_BASE_URL when Settings empty).
	PublicBaseURL string
}

// Load reads bootstrap env (port, public URL, POT provider) and selects path layout.
func Load() Config {
	paths := pathLayout()
	return Config{
		Host:                  "0.0.0.0",
		Port:                  getenvInt("CREATORR_PORT", 8787),
		DBPath:                paths.db,
		LibraryRoot:           paths.library,
		ImportRoot:            paths.importRoot,
		YtDlpBin:              "", // main: ytdlp.ResolveBin
		YtDlpPluginsDir:       paths.plugins,
		YtDlpSystemPluginsDir: paths.systemPlugins,
		PotProviderURL:        strings.TrimSpace(os.Getenv("CREATORR_POT_PROVIDER_URL")),
		CacheDir:              paths.cache,
		PublicBaseURL:         strings.TrimRight(strings.TrimSpace(getenv("CREATORR_PUBLIC_BASE_URL", "")), "/"),
	}
}

type layout struct {
	db, library, importRoot, cache, plugins, systemPlugins string
}

// pathLayout: container absolutes when /data is a directory (image + compose mounts);
// otherwise local Go under var/.
func pathLayout() layout {
	if isDir("/data") {
		return layout{
			db:            "/data/creatorr.db",
			library:       "/media/library",
			importRoot:    "/media/import",
			cache:         "/cache",
			plugins:       "/yt-dlp-plugins",
			systemPlugins: "/usr/local/share/yt-dlp-plugins/bgutil",
		}
	}
	return layout{
		db:            filepath.Join("var", "data", "creatorr.db"),
		library:       filepath.Join("var", "media", "library"),
		importRoot:    filepath.Join("var", "media", "import"),
		cache:         filepath.Join("var", "cache"),
		plugins:       filepath.Join("var", "yt-dlp-plugins"),
		systemPlugins: filepath.Join("var", "yt-dlp-plugins", "bgutil"),
	}
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
