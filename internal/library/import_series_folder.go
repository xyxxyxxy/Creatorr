package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ImportSeriesFolder is one tvshow.nfo tree discovered during import scan.
type ImportSeriesFolder struct {
	DraftKey   string          `json:"draft_key"`
	FolderPath string          `json:"folder_path"`
	SeriesID   *int64          `json:"series_id,omitempty"`
	Unknown    bool            `json:"unknown"`
	Title      string          `json:"title"`
	Monitored  bool            `json:"monitored"`
	Parsed     ParsedSeriesNFO `json:"parsed"`
	ArtRoles   []string        `json:"art_roles"`
	ArtPaths   []string        `json:"art_paths"`
	MediaCount int             `json:"media_count"`
}

// seriesTreeLock maps a cleaned absolute folder → tree ownership for descendant media.
type seriesTreeLock struct {
	Folder   string
	DraftKey string
	SeriesID *int64
	Unknown  bool
	Title    string
}

// discoverSeriesTrees finds directories containing tvshow.nfo under scanRoot.
// Nested trees: longer path wins for ownership of descendant files.
// Matches known series library-wide by uniqueid / title / folder basename.
func (s *Store) discoverSeriesTrees(scanRoot string) ([]ImportSeriesFolder, map[string]seriesTreeLock, error) {
	scanRoot = filepath.Clean(scanRoot)
	var nfoDirs []string
	err := filepath.WalkDir(scanRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "tvshow.nfo") {
			nfoDirs = append(nfoDirs, filepath.Clean(filepath.Dir(path)))
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(nfoDirs) == 0 {
		return nil, map[string]seriesTreeLock{}, nil
	}
	sort.Slice(nfoDirs, func(i, j int) bool {
		return len(nfoDirs[i]) > len(nfoDirs[j])
	})

	folders := make([]ImportSeriesFolder, 0, len(nfoDirs))
	locks := map[string]seriesTreeLock{}
	for _, dir := range nfoDirs {
		nfoPath := filepath.Join(dir, "tvshow.nfo")
		parsed, perr := ParseSeriesNFOFile(nfoPath)
		if perr != nil {
			parsed = ParsedSeriesNFO{Title: filepath.Base(dir), Monitored: true}
		}
		title := strings.TrimSpace(parsed.Title)
		if title == "" {
			title = filepath.Base(dir)
			parsed.Title = title
		}
		art := DiscoverSeriesFolderArt(dir)
		roles := make([]string, 0, len(art))
		paths := make([]string, 0, len(art))
		for _, role := range seriesArtRoles {
			if p := art[role]; p != "" {
				roles = append(roles, role)
				paths = append(paths, p)
			}
		}
		var seriesID *int64
		unknown := true
		if id, ok := s.matchSeriesForInboxFolder(dir, parsed); ok {
			seriesID = &id
			unknown = false
		}
		draftKey := dir
		f := ImportSeriesFolder{
			DraftKey:   draftKey,
			FolderPath: dir,
			SeriesID:   seriesID,
			Unknown:    unknown,
			Title:      title,
			Monitored:  parsed.Monitored,
			Parsed:     parsed,
			ArtRoles:   roles,
			ArtPaths:   paths,
		}
		folders = append(folders, f)
		locks[dir] = seriesTreeLock{
			Folder:   dir,
			DraftKey: draftKey,
			SeriesID: seriesID,
			Unknown:  unknown,
			Title:    title,
		}
	}
	// Stable order for API: shorter paths first (outer trees).
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].FolderPath < folders[j].FolderPath
	})
	return folders, locks, nil
}

// matchSeriesForInboxFolder matches a tvshow.nfo tree under the import folder to an
// existing series (any root) by uniqueid, title, or sanitized folder basename.
func (s *Store) matchSeriesForInboxFolder(folder string, parsed ParsedSeriesNFO) (int64, bool) {
	folder = filepath.Clean(folder)
	if uid := strings.TrimSpace(parsed.UniqueIDValue); uid != "" {
		var id int64
		err := s.DB.SQL.QueryRow(`
			SELECT id FROM series
			WHERE uniqueid_value = ? COLLATE NOCASE
			LIMIT 1
		`, uid).Scan(&id)
		if err == nil && id > 0 {
			return id, true
		}
	}
	title := strings.TrimSpace(parsed.Title)
	if title != "" {
		var id int64
		err := s.DB.SQL.QueryRow(`
			SELECT id FROM series WHERE title = ? COLLATE NOCASE LIMIT 1
		`, title).Scan(&id)
		if err == nil && id > 0 {
			return id, true
		}
	}
	base := filepath.Base(folder)
	rows, qerr := s.DB.SQL.Query(`SELECT id, title FROM series`)
	if qerr != nil {
		return 0, false
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sid int64
		var stitle string
		if rows.Scan(&sid, &stitle) != nil {
			continue
		}
		if sanitizeName(stitle, SeriesDirMaxRunes) == base {
			return sid, true
		}
	}
	return 0, false
}

// owningSeriesTree returns the innermost tvshow.nfo tree containing absPath, if any.
func owningSeriesTree(absPath string, locks map[string]seriesTreeLock) (seriesTreeLock, bool) {
	if len(locks) == 0 {
		return seriesTreeLock{}, false
	}
	dir := filepath.Clean(filepath.Dir(absPath))
	var best seriesTreeLock
	bestLen := -1
	for folder, lock := range locks {
		if dir == folder || strings.HasPrefix(dir, folder+string(filepath.Separator)) {
			if len(folder) > bestLen {
				best = lock
				bestLen = len(folder)
			}
		}
	}
	if bestLen < 0 {
		return seriesTreeLock{}, false
	}
	return best, true
}

func isSeriesMetaUnderAnyTree(abs string, locks map[string]seriesTreeLock) bool {
	if !isSeriesFolderMetaBasename(abs) {
		return false
	}
	_, ok := owningSeriesTree(abs, locks)
	return ok
}

func applySeriesTreeLock(c *ImportCandidate, lock seriesTreeLock) {
	c.SeriesFolderLocked = true
	if lock.SeriesID != nil {
		sid := *lock.SeriesID
		c.SuggestedSeriesID = &sid
		c.SeriesFolderDraftKey = ""
		if c.MatchType == "" {
			c.MatchType = "series_folder"
			c.MatchLabel = fmt.Sprintf("Locked to series folder (%s)", lock.Title)
		}
		return
	}
	c.SeriesFolderDraftKey = lock.DraftKey
	if c.MatchType == "" || c.SuggestedSeriesID == nil {
		c.MatchType = "series_folder"
		c.MatchLabel = fmt.Sprintf("Locked to series folder (%s); create series on confirm", lock.Title)
	}
}

// owningTVShowDir returns the nearest ancestor directory containing tvshow.nfo, or "".
func owningTVShowDir(abs string) string {
	abs = filepath.Clean(strings.TrimSpace(abs))
	if abs == "" || abs == "." {
		return ""
	}
	dir := abs
	if !isDir(abs) {
		dir = filepath.Dir(abs)
	}
	for {
		if fileExists(filepath.Join(dir, "tvshow.nfo")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// importSeriesLock is a tvshow.nfo ownership constraint for an import path.
type importSeriesLock struct {
	Folder   string
	SeriesID int64  // >0 when the folder matches an existing library series
	DraftKey string // set when unknown (create via import_plan); equals Folder
}

// importSeriesLockForPath reports whether abs sits under a tvshow.nfo tree and
// whether that tree matches a known series or is an unknown draft.
func (s *Store) importSeriesLockForPath(abs string) (importSeriesLock, bool, error) {
	abs = filepath.Clean(strings.TrimSpace(abs))
	folder := owningTVShowDir(abs)
	if folder == "" {
		return importSeriesLock{}, false, nil
	}
	if !s.pathUnderImportInbox(folder) {
		// Import paths must be under the inbox; library trees are not scanned.
		return importSeriesLock{}, false, nil
	}
	parsed, perr := ParseSeriesNFOFile(filepath.Join(folder, "tvshow.nfo"))
	if perr != nil {
		parsed = ParsedSeriesNFO{Title: filepath.Base(folder)}
	}
	if id, ok := s.matchSeriesForInboxFolder(folder, parsed); ok {
		return importSeriesLock{Folder: folder, SeriesID: id}, true, nil
	}
	return importSeriesLock{Folder: folder, DraftKey: folder}, true, nil
}

// assertImportPathAllowsVideo rejects binding media under a tvshow.nfo tree to the wrong series
// (or any video when the tree is an unknown draft).
func (s *Store) assertImportPathAllowsVideo(abs string, videoID int64) error {
	lock, ok, err := s.importSeriesLockForPath(abs)
	if err != nil || !ok {
		return err
	}
	if lock.DraftKey != "" {
		return fmt.Errorf("%w: path under unknown tvshow.nfo (%s); use import confirm to create the series", ErrInvalid, lock.Folder)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	if v.SeriesID != lock.SeriesID {
		return fmt.Errorf("%w: path locked to series %d by tvshow.nfo; video belongs to series %d", ErrInvalid, lock.SeriesID, v.SeriesID)
	}
	return nil
}

// assertImportPathAllowsSeries rejects create-under-series when the path's tvshow.nfo lock disagrees.
func (s *Store) assertImportPathAllowsSeries(abs string, seriesID int64) error {
	lock, ok, err := s.importSeriesLockForPath(abs)
	if err != nil || !ok {
		return err
	}
	if lock.DraftKey != "" {
		return fmt.Errorf("%w: path under unknown tvshow.nfo (%s); use import confirm to create the series", ErrInvalid, lock.Folder)
	}
	if seriesID != lock.SeriesID {
		return fmt.Errorf("%w: path locked to series %d by tvshow.nfo", ErrInvalid, lock.SeriesID)
	}
	return nil
}

// assertImportPlanJobLock ensures a plan job respects the path's tvshow.nfo ownership.
func (s *Store) assertImportPlanJobLock(j ImportPlanJob, draftKeys map[string]struct{}) error {
	path := strings.TrimSpace(j.Path)
	if path == "" && len(j.Paths) > 0 {
		path = strings.TrimSpace(j.Paths[0])
	}
	if path == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	lock, ok, err := s.importSeriesLockForPath(abs)
	if err != nil {
		return err
	}
	if !ok {
		if j.SeriesDraftKey != "" {
			return fmt.Errorf("%w: series_draft_key set but path is not under that tvshow.nfo tree", ErrInvalid)
		}
		return nil
	}
	if lock.DraftKey != "" {
		if j.VideoID > 0 {
			return fmt.Errorf("%w: path under unknown tvshow.nfo cannot bind to an existing video", ErrInvalid)
		}
		if j.SeriesID > 0 {
			return fmt.Errorf("%w: path under unknown tvshow.nfo cannot target an existing series_id", ErrInvalid)
		}
		if strings.TrimSpace(j.SeriesDraftKey) != lock.DraftKey {
			return fmt.Errorf("%w: path locked to draft %s", ErrInvalid, lock.DraftKey)
		}
		if _, ok := draftKeys[lock.DraftKey]; !ok {
			return fmt.Errorf("%w: path under unknown tvshow.nfo missing series draft", ErrInvalid)
		}
		return nil
	}
	// Known series folder.
	if j.SeriesDraftKey != "" {
		return fmt.Errorf("%w: path locked to existing series %d; series_draft_key not allowed", ErrInvalid, lock.SeriesID)
	}
	if j.VideoID > 0 {
		return s.assertImportPathAllowsVideo(abs, j.VideoID)
	}
	if j.SeriesID > 0 && j.SeriesID != lock.SeriesID {
		return fmt.Errorf("%w: path locked to series %d by tvshow.nfo", ErrInvalid, lock.SeriesID)
	}
	return nil
}

func countMediaUnderFolders(candidates []ImportCandidate, folders []ImportSeriesFolder) {
	for i := range folders {
		prefix := folders[i].FolderPath + string(filepath.Separator)
		n := 0
		for _, c := range candidates {
			if c.Role != ImportRoleVideo {
				continue
			}
			if c.Path == folders[i].FolderPath || strings.HasPrefix(c.Path, prefix) {
				n++
			}
		}
		folders[i].MediaCount = n
	}
}

// NearestImportSeriesFolder returns the closest ancestor with tvshow.nfo under ImportRoot.
func (s *Store) NearestImportSeriesFolder(abs string) string {
	abs = filepath.Clean(strings.TrimSpace(abs))
	if abs == "" || !s.pathUnderImportInbox(abs) {
		return ""
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(s.ImportRoot))
	if err != nil {
		return ""
	}
	absRoot = filepath.Clean(absRoot)
	dir := abs
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		dir = filepath.Dir(abs)
	}
	for {
		if fileExists(filepath.Join(dir, "tvshow.nfo")) {
			return dir
		}
		if dir == absRoot {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		rel, err := filepath.Rel(absRoot, parent)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		dir = parent
	}
}

// RemoveImportSeriesFolderIfDrained deletes an inbox tvshow.nfo tree when only series
// meta / empty dirs remain (no media and no other files). Never touches library roots.
func (s *Store) RemoveImportSeriesFolderIfDrained(folder string) error {
	folder = filepath.Clean(strings.TrimSpace(folder))
	if folder == "" || !s.pathUnderImportInbox(folder) {
		return nil
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(s.ImportRoot))
	if err != nil {
		return nil
	}
	absRoot = filepath.Clean(absRoot)
	if folder == absRoot {
		return nil
	}
	if !fileExists(filepath.Join(folder, "tvshow.nfo")) {
		return nil
	}
	keep, err := importSeriesFolderHasKeepers(folder)
	if err != nil || keep {
		return err
	}
	return os.RemoveAll(folder)
}

func importSeriesFolderHasKeepers(folder string) (bool, error) {
	meta := map[string]struct{}{}
	for _, p := range SeriesFolderMetaPaths(folder) {
		meta[filepath.Clean(p)] = struct{}{}
	}
	var keep bool
	err := filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		clean := filepath.Clean(path)
		if _, ok := meta[clean]; ok {
			return nil
		}
		if strings.EqualFold(d.Name(), "tvshow.nfo") {
			return nil
		}
		keep = true
		return filepath.SkipAll
	})
	if err != nil {
		return true, err
	}
	return keep, nil
}
