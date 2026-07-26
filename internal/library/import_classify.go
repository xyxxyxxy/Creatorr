package library

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Import file roles returned by scan.
const (
	ImportRoleVideo = "video"
	ImportRoleNFO   = "nfo"
	ImportRoleJSON  = "json"
	ImportRoleThumb = "thumb"
	ImportRoleSub   = "sub"
	ImportRoleStrm  = "strm"
	ImportRoleOther = "other"
)

var (
	thumbExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	}
	subExts = map[string]bool{
		".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true,
	}
	// Strip [remote id] from episode stems so NFO/thumb/media group together when
	// thumbs omit the bracket (e.g. S2026E031700 [id].nfo + s2026e031700-thumb.jpg).
	groupStemBracket = regexp.MustCompile(`\[.*?\]`)
)

// IsImportSidecarRole reports whether role is an attachable orphan sidecar kind.
// info.json (ImportRoleJSON) is excluded: provenance travels only with media pack/bind.
// NFO / thumb are included for attach beside packed media only when the same-stem video sits
// next to them; see ImportSidecarStemMatchesMedia.
func IsImportSidecarRole(role string) bool {
	switch role {
	case ImportRoleNFO, ImportRoleThumb, ImportRoleSub:
		return true
	default:
		return false
	}
}

// ImportSidecarStemMatchesMedia reports whether sidecar and media share a directory
// and the same normalized basename stem (brackets stripped). NFO and thumb imports require this.
func ImportSidecarStemMatchesMedia(sidecarPath, mediaPath string) bool {
	sidecarPath = strings.TrimSpace(sidecarPath)
	mediaPath = strings.TrimSpace(mediaPath)
	if sidecarPath == "" || mediaPath == "" {
		return false
	}
	sideAbs, err := filepath.Abs(sidecarPath)
	if err != nil {
		sideAbs = sidecarPath
	}
	mediaAbs, err := filepath.Abs(mediaPath)
	if err != nil {
		mediaAbs = mediaPath
	}
	if filepath.Dir(sideAbs) != filepath.Dir(mediaAbs) {
		return false
	}
	sideRole, sideStem := ClassifyImportFile(filepath.Base(sideAbs))
	if sideRole == ImportRoleOther {
		return false
	}
	mediaStem := strings.TrimSuffix(filepath.Base(mediaAbs), filepath.Ext(filepath.Base(mediaAbs)))
	return NormalizeImportGroupStem(sideStem) == NormalizeImportGroupStem(mediaStem)
}

// NormalizeImportGroupStem lowers case, strips [id] brackets, and collapses space.
// Used for Import UI stem groups and sidecar→video stem lookup.
func NormalizeImportGroupStem(stem string) string {
	stem = groupStemBracket.ReplaceAllString(stem, "")
	stem = strings.Join(strings.Fields(stem), " ")
	return strings.ToLower(strings.TrimSpace(stem))
}

// ClassifyImportFile returns the import role and the media-like stem basename
// used to match orphan sidecars to an existing video file (same directory).
func ClassifyImportFile(filename string) (role, stemBase string) {
	name := filepath.Base(filename)
	lower := strings.ToLower(name)
	ext := strings.ToLower(filepath.Ext(name))

	if mediaExts[ext] {
		return ImportRoleVideo, strings.TrimSuffix(name, filepath.Ext(name))
	}
	if ext == ".strm" {
		// Stream links must be regenerated with the current External Creatorr URL + token.
		return ImportRoleStrm, strings.TrimSuffix(name, filepath.Ext(name))
	}
	if strings.HasSuffix(lower, ".info.json") {
		base := name[:len(name)-len(".info.json")]
		// media.info.json → media stem is file with ext stripped of .info.json only
		return ImportRoleJSON, base
	}
	if ext == ".nfo" {
		return ImportRoleNFO, strings.TrimSuffix(name, filepath.Ext(name))
	}
	if subExts[ext] {
		return ImportRoleSub, strings.TrimSuffix(name, filepath.Ext(name))
	}
	if thumbExts[ext] {
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		lowerStem := strings.ToLower(stem)
		if strings.HasSuffix(lowerStem, "-thumb") {
			stem = stem[:len(stem)-len("-thumb")]
		} else if strings.HasSuffix(lowerStem, ".thumb") {
			stem = stem[:len(stem)-len(".thumb")]
		}
		return ImportRoleThumb, stem
	}
	return ImportRoleOther, strings.TrimSuffix(name, filepath.Ext(name))
}
