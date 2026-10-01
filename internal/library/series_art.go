package library

import (
	"os"
	"path/filepath"
	"strings"
)

// Series art filenames (Emby/Kodi convention under the series folder).
const (
	ArtPoster    = "poster"
	ArtBanner    = "banner"
	ArtFanart    = "fanart"
	ArtClearlogo = "clearlogo"
)

var seriesArtRoles = []string{ArtPoster, ArtBanner, ArtFanart, ArtClearlogo}

// SeriesArtFlags reports which art files exist under the series folder.
type SeriesArtFlags struct {
	Poster    bool
	Banner    bool
	Fanart    bool
	Clearlogo bool
}

func artFilename(role, ext string) string {
	if ext == "" {
		ext = ".jpg"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if role == ArtClearlogo && ext == ".jpg" {
		ext = ".png"
	}
	return role + ext
}

func findArtFile(dir, role string) string {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		p := filepath.Join(dir, role+ext)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// SeriesArtFlagsForDir reports which art files exist.
func SeriesArtFlagsForDir(dir string) SeriesArtFlags {
	return SeriesArtFlags{
		Poster:    findArtFile(dir, ArtPoster) != "",
		Banner:    findArtFile(dir, ArtBanner) != "",
		Fanart:    findArtFile(dir, ArtFanart) != "",
		Clearlogo: findArtFile(dir, ArtClearlogo) != "",
	}
}

// SeriesMetaFileRoleNFO is the list/preview role for tvshow.nfo.
const SeriesMetaFileRoleNFO = "nfo"

// SeriesMetaFile is one on-disk series folder metadata artifact (art or tvshow.nfo).
type SeriesMetaFile struct {
	Role string // poster|banner|fanart|clearlogo|nfo
	Path string
}

// ListSeriesMetaFiles returns existing show metadata files under dir (nfo then art).
func ListSeriesMetaFiles(dir string) []SeriesMetaFile {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	var out []SeriesMetaFile
	nfo := filepath.Join(dir, "tvshow.nfo")
	if fileExists(nfo) {
		out = append(out, SeriesMetaFile{Role: SeriesMetaFileRoleNFO, Path: nfo})
	}
	for _, role := range seriesArtRoles {
		if p := findArtFile(dir, role); p != "" {
			out = append(out, SeriesMetaFile{Role: role, Path: p})
		}
	}
	return out
}

// ResolveSeriesMetaFile returns the path for a metadata role under dir, or "".
func ResolveSeriesMetaFile(dir, role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case SeriesMetaFileRoleNFO:
		p := filepath.Join(dir, "tvshow.nfo")
		if fileExists(p) {
			return p
		}
		return ""
	case ArtPoster, ArtBanner, ArtFanart, ArtClearlogo:
		return findArtFile(dir, role)
	default:
		return ""
	}
}

// SeriesArtMtimes returns unix-nano mtimes for existing art files (cache-bust query values).
func SeriesArtMtimes(dir string) map[string]int64 {
	out := map[string]int64{}
	for _, role := range seriesArtRoles {
		p := findArtFile(dir, role)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		out[role] = st.ModTime().UnixNano()
	}
	return out
}

func removeArtRole(dir, role string) {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		_ = os.Remove(filepath.Join(dir, role+ext))
	}
}

func installArtFile(dir, role, src string) error {
	if src == "" || !fileExists(src) {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(src))
	if ext == "" {
		ext = ".jpg"
	}
	removeArtRole(dir, role)
	dest := filepath.Join(dir, artFilename(role, ext))
	return copyFile(src, dest)
}
