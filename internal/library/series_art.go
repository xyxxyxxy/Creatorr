package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
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

// seriesArtLookupDirs returns folders to search for show art. The current series
// dir is first; while a series_move is open, old and new dirs from the payload
// are included so UI/art serving survive the DB-update / folder-rename window.
func (s *Store) seriesArtLookupDirs(ser *Series) []string {
	if ser == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var dirs []string
	add := func(rootPath, title string) {
		title = strings.TrimSpace(title)
		if rootPath == "" || title == "" {
			return
		}
		for _, dir := range []string{
			SeriesDir(rootPath, title),
			filepath.Join(rootPath, sanitizeName(title, 0)),
		} {
			dir = filepath.Clean(dir)
			if _, ok := seen[dir]; ok {
				continue
			}
			seen[dir] = struct{}{}
			dirs = append(dirs, dir)
		}
	}
	if root, err := s.GetRoot(ser.RootID); err == nil {
		add(root.Path, ser.Title)
	}
	if p, ok := s.openSeriesMovePayload(ser.ID); ok {
		if root, err := s.GetRoot(p.OldRootID); err == nil {
			add(root.Path, p.OldTitle)
		}
		if root, err := s.GetRoot(p.NewRootID); err == nil {
			add(root.Path, p.NewTitle)
		}
	}
	return dirs
}

func (s *Store) openSeriesMovePayload(seriesID int64) (seriesMovePayload, bool) {
	if s.Queue == nil || seriesID <= 0 {
		return seriesMovePayload{}, false
	}
	var raw string
	err := s.DB.SQL.QueryRow(`
		SELECT payload FROM tasks
		WHERE kind = ? AND series_id = ? AND status IN (?, ?)
		ORDER BY id DESC LIMIT 1
	`, queue.KindSeriesMove, seriesID, queue.StatusPending, queue.StatusRunning).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return seriesMovePayload{}, false
	}
	var p seriesMovePayload
	if json.Unmarshal([]byte(raw), &p) != nil {
		return seriesMovePayload{}, false
	}
	return p, true
}

// FindSeriesArtFile returns the on-disk path for a series art role, or "".
func (s *Store) FindSeriesArtFile(ser *Series, role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case ArtPoster, ArtBanner, ArtFanart, ArtClearlogo:
	default:
		return ""
	}
	for _, dir := range s.seriesArtLookupDirs(ser) {
		if p := findArtFile(dir, role); p != "" {
			return p
		}
	}
	return ""
}

// SeriesArtFlagsFor reports which art files exist for the series (move-aware).
func (s *Store) SeriesArtFlagsFor(ser *Series) SeriesArtFlags {
	return SeriesArtFlags{
		Poster:    s.FindSeriesArtFile(ser, ArtPoster) != "",
		Banner:    s.FindSeriesArtFile(ser, ArtBanner) != "",
		Fanart:    s.FindSeriesArtFile(ser, ArtFanart) != "",
		Clearlogo: s.FindSeriesArtFile(ser, ArtClearlogo) != "",
	}
}

// SeriesArtMtimesFor returns unix-nano mtimes for existing art (move-aware).
func (s *Store) SeriesArtMtimesFor(ser *Series) map[string]int64 {
	out := map[string]int64{}
	for _, role := range seriesArtRoles {
		p := s.FindSeriesArtFile(ser, role)
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
