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
// rootID > 0 enables SeriesDir / uniqueid / title match against that library root.
func (s *Store) discoverSeriesTrees(scanRoot string, rootID int64) ([]ImportSeriesFolder, map[string]seriesTreeLock, error) {
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

	var dirBySeriesID map[string]int64
	if rootID > 0 {
		dirBySeriesID, err = s.seriesDirIDsForRoot(rootID, scanRoot)
		if err != nil {
			return nil, nil, err
		}
	}

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
		if rootID > 0 {
			if id, ok := dirBySeriesID[dir]; ok {
				seriesID = &id
				unknown = false
			} else if id, ok := s.matchSeriesForFolder(rootID, scanRoot, dir, parsed); ok {
				seriesID = &id
				unknown = false
			}
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

func (s *Store) seriesDirIDsForRoot(rootID int64, absRoot string) (map[string]int64, error) {
	rows, err := s.DB.SQL.Query(`SELECT id, title FROM series WHERE root_id = ?`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[filepath.Clean(SeriesDir(absRoot, title))] = id
	}
	return out, rows.Err()
}

func (s *Store) matchSeriesForFolder(rootID int64, absRoot, folder string, parsed ParsedSeriesNFO) (int64, bool) {
	folder = filepath.Clean(folder)
	if uid := strings.TrimSpace(parsed.UniqueIDValue); uid != "" {
		var id int64
		err := s.DB.SQL.QueryRow(`
			SELECT id FROM series
			WHERE root_id = ? AND uniqueid_value = ? COLLATE NOCASE
			LIMIT 1
		`, rootID, uid).Scan(&id)
		if err == nil && id > 0 {
			return id, true
		}
	}
	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		return 0, false
	}
	if filepath.Clean(SeriesDir(absRoot, title)) == folder {
		var id int64
		err := s.DB.SQL.QueryRow(`
			SELECT id FROM series WHERE root_id = ? AND title = ? COLLATE NOCASE LIMIT 1
		`, rootID, title).Scan(&id)
		if err == nil && id > 0 {
			return id, true
		}
	}
	base := filepath.Base(folder)
	rows, qerr := s.DB.SQL.Query(`SELECT id, title FROM series WHERE root_id = ?`, rootID)
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
// whether that tree is a known series on a library root or an unknown draft.
func (s *Store) importSeriesLockForPath(abs string) (importSeriesLock, bool, error) {
	abs = filepath.Clean(strings.TrimSpace(abs))
	folder := owningTVShowDir(abs)
	if folder == "" {
		return importSeriesLock{}, false, nil
	}
	parsed, perr := ParseSeriesNFOFile(filepath.Join(folder, "tvshow.nfo"))
	if perr != nil {
		parsed = ParsedSeriesNFO{Title: filepath.Base(folder)}
	}
	roots, err := s.ListRoots()
	if err != nil {
		return importSeriesLock{}, false, err
	}
	for _, root := range roots {
		absRoot, aerr := filepath.Abs(root.Path)
		if aerr != nil {
			absRoot = filepath.Clean(root.Path)
		}
		if folder != absRoot && !strings.HasPrefix(folder, absRoot+string(filepath.Separator)) {
			continue
		}
		dirByID, err := s.seriesDirIDsForRoot(root.ID, absRoot)
		if err != nil {
			return importSeriesLock{}, false, err
		}
		if id, ok := dirByID[folder]; ok {
			return importSeriesLock{Folder: folder, SeriesID: id}, true, nil
		}
		if id, ok := s.matchSeriesForFolder(root.ID, absRoot, folder, parsed); ok {
			return importSeriesLock{Folder: folder, SeriesID: id}, true, nil
		}
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
