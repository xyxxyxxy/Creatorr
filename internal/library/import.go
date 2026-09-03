package library

import (
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

var (
	mediaExts = map[string]bool{
		".mkv": true, ".mp4": true, ".mov": true, ".webm": true, ".m4v": true,
		".mka": true, ".m4a": true, ".mp3": true, ".opus": true, ".ogg": true, ".flac": true,
	}
	uniqueIDTyped = regexp.MustCompile(`(?i)<uniqueid[^>]*type="([^"]*)"[^>]*>([^<]+)</uniqueid>`)
	uniqueIDAny   = regexp.MustCompile(`(?i)<uniqueid[^>]*>([^<]+)</uniqueid>`)
	bracketID     = regexp.MustCompile(`\[([^\]]+)\]`)
	stripBracket  = regexp.MustCompile(`\[.*?\]`)
	stripSE       = regexp.MustCompile(`(?i)S\d+E\d+-?\d*\s*`)
)

// ImportCandidate is one untracked file (inbox or library) with match suggestions.
type ImportCandidate struct {
	Path              string             `json:"path"`
	Filename          string             `json:"filename"`
	Source            string             `json:"source"` // inbox | library
	Role              string             `json:"role"`   // video | nfo | json | thumb | sub | other
	IDs               []ImportIDHint     `json:"ids"`
	SuggestedVideoID  *int64             `json:"suggested_video_id"`
	SuggestedSeriesID *int64             `json:"suggested_series_id"`
	SuggestedTitle    string             `json:"suggested_title,omitempty"`
	SuggestedRemoteID string             `json:"suggested_remote_id,omitempty"`
	// SuggestedRemoteIDGenerated is true when SuggestedRemoteID was derived (no id in filename/sidecars).
	SuggestedRemoteIDGenerated bool `json:"suggested_remote_id_generated,omitempty"`
	// SuggestedUploadDate is RFC3339 UTC prefill for unmatched create (sidecar, else file mtime).
	SuggestedUploadDate string `json:"suggested_upload_date,omitempty"`
	// SuggestedUploadDateFromMtime is true when SuggestedUploadDate came from file mtime (not sidecar).
	SuggestedUploadDateFromMtime bool   `json:"suggested_upload_date_from_mtime,omitempty"`
	SuggestedHandler             string `json:"suggested_handler_id,omitempty"`
	SuggestedWebpageURL          string `json:"suggested_webpage_url,omitempty"`
	MatchType                    string `json:"match_type,omitempty"`
	MatchLabel                   string `json:"match_label,omitempty"`
	VideoSuggestions             []VideoSuggestion `json:"video_suggestions"`
	SeriesSuggestions []SeriesSuggestion `json:"series_suggestions"`
}

// ImportIDHint is an extracted remote id from filename/sidecars.
type ImportIDHint struct {
	HandlerID string `json:"handler_id"`
	RemoteID  string `json:"remote_id"`
}

// VideoSuggestion is a title-similarity hit.
type VideoSuggestion struct {
	VideoID     int64   `json:"video_id"`
	SeriesID    int64   `json:"series_id"`
	Title       string  `json:"title"`
	SeriesTitle string  `json:"series_title"`
	RemoteID    string  `json:"remote_id"`
	Score       float64 `json:"score"`
}

// SeriesSuggestion is a series-title similarity hit.
type SeriesSuggestion struct {
	SeriesID int64   `json:"series_id"`
	Title    string  `json:"title"`
	Score    float64 `json:"score"`
}

// ImportScanResult is the scan response body.
type ImportScanResult struct {
	ImportPath string            `json:"import_path"`
	Candidates []ImportCandidate `json:"candidates"`
}

const (
	ImportSourceInbox   = "inbox"
	ImportSourceLibrary = "library"
)

// ScanImportInbox lists untracked files under ImportRoot only. Never binds.
func (s *Store) ScanImportInbox() (*ImportScanResult, error) {
	return s.ScanImport(0)
}

// ScanImport lists untracked files under the import inbox (rootID 0) or one
// online library root (rootID > 0). Never binds. Library scans skip Creatorr
// series-folder metadata (tvshow.nfo + poster/banner/fanart/clearlogo).
func (s *Store) ScanImport(rootID int64) (*ImportScanResult, error) {
	known, err := s.knownTrackedPaths()
	if err != nil {
		return nil, err
	}
	videoByStem, err := s.videoStemIndex()
	if err != nil {
		return nil, err
	}
	catalog, err := s.loadImportSuggestCatalog()
	if err != nil {
		return nil, err
	}

	if rootID > 0 {
		return s.scanImportLibraryRoot(rootID, known, videoByStem, catalog)
	}
	return s.scanImportInboxOnly(known, videoByStem, catalog)
}

func (s *Store) scanImportInboxOnly(known map[string]struct{}, videoByStem map[string]videoStemRef, catalog *importSuggestCatalog) (*ImportScanResult, error) {
	root := strings.TrimSpace(s.ImportRoot)
	if root == "" {
		return nil, fmt.Errorf("%w: import root not configured", ErrInvalid)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	out := &ImportScanResult{ImportPath: absRoot, Candidates: []ImportCandidate{}}
	inbox, err := listAllFilesUnder(absRoot)
	if err != nil {
		return nil, err
	}
	for _, path := range inbox {
		c, err := s.buildImportCandidate(path, ImportSourceInbox, known, videoByStem, catalog)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out.Candidates = append(out.Candidates, *c)
		}
	}
	return out, nil
}

func (s *Store) scanImportLibraryRoot(rootID int64, known map[string]struct{}, videoByStem map[string]videoStemRef, catalog *importSuggestCatalog) (*ImportScanResult, error) {
	root, err := s.GetRoot(rootID)
	if err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: library root path", ErrInvalid)
	}
	if !rootOnline(absRoot) {
		return nil, fmt.Errorf("%w: library root offline", ErrInvalid)
	}
	importAbs := ""
	if ir := strings.TrimSpace(s.ImportRoot); ir != "" {
		importAbs, _ = filepath.Abs(ir)
	}
	if importAbs != "" && absRoot == importAbs {
		return nil, fmt.Errorf("%w: use inbox scan for the import folder", ErrInvalid)
	}

	out := &ImportScanResult{ImportPath: absRoot, Candidates: []ImportCandidate{}}
	seriesDirs, err := s.seriesDirsForRoot(rootID, absRoot)
	if err != nil {
		return nil, err
	}
	found, err := listAllFilesUnder(absRoot)
	if err != nil {
		return nil, err
	}
	for _, path := range found {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if _, ok := known[abs]; ok {
			continue
		}
		if isSeriesFolderMetaPath(abs, seriesDirs) {
			continue
		}
		c, err := s.buildImportCandidate(abs, ImportSourceLibrary, known, videoByStem, catalog)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out.Candidates = append(out.Candidates, *c)
		}
	}
	return out, nil
}

func listAllFilesUnder(absRoot string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// seriesDirsForRoot returns cleaned SeriesDir paths for every series on rootID.
func (s *Store) seriesDirsForRoot(rootID int64, absRoot string) (map[string]struct{}, error) {
	rows, err := s.DB.SQL.Query(`SELECT title FROM series WHERE root_id = ?`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]struct{}{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		out[filepath.Clean(SeriesDir(absRoot, title))] = struct{}{}
	}
	return out, rows.Err()
}

// isSeriesFolderMetaBasename reports Creatorr-managed show metadata basenames
// (tvshow.nfo + poster/banner/fanart/clearlogo) that live under SeriesDir only
// and are not tracked in the files table.
func isSeriesFolderMetaBasename(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	if base == "tvshow.nfo" {
		return true
	}
	for _, role := range seriesArtRoles {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if base == role+ext {
				return true
			}
		}
	}
	return false
}

func isSeriesFolderMetaPath(abs string, seriesDirs map[string]struct{}) bool {
	if len(seriesDirs) == 0 || !isSeriesFolderMetaBasename(abs) {
		return false
	}
	_, ok := seriesDirs[filepath.Clean(filepath.Dir(abs))]
	return ok
}

func (s *Store) knownTrackedPaths() (map[string]struct{}, error) {
	known := map[string]struct{}{}
	rows, err := s.DB.SQL.Query(`SELECT path FROM files`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if abs, err := filepath.Abs(p); err == nil {
			known[abs] = struct{}{}
		} else {
			known[p] = struct{}{}
		}
	}
	return known, rows.Err()
}

// videoStemKey maps dir + media stem basename → video metadata for sidecar matching.
type videoStemRef struct {
	VideoID     int64
	SeriesID    int64
	Title       string
	SeriesTitle string
}

func videoStemKey(dir, stemBase string) string {
	return filepath.Clean(dir) + "\x00" + NormalizeImportGroupStem(stemBase)
}

func (s *Store) videoStemIndex() (map[string]videoStemRef, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT f.path, v.id, v.series_id, v.title, s.title
		FROM files f
		JOIN videos v ON v.id = f.video_id
		JOIN series s ON s.id = v.series_id
		WHERE f.kind = 'video'
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]videoStemRef{}
	for rows.Next() {
		var path, title, seriesTitle string
		var videoID, seriesID int64
		if err := rows.Scan(&path, &videoID, &seriesID, &title, &seriesTitle); err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		stem := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
		out[videoStemKey(filepath.Dir(abs), stem)] = videoStemRef{
			VideoID: videoID, SeriesID: seriesID, Title: title, SeriesTitle: seriesTitle,
		}
	}
	return out, rows.Err()
}

func (s *Store) buildImportCandidate(path, source string, known map[string]struct{}, videoByStem map[string]videoStemRef, catalog *importSuggestCatalog) (*ImportCandidate, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if _, ok := known[abs]; ok {
		return nil, nil
	}
	role, stemBase := ClassifyImportFile(filepath.Base(abs))
	c := ImportCandidate{
		Path:              abs,
		Filename:          filepath.Base(abs),
		Source:            source,
		Role:              role,
		IDs:               []ImportIDHint{},
		VideoSuggestions:  []VideoSuggestion{},
		SeriesSuggestions: []SeriesSuggestion{},
	}

	if IsImportSidecarRole(role) {
		if ref, ok := videoByStem[videoStemKey(filepath.Dir(abs), stemBase)]; ok {
			vid, sid := ref.VideoID, ref.SeriesID
			c.SuggestedVideoID = &vid
			c.SuggestedSeriesID = &sid
			c.MatchType = "sidecar_stem"
			c.MatchLabel = fmt.Sprintf("Matched by filename stem (%s) to %s / %s", role, ref.SeriesTitle, ref.Title)
			c.VideoSuggestions = []VideoSuggestion{{
				VideoID: ref.VideoID, SeriesID: ref.SeriesID,
				Title: ref.Title, SeriesTitle: ref.SeriesTitle, Score: 1,
			}}
		}
		// Orphan NFO/thumb without a same-stem sibling video is list-only (needs
		// that video beside it, either untracked in a media group or already packed).
		return &c, nil
	}

	// info.json is download-time provenance: list only; never stem-match or attach alone.
	if role == ImportRoleJSON || role == ImportRoleOther {
		return &c, nil
	}

	// video media: existing id / title / series matching
	c.IDs = extractImportIDs(abs)
	meta := readImportMeta(abs, c.IDs)
	c.SuggestedTitle = meta.Title
	c.SuggestedRemoteID = meta.RemoteID
	if c.SuggestedRemoteID == "" {
		c.SuggestedRemoteID = deriveImportRemoteID(abs)
		c.SuggestedRemoteIDGenerated = true
	}
	c.SuggestedUploadDate = meta.UploadDate
	if c.SuggestedUploadDate == "" {
		c.SuggestedUploadDate = fileModTimeUploadDate(abs)
		c.SuggestedUploadDateFromMtime = c.SuggestedUploadDate != ""
	}
	c.SuggestedHandler = meta.HandlerID
	c.SuggestedWebpageURL = meta.WebpageURL
	if catalog == nil {
		catalog = &importSuggestCatalog{byRemote: map[string]importRemoteHit{}}
	}
	for _, hint := range c.IDs {
		hit, ok := catalog.byRemote[hint.RemoteID]
		if !ok {
			continue
		}
		vid, seriesID := hit.VideoID, hit.SeriesID
		c.SuggestedVideoID = &vid
		c.SuggestedSeriesID = &seriesID
		c.MatchType = "id"
		c.MatchLabel = fmt.Sprintf("Matched by remote ID to %s / %s", hit.SeriesTitle, hit.Title)
		break
	}
	// ID match is enough for auto-match; skip O(videos) title scans.
	if c.SuggestedVideoID != nil {
		return &c, nil
	}
	c.VideoSuggestions = titleSuggestionsFrom(catalog.videos, stemBase, 8)
	c.SeriesSuggestions = seriesSuggestionsFrom(catalog.series, abs, 6)
	if c.SuggestedVideoID == nil && len(c.VideoSuggestions) > 0 {
		top := c.VideoSuggestions[0]
		c.SuggestedVideoID = &top.VideoID
		c.SuggestedSeriesID = &top.SeriesID
		c.MatchType = "title"
		c.MatchLabel = fmt.Sprintf("Matched by title (%.0f%%) to %s / %s", top.Score*100, top.SeriesTitle, top.Title)
	} else if c.SuggestedSeriesID == nil && len(c.SeriesSuggestions) > 0 {
		top := c.SeriesSuggestions[0]
		c.SuggestedSeriesID = &top.SeriesID
		c.MatchType = "series_title"
		c.MatchLabel = fmt.Sprintf("Series match (%.0f%%): %s - pick a video", top.Score*100, top.Title)
	}
	return &c, nil
}

// ImportPickerVideo is a dropdown row for the Import UI.
type ImportPickerVideo struct {
	ID          int64  `json:"id"`
	SeriesID    int64  `json:"series_id"`
	Title       string `json:"title"`
	SeriesTitle string `json:"series_title"`
	Status      string `json:"status"`
	HasMedia    bool   `json:"has_media"` // true when a kind=video files row exists
	HasThumb    bool   `json:"has_thumb"` // true when a kind=thumb files row exists
}

// ListImportPickerVideos returns all indexed videos for Import dropdowns.
func (s *Store) ListImportPickerVideos() ([]ImportPickerVideo, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, v.title, s.title, v.status,
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video') AS has_media,
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'thumb') AS has_thumb
		FROM videos v
		JOIN series s ON s.id = v.series_id
		ORDER BY s.title COLLATE NOCASE, v.title COLLATE NOCASE, v.id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ImportPickerVideo
	for rows.Next() {
		var v ImportPickerVideo
		var hasMedia, hasThumb int
		if err := rows.Scan(&v.ID, &v.SeriesID, &v.Title, &v.SeriesTitle, &v.Status, &hasMedia, &hasThumb); err != nil {
			return nil, err
		}
		v.HasMedia = hasMedia != 0
		v.HasThumb = hasThumb != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateImportVideoParams creates an indexed video for an unmatched import.
type CreateImportVideoParams struct {
	SeriesID    int64
	Title       string
	RemoteID    string // empty → derived from path/sidecars
	HandlerID   string // site hint for history only (not a DB column)
	WebpageURL  string
	UploadDate  string // required after merge (RFC3339 UTC; sidecars / UI date-only adapted)
	Description string
	Verify      bool // enqueue media_verify after pack/bind
}

// EnqueueImportCreate creates a new video under seriesID from path metadata, then enqueues import.
func (s *Store) EnqueueImportCreate(path string, p CreateImportVideoParams) (taskID, videoID int64, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, 0, fmt.Errorf("%w: path required", ErrInvalid)
	}
	if p.SeriesID <= 0 {
		return 0, 0, fmt.Errorf("%w: series_id required", ErrInvalid)
	}
	if _, err := s.GetSeries(p.SeriesID, false); err != nil {
		return 0, 0, err
	}
	abs, _, err := s.ValidateImportSourcePath(path)
	if err != nil {
		return 0, 0, err
	}
	hints := extractImportIDs(abs)
	meta := readImportMeta(abs, hints)
	if strings.TrimSpace(p.Title) != "" {
		meta.Title = strings.TrimSpace(p.Title)
	}
	if strings.TrimSpace(p.RemoteID) != "" {
		meta.RemoteID = strings.TrimSpace(p.RemoteID)
	}
	if strings.TrimSpace(p.HandlerID) != "" {
		meta.HandlerID = strings.TrimSpace(p.HandlerID)
	}
	if strings.TrimSpace(p.WebpageURL) != "" {
		meta.WebpageURL = strings.TrimSpace(p.WebpageURL)
	}
	if strings.TrimSpace(p.UploadDate) != "" {
		meta.UploadDate = sidecarUploadTime(p.UploadDate)
	}
	if meta.UploadDate == "" {
		meta.UploadDate = fileModTimeUploadDate(abs)
	}
	if meta.UploadDate == "" {
		return 0, 0, fmt.Errorf("%w: upload_date required for unmatched import", ErrInvalid)
	}
	if strings.TrimSpace(p.Description) != "" {
		meta.Description = strings.TrimSpace(p.Description)
	}
	if meta.Title == "" {
		return 0, 0, fmt.Errorf("%w: title required for unmatched import", ErrInvalid)
	}
	if meta.RemoteID == "" {
		meta.RemoteID = deriveImportRemoteID(abs)
	}
	if meta.HandlerID == "" {
		meta.HandlerID = "yt-dlp"
	}
	season, episode, err := s.AssignSeasonEpisode(p.SeriesID, meta.UploadDate, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	var uploadVal, webpage, seasonVal, episodeVal any
	if meta.UploadDate != "" {
		uploadVal = meta.UploadDate
	}
	// Pre-insert assign only seeds when dated; reindex after insert for correct day-index.
	if meta.UploadDate != "" {
		seasonVal = season
		episodeVal = episode
	}
	if meta.WebpageURL != "" {
		webpage = meta.WebpageURL
	}
	var res sql.Result
	res, err = s.DB.SQL.Exec(`
		INSERT INTO videos (
		  series_id, source_id, remote_id, title, upload_date,
		  source_url, status, season, episode, description, thumbnail_url
		) VALUES (?, NULL, ?, ?, ?, ?, 'wanted', ?, ?, ?, NULL)
	`, p.SeriesID, meta.RemoteID, meta.Title, uploadVal, webpage, seasonVal, episodeVal, meta.Description)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return 0, 0, fmt.Errorf("%w: video with this remote_id already exists in series", ErrConflict)
		}
		return 0, 0, err
	}
	videoID, _ = res.LastInsertId()
	taskID, err = s.EnqueueImport(abs, videoID, p.Verify, false)
	if err != nil {
		// Best-effort cleanup so a failed enqueue does not leave an orphan wanted row.
		_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
		return 0, 0, err
	}
	if meta.UploadDate != "" {
		changed, rerr := s.ReindexSeriesUTCDay(p.SeriesID, UploadCalendarDate(meta.UploadDate))
		if rerr != nil {
			_ = s.Queue.Cancel(taskID)
			_, _ = s.DB.SQL.Exec(`DELETE FROM videos WHERE id = ?`, videoID)
			return 0, 0, rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, taskID)
	}
	_ = s.AddVideoHistory(videoID, "import_created", "Created from unmatched import", map[string]any{
		"path":      abs,
		"remote_id": meta.RemoteID,
		"site":      meta.HandlerID,
	}, taskID)
	return taskID, videoID, nil
}

type importMeta struct {
	Title       string
	RemoteID    string
	HandlerID   string
	WebpageURL  string
	UploadDate  string
	Description string
}

func readImportMeta(path string, hints []ImportIDHint) importMeta {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m := importMeta{
		Title: cleanStem(stem),
	}
	if m.Title == "" {
		m.Title = stem
	}
	if len(hints) > 0 {
		m.RemoteID = hints[0].RemoteID
		if hints[0].HandlerID != "" && hints[0].HandlerID != "unknown" {
			m.HandlerID = hints[0].HandlerID
		}
	}
	for _, cand := range []string{
		strings.TrimSuffix(path, filepath.Ext(path)) + ".info.json",
		path + ".info.json",
	} {
		b, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(b, &data) != nil {
			continue
		}
		if t, ok := data["title"].(string); ok && strings.TrimSpace(t) != "" {
			m.Title = strings.TrimSpace(t)
		}
		if d, ok := data["description"].(string); ok {
			m.Description = d
		}
		if u, ok := data["webpage_url"].(string); ok && u != "" {
			m.WebpageURL = u
		} else if u, ok := data["original_url"].(string); ok && u != "" {
			m.WebpageURL = u
		}
		switch v := data["upload_date"].(type) {
		case string:
			m.UploadDate = sidecarUploadTime(v)
		}
		if id, ok := data["id"].(string); ok && id != "" && m.RemoteID == "" {
			m.RemoteID = id
		}
		if ek, ok := data["extractor_key"].(string); ok && ek != "" {
			m.HandlerID = normalizeImportHandler(ek)
		} else if ek, ok := data["extractor"].(string); ok && ek != "" {
			m.HandlerID = normalizeImportHandler(ek)
		}
		break
	}
	nfo := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if b, err := os.ReadFile(nfo); err == nil {
		text := string(b)
		if m.Title == "" || m.Title == cleanStem(stem) {
			if tm := regexp.MustCompile(`(?i)<title>([^<]+)</title>`).FindStringSubmatch(text); len(tm) == 2 {
				if t := strings.TrimSpace(tm[1]); t != "" {
					m.Title = t
				}
			}
		}
		if m.UploadDate == "" {
			if dm := regexp.MustCompile(`(?i)<aired>([^<]+)</aired>`).FindStringSubmatch(text); len(dm) == 2 {
				m.UploadDate = sidecarUploadTime(strings.TrimSpace(dm[1]))
			}
		}
	}
	return m
}

func normalizeImportHandler(extractor string) string {
	e := strings.ToLower(strings.TrimSpace(extractor))
	switch e {
	case "youtube", "youtu", "ytdlp", "yt-dlp":
		return "yt-dlp"
	default:
		return e
	}
}

// sidecarUploadTime adapts yt-dlp info.json / NFO date fields to RFC3339 UTC for storage.
// Handler protocol is RFC3339-only; this is only for on-disk sidecar formats.
func sidecarUploadTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, ok := ParseUploadTime(s); ok {
		return t.UTC().Format(time.RFC3339)
	}
	// Form / datetime-local style (UTC, no zone suffix).
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	compact := strings.ReplaceAll(s, "-", "")
	if len(compact) >= 8 {
		if t, err := time.ParseInLocation("20060102", compact[:8], time.UTC); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

// uploadFormHasTime reports whether a Metadata upload_date form value includes a clock time.
// Date-only YYYY-MM-DD / YYYYMMDD → false; datetime-local / RFC3339 → true.
func uploadFormHasTime(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if len(raw) == 10 && raw[4] == '-' && raw[7] == '-' {
		return false
	}
	compact := strings.ReplaceAll(raw, "-", "")
	if len(compact) == 8 && !strings.ContainsAny(raw, "T :") {
		return false
	}
	return true
}

// UploadFormParts splits a stored upload_date for the Metadata date+time join (UTC).
// Midnight → day only with empty clock (time optional). Non-midnight → HH:MM.
func UploadFormParts(raw string) (day, clock string) {
	t, ok := ParseUploadTime(raw)
	if !ok {
		return "", ""
	}
	t = t.UTC()
	day = t.Format("2006-01-02")
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return day, ""
	}
	return day, t.Format("15:04")
}

// CombineUploadFormDateTime joins Metadata date + optional time fields into a value
// for SaveVideoMetadata (YYYY-MM-DD or YYYY-MM-DDTHH:MM). Empty day clears.
func CombineUploadFormDateTime(day, clock string) string {
	day = strings.TrimSpace(day)
	clock = strings.TrimSpace(clock)
	if day == "" {
		return ""
	}
	if clock == "" {
		return day
	}
	return day + "T" + clock
}

// UploadFormValue formats a stored upload_date for display/tests (UTC).
// Midnight → YYYY-MM-DD; otherwise YYYY-MM-DDTHH:MM.
func UploadFormValue(raw string) string {
	day, clock := UploadFormParts(raw)
	return CombineUploadFormDateTime(day, clock)
}

func deriveImportRemoteID(path string) string {
	base := filepath.Base(path)
	sum := sha1.Sum([]byte(base))
	return fmt.Sprintf("import-%x", sum[:6])
}

// fileModTimeUploadDate returns the file's modification time as RFC3339 UTC (creation
// time is not portable across filesystems; mtime is the fallback when sidecars omit date).
func fileModTimeUploadDate(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().UTC().Format(time.RFC3339)
}

// EnqueueImport queues an import task that installs media into the series folder
// (inbox) or binds a library orphan in place. Sidecar paths attach to a video that
// already has media (in-place files row update). When verify is true, the import
// task enqueues media_verify after a successful pack/bind (ignores profile mature gate).
// When replace is true and the video already has packed media, existing library
// media (and companion sidecars) are removed during the import task.
func (s *Store) EnqueueImport(path string, videoID int64, verify, replace bool) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("%w: path required", ErrInvalid)
	}
	abs, _, err := s.ValidateImportAnyPath(path)
	if err != nil {
		return 0, err
	}
	role, _ := ClassifyImportFile(filepath.Base(abs))
	if role == ImportRoleJSON {
		return 0, fmt.Errorf("%w: info.json cannot be imported alone (provenance travels with media)", ErrInvalid)
	}
	if IsImportSidecarRole(role) {
		return s.EnqueueAttachSidecars(videoID, []string{abs})
	}
	if role != ImportRoleVideo {
		return 0, fmt.Errorf("%w: only media or sidecar files can be imported", ErrInvalid)
	}
	abs, inPlace, err := s.ValidateImportSourcePath(path)
	if err != nil {
		return 0, err
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	hasMedia := false
	if _, ok, err := s.HasVideoFile(videoID); err != nil {
		return 0, err
	} else if ok {
		if !replace {
			return 0, fmt.Errorf("%w: video already has a file on disk", ErrConflict)
		}
		hasMedia = true
	}
	busy, err := s.hasPendingImport(videoID, abs)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: import already queued", ErrConflict)
	}
	msg := fmt.Sprintf("Import %s", filepath.Base(abs))
	if inPlace {
		msg = fmt.Sprintf("Bind library file %s", filepath.Base(abs))
	}
	if hasMedia {
		msg = fmt.Sprintf("Replace %s", filepath.Base(abs))
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindImport,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Payload: map[string]any{
			"path": abs, "video_id": videoID, "in_place": inPlace,
			"verify": verify, "replace": hasMedia,
		},
		Message: msg,
	})
}

// EnqueueAttachSidecars queues a task that attaches orphan sidecar paths to a
// video that already has media. Inbox paths are moved beside the media on run;
// library orphans must already sit beside the media file.
func (s *Store) EnqueueAttachSidecars(videoID int64, paths []string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if len(paths) == 0 {
		return 0, fmt.Errorf("%w: paths required", ErrInvalid)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	mediaPath, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("%w: video has no media file to attach sidecars to", ErrInvalid)
	}
	absPaths := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		abs, _, err := s.ValidateImportAnyPath(p)
		if err != nil {
			return 0, err
		}
		role, _ := ClassifyImportFile(filepath.Base(abs))
		if role == ImportRoleJSON {
			return 0, fmt.Errorf("%w: info.json cannot be attached alone (provenance travels with media)", ErrInvalid)
		}
		if !IsImportSidecarRole(role) {
			return 0, fmt.Errorf("%w: %s is not a sidecar", ErrInvalid, filepath.Base(abs))
		}
		if role == ImportRoleNFO && !ImportSidecarStemMatchesMedia(abs, mediaPath) {
			return 0, fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
		}
		if role == ImportRoleThumb && !ImportSidecarStemMatchesMedia(abs, mediaPath) {
			return 0, fmt.Errorf("%w: thumbnail import requires a same-basename video file beside it", ErrInvalid)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		absPaths = append(absPaths, abs)
	}
	if len(absPaths) == 0 {
		return 0, fmt.Errorf("%w: paths required", ErrInvalid)
	}
	busy, err := s.hasPendingImport(videoID, absPaths[0])
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: import already queued", ErrConflict)
	}
	msg := fmt.Sprintf("Attach %d sidecar(s)", len(absPaths))
	if len(absPaths) == 1 {
		msg = fmt.Sprintf("Attach sidecar %s", filepath.Base(absPaths[0]))
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindImport,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Payload: map[string]any{
			"mode": "sidecars", "paths": absPaths, "video_id": videoID, "in_place": true,
		},
		Message: msg,
	})
}

// AttachSidecarFiles registers sidecar paths on a video that already has media.
// Paths already beside the media file are registered in place. Inbox paths
// (under ImportRoot) are moved beside the media first (subs keep language
// suffix; thumbs become `{stem}-thumb{ext}`). Library orphans in another folder
// are rejected. NFO is data import only (same-stem beside media); source XML is
// not kept. Rejects info.json (provenance packs only with media).
func (s *Store) AttachSidecarFiles(videoID int64, paths []string, taskID int64) error {
	mediaPath, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: video has no media file", ErrInvalid)
	}
	mediaAbs, err := filepath.Abs(mediaPath)
	if err != nil {
		mediaAbs = mediaPath
	}
	mediaDir := filepath.Dir(mediaAbs)
	mediaStem := strings.TrimSuffix(filepath.Base(mediaAbs), filepath.Ext(mediaAbs))

	type item struct {
		kind string
		path string
	}
	var items []item
	var nfoPaths []string
	for _, p := range paths {
		abs, err := filepath.Abs(strings.TrimSpace(p))
		if err != nil {
			abs = strings.TrimSpace(p)
		}
		if !fileExists(abs) {
			return fmt.Errorf("%w: sidecar not found: %s", ErrNotFound, abs)
		}
		role, _ := ClassifyImportFile(filepath.Base(abs))
		if role == ImportRoleJSON {
			return fmt.Errorf("%w: info.json cannot be attached alone (provenance travels with media)", ErrInvalid)
		}
		if filepath.Dir(abs) != mediaDir {
			if !s.pathUnderImportInbox(abs) {
				return fmt.Errorf("%w: sidecar must be beside the video media file", ErrInvalid)
			}
			if role == ImportRoleNFO {
				return fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
			}
			dest, derr := inboxSidecarLibraryDest(abs, mediaDir, mediaStem, role)
			if derr != nil {
				return derr
			}
			if DestinationOccupied(dest, nil) {
				return fmt.Errorf("%w: destination exists: %s", ErrInvalid, filepath.Base(dest))
			}
			if err := moveFile(abs, dest); err != nil {
				return fmt.Errorf("move sidecar into library: %w", err)
			}
			abs = dest
		}
		if role == ImportRoleNFO {
			if !ImportSidecarStemMatchesMedia(abs, mediaAbs) {
				return fmt.Errorf("%w: .nfo data import requires a same-basename video file beside it", ErrInvalid)
			}
			nfoPaths = append(nfoPaths, abs)
			continue
		}
		if role == ImportRoleThumb && !ImportSidecarStemMatchesMedia(abs, mediaAbs) {
			return fmt.Errorf("%w: thumbnail import requires a same-basename video file beside it", ErrInvalid)
		}
		if !IsImportSidecarRole(role) {
			return fmt.Errorf("%w: not a sidecar: %s", ErrInvalid, filepath.Base(abs))
		}
		items = append(items, item{kind: role, path: abs})
	}
	if len(items) == 0 && len(nfoPaths) == 0 {
		return fmt.Errorf("%w: paths required", ErrInvalid)
	}

	for _, nfo := range nfoPaths {
		if err := s.ApplyImportNFO(videoID, nfo, taskID); err != nil {
			return err
		}
	}
	if len(items) == 0 {
		return nil
	}

	acquired := nowRFC3339()
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	attached := make([]string, 0, len(items))
	for _, it := range items {
		if it.kind != ImportRoleSub {
			if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind = ?`, videoID, it.kind); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind = 'sub' AND path = ?`, videoID, it.path); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, ?, ?, NULL)
		`, videoID, it.path, it.kind, acquired); err != nil {
			return err
		}
		attached = append(attached, it.kind+":"+filepath.Base(it.path))
	}
	detail, _ := json.Marshal(map[string]any{"paths": attached})
	if taskID <= 0 {
		return fmt.Errorf("%w: task_id required for sidecar_attach history", ErrInvalid)
	}
	if _, err := tx.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'sidecar_attach', ?, ?, ?)
	`, videoID, acquired, fmt.Sprintf("Attached %d sidecar(s)", len(items)), string(detail), taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// pathUnderImportInbox reports whether abs lives under ImportRoot (inbox move source).
func (s *Store) pathUnderImportInbox(abs string) bool {
	root := strings.TrimSpace(s.ImportRoot)
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// inboxSidecarLibraryDest builds the library path for an inbox sub/thumb beside media.
func inboxSidecarLibraryDest(src, mediaDir, mediaStem, role string) (string, error) {
	switch role {
	case ImportRoleSub:
		srcStem := guessSubtitleWorkStem(src)
		suffix := SubtitleLangAndExt(src, srcStem)
		if suffix == "" {
			suffix = filepath.Ext(src)
		}
		if suffix == "" {
			return "", fmt.Errorf("%w: subtitle has no extension", ErrInvalid)
		}
		return filepath.Join(mediaDir, mediaStem+suffix), nil
	case ImportRoleThumb:
		ext := strings.ToLower(filepath.Ext(src))
		if ext == "" {
			ext = ".jpg"
		}
		return filepath.Join(mediaDir, mediaStem+"-thumb"+ext), nil
	default:
		return "", fmt.Errorf("%w: cannot move %s from inbox", ErrInvalid, role)
	}
}

// ValidateImportPath ensures path is an existing media file under ImportRoot.
func (s *Store) ValidateImportPath(path string) (string, error) {
	abs, inPlace, err := s.ValidateImportSourcePath(path)
	if err != nil {
		return "", err
	}
	if inPlace {
		return "", fmt.Errorf("%w: path must be under import root", ErrInvalid)
	}
	return abs, nil
}

// ValidateImportAnyPath accepts any file under the import inbox or an online library root.
func (s *Store) ValidateImportAnyPath(path string) (abs string, inPlace bool, err error) {
	abs, err = filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return "", false, fmt.Errorf("%w: file not found", ErrNotFound)
	}

	if root := strings.TrimSpace(s.ImportRoot); root != "" {
		absRoot, rerr := filepath.Abs(root)
		if rerr == nil {
			rel, rerr := filepath.Rel(absRoot, abs)
			if rerr == nil && !strings.HasPrefix(rel, "..") {
				return abs, false, nil
			}
		}
	}

	roots, err := s.ListRoots()
	if err != nil {
		return "", false, err
	}
	for _, r := range roots {
		absRoot, rerr := filepath.Abs(r.Path)
		if rerr != nil || !rootOnline(absRoot) {
			continue
		}
		rel, rerr := filepath.Rel(absRoot, abs)
		if rerr == nil && !strings.HasPrefix(rel, "..") {
			return abs, true, nil
		}
	}
	return "", false, fmt.Errorf("%w: path must be under import root or a library root", ErrInvalid)
}

// ValidateImportSourcePath accepts media under the import inbox or an online library root.
// inPlace is true when the file already lives under a library root (bind without move).
func (s *Store) ValidateImportSourcePath(path string) (abs string, inPlace bool, err error) {
	abs, inPlace, err = s.ValidateImportAnyPath(path)
	if err != nil {
		return "", false, err
	}
	if !mediaExts[strings.ToLower(filepath.Ext(abs))] {
		return "", false, fmt.Errorf("%w: unsupported media extension", ErrInvalid)
	}
	return abs, inPlace, nil
}

func (s *Store) hasPendingImport(videoID int64, path string) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE kind = ? AND status IN ('pending','running')
		  AND (video_id = ? OR instr(payload, ?) > 0)
	`, queue.KindImport, videoID, path).Scan(&n)
	return n > 0, err
}

type importRemoteHit struct {
	VideoID     int64
	SeriesID    int64
	Title       string
	SeriesTitle string
	HasMedia    bool
}

// importSuggestCatalog is loaded once per ScanImport so title/id matching is O(files×catalog)
// in memory instead of re-querying SQLite for every media file.
type importSuggestCatalog struct {
	videos   []VideoSuggestion
	series   []SeriesSuggestion
	byRemote map[string]importRemoteHit
}

func (s *Store) loadImportSuggestCatalog() (*importSuggestCatalog, error) {
	cat := &importSuggestCatalog{byRemote: map[string]importRemoteHit{}}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, v.title, v.remote_id, s.title,
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video') AS has_media
		FROM videos v
		JOIN series s ON s.id = v.series_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sug VideoSuggestion
		var hasMedia bool
		if err := rows.Scan(&sug.VideoID, &sug.SeriesID, &sug.Title, &sug.RemoteID, &sug.SeriesTitle, &hasMedia); err != nil {
			return nil, err
		}
		cat.videos = append(cat.videos, sug)
		rid := strings.TrimSpace(sug.RemoteID)
		if rid == "" {
			continue
		}
		hit := importRemoteHit{
			VideoID: sug.VideoID, SeriesID: sug.SeriesID,
			Title: sug.Title, SeriesTitle: sug.SeriesTitle, HasMedia: hasMedia,
		}
		prev, ok := cat.byRemote[rid]
		if !ok || (!hit.HasMedia && prev.HasMedia) || (hit.HasMedia == prev.HasMedia && hit.VideoID < prev.VideoID) {
			cat.byRemote[rid] = hit
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	srows, err := s.DB.SQL.Query(`SELECT id, title FROM series ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srows.Close() }()
	for srows.Next() {
		var sug SeriesSuggestion
		if err := srows.Scan(&sug.SeriesID, &sug.Title); err != nil {
			return nil, err
		}
		cat.series = append(cat.series, sug)
	}
	return cat, srows.Err()
}

func titleSuggestionsFrom(videos []VideoSuggestion, stem string, limit int) []VideoSuggestion {
	clean := cleanStem(stem)
	type scored struct {
		score float64
		v     VideoSuggestion
	}
	var hits []scored
	for _, sug := range videos {
		ratio := seqRatio(clean, sug.Title)
		if ratio >= 0.45 {
			sug.Score = round3(ratio)
			hits = append(hits, scored{ratio, sug})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]VideoSuggestion, len(hits))
	for i, h := range hits {
		out[i] = h.v
	}
	return out
}

func seriesSuggestionsFrom(series []SeriesSuggestion, mediaPath string, limit int) []SeriesSuggestion {
	needles := []string{
		filepath.Base(filepath.Dir(mediaPath)),
		cleanStem(strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))),
	}
	type scored struct {
		score float64
		s     SeriesSuggestion
	}
	var hits []scored
	for _, sug := range series {
		best := 0.0
		for _, needle := range needles {
			if needle == "" || needle == "." || needle == ".." {
				continue
			}
			if r := seqRatio(needle, sug.Title); r > best {
				best = r
			}
		}
		if best >= 0.5 {
			copySug := sug
			copySug.Score = round3(best)
			hits = append(hits, scored{best, copySug})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SeriesSuggestion, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}

func extractImportIDs(path string) []ImportIDHint {
	// Priority for ID match: filename [id], then info.json, then NFO uniqueid.
	var found []ImportIDHint
	for _, m := range bracketID.FindAllStringSubmatch(filepath.Base(path), -1) {
		found = append(found, ImportIDHint{HandlerID: "yt-dlp", RemoteID: strings.TrimSpace(m[1])})
	}
	for _, cand := range []string{
		strings.TrimSuffix(path, filepath.Ext(path)) + ".info.json",
		path + ".info.json",
	} {
		b, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(b, &data) != nil {
			continue
		}
		id := ""
		switch v := data["id"].(type) {
		case string:
			id = v
		case float64:
			id = fmt.Sprintf("%.0f", v)
		}
		if id == "" {
			continue
		}
		extractor := ""
		if v, ok := data["extractor_key"].(string); ok && v != "" {
			extractor = strings.ToLower(v)
		} else if v, ok := data["extractor"].(string); ok && v != "" {
			extractor = strings.ToLower(v)
		}
		handler := normalizeImportHandler(extractor)
		if extractor == "" {
			// yt-dlp info.json without extractor_key - treat as catch-all id.
			handler = "yt-dlp"
		}
		found = append(found, ImportIDHint{HandlerID: handler, RemoteID: id})
	}
	nfo := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	if b, err := os.ReadFile(nfo); err == nil {
		text := string(b)
		for _, m := range uniqueIDTyped.FindAllStringSubmatch(text, -1) {
			found = append(found, ImportIDHint{HandlerID: strings.ToLower(m[1]), RemoteID: strings.TrimSpace(m[2])})
		}
		for _, m := range uniqueIDAny.FindAllStringSubmatch(text, -1) {
			found = append(found, ImportIDHint{HandlerID: "unknown", RemoteID: strings.TrimSpace(m[1])})
		}
	}
	seen := map[string]bool{}
	var out []ImportIDHint
	for _, h := range found {
		if h.RemoteID == "" {
			continue
		}
		key := h.RemoteID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

func cleanStem(stem string) string {
	clean := stripBracket.ReplaceAllString(stem, "")
	clean = stripSE.ReplaceAllString(clean, "")
	return strings.TrimSpace(clean)
}

func seqRatio(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		if a == "" {
			return 0
		}
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	l := lcsLen(ra, rb)
	return 2 * float64(l) / float64(len(ra)+len(rb))
}

func lcsLen(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}
