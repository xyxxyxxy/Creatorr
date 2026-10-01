package settings

import (
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const DefaultArchiveFallback = "1"

// NormalizeMetadataFlag returns "1" or "0".
func NormalizeMetadataFlag(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "1" || s == "true" || s == "on" || s == "yes" {
		return "1"
	}
	return "0"
}

func validateMetadataFlag(value string) error {
	_ = NormalizeMetadataFlag(value)
	return nil
}

// ArchiveFallbackEnabled reports whether Web Archive download fallback is on.
func ArchiveFallbackEnabled(database *db.DB) (bool, error) {
	raw, err := Get(database, KeyArchiveFallback)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(raw) == "" {
		return NormalizeMetadataFlag(DefaultArchiveFallback) == "1", nil
	}
	return NormalizeMetadataFlag(raw) == "1", nil
}
