package library

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	epyear "github.com/xyxxyxxy/Creatorr/internal/library/episode"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
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
func SeasonYearFromUpload(upload string) int {
	return epyear.YearFromUpload(upload)
}

// SeasonYearFromCalendarDay returns UTC year for YYYYMMDD (empty/invalid -> 0).
func SeasonYearFromCalendarDay(dayYYYYMMDD string) int {
	return epyear.YearFromCalendarDay(dayYYYYMMDD)
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

// AssignPackNumbers returns season/episode for pack using the video's special_feature bucket.
// Regulars use year reindex; Specials/features reindex their role bucket.
func (s *Store) AssignPackNumbers(v *Video, upload string, _ int64) (season, episode int, err error) {
	if v == nil {
		return 0, 0, fmt.Errorf("video required")
	}
	role := NormalizePackRole(v.PackRole)
	upload = NormalizeUploadTime(upload)
	if upload == "" && v.UploadDate.Valid {
		upload = NormalizeUploadTime(v.UploadDate.String)
	}
	if IsSpecialEpisode(role) || IsSpecialFeature(role) {
		if _, err := s.ReindexPackRoleBucket(v.SeriesID, role); err != nil {
			return 0, 0, err
		}
		fresh, gerr := s.GetVideo(v.ID)
		if gerr != nil {
			return 0, 0, gerr
		}
		if fresh.Season.Valid {
			season = int(fresh.Season.Int64)
		}
		if fresh.Episode.Valid {
			episode = int(fresh.Episode.Int64)
		}
		return season, episode, nil
	}
	if upload == "" {
		if v.Season.Valid {
			season = int(v.Season.Int64)
		}
		if v.Episode.Valid {
			episode = int(v.Episode.Int64)
		}
		return season, episode, nil
	}
	return s.AssignSeasonEpisode(v.SeriesID, upload, 0, v.ID)
}

type yearPeer struct {
	ID         int64
	UploadDate string
	Season     sql.NullInt64
	Episode    sql.NullInt64
	Status     string
}

// packedVideoIDs filters video IDs to those with packed media (downloaded / integrity_check_failed).
func (s *Store) packedVideoIDs(videoIDs []int64) ([]int64, error) {
	ids := uniqInt64(videoIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id FROM videos WHERE id IN (` + sqlIntPlaceholders(len(ids)) + `)
		AND status IN ('downloaded', 'integrity_check_failed')`
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
	if errors.Is(err, queue.ErrDuplicate) {
		// series_move open: its final ApplySeriesEpisodeNaming covers these peers.
		return 0, false, nil
	}
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
