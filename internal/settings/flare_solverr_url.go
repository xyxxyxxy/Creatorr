package settings

import (
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// EnvFlareSolverrURL is the optional first-boot seed for flare_solverr_url.
const EnvFlareSolverrURL = "CREATORR_FLARESOLVERR_URL"

// NormalizeFlareSolverrURL trims space and strips a trailing slash.
func NormalizeFlareSolverrURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// FlareSolverrURL returns the Settings FlareSolverr base URL (may be empty).
func FlareSolverrURL(database *db.DB) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database required")
	}
	v, err := Get(database, KeyFlareSolverrURL)
	if err != nil {
		return "", err
	}
	return NormalizeFlareSolverrURL(v), nil
}

// MigrateFlareSolverrURLFromEnv copies CREATORR_FLARESOLVERR_URL into settings when
// the settings value is empty (one-shot bootstrap for Compose sidecar installs).
func MigrateFlareSolverrURLFromEnv(database *db.DB) error {
	if database == nil {
		return fmt.Errorf("database required")
	}
	cur, err := FlareSolverrURL(database)
	if err != nil {
		return err
	}
	if cur != "" {
		return nil
	}
	env := NormalizeFlareSolverrURL(os.Getenv(EnvFlareSolverrURL))
	if env == "" {
		return nil
	}
	return Set(database, KeyFlareSolverrURL, env)
}
