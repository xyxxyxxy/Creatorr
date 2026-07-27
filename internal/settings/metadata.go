package settings

import (
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	DefaultMetadataDomainTag            = "1"
	DefaultMetadataGenresFromCategories = "1"
)

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

// MetadataDomainTagEnabled reports whether source domain tags are auto-managed.
func MetadataDomainTagEnabled(database *db.DB) (bool, error) {
	raw, err := Get(database, KeyMetadataDomainTag)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(raw) == "" {
		return NormalizeMetadataFlag(DefaultMetadataDomainTag) == "1", nil
	}
	return NormalizeMetadataFlag(raw) == "1", nil
}

// MetadataGenresFromCategoriesEnabled reports whether yt-dlp categories are auto-managed as genres.
func MetadataGenresFromCategoriesEnabled(database *db.DB) (bool, error) {
	raw, err := Get(database, KeyMetadataGenresFromCategories)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(raw) == "" {
		return NormalizeMetadataFlag(DefaultMetadataGenresFromCategories) == "1", nil
	}
	return NormalizeMetadataFlag(raw) == "1", nil
}
