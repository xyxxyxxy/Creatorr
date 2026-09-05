package settings

import (
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/nametemplate"
)

// DefaultEpisodeFormat is the relative path stem (no extension) under the series folder.
const DefaultEpisodeFormat = "S{year}/S{year}E{episode} [{id}]"

// NormalizeEpisodeFormat trims and applies the default when empty.
func NormalizeEpisodeFormat(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultEpisodeFormat
	}
	return raw
}

// ValidateEpisodeFormat validates a stored episode_format value.
func ValidateEpisodeFormat(raw string) error {
	raw = NormalizeEpisodeFormat(raw)
	if err := nametemplate.Validate(raw); err != nil {
		return fmt.Errorf("episode_format: %w", err)
	}
	return nil
}
