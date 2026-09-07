package library

import (
	"database/sql"
	"strings"
	"sync"
)

// seriesRenameMu serializes two-phase peer renames per series (single-process).
var seriesRenameMu sync.Map // seriesID int64 -> *sync.Mutex

func lockSeriesRename(seriesID int64) func() {
	v, _ := seriesRenameMu.LoadOrStore(seriesID, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// SeasonYearFromUpload returns the UTC calendar year used as {year} (year-season, e.g. 2026).
// Undated upload returns 0.
func SeasonYearFromUpload(upload string) int {
	t, ok := ParseUploadTime(upload)
	if !ok {
		return 0
	}
	return t.UTC().Year()
}

// SeasonYearFromCalendarDay parses YYYY-MM-DD as UTC and returns the year, or 0.
func SeasonYearFromCalendarDay(dayYYYYMMDD string) int {
	dayYYYYMMDD = strings.TrimSpace(dayYYYYMMDD)
	if dayYYYYMMDD == "" {
		return 0
	}
	t, ok := ParseUploadTime(dayYYYYMMDD + "T00:00:00Z")
	if !ok {
		return 0
	}
	return t.UTC().Year()
}

// AssignSeasonEpisode assigns season/episode for a dated video after ensuring it is in the DB
// (videoID > 0). Reindexes the whole UTC calendar year and returns this video's numbers.
// Undated upload returns season=0, episode=0 without reindexing.
func (s *Store) AssignSeasonEpisode(seriesID int64, upload string, _ int, videoID int64) (season, episode int, err error) {
	upload = NormalizeUploadTime(upload)
	if upload == "" && videoID > 0 {
		var dbUpload sql.NullString
		if err := s.DB.SQL.QueryRow(`SELECT upload_date FROM videos WHERE id = ?`, videoID).Scan(&dbUpload); err != nil {
			return 0, 0, err
		}
		if dbUpload.Valid {
			upload = NormalizeUploadTime(dbUpload.String)
		}
	}
	if upload == "" {
		return 0, 0, nil
	}
	if videoID <= 0 {
		// Pre-insert provisional: year + episode 1; callers insert then Reindex.
		t, ok := ParseUploadTime(upload)
		if !ok {
			return 0, 0, nil
		}
		return t.UTC().Year(), 1, nil
	}
	year := SeasonYearFromUpload(upload)
	if year == 0 {
		return 0, 0, nil
	}
	if _, err := s.ReindexSeriesUTCYear(seriesID, year); err != nil {
		return 0, 0, err
	}
	var se, ep sql.NullInt64
	err = s.DB.SQL.QueryRow(`SELECT season, episode FROM videos WHERE id = ?`, videoID).Scan(&se, &ep)
	if err != nil {
		return 0, 0, err
	}
	if se.Valid {
		season = int(se.Int64)
	}
	if ep.Valid {
		episode = int(ep.Int64)
	}
	return season, episode, nil
}

type yearPeer struct {
	ID         int64
	UploadDate string
	Season     sql.NullInt64
	Episode    sql.NullInt64
	Status     string
}

// ReindexSeriesUTCYear sets season/episode for all dated series videos in the UTC year.
// Order: upload_date ASC, id ASC. Episode is 1-based. Returns video IDs whose numbers changed.
func (s *Store) ReindexSeriesUTCYear(seriesID int64, year int) (changed []int64, err error) {
	if seriesID == 0 || year <= 0 {
		return nil, nil
	}

	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode, status
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

	var peers []yearPeer
	for rows.Next() {
		var p yearPeer
		if err := rows.Scan(&p.ID, &p.UploadDate, &p.Season, &p.Episode, &p.Status); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, p := range peers {
		wantEp := i + 1
		curSe, curEp := 0, 0
		if p.Season.Valid {
			curSe = int(p.Season.Int64)
		}
		if p.Episode.Valid {
			curEp = int(p.Episode.Int64)
		}
		if curSe == year && curEp == wantEp {
			continue
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET season = ?, episode = ? WHERE id = ?`, year, wantEp, p.ID); err != nil {
			return changed, err
		}
		changed = append(changed, p.ID)
	}
	return changed, nil
}

// packedVideoIDs filters video IDs to those with packed media (downloaded / verify_failed).
func (s *Store) packedVideoIDs(videoIDs []int64) ([]int64, error) {
	ids := uniqInt64(videoIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id FROM videos WHERE id IN (` + sqlIntPlaceholders(len(ids)) + `)
		AND status IN ('downloaded', 'verify_failed')`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EnqueueApplyForPackedEpisodeChanges queues scoped Apply when reindex shifted packed peers.
// No-ops when none packed, queue unavailable, or Apply already covers them.
func (s *Store) EnqueueApplyForPackedEpisodeChanges(changed []int64) (taskID int64, queued bool, err error) {
	packed, err := s.packedVideoIDs(changed)
	if err != nil || len(packed) == 0 {
		return 0, false, err
	}
	if s.Queue == nil {
		return 0, false, nil
	}
	if s.renameEpisodesCoversVideos(packed) {
		return 0, false, nil
	}
	tid, err := s.EnqueueRenameEpisodesVideos(packed)
	if err != nil {
		return 0, false, err
	}
	return tid, true, nil
}

// repackEpisodeNumberChanges renames on-disk episode sets + rewrites NFO for packed videos
// whose season/episode changed. Skips busy download/pack tasks. taskID links renamed history.
func (s *Store) repackEpisodeNumberChanges(videoIDs []int64, taskID int64) error {
	if len(videoIDs) == 0 {
		return nil
	}
	// Group by series for mutex.
	bySeries := map[int64][]int64{}
	for _, videoID := range uniqInt64(videoIDs) {
		var seriesID int64
		if err := s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, videoID).Scan(&seriesID); err != nil {
			continue
		}
		bySeries[seriesID] = append(bySeries[seriesID], videoID)
	}
	var leftovers []int64
	for seriesID, ids := range bySeries {
		unlock := lockSeriesRename(seriesID)
		_, failed := s.twoPhaseRepackEpisodeNumbers(ids, taskID)
		unlock()
		leftovers = append(leftovers, failed...)
	}
	if len(leftovers) > 0 {
		_ = s.notifyPeerMoveNeedsApply(leftovers)
	}
	return nil
}
