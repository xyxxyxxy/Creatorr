package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanImport lists untracked files under ImportRoot only. Never binds.
// Skips series-folder metadata (tvshow.nfo + poster/banner/fanart/clearlogo)
// under any discovered tvshow tree. Trees lock descendant media to a known
// series_id or a draft_key for confirm.
func (s *Store) ScanImport() (*ImportScanResult, error) {
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

	folders, locks, err := s.discoverSeriesTrees(absRoot)
	if err != nil {
		return nil, err
	}
	out := &ImportScanResult{
		ImportPath:    absRoot,
		Candidates:    []ImportCandidate{},
		SeriesFolders: folders,
	}
	inbox, err := listAllFilesUnder(absRoot)
	if err != nil {
		return nil, err
	}
	for _, path := range inbox {
		abs, aerr := filepath.Abs(path)
		if aerr != nil {
			abs = path
		}
		if isSeriesMetaUnderAnyTree(abs, locks) {
			continue
		}
		c, err := s.buildImportCandidate(path, known, videoByStem, catalog)
		if err != nil {
			return nil, err
		}
		if c != nil {
			if lock, ok := owningSeriesTree(c.Path, locks); ok {
				applySeriesTreeLock(c, lock)
			}
			out.Candidates = append(out.Candidates, *c)
		}
	}
	countMediaUnderFolders(out.Candidates, out.SeriesFolders)
	return out, nil
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

func (s *Store) buildImportCandidate(path string, known map[string]struct{}, videoByStem map[string]videoStemRef, catalog *importSuggestCatalog) (*ImportCandidate, error) {
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
	// Site/bracket/info.json id only - no synthetic hash. Empty → create assigns videos.id.
	c.SuggestedRemoteID = meta.RemoteID
	c.SuggestedRemoteIDGenerated = false
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
		c.SuggestedPackRole = s.suggestPackRoleForPath(abs, *c.SuggestedSeriesID)
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
	if c.SuggestedSeriesID != nil {
		c.SuggestedPackRole = s.suggestPackRoleForPath(abs, *c.SuggestedSeriesID)
	}
	return &c, nil
}
