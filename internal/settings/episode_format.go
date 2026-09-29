package settings

import (
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/nametemplate"
)

// DefaultEpisodeFormat is the relative path stem (no extension) under the series folder.
const DefaultEpisodeFormat = "S{year}/S{year}E{episode:04} [{id}]"

// LegacyDefaultEpisodeFormat is the pre-year-sequential default (exact-match migrate only).
const LegacyDefaultEpisodeFormat = "S{year}/S{year}E{episode} [{id}]"

// DefaultSpecialEpisodeFormat is the stem after locked S00E{episode:04} under Specials/.
// Full stem: S00E{episode:04} [{id}]
const DefaultSpecialEpisodeFormat = "[{id}]"

// DefaultSpecialFeatureFormat is the stem under series-level <kind>/.
const DefaultSpecialFeatureFormat = "{episode:02} {title:100}"

// LockedSpecialEpisodePrefix is always prepended before special_episode_format.
const LockedSpecialEpisodePrefix = "S00E{episode:04}"

// NormalizeEpisodeFormat trims and applies the default when empty.
func NormalizeEpisodeFormat(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultEpisodeFormat
	}
	return raw
}

// NormalizeSpecialEpisodeFormat trims; empty becomes default. Stem-only (no /).
func NormalizeSpecialEpisodeFormat(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultSpecialEpisodeFormat
	}
	return raw
}

// NormalizeSpecialFeatureFormat trims; empty becomes default. Stem-only (no /).
func NormalizeSpecialFeatureFormat(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultSpecialFeatureFormat
	}
	return raw
}

// ValidateEpisodeFormat validates a stored episode_format value and rejects reserved first segments.
func ValidateEpisodeFormat(raw string) error {
	raw = NormalizeEpisodeFormat(raw)
	if err := nametemplate.Validate(raw); err != nil {
		return fmt.Errorf("episode_format: %w", err)
	}
	if err := rejectReservedEpisodeFormatPrefix(raw); err != nil {
		return err
	}
	return nil
}

// ValidateSpecialEpisodeFormat validates stem-only special_episode_format (no path separators).
func ValidateSpecialEpisodeFormat(raw string) error {
	raw = NormalizeSpecialEpisodeFormat(raw)
	if strings.ContainsAny(raw, `/\`) {
		return fmt.Errorf("special_episode_format: must be stem-only (no / or \\)")
	}
	if err := nametemplate.Validate(raw); err != nil {
		return fmt.Errorf("special_episode_format: %w", err)
	}
	return nil
}

// ValidateSpecialFeatureFormat validates stem-only special_feature_format.
func ValidateSpecialFeatureFormat(raw string) error {
	raw = NormalizeSpecialFeatureFormat(raw)
	if strings.ContainsAny(raw, `/\`) {
		return fmt.Errorf("special_feature_format: must be stem-only (no / or \\)")
	}
	if err := nametemplate.Validate(raw); err != nil {
		return fmt.Errorf("special_feature_format: %w", err)
	}
	return nil
}

func rejectReservedEpisodeFormatPrefix(format string) error {
	format = strings.ReplaceAll(strings.TrimSpace(format), `\`, "/")
	seg := ""
	for _, p := range strings.Split(format, "/") {
		p = strings.TrimSpace(p)
		if p != "" {
			seg = p
			break
		}
	}
	if seg == "" {
		return nil
	}
	// Literal reserved names (templates like S{year} are fine).
	lower := strings.ToLower(seg)
	reserved := []string{
		"specials", "season 0", "season 00",
		"extras", "shorts", "scenes", "featurettes",
		"behind the scenes", "deleted scenes", "interviews",
		"trailers", "clips", "samples", "other",
	}
	for _, r := range reserved {
		if lower == r {
			return fmt.Errorf("episode_format: first path segment %q clashes with Specials/extras folders", seg)
		}
	}
	return nil
}
