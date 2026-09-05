package library

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Series art filenames (Emby/Kodi convention under the series folder).
const (
	ArtPoster    = "poster"
	ArtBanner    = "banner"
	ArtFanart    = "fanart"
	ArtClearlogo = "clearlogo"
)

var seriesArtRoles = []string{ArtPoster, ArtBanner, ArtFanart, ArtClearlogo}

// SeriesActor is one <actor> for tvshow.nfo.
type SeriesActor struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Order int    `json:"order,omitempty"`
}

// SeriesMeta is editable show metadata (plus computed premiered). Art is on disk only.
type SeriesMeta struct {
	Plot          string
	SortTitle     string
	OriginalTitle string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	Premiered     string // YYYY-MM-DD UTC day; computed, not operator-editable
}

// SeriesArtFlags reports which art files exist under the series folder.
type SeriesArtFlags struct {
	Poster    bool
	Banner    bool
	Fanart    bool
	Clearlogo bool
}

// SeriesNFO is input for WriteSeriesNFO.
type SeriesNFO struct {
	Title         string
	SortTitle     string
	OriginalTitle string
	Plot          string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	Premiered     string // YYYY-MM-DD
	Monitored     bool
}

// SeriesDirMaxRunes caps the on-disk series folder name (same as {series:100}).
const SeriesDirMaxRunes = 100

// SeriesDir returns root/sanitizeName(title) with a 100-rune cap.
func SeriesDir(root, title string) string {
	return filepath.Join(root, sanitizeName(title, SeriesDirMaxRunes))
}

// EnsureSeriesDirCapped renames an uncapped historical series folder to the capped path when needed.
func (s *Store) EnsureSeriesDirCapped(rootPath, title string) error {
	capped := SeriesDir(rootPath, title)
	uncapped := filepath.Join(rootPath, sanitizeName(title, 0))
	if filepath.Clean(capped) == filepath.Clean(uncapped) {
		return nil
	}
	if !dirExists(uncapped) {
		return nil
	}
	if dirExists(capped) || fileExists(capped) {
		return fmt.Errorf("%w: capped series folder already exists: %s", ErrConflict, capped)
	}
	if err := os.MkdirAll(filepath.Dir(capped), 0o755); err != nil {
		return err
	}
	if err := os.Rename(uncapped, capped); err != nil {
		return fmt.Errorf("rename series folder to capped name: %w", err)
	}
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET path = ? || substr(path, ?)
		WHERE path = ? OR path LIKE ?
	`, capped, len(uncapped)+1, uncapped, uncapped+string(filepath.Separator)+"%")
	if err != nil {
		return fmt.Errorf("update file paths after series dir cap: %w", err)
	}
	return nil
}

// WriteSeriesNFO writes Kodi/Emby tvshow.nfo at path.
func WriteSeriesNFO(path string, meta SeriesNFO) error {
	return os.WriteFile(path, FormatSeriesNFO(meta), 0o644)
}

// FormatSeriesNFO returns tvshow XML bytes.
func FormatSeriesNFO(meta SeriesNFO) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString("<tvshow>\n")
	b.WriteString("  <title>" + xmlEscape(meta.Title) + "</title>\n")
	if st := omitWhenEqualTitle(meta.SortTitle, meta.Title); st != "" {
		b.WriteString("  <sorttitle>" + xmlEscape(st) + "</sorttitle>\n")
	}
	if ot := omitWhenEqualTitle(meta.OriginalTitle, meta.Title); ot != "" {
		b.WriteString("  <originaltitle>" + xmlEscape(ot) + "</originaltitle>\n")
	}
	if meta.Plot != "" {
		b.WriteString("  <plot>" + xmlEscape(meta.Plot) + "</plot>\n")
	}
	if meta.Tagline != "" {
		b.WriteString("  <tagline>" + xmlEscape(meta.Tagline) + "</tagline>\n")
	}
	if meta.Studio != "" {
		b.WriteString("  <studio>" + xmlEscape(meta.Studio) + "</studio>\n")
	}
	for _, g := range meta.Genres {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		b.WriteString("  <genre>" + xmlEscape(g) + "</genre>\n")
	}
	for _, t := range meta.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		b.WriteString("  <tag>" + xmlEscape(t) + "</tag>\n")
	}
	if meta.Premiered != "" {
		day := UploadCalendarDate(meta.Premiered)
		if day == "" {
			day = meta.Premiered
		}
		b.WriteString("  <premiered>" + xmlEscape(day) + "</premiered>\n")
		if len(day) >= 4 {
			b.WriteString("  <year>" + xmlEscape(day[:4]) + "</year>\n")
		}
	}
	status := "Ended"
	if meta.Monitored {
		status = "Continuing"
	}
	b.WriteString("  <status>" + status + "</status>\n")
	if meta.Country != "" {
		b.WriteString("  <country>" + xmlEscape(meta.Country) + "</country>\n")
	}
	if meta.MPAA != "" {
		b.WriteString("  <mpaa>" + xmlEscape(meta.MPAA) + "</mpaa>\n")
	}
	if meta.UniqueIDValue != "" {
		typ := meta.UniqueIDType
		if typ == "" {
			typ = "creatorr"
		}
		fmt.Fprintf(&b, `  <uniqueid type="%s" default="true">%s</uniqueid>`+"\n",
			xmlEscape(typ), xmlEscape(meta.UniqueIDValue))
	}
	for i, a := range meta.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		order := a.Order
		if order == 0 {
			order = i
		}
		b.WriteString("  <actor>\n")
		b.WriteString("    <name>" + xmlEscape(name) + "</name>\n")
		if role := strings.TrimSpace(a.Role); role != "" {
			b.WriteString("    <role>" + xmlEscape(role) + "</role>\n")
		}
		fmt.Fprintf(&b, "    <order>%d</order>\n", order)
		b.WriteString("  </actor>\n")
	}
	b.WriteString("</tvshow>\n")
	return []byte(b.String())
}

func encodeStringSlice(ss []string) string {
	if ss == nil {
		ss = []string{}
	}
	b, _ := json.Marshal(ss)
	return string(b)
}

func decodeStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func encodeActors(actors []SeriesActor) string {
	if actors == nil {
		actors = []SeriesActor{}
	}
	b, _ := json.Marshal(actors)
	return string(b)
}

// coalesceUniqueID keeps existing NFO uniqueid when the editor does not supply one
// (uniqueid is not operator-edited in Metadata modals; Fetch/prefetch may still set it).
func coalesceUniqueID(inType, inVal, keepType, keepVal string) (string, string) {
	inType = strings.TrimSpace(inType)
	inVal = strings.TrimSpace(inVal)
	if inType == "" && inVal == "" {
		return strings.TrimSpace(keepType), strings.TrimSpace(keepVal)
	}
	if inType == "" {
		inType = strings.TrimSpace(keepType)
	}
	if inVal == "" {
		inVal = strings.TrimSpace(keepVal)
	}
	return inType, inVal
}

func decodeActors(raw string) []SeriesActor {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []SeriesActor
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ParseCSVList splits comma-separated values (trim, drop empty).
func ParseCSVList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseStringListFields builds a string list from repeated form fields (genres/tags editors).
// Duplicate values (case-insensitive) are dropped; first wins.
func ParseStringListFields(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

// ParseActorsForm parses lines "Name" or "Name|Role".
func ParseActorsForm(raw string) []SeriesActor {
	var out []SeriesActor
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, role, _ := strings.Cut(line, "|")
		name = strings.TrimSpace(name)
		role = strings.TrimSpace(role)
		if name == "" {
			continue
		}
		out = append(out, SeriesActor{Name: name, Role: role, Order: i})
	}
	return out
}

// ParseActorsFromFields builds actors from parallel name/role form fields (UI add/remove editor).
// Duplicate names (case-insensitive) are dropped; first wins.
func ParseActorsFromFields(names, roles []string) []SeriesActor {
	n := len(names)
	if len(roles) > n {
		n = len(roles)
	}
	var out []SeriesActor
	seen := map[string]struct{}{}
	for i := 0; i < n; i++ {
		name := ""
		role := ""
		if i < len(names) {
			name = strings.TrimSpace(names[i])
		}
		if i < len(roles) {
			role = strings.TrimSpace(roles[i])
		}
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, SeriesActor{Name: name, Role: role, Order: len(out)})
	}
	return out
}

// FormatActorsForm is the inverse of ParseActorsForm.
func FormatActorsForm(actors []SeriesActor) string {
	var lines []string
	for _, a := range actors {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		if strings.TrimSpace(a.Role) != "" {
			lines = append(lines, a.Name+"|"+a.Role)
		} else {
			lines = append(lines, a.Name)
		}
	}
	return strings.Join(lines, "\n")
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

// SaveSeriesMetadataParams updates editable metadata + optional art ops.
type SaveSeriesMetadataParams struct {
	Plot          string
	SortTitle     string
	OriginalTitle string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	// ArtSrc maps role → local path to copy (upload or prefetch cache). Empty = leave.
	ArtSrc map[string]string
	// ArtClear roles to delete on disk.
	ArtClear map[string]bool
}

// SaveSeriesMetadata writes DB fields, ensures series dir, applies art, writes tvshow.nfo.
func (s *Store) SaveSeriesMetadata(seriesID int64, p SaveSeriesMetadataParams) error {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	uidType, uidVal := coalesceUniqueID(p.UniqueIDType, p.UniqueIDValue, ser.Meta.UniqueIDType, ser.Meta.UniqueIDValue)
	sortTitle := omitWhenEqualTitle(p.SortTitle, ser.Title)
	origTitle := omitWhenEqualTitle(p.OriginalTitle, ser.Title)
	_, err = s.DB.SQL.Exec(`
		UPDATE series SET
		  plot = ?, sorttitle = ?, originaltitle = ?, studio = ?,
		  genres = ?, tags = ?, uniqueid_type = ?, uniqueid_value = ?,
		  actors = ?, tagline = ?, country = ?, mpaa = ?
		WHERE id = ?
	`, strings.TrimSpace(p.Plot), sortTitle, origTitle,
		strings.TrimSpace(p.Studio), encodeStringSlice(p.Genres), encodeStringSlice(p.Tags),
		uidType, uidVal,
		encodeActors(p.Actors), strings.TrimSpace(p.Tagline), strings.TrimSpace(p.Country),
		strings.TrimSpace(p.MPAA), seriesID)
	if err != nil {
		return err
	}
	dir := SeriesDir(root.Path, ser.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create series dir: %w", err)
	}
	for _, role := range seriesArtRoles {
		if p.ArtClear[role] {
			removeArtRole(dir, role)
			continue
		}
		if src := p.ArtSrc[role]; src != "" {
			if err := installArtFile(dir, role, src); err != nil {
				return fmt.Errorf("install %s: %w", role, err)
			}
		}
	}
	ser, err = s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	return s.writeSeriesNFOFor(ser, root.Path)
}

func (s *Store) writeSeriesNFOFor(ser *Series, rootPath string) error {
	dir := SeriesDir(rootPath, ser.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	premiered := ser.Meta.Premiered
	if premiered == "" {
		if day, err := s.OldestVideoUploadDay(ser.ID); err == nil {
			premiered = day
		}
	}
	return WriteSeriesNFO(filepath.Join(dir, "tvshow.nfo"), SeriesNFO{
		Title:         ser.Title,
		SortTitle:     ser.Meta.SortTitle,
		OriginalTitle: ser.Meta.OriginalTitle,
		Plot:          ser.Meta.Plot,
		Studio:        ser.Meta.Studio,
		Genres:        ser.Meta.Genres,
		Tags:          ser.Meta.Tags,
		UniqueIDType:  ser.Meta.UniqueIDType,
		UniqueIDValue: ser.Meta.UniqueIDValue,
		Actors:        ser.Meta.Actors,
		Tagline:       ser.Meta.Tagline,
		Country:       ser.Meta.Country,
		MPAA:          ser.Meta.MPAA,
		Premiered:     premiered,
		Monitored:     ser.Monitored,
	})
}

// WriteSeriesNFODisk rewrites tvshow.nfo for a series when the series folder already exists
// (or creates it). Prefer EnsureSeriesNFO for monitor/title hooks that should only touch existing dirs.
func (s *Store) WriteSeriesNFODisk(seriesID int64) error {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	return s.writeSeriesNFOFor(ser, root.Path)
}

// RewriteSeriesNFOIfPresent rewrites tvshow.nfo only when the series folder already exists.
func (s *Store) RewriteSeriesNFOIfPresent(seriesID int64) error {
	_, err := s.RewriteSeriesNFOIfChanged(seriesID)
	return err
}

// RewriteSeriesNFOIfChanged rewrites tvshow.nfo when the series folder exists and content differs.
// Returns changed=false when folder missing or bytes already match.
func (s *Store) RewriteSeriesNFOIfChanged(seriesID int64) (changed bool, err error) {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return false, err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return false, err
	}
	dir := SeriesDir(root.Path, ser.Title)
	if !dirExists(dir) {
		return false, nil
	}
	premiered := ser.Meta.Premiered
	if premiered == "" {
		if day, err := s.OldestVideoUploadDay(ser.ID); err == nil {
			premiered = day
		}
	}
	meta := SeriesNFO{
		Title:         ser.Title,
		SortTitle:     ser.Meta.SortTitle,
		OriginalTitle: ser.Meta.OriginalTitle,
		Plot:          ser.Meta.Plot,
		Studio:        ser.Meta.Studio,
		Genres:        ser.Meta.Genres,
		Tags:          ser.Meta.Tags,
		UniqueIDType:  ser.Meta.UniqueIDType,
		UniqueIDValue: ser.Meta.UniqueIDValue,
		Actors:        ser.Meta.Actors,
		Tagline:       ser.Meta.Tagline,
		Country:       ser.Meta.Country,
		MPAA:          ser.Meta.MPAA,
		Premiered:     premiered,
		Monitored:     ser.Monitored,
	}
	path := filepath.Join(dir, "tvshow.nfo")
	want := FormatSeriesNFO(meta)
	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, want) {
		return false, nil
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// OldestVideoUploadDay returns YYYY-MM-DD of the oldest non-empty upload_date, or "".
func (s *Store) OldestVideoUploadDay(seriesID int64) (string, error) {
	var raw string
	err := s.DB.SQL.QueryRow(`
		SELECT upload_date FROM videos
		WHERE series_id = ? AND upload_date IS NOT NULL AND trim(upload_date) != ''
		ORDER BY upload_date ASC LIMIT 1
	`, seriesID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	day := UploadCalendarDate(raw)
	if day == "" {
		return strings.TrimSpace(raw), nil
	}
	return day, nil
}

// RecomputeSeriesPremiered updates stored premiered from oldest video; rewrites NFO if changed.
// Returns whether premiered changed.
func (s *Store) RecomputeSeriesPremiered(seriesID int64) (changed bool, err error) {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return false, err
	}
	day, err := s.OldestVideoUploadDay(seriesID)
	if err != nil {
		return false, err
	}
	if day == ser.Meta.Premiered {
		return false, nil
	}
	if _, err := s.DB.SQL.Exec(`UPDATE series SET premiered = ? WHERE id = ?`, day, seriesID); err != nil {
		return false, err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return true, err
	}
	ser.Meta.Premiered = day
	if err := s.writeSeriesNFOFor(ser, root.Path); err != nil {
		return true, err
	}
	return true, nil
}

// SeriesHasBusyMediaTasks reports pending/running download, sponsorblock_cut, or media_verify.
func (s *Store) SeriesHasBusyMediaTasks(seriesID int64) (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks t
		LEFT JOIN videos v ON v.id = t.video_id
		WHERE t.status IN ('pending', 'running')
		  AND t.kind IN ('download', 'sponsorblock_cut', 'media_verify')
		  AND (t.series_id = ? OR v.series_id = ?)
	`, seriesID, seriesID).Scan(&n)
	return n > 0, err
}

// RewriteSeriesEpisodeNFOs rewrites episode NFOs for all videos with media in the series.
// taskID is required when any episode NFO bytes change.
func (s *Store) RewriteSeriesEpisodeNFOs(seriesID, taskID int64) (rewrote, failed int, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT f.video_id FROM files f
		JOIN videos v ON v.id = f.video_id
		WHERE v.series_id = ? AND f.kind = 'video'
		ORDER BY f.video_id
	`, seriesID)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		wrote, err := s.RewriteVideoNFO(id, taskID)
		if err != nil {
			failed++
			continue
		}
		if wrote {
			rewrote++
		}
	}
	return rewrote, failed, nil
}

func downloadURLToCache(rawURL, dest string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Creatorr/1.0)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

// PrefetchDraft is ephemeral form fill data (not persisted on series until Save).
type PrefetchDraft struct {
	Title         string            `json:"title"` // display name for Add series; not a tvshow.nfo alt title
	Plot          string            `json:"plot"`
	SortTitle     string            `json:"sorttitle"`
	OriginalTitle string            `json:"originaltitle"`
	Studio        string            `json:"studio"`
	UniqueIDType  string            `json:"uniqueid_type"`
	UniqueIDValue string            `json:"uniqueid_value"`
	Actors        []SeriesActor     `json:"actors"`
	ArtFiles      map[string]string `json:"art_files"` // role → local path under cache
	PlaylistOnly  bool              `json:"playlist_only"`
	Error         string            `json:"error,omitempty"`
}

func (s *Store) prefetchDraftPath(seriesID, taskID int64) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "series-meta", strconv.FormatInt(seriesID, 10),
		fmt.Sprintf("prefetch-%d.json", taskID))
}

func (s *Store) addSeriesDraftDir(token string) string {
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	return filepath.Join(root, "add-series", token)
}

func (s *Store) addSeriesDraftPath(token string) string {
	return filepath.Join(s.addSeriesDraftDir(token), "draft.json")
}

// WriteAddSeriesDraft stores a pre-create metadata draft under cache/add-series/{token}/.
func (s *Store) WriteAddSeriesDraft(token string, draft PrefetchDraft) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return fmt.Errorf("%w: draft token", ErrInvalid)
	}
	dir := s.addSeriesDraftDir(token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Persist art into the draft dir so temp work dirs can be removed.
	persisted := map[string]string{}
	for role, src := range draft.ArtFiles {
		if src == "" {
			continue
		}
		ext := filepath.Ext(src)
		if ext == "" {
			ext = ".jpg"
		}
		dest := filepath.Join(dir, role+ext)
		b, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			continue
		}
		persisted[role] = dest
	}
	draft.ArtFiles = persisted
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.addSeriesDraftPath(token), b, 0o644)
}

// ReadAddSeriesDraft loads a pre-create metadata draft.
func (s *Store) ReadAddSeriesDraft(token string) (PrefetchDraft, error) {
	var draft PrefetchDraft
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return draft, fmt.Errorf("%w: draft token", ErrInvalid)
	}
	b, err := os.ReadFile(s.addSeriesDraftPath(token))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// WritePrefetchDraft stores a draft JSON under cache.
func (s *Store) WritePrefetchDraft(seriesID, taskID int64, draft PrefetchDraft) error {
	path := s.prefetchDraftPath(seriesID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ReadPrefetchDraft loads a draft if present.
func (s *Store) ReadPrefetchDraft(seriesID, taskID int64) (PrefetchDraft, error) {
	var draft PrefetchDraft
	b, err := os.ReadFile(s.prefetchDraftPath(seriesID, taskID))
	if err != nil {
		return draft, err
	}
	err = json.Unmarshal(b, &draft)
	return draft, err
}

// ClearPrefetchDraft removes an ephemeral series-meta prefetch draft and its art dir
// under cache (library art is the lasting copy after Save).
func (s *Store) ClearPrefetchDraft(seriesID, taskID int64) error {
	if seriesID <= 0 || taskID <= 0 {
		return nil
	}
	draft, err := s.ReadPrefetchDraft(seriesID, taskID)
	if err == nil {
		for _, p := range draft.ArtFiles {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			_ = os.Remove(p)
		}
	}
	_ = os.Remove(s.prefetchDraftPath(seriesID, taskID))
	root := strings.TrimSpace(s.CacheDir)
	if root == "" {
		root = filepath.Join("data", "cache")
	}
	seriesCache := filepath.Join(root, "series-meta", strconv.FormatInt(seriesID, 10))
	_ = os.RemoveAll(filepath.Join(seriesCache, fmt.Sprintf("art-%d", taskID)))
	entries, err := os.ReadDir(seriesCache)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(seriesCache)
	}
	return nil
}

// ClearAddSeriesDraft removes cache/add-series/{token}/ after series create copies art into the library.
func (s *Store) ClearAddSeriesDraft(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "/") || strings.Contains(token, "..") {
		return fmt.Errorf("%w: draft token", ErrInvalid)
	}
	return os.RemoveAll(s.addSeriesDraftDir(token))
}

// MetaSuggestions lists distinct values for Metadata form datalists.
// One library-wide pool per field name across series + videos (never entity-scoped).
type MetaSuggestions struct {
	Studios    []string
	Genres     []string
	Tags       []string
	Countries  []string
	MPAAs      []string
	ActorNames []string
	ActorRoles []string
}

// DefaultMPAASuggestions are US TV Parental Guidelines seeded into the Content
// rating datalist (free-text; operators may still type any value).
var DefaultMPAASuggestions = []string{
	"TV-Y", "TV-Y7", "TV-G", "TV-PG", "TV-14", "TV-MA",
}

// ListMetaSuggestions returns sorted unique values for studio, genres, tags, country,
// mpaa, actor name, and actor role - pooled from both series and videos rows.
// Same field name ⇒ same pool whether the form is series or video Metadata.
// mpaa also includes US TV Parental Guidelines defaults (union with library values).
func (s *Store) ListMetaSuggestions() (MetaSuggestions, error) {
	var out MetaSuggestions
	studios := map[string]struct{}{}
	genres := map[string]struct{}{}
	tags := map[string]struct{}{}
	countries := map[string]struct{}{}
	mpaas := map[string]struct{}{}
	names := map[string]struct{}{}
	roles := map[string]struct{}{}

	for _, v := range DefaultMPAASuggestions {
		mpaas[v] = struct{}{}
	}

	absorb := func(studio, genresJSON, tagsJSON, country, mpaa, actorsJSON string) {
		if v := strings.TrimSpace(studio); v != "" {
			studios[v] = struct{}{}
		}
		if v := strings.TrimSpace(country); v != "" {
			countries[v] = struct{}{}
		}
		if v := strings.TrimSpace(mpaa); v != "" {
			mpaas[v] = struct{}{}
		}
		for _, g := range decodeStringSlice(genresJSON) {
			if v := strings.TrimSpace(g); v != "" {
				genres[v] = struct{}{}
			}
		}
		for _, t := range decodeStringSlice(tagsJSON) {
			if v := strings.TrimSpace(t); v != "" {
				tags[v] = struct{}{}
			}
		}
		for _, a := range decodeActors(actorsJSON) {
			if v := strings.TrimSpace(a.Name); v != "" {
				names[v] = struct{}{}
			}
			if v := strings.TrimSpace(a.Role); v != "" {
				roles[v] = struct{}{}
			}
		}
	}

	for _, q := range []string{
		`SELECT studio, genres, tags, country, mpaa, actors FROM series`,
		`SELECT studio, genres, tags, country, mpaa, actors FROM videos`,
	} {
		rows, err := s.DB.SQL.Query(q)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var studio, genresJSON, tagsJSON, country, mpaa, actorsJSON string
			if err := rows.Scan(&studio, &genresJSON, &tagsJSON, &country, &mpaa, &actorsJSON); err != nil {
				_ = rows.Close()
				return out, err
			}
			absorb(studio, genresJSON, tagsJSON, country, mpaa, actorsJSON)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return out, err
		}
	}

	out.Studios = sortedKeys(studios)
	out.Genres = sortedKeys(genres)
	out.Tags = sortedKeys(tags)
	out.Countries = sortedKeys(countries)
	out.MPAAs = sortedKeys(mpaas)
	out.ActorNames = sortedKeys(names)
	out.ActorRoles = sortedKeys(roles)
	return out, nil
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

