package library

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// SeasonYearFromUpload returns the UTC calendar year used as {year} (year-season, e.g. 2026).
// Undated upload returns 0.
func SeasonYearFromUpload(upload string) int {
	t, ok := ParseUploadTime(upload)
	if !ok {
		return 0
	}
	return t.UTC().Year()
}

// EpisodeMMDDIndex encodes month, day-of-month, and 0-based same-day index as MMDDII.
func EpisodeMMDDIndex(month, day, dayIndex int) int {
	return month*10000 + day*100 + dayIndex
}

// AssignSeasonEpisode assigns season/episode for a dated video after ensuring it is in the DB
// (videoID > 0). Reindexes the whole UTC calendar day and returns this video's numbers.
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
		// Pre-insert: only valid when no same-day peers yet; callers should insert then Reindex.
		t, ok := ParseUploadTime(upload)
		if !ok {
			return 0, 0, nil
		}
		t = t.UTC()
		return t.Year(), EpisodeMMDDIndex(int(t.Month()), t.Day(), 0), nil
	}
	day := UploadCalendarDate(upload)
	if _, err := s.ReindexSeriesUTCDay(seriesID, day); err != nil {
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

type dayPeer struct {
	ID         int64
	UploadDate string
	Season     sql.NullInt64
	Episode    sql.NullInt64
	Status     string
}

// ReindexSeriesUTCDay sets season/episode for all series videos on the given UTC calendar day
// (YYYY-MM-DD). Order: upload_date ASC, id ASC. Returns video IDs whose episode (or season) changed.
func (s *Store) ReindexSeriesUTCDay(seriesID int64, dayYYYYMMDD string) (changed []int64, err error) {
	dayYYYYMMDD = strings.TrimSpace(dayYYYYMMDD)
	if dayYYYYMMDD == "" || seriesID == 0 {
		return nil, nil
	}
	dayStart, err := time.ParseInLocation("2006-01-02", dayYYYYMMDD, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("reindex day: %w", err)
	}
	year := dayStart.Year()
	month := int(dayStart.Month())
	dom := dayStart.Day()

	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode, status
		FROM videos
		WHERE series_id = ?
		  AND upload_date IS NOT NULL AND trim(upload_date) != ''
		  AND date(upload_date) = date(?)
		ORDER BY upload_date ASC, id ASC
	`, seriesID, dayYYYYMMDD)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var peers []dayPeer
	for rows.Next() {
		var p dayPeer
		if err := rows.Scan(&p.ID, &p.UploadDate, &p.Season, &p.Episode, &p.Status); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, p := range peers {
		wantEp := EpisodeMMDDIndex(month, dom, i)
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

// repackEpisodeNumberChanges renames on-disk episode sets + rewrites NFO for packed videos
// whose season/episode changed. Skips busy download/pack tasks. taskID links renamed history.
func (s *Store) repackEpisodeNumberChanges(videoIDs []int64, taskID int64) error {
	if len(videoIDs) == 0 {
		return nil
	}
	cfg := LoadNamingConfig(s.DB)
	for _, videoID := range videoIDs {
		if err := s.repackOneEpisodeNumbers(videoID, cfg, taskID); err != nil {
			// Best-effort: continue other videos
			continue
		}
	}
	return nil
}

func (s *Store) repackOneEpisodeNumbers(videoID int64, cfg NamingConfig, taskID int64) error {
	busy, err := s.videoBusyForRename(videoID, taskID)
	if err != nil || busy {
		return err
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	if v.Status != "downloaded" && v.Status != "verify_failed" {
		return nil
	}
	// Undated / cleared index rows leave season/episode NULL → year-season 0 (S0000).
	season, episode := 0, 0
	if v.Season.Valid {
		season = int(v.Season.Int64)
	}
	if v.Episode.Valid {
		episode = int(v.Episode.Int64)
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	aired := ""
	if v.UploadDate.Valid {
		aired = v.UploadDate.String
	}
	domain := ""
	if v.SourceURL.Valid {
		domain = namingDomain(v.SourceURL.String)
	}
	ok, _, fail := s.renameVideoEpisodeSet(taskID, videoID, ser.Title, v.Title, v.RemoteID,
		season, episode, aired, domain, root.Path, cfg)
	if fail {
		return fmt.Errorf("repack rename failed for video %d", videoID)
	}
	var mediaPath string
	_ = s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'video' ORDER BY id LIMIT 1
	`, videoID).Scan(&mediaPath)
	if mediaPath == "" || !fileExists(mediaPath) {
		return nil
	}
	nfoBeside := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".nfo"
	if fileExists(nfoBeside) || ok {
		_, _ = s.writeEpisodeNFOBeside(v, mediaPath)
	}
	return nil
}
