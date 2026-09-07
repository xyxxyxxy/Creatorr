package library

import (
	"database/sql"
	"path/filepath"
	"strings"
)

// PreviewApplyRenameCap is the max rename rows returned in a preview modal.
const PreviewApplyRenameCap = 500

// ApplyRenamePreviewItem is one planned disk rename for PreviewApplyEpisodeNaming.
type ApplyRenamePreviewItem struct {
	VideoID     int64
	Title       string
	SeriesTitle string
	From        string // path under root when possible
	To          string
}

// ApplyRenamePreview is a dry-run of Apply episode format for a maintenance scope.
type ApplyRenamePreview struct {
	Items        []ApplyRenamePreviewItem
	TotalChanges int
	SkippedBusy  int
	Unchanged    int
	Truncated    bool
}

// PreviewApplyEpisodeNaming simulates year reindex (no DB writes) and lists packed
// videos whose on-disk stem would move to the current root episode_format ideal.
// seriesIDs and videoIDs are mutually exclusive; both empty = whole library.
func (s *Store) PreviewApplyEpisodeNaming(seriesIDs, videoIDs []int64) (*ApplyRenamePreview, error) {
	out := &ApplyRenamePreview{}
	p := applyNamingPayload{Cursor: 0}
	switch {
	case len(videoIDs) > 0:
		p.VideoIDs = uniqInt64(videoIDs)
	case len(seriesIDs) == 1:
		p.SeriesID = seriesIDs[0]
	case len(seriesIDs) > 1:
		p.SeriesIDs = uniqInt64(seriesIDs)
	}

	q, args := buildApplyNamingQuery(p)
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type prow struct {
		ID          int64
		SeriesID    int64
		Title       string
		RemoteID    string
		Season      int
		Episode     int
		SeriesTitle string
		RootID      int64
		RootPath    string
		UploadDate  sql.NullString
		SourceURL   string
	}
	var list []prow
	yearsBySeries := map[int64]map[int]bool{}
	for rows.Next() {
		var r prow
		if err := rows.Scan(&r.ID, &r.Title, &r.RemoteID, &r.Season, &r.Episode, &r.SeriesTitle, &r.RootID, &r.RootPath, &r.UploadDate, &r.SourceURL); err != nil {
			return nil, err
		}
		_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, r.ID).Scan(&r.SeriesID)
		list = append(list, r)
		if r.UploadDate.Valid {
			if y := SeasonYearFromUpload(r.UploadDate.String); y > 0 && r.SeriesID > 0 {
				if yearsBySeries[r.SeriesID] == nil {
					yearsBySeries[r.SeriesID] = map[int]bool{}
				}
				yearsBySeries[r.SeriesID][y] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sim := map[int64]map[int]map[int64]int{}
	for seriesID, years := range yearsBySeries {
		sim[seriesID] = map[int]map[int64]int{}
		for y := range years {
			epMap, err := s.simulateSeriesYearEpisodes(seriesID, y)
			if err != nil {
				return nil, err
			}
			sim[seriesID][y] = epMap
		}
	}

	rootCfg := map[int64]NamingConfig{}
	for _, r := range list {
		wantSeason, wantEpisode := r.Season, r.Episode
		if r.UploadDate.Valid {
			if y := SeasonYearFromUpload(r.UploadDate.String); y > 0 {
				if ep, ok := sim[r.SeriesID][y][r.ID]; ok {
					wantSeason, wantEpisode = y, ep
				}
			}
		}

		primary, _, err := s.episodeFileSet(r.ID)
		if err != nil || primary == "" {
			out.Unchanged++
			continue
		}
		cfg, ok := rootCfg[r.RootID]
		if !ok {
			root, gerr := s.GetRoot(r.RootID)
			if gerr != nil {
				out.Unchanged++
				continue
			}
			cfg = NamingConfigFromRoot(root)
			rootCfg[r.RootID] = cfg
		}
		aired := ""
		if r.UploadDate.Valid {
			aired = r.UploadDate.String
		}
		domain := ""
		if strings.TrimSpace(r.SourceURL) != "" {
			domain = namingDomain(r.SourceURL)
		}
		dest, err := BuildEpisodePaths(r.RootPath, EpisodeNFO{
			SeriesTitle: r.SeriesTitle, Title: r.Title, Season: wantSeason, Episode: wantEpisode,
			Aired: aired, UniqueID: r.RemoteID, Domain: domain,
		}, cfg)
		if err != nil {
			out.Unchanged++
			continue
		}
		oldBase := strings.TrimSuffix(primary, filepath.Ext(primary))
		newBase := dest.PrimaryBase
		if filepath.Clean(oldBase) == filepath.Clean(newBase) {
			out.Unchanged++
			continue
		}

		busy, berr := s.videoBusyForRename(r.ID, 0)
		if berr != nil || busy {
			out.SkippedBusy++
			continue
		}

		out.TotalChanges++
		if len(out.Items) >= PreviewApplyRenameCap {
			out.Truncated = true
			continue
		}
		out.Items = append(out.Items, ApplyRenamePreviewItem{
			VideoID:     r.ID,
			Title:       r.Title,
			SeriesTitle: r.SeriesTitle,
			From:        displayPathUnderRoot(r.RootPath, oldBase+filepath.Ext(primary)),
			To:          displayPathUnderRoot(r.RootPath, newBase+filepath.Ext(primary)),
		})
	}
	return out, nil
}

func (s *Store) simulateSeriesYearEpisodes(seriesID int64, year int) (map[int64]int, error) {
	out := map[int64]int{}
	if seriesID <= 0 || year <= 0 {
		return out, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id
		FROM videos
		WHERE series_id = ?
		  AND upload_date IS NOT NULL AND trim(upload_date) != ''
		  AND CAST(strftime('%Y', upload_date) AS INTEGER) = ?
		ORDER BY upload_date ASC, id ASC
	`, seriesID, year)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	i := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		i++
		out[id] = i
	}
	return out, rows.Err()
}

func displayPathUnderRoot(root, abs string) string {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}
