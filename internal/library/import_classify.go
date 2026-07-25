package library

import (
	"path/filepath"
	"strings"
)

// Import file roles returned by scan.
const (
	ImportRoleVideo = "video"
	ImportRoleNFO   = "nfo"
	ImportRoleJSON  = "json"
	ImportRoleThumb = "thumb"
	ImportRoleSub   = "sub"
	ImportRoleOther = "other"
)

var (
	thumbExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	}
	subExts = map[string]bool{
		".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true,
	}
)

// IsImportSidecarRole reports whether role is an attachable sidecar kind.
func IsImportSidecarRole(role string) bool {
	switch role {
	case ImportRoleNFO, ImportRoleJSON, ImportRoleThumb, ImportRoleSub:
		return true
	default:
		return false
	}
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
