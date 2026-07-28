package settings

import (
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// EnvFlareSolverrURL is the process env for the FlareSolverr base URL (not a Settings key).
const EnvFlareSolverrURL = "CREATORR_FLARESOLVERR_URL"

// legacyFlareSolverrURLKey is the removed Settings key; deleted on boot.
const legacyFlareSolverrURLKey = "flare_solverr_url"

// NormalizeFlareSolverrURL trims space and strips a trailing slash.
func NormalizeFlareSolverrURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// FlareSolverrURL returns CREATORR_FLARESOLVERR_URL (may be empty).
func FlareSolverrURL() string {
	return NormalizeFlareSolverrURL(os.Getenv(EnvFlareSolverrURL))
}

// DropFlareSolverrURLSetting removes the legacy Settings row and clears Use FlareSolverr
// flags when the env URL is empty.
func DropFlareSolverrURLSetting(database *db.DB) error {
	if database == nil {
		return fmt.Errorf("database required")
	}
	if _, err := database.SQL.Exec(`DELETE FROM settings WHERE key = ?`, legacyFlareSolverrURLKey); err != nil {
		return err
	}
	if FlareSolverrURL() == "" {
		return ClearUseFlareSolverr(database)
	}
	return nil
}
