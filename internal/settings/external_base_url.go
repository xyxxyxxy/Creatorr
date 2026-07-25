package settings

import (
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// NormalizeExternalBaseURL trims space and strips a trailing slash.
func NormalizeExternalBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// ExternalBaseURL returns the Settings external Creatorr origin (may be empty).
func ExternalBaseURL(database *db.DB) (string, error) {
	if database == nil {
		return "", fmt.Errorf("database required")
	}
	v, err := Get(database, KeyExternalBaseURL)
	if err != nil {
		return "", err
	}
	return NormalizeExternalBaseURL(v), nil
}

// MigrateExternalBaseURLFromEnv copies CREATORR_PUBLIC_BASE_URL into settings when
// the settings value is empty (one-shot bootstrap for existing compose/.env installs).
func MigrateExternalBaseURLFromEnv(database *db.DB) error {
	if database == nil {
		return fmt.Errorf("database required")
	}
	cur, err := ExternalBaseURL(database)
	if err != nil {
		return err
	}
	if cur != "" {
		return nil
	}
	env := NormalizeExternalBaseURL(os.Getenv("CREATORR_PUBLIC_BASE_URL"))
	if env == "" {
		return nil
	}
	return Set(database, KeyExternalBaseURL, env)
}
