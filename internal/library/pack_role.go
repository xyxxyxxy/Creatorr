package library

import (
	"fmt"
	"strings"
)

// Pack role values stored in videos.special_feature.
// Capitalized Episode/Specials layouts are NFO-parsed; other values are special-feature kinds.
const (
	PackRoleRegular        = "episode"
	PackRoleSpecialEpisode = "special_episode"
)

// FeatureKindFolders are series-level extras folder names (exact).
var FeatureKindFolders = []string{
	"extras",
	"shorts",
	"scenes",
	"featurettes",
	"behind the scenes",
	"deleted scenes",
	"interviews",
	"trailers",
	"clips",
	"samples",
	"other",
}

var featureKindSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(FeatureKindFolders))
	for _, k := range FeatureKindFolders {
		m[k] = struct{}{}
	}
	return m
}()

// NormalizePackRole maps empty/NULL legacy values and "episode" to PackRoleRegular; lowercases feature kinds.
func NormalizePackRole(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == PackRoleRegular {
		return PackRoleRegular
	}
	if raw == PackRoleSpecialEpisode {
		return PackRoleSpecialEpisode
	}
	lower := strings.ToLower(raw)
	if _, ok := featureKindSet[lower]; ok {
		return lower
	}
	return raw
}

// PackRoleDBValue returns the SQLite value for videos.special_feature / sources.special_feature.
// Regular episodes are SQL NULL (not the literal "episode").
func PackRoleDBValue(role string) any {
	r := strings.TrimSpace(role)
	if r == "" || NormalizePackRole(r) == PackRoleRegular {
		return nil
	}
	return NormalizePackRole(r)
}

// ValidatePackRole accepts episode, special_episode, or a known feature kind.
func ValidatePackRole(raw string) error {
	r := NormalizePackRole(raw)
	if r == PackRoleRegular || r == PackRoleSpecialEpisode {
		return nil
	}
	if _, ok := featureKindSet[r]; ok {
		return nil
	}
	return fmt.Errorf("invalid special_feature %q", raw)
}

// IsSpecialEpisode reports Season 00 Special packing.
func IsSpecialEpisode(packRole string) bool {
	return NormalizePackRole(packRole) == PackRoleSpecialEpisode
}

// IsSpecialFeature reports series-level extras packing.
func IsSpecialFeature(packRole string) bool {
	r := NormalizePackRole(packRole)
	if r == PackRoleRegular || r == PackRoleSpecialEpisode {
		return false
	}
	_, ok := featureKindSet[r]
	return ok
}

// FeatureKind returns the extras folder name when packRole is a feature kind.
func FeatureKind(packRole string) string {
	r := NormalizePackRole(packRole)
	if _, ok := featureKindSet[r]; ok {
		return r
	}
	return ""
}

// PackRoleKindFolder is the {kind} / pack folder name.
// Capitalized Episode and Specials are NFO-parsed layouts; lowercase feature kinds are special features.
// episode → "Episode"; special_episode → "Specials"; feature → kind folder name.
func PackRoleKindFolder(packRole string) string {
	r := NormalizePackRole(packRole)
	switch {
	case r == PackRoleRegular:
		return "Episode"
	case r == PackRoleSpecialEpisode:
		return "Specials"
	case IsSpecialFeature(r):
		return r
	default:
		return "Episode"
	}
}

// ReservedSeriesFolderNames are first-segment names regular episode_format must not use.
func ReservedSeriesFolderNames() []string {
	out := make([]string, 0, len(FeatureKindFolders)+3)
	out = append(out, "Specials", "Season 0", "Season 00")
	out = append(out, FeatureKindFolders...)
	return out
}

// IsReservedSeriesFolder reports whether name (case-insensitive) is Specials/Season 0/00 or a feature kind.
func IsReservedSeriesFolder(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	lower := strings.ToLower(n)
	if lower == "specials" || lower == "season 0" || lower == "season 00" {
		return true
	}
	_, ok := featureKindSet[lower]
	return ok
}

// PackRoleSelectOptions returns Metadata dropdown options for non-regular roles.
func PackRoleSelectOptions() []struct{ Value, Label string } {
	out := []struct{ Value, Label string }{
		{PackRoleSpecialEpisode, "Special episode"},
	}
	for _, k := range FeatureKindFolders {
		out = append(out, struct{ Value, Label string }{k, "Feature: " + k})
	}
	return out
}

// IsSpecialPackRole reports Special episode or a feature kind (not regular episode).
func IsSpecialPackRole(packRole string) bool {
	return NormalizePackRole(packRole) != PackRoleRegular
}

// PackRoleBadgeLabel is a short UI badge for a video row.
func PackRoleBadgeLabel(packRole string) string {
	role := NormalizePackRole(packRole)
	switch {
	case role == PackRoleSpecialEpisode:
		return "Special"
	case IsSpecialFeature(role):
		return role
	default:
		return ""
	}
}

// FirstPathSegment returns the first non-empty /-separated segment of a relative format.
func FirstPathSegment(format string) string {
	format = strings.ReplaceAll(strings.TrimSpace(format), `\`, "/")
	for _, p := range strings.Split(format, "/") {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return ""
}

// SQLPackRoleRegularPred is an SQL fragment matching regular (NULL/empty/legacy episode) videos.
const SQLPackRoleRegularPred = `(COALESCE(special_feature,'') = '' OR special_feature = 'episode')`
