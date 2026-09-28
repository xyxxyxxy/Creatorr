package library

import (
	"path/filepath"
	"regexp"
	"strings"
)

var s00eStemRE = regexp.MustCompile(`(?i)S00E\d+`)

// DetectPackRoleFromPath infers pack_role from a media path relative to the series folder.
// seriesDir and mediaPath should be absolute or share the same base.
// Returns empty for regular episodes. Season-nested kind folders are ignored.
func DetectPackRoleFromPath(seriesDir, mediaPath string) string {
	seriesDir = filepath.Clean(seriesDir)
	mediaPath = filepath.Clean(mediaPath)
	rel, err := filepath.Rel(seriesDir, mediaPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return PackRoleRegular
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return PackRoleRegular
	}
	first := parts[0]
	stem := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
	lowerFirst := strings.ToLower(first)

	// Specials / Season 0 / Season 00 + S00E in stem → special episode.
	if lowerFirst == "specials" || lowerFirst == "season 0" || lowerFirst == "season 00" {
		if s00eStemRE.MatchString(stem) {
			return PackRoleSpecialEpisode
		}
		return PackRoleRegular
	}

	// Only direct series child kind folders (not under S{year}/).
	if len(parts) >= 2 && IsSpecialFeature(lowerFirst) {
		// parts[0]=kind, parts[1]=file → series/kind/file
		// if more than kind/file with intermediate season-like dirs, skip
		if len(parts) == 2 {
			return FeatureKind(lowerFirst)
		}
		// kind/subdir/file still under series-level kind
		if len(parts) > 2 {
			return FeatureKind(lowerFirst)
		}
	}
	return PackRoleRegular
}
