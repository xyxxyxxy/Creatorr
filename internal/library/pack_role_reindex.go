package library

import (
	"database/sql"
	"fmt"
)

// ResolveSpecialDisplayPlacement sets displayseason/displayepisode on meta for a Special episode.
// regularPeers must be dated regulars in the series ordered upload_date ASC, id ASC.
func ResolveSpecialDisplayPlacement(meta *EpisodeNFO, specialUpload string, specialID int64, peers []displayPeer) {
	if meta == nil || !IsSpecialEpisode(meta.PackRole) {
		return
	}
	specialUpload = NormalizeUploadTime(specialUpload)
	if specialUpload == "" {
		return
	}
	for _, p := range peers {
		if uploadIDAfter(p.UploadDate, p.ID, specialUpload, specialID) {
			if p.Season > 0 && p.Episode > 0 {
				meta.DisplaySeason = p.Season
				meta.DisplayEpisode = p.Episode
				meta.DisplayEpisodeSet = true
			}
			return
		}
	}
	// After all dated regulars: displayseason = last regular's season only.
	if len(peers) > 0 {
		last := peers[len(peers)-1]
		if last.Season > 0 {
			meta.DisplaySeason = last.Season
			meta.DisplayEpisodeSet = false
		}
	}
}

type displayPeer struct {
	ID         int64
	UploadDate string
	Season     int
	Episode    int
}

func uploadIDAfter(aUpload string, aID int64, bUpload string, bID int64) bool {
	aUpload = NormalizeUploadTime(aUpload)
	bUpload = NormalizeUploadTime(bUpload)
	if aUpload != bUpload {
		return aUpload > bUpload
	}
	return aID > bID
}

// ListDatedRegularPeers returns dated regular videos in series ordered for placement.
func (s *Store) ListDatedRegularPeers(seriesID int64) ([]displayPeer, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode
		FROM videos
		WHERE series_id = ?
		  AND `+SQLPackRoleRegularPred+`
		  AND upload_date IS NOT NULL AND trim(upload_date) != ''
		ORDER BY upload_date ASC, id ASC
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []displayPeer
	for rows.Next() {
		var p displayPeer
		var se, ep sql.NullInt64
		if err := rows.Scan(&p.ID, &p.UploadDate, &se, &ep); err != nil {
			return nil, err
		}
		if se.Valid {
			p.Season = int(se.Int64)
		}
		if ep.Valid {
			p.Episode = int(ep.Int64)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ApplySpecialDisplay fills display* on meta using current series regulars.
func (s *Store) ApplySpecialDisplay(meta *EpisodeNFO, seriesID, videoID int64, upload string) error {
	if meta == nil || !IsSpecialEpisode(meta.PackRole) {
		return nil
	}
	peers, err := s.ListDatedRegularPeers(seriesID)
	if err != nil {
		return err
	}
	ResolveSpecialDisplayPlacement(meta, upload, videoID, peers)
	return nil
}

// ReindexSeriesUTCYear sets season/episode for dated **regular** series videos in the UTC year.
// Special episodes and features are excluded. Order: upload_date ASC, id ASC.
func (s *Store) ReindexSeriesUTCYear(seriesID int64, year int) (changed []int64, err error) {
	if seriesID == 0 || year <= 0 {
		return nil, nil
	}

	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode, status
		FROM videos
		WHERE series_id = ?
		  AND `+SQLPackRoleRegularPred+`
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

// ReindexSpecialEpisodes reindexes all Special episodes in a series (season=0, episode 1..n).
func (s *Store) ReindexSpecialEpisodes(seriesID int64) (changed []int64, err error) {
	if seriesID == 0 {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode
		FROM videos
		WHERE series_id = ? AND special_feature = ?
		ORDER BY
		  (upload_date IS NULL OR trim(upload_date) = '') ASC,
		  upload_date ASC, id ASC
	`, seriesID, PackRoleSpecialEpisode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type peer struct {
		ID      int64
		Season  sql.NullInt64
		Episode sql.NullInt64
	}
	var peers []peer
	for rows.Next() {
		var p peer
		var upload sql.NullString
		if err := rows.Scan(&p.ID, &upload, &p.Season, &p.Episode); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, p := range peers {
		wantEp := i + 1
		curSe, curEp := -1, -1
		if p.Season.Valid {
			curSe = int(p.Season.Int64)
		}
		if p.Episode.Valid {
			curEp = int(p.Episode.Int64)
		}
		if curSe == 0 && curEp == wantEp {
			continue
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET season = 0, episode = ? WHERE id = ?`, wantEp, p.ID); err != nil {
			return changed, err
		}
		changed = append(changed, p.ID)
	}
	return changed, nil
}

// ReindexSpecialFeatures reindexes all videos of one feature kind in a series (episode 1..n; season cleared).
func (s *Store) ReindexSpecialFeatures(seriesID int64, kind string) (changed []int64, err error) {
	kind = FeatureKind(kind)
	if seriesID == 0 || kind == "" {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, upload_date, season, episode
		FROM videos
		WHERE series_id = ? AND special_feature = ?
		ORDER BY
		  (upload_date IS NULL OR trim(upload_date) = '') ASC,
		  upload_date ASC, id ASC
	`, seriesID, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type peer struct {
		ID      int64
		Season  sql.NullInt64
		Episode sql.NullInt64
	}
	var peers []peer
	for rows.Next() {
		var p peer
		var upload sql.NullString
		if err := rows.Scan(&p.ID, &upload, &p.Season, &p.Episode); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, p := range peers {
		wantEp := i + 1
		curEp := -1
		if p.Episode.Valid {
			curEp = int(p.Episode.Int64)
		}
		// Features: episode set; season NULL (pack year token from upload when present via meta.Season override at path build).
		if !p.Season.Valid && curEp == wantEp {
			continue
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET season = NULL, episode = ? WHERE id = ?`, wantEp, p.ID); err != nil {
			return changed, err
		}
		changed = append(changed, p.ID)
	}
	return changed, nil
}

// ReindexPackRoleBucket reindexes the bucket for a video's current special_feature.
func (s *Store) ReindexPackRoleBucket(seriesID int64, packRole string) (changed []int64, err error) {
	role := NormalizePackRole(packRole)
	switch {
	case role == PackRoleSpecialEpisode:
		return s.ReindexSpecialEpisodes(seriesID)
	case IsSpecialFeature(role):
		return s.ReindexSpecialFeatures(seriesID, role)
	default:
		return nil, nil
	}
}

func (s *Store) listFeatureKindsInSeries(seriesID int64) ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT special_feature FROM videos
		WHERE series_id = ? AND special_feature != '' AND special_feature != ? AND special_feature != ?
	`, seriesID, PackRoleRegular, PackRoleSpecialEpisode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		if IsSpecialFeature(role) {
			out = append(out, FeatureKind(role))
		}
	}
	return out, rows.Err()
}

// SetVideoPackRole updates special_feature and reindexes old+new buckets.
func (s *Store) SetVideoPackRole(videoID int64, packRole string) error {
	role := NormalizePackRole(packRole)
	if err := ValidatePackRole(role); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	old := NormalizePackRole(v.PackRole)
	if old == role {
		return nil
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET special_feature = ? WHERE id = ?`, role, videoID); err != nil {
		return err
	}
	if _, err := s.ReindexPackRoleBucket(v.SeriesID, old); err != nil {
		return err
	}
	if _, err := s.ReindexPackRoleBucket(v.SeriesID, role); err != nil {
		return err
	}
	// Regular year reindex when leaving/entering regular.
	if old == PackRoleRegular || role == PackRoleRegular {
		upload := ""
		if v.UploadDate.Valid {
			upload = v.UploadDate.String
		}
		if year := SeasonYearFromUpload(upload); year > 0 {
			if _, err := s.ReindexSeriesUTCYear(v.SeriesID, year); err != nil {
				return err
			}
		}
	}
	return nil
}
