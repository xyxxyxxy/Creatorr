package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is process bootstrap from environment. Runtime knobs live in Settings (SQLite).
// Paths use a fixed layout when /data exists (container: seed /library); otherwise local var/.
type Config struct {
	Host              string
	Port              int
	DBPath            string
	FlareSolverrURL   string // CREATORR_FLARESOLVERR_URL; empty skips FlareSolverr health + pre-solve
	InitialRootFolder string // first root_folders seed path (/library in container; var/library local)
	ImportRoot        string
	// YtDlpBin is the managed yt-dlp executable (/data/bin/yt-dlp or var/data/bin/yt-dlp).
	// main sets via ytdlp.PrepareManagedBin after boot copy from image bootstrap.
	YtDlpBin string
	// YtDlpPluginsDir is always passed as --plugin-dirs (operator mounts).
	YtDlpPluginsDir string
	// YtDlpSystemPluginsDir is the baked POT provider plugin path (also --plugin-dirs).
	YtDlpSystemPluginsDir string
	// PotProviderURL is CREATORR_POT_PROVIDER_URL (bgutil HTTP base URL). Empty disables POT.
	PotProviderURL string
	// CacheDir holds Creatorr-managed working artifacts.
	CacheDir string
	// TrustProxy enables trusting X-Forwarded-Proto for Secure session cookies (CREATORR_TRUST_PROXY).
	TrustProxy bool
}

// Load reads bootstrap env (port, sidecars) and selects path layout.
func Load() Config {
	paths := pathLayout()
	return Config{
		Host:                  "0.0.0.0",
		Port:                  getenvInt("CREATORR_PORT", 8787),
		DBPath:                paths.db,
		InitialRootFolder:     paths.library,
		ImportRoot:            paths.importRoot,
		YtDlpBin:              "", // main: PrepareManagedBin
		YtDlpPluginsDir:       paths.plugins,
		YtDlpSystemPluginsDir: paths.systemPlugins,
		FlareSolverrURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("CREATORR_FLARESOLVERR_URL")), "/"),
		PotProviderURL:        strings.TrimSpace(os.Getenv("CREATORR_POT_PROVIDER_URL")),
		CacheDir:              paths.cache,
		TrustProxy:            getenvBool("CREATORR_TRUST_PROXY"),
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
			library:       "/library",
			importRoot:    "/import",
			cache:         "/data/cache",
			plugins:       "/yt-dlp-plugins",
			systemPlugins: "/usr/local/share/yt-dlp-plugins/bgutil",
		}
	}
	return layout{
		db:            filepath.Join("var", "data", "creatorr.db"),
		library:       filepath.Join("var", "library"),
		importRoot:    filepath.Join("var", "import"),
		cache:         filepath.Join("var", "data", "cache"),
		plugins:       filepath.Join("var", "yt-dlp-plugins"),
		systemPlugins: filepath.Join("var", "yt-dlp-plugins", "bgutil"),
	}
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
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

func getenvBool(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
