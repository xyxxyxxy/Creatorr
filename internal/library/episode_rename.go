package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	episode "github.com/xyxxyxxy/Creatorr/internal/library/episode"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

const renamingStemPrefix = ".creatorr-renaming-"

// renameEpisodesCoversVideos reports whether a pending/running rename_episodes
// task already covers all of the given video IDs (full library, their series, or ids).
func (s *Store) renameEpisodesCoversVideos(videoIDs []int64) bool {
	ids := uniqInt64(videoIDs)
	if len(ids) == 0 || s.DB == nil {
		return false
	}
	rows, err := s.listActiveRenamePayloads()
	if err != nil {
		return false
	}
	needSeries := map[int64]bool{}
	for _, id := range ids {
		var seriesID int64
		if err := s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, id).Scan(&seriesID); err != nil {
			continue
		}
		needSeries[seriesID] = true
	}
	idSet := map[int64]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for _, raw := range rows {
		var p applyNamingPayload
		_ = json.Unmarshal([]byte(raw), &p)
		if p.isFullLibrary() {
			return true
		}
		if p.SeriesID > 0 && needSeries[p.SeriesID] {
			// Covers entire series; if all needed videos are in that series, ok.
			allIn := true
			for sid := range needSeries {
				if sid != p.SeriesID {
					allIn = false
					break
				}
			}
			if allIn {
				return true
			}
		}
		if len(p.SeriesIDs) > 0 {
			cov := map[int64]bool{}
			for _, sid := range p.SeriesIDs {
				cov[sid] = true
			}
			allIn := true
			for sid := range needSeries {
				if !cov[sid] {
					allIn = false
					break
				}
			}
			if allIn {
				return true
			}
		}
		if len(p.VideoIDs) > 0 {
			cov := map[int64]bool{}
			for _, vid := range p.VideoIDs {
				cov[vid] = true
			}
			allIn := true
			for _, id := range ids {
				if !cov[id] {
					allIn = false
					break
				}
			}
			if allIn {
				return true
			}
		}
	}
	return false
}

// renameEpisodesCoversSeries reports pending/running Apply covering this series.
func (s *Store) renameEpisodesCoversSeries(seriesID int64) bool {
	if seriesID <= 0 {
		return false
	}
	rows, err := s.listActiveRenamePayloads()
	if err != nil {
		return false
	}
	for _, raw := range rows {
		var p applyNamingPayload
		_ = json.Unmarshal([]byte(raw), &p)
		if p.isFullLibrary() {
			return true
		}
		if p.SeriesID == seriesID {
			return true
		}
		for _, sid := range p.SeriesIDs {
			if sid == seriesID {
				return true
			}
		}
		if len(p.VideoIDs) > 0 {
			var n int
			_ = s.DB.SQL.QueryRow(`
				SELECT COUNT(*) FROM videos
				WHERE series_id = ? AND id IN (`+sqlIntPlaceholders(len(p.VideoIDs))+`)
			`, append([]any{seriesID}, int64sToAny(p.VideoIDs)...)...).Scan(&n)
			if n > 0 {
				// Partial video scope - treat as covering for peer-move no-op on this series.
				return true
			}
		}
	}
	return false
}

func int64sToAny(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// AlignSeriesYearEpisodes reindexes the year and two-phase-renames packed peers.
// No-ops disk work when Apply already covers the series. Returns leftover video IDs.
func (s *Store) AlignSeriesYearEpisodes(seriesID int64, year, taskID int64) (leftovers []int64, err error) {
	if seriesID <= 0 || year <= 0 {
		return nil, nil
	}
	changed, err := s.ReindexSeriesUTCYear(seriesID, int(year))
	if err != nil {
		return nil, err
	}
	if s.renameEpisodesCoversSeries(seriesID) {
		return nil, nil
	}
	packed, err := s.packedVideoIDs(changed)
	if err != nil {
		return nil, err
	}
	// Also include any packed video in the year not at ideal (e.g. current pack).
	yearPacked, err := s.packedVideoIDsInYear(seriesID, int(year))
	if err != nil {
		return nil, err
	}
	packed = uniqInt64(append(packed, yearPacked...))
	if len(packed) == 0 {
		return nil, nil
	}
	unlock := lockSeriesRename(seriesID)
	_, leftovers = s.twoPhaseRepackEpisodeNumbers(packed, taskID)
	unlock()
	if len(leftovers) > 0 {
		_ = s.notifyPeerMoveNeedsApply(leftovers)
	}
	return leftovers, nil
}

func (s *Store) packedVideoIDsInYear(seriesID int64, year int) ([]int64, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM videos
		WHERE series_id = ? AND status IN ('downloaded', 'integrity_check_failed')
		  AND season = ?
	`, seriesID, year)
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

// twoPhaseRepackEpisodeNumbers moves packed episode sets via temp stems then ideals.
// Returns count successfully renamed and video IDs that remain off ideal (busy, failed, or blocked).
func (s *Store) twoPhaseRepackEpisodeNumbers(videoIDs []int64, taskID int64) (renamed int, leftovers []int64) {
	triggerVideoID, triggerTitle := s.renameTriggerVideo(taskID)
	type item struct {
		videoID  int64
		oldBase  string
		tempBase string
		newBase  string
		files    []struct {
			id   int64
			path string
		}
		seriesTitle, title, remoteID, aired, domain, root string
		season, episode                                   int
		cfg                                               NamingConfig
	}
	var items []item
	for _, videoID := range uniqInt64(videoIDs) {
		busy, err := s.videoBusyForRename(videoID, taskID)
		if err != nil || busy {
			leftovers = append(leftovers, videoID)
			continue
		}
		v, err := s.GetVideo(videoID)
		if err != nil {
			leftovers = append(leftovers, videoID)
			continue
		}
		if v.Status != "downloaded" && v.Status != "integrity_check_failed" {
			continue
		}
		season, episode := 0, 0
		if v.Season.Valid {
			season = int(v.Season.Int64)
		}
		if v.Episode.Valid {
			episode = int(v.Episode.Int64)
		}
		ser, err := s.GetSeries(v.SeriesID, false)
		if err != nil {
			leftovers = append(leftovers, videoID)
			continue
		}
		root, err := s.GetRoot(ser.RootID)
		if err != nil {
			leftovers = append(leftovers, videoID)
			continue
		}
		cfg := NamingConfigFromRoot(root)
		aired := ""
		if v.UploadDate.Valid {
			aired = v.UploadDate.String
		}
		domain := ""
		if v.SourceURL.Valid {
			domain = namingDomain(v.SourceURL.String)
		}
		primary, files, err := s.episodeFileSet(videoID)
		if err != nil || primary == "" {
			if healed := s.healMissingEpisodePath(v, ser.Title, season, episode, aired, domain, root.Path, cfg); healed != "" {
				primary, files, err = s.episodeFileSet(videoID)
			}
			if err != nil || primary == "" {
				leftovers = append(leftovers, videoID)
				continue
			}
		}
		meta := EpisodeNFO{
			SeriesTitle: ser.Title, Title: v.Title, Season: season, Episode: episode,
			Aired: aired, UniqueID: v.RemoteID, Domain: domain,
		}
		_ = s.EnsureSeriesDirCapped(root.Path, ser.Title)
		dest, err := BuildEpisodePaths(root.Path, meta, cfg)
		if err != nil {
			leftovers = append(leftovers, videoID)
			continue
		}
		oldBase := strings.TrimSuffix(primary, filepath.Ext(primary))
		newBase := dest.PrimaryBase
		if filepath.Clean(oldBase) == filepath.Clean(newBase) {
			continue
		}
		seriesDir := SeriesDir(root.Path, ser.Title)
		tempBase := filepath.Join(seriesDir, fmt.Sprintf("%s%d", renamingStemPrefix, videoID))
		it := item{
			videoID: videoID, oldBase: oldBase, tempBase: tempBase, newBase: newBase,
			seriesTitle: ser.Title, title: v.Title, remoteID: v.RemoteID,
			aired: aired, domain: domain, root: root.Path,
			season: season, episode: episode, cfg: cfg,
		}
		for _, f := range files {
			it.files = append(it.files, struct {
				id   int64
				path string
			}{f.id, f.path})
		}
		items = append(items, it)
	}

	// Phase A: to temp
	var atTemp []item
	for _, it := range items {
		if err := os.MkdirAll(filepath.Dir(it.tempBase), 0o755); err != nil {
			leftovers = append(leftovers, it.videoID)
			continue
		}
		ok := true
		var done []struct{ from, to string }
		for _, f := range it.files {
			if !strings.HasPrefix(f.path, it.oldBase) {
				ok = false
				break
			}
			rel := strings.TrimPrefix(f.path, it.oldBase)
			to := it.tempBase + rel
			if err := moveFile(f.path, to); err != nil {
				ok = false
				for i := len(done) - 1; i >= 0; i-- {
					_ = moveFile(done[i].to, done[i].from)
				}
				break
			}
			done = append(done, struct{ from, to string }{f.path, to})
		}
		if !ok {
			leftovers = append(leftovers, it.videoID)
			continue
		}
		for i := range it.files {
			rel := strings.TrimPrefix(it.files[i].path, it.oldBase)
			newPath := it.tempBase + rel
			_, _ = s.DB.SQL.Exec(`UPDATE files SET path = ? WHERE id = ?`, newPath, it.files[i].id)
			it.files[i].path = newPath
		}
		atTemp = append(atTemp, it)
		_ = PruneEmptyDir(filepath.Dir(it.oldBase))
	}

	// Phase B: temp → ideal
	for _, it := range atTemp {
		currentPaths := make([]string, 0, len(it.files))
		for _, f := range it.files {
			currentPaths = append(currentPaths, f.path)
		}
		blocked := false
		for _, f := range it.files {
			rel := strings.TrimPrefix(f.path, it.tempBase)
			to := it.newBase + rel
			if DestinationOccupied(to, currentPaths) {
				blocked = true
				break
			}
		}
		if blocked {
			leftovers = append(leftovers, it.videoID)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(it.newBase), 0o755); err != nil {
			leftovers = append(leftovers, it.videoID)
			continue
		}
		ok := true
		var done []struct{ from, to string }
		for _, f := range it.files {
			rel := strings.TrimPrefix(f.path, it.tempBase)
			to := it.newBase + rel
			if err := moveFile(f.path, to); err != nil {
				ok = false
				for i := len(done) - 1; i >= 0; i-- {
					_ = moveFile(done[i].to, done[i].from)
				}
				break
			}
			done = append(done, struct{ from, to string }{f.path, to})
		}
		if !ok {
			leftovers = append(leftovers, it.videoID)
			continue
		}
		for i := range it.files {
			rel := strings.TrimPrefix(it.files[i].path, it.tempBase)
			newPath := it.newBase + rel
			_, _ = s.DB.SQL.Exec(`UPDATE files SET path = ? WHERE id = ?`, newPath, it.files[i].id)
		}
		_ = PruneEmptyDir(filepath.Dir(it.tempBase))
		msg, detail := renamedHistoryPayload(it.videoID, it.oldBase, it.newBase, triggerVideoID, triggerTitle)
		_ = s.AddVideoHistory(it.videoID, "renamed", msg, detail, taskID)
		renamed++
		var mp string
		_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video' ORDER BY id LIMIT 1`, it.videoID).Scan(&mp)
		if mp != "" {
			v, _ := s.GetVideo(it.videoID)
			if v != nil {
				_, _ = s.writeEpisodeNFOBeside(v, mp)
			}
		}
	}
	return renamed, uniqInt64(leftovers)
}

// renameTriggerVideo returns the task's video_id and title when the rename was driven
// by a video-scoped task (e.g. download peer-move). Zero id when none.
func (s *Store) renameTriggerVideo(taskID int64) (videoID int64, title string) {
	if s.Queue == nil || taskID <= 0 {
		return 0, ""
	}
	t, err := s.Queue.GetTask(taskID)
	if err != nil || t == nil || !t.VideoID.Valid || t.VideoID.Int64 <= 0 {
		return 0, ""
	}
	videoID = t.VideoID.Int64
	if v, err := s.GetVideo(videoID); err == nil && v != nil {
		title = strings.TrimSpace(v.Title)
	}
	return videoID, title
}

// RenamedHistoryMessage is the video History message for a successful episode rename.
// When triggerVideoID is another video (peer-move after its pack/download), the message
// names that trigger so History is not mistaken for an Apply-only rename.
func RenamedHistoryMessage(thisVideoID, triggerVideoID int64, triggerTitle string) string {
	return episode.RenamedHistoryMessage(thisVideoID, triggerVideoID, triggerTitle)
}

func renamedHistoryPayload(videoID int64, oldBase, newBase string, triggerVideoID int64, triggerTitle string) (string, map[string]any) {
	detail := map[string]any{
		"previous":      filepath.Base(oldBase),
		"new":           filepath.Base(newBase),
		"previous_path": oldBase,
		"new_path":      newBase,
	}
	if triggerVideoID > 0 && triggerVideoID != videoID {
		detail["trigger_video_id"] = triggerVideoID
		detail["reason"] = "peer_move"
	}
	return RenamedHistoryMessage(videoID, triggerVideoID, triggerTitle), detail
}

type epFile struct {
	id   int64
	path string
}

func (s *Store) episodeFileSet(videoID int64) (primary string, files []epFile, err error) {
	fileRows, err := s.DB.SQL.Query(`SELECT id, kind, path FROM files WHERE video_id = ?`, videoID)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = fileRows.Close() }()
	for fileRows.Next() {
		var id int64
		var kind, path string
		if err := fileRows.Scan(&id, &kind, &path); err != nil {
			return "", nil, err
		}
		files = append(files, epFile{id, path})
		if kind == "video" && primary == "" {
			primary = path
		}
	}
	if err := fileRows.Err(); err != nil {
		return "", nil, err
	}
	if primary == "" || !fileExists(primary) {
		return "", files, nil
	}
	return primary, files, nil
}

func (s *Store) healMissingEpisodePath(v *Video, seriesTitle string, season, episode int, aired, domain, root string, cfg NamingConfig) string {
	if v == nil {
		return ""
	}
	seriesDir := SeriesDir(root, seriesTitle)
	tempBase := filepath.Join(seriesDir, fmt.Sprintf("%s%d", renamingStemPrefix, v.ID))
	for _, ext := range []string{".mkv", ".mka", ".mp4", ".webm"} {
		cand := tempBase + ext
		if fileExists(cand) {
			_, _ = s.DB.SQL.Exec(`UPDATE files SET path = ? WHERE video_id = ? AND kind = 'video'`, cand, v.ID)
			return cand
		}
	}
	meta := EpisodeNFO{
		SeriesTitle: seriesTitle, Title: v.Title, Season: season, Episode: episode,
		Aired: aired, UniqueID: v.RemoteID, Domain: domain,
	}
	dest, err := BuildEpisodePaths(root, meta, cfg)
	if err != nil {
		return ""
	}
	for _, ext := range []string{".mkv", ".mka", ".mp4", ".webm"} {
		cand := dest.PrimaryBase + ext
		if fileExists(cand) {
			_, _ = s.DB.SQL.Exec(`UPDATE files SET path = ? WHERE video_id = ? AND kind = 'video'`, cand, v.ID)
			return cand
		}
	}
	return ""
}

func (s *Store) notifyPeerMoveNeedsApply(leftoverIDs []int64) error {
	if len(leftoverIDs) == 0 || s.DB == nil {
		return nil
	}
	_, _, err := s.EnqueueApplyForPackedEpisodeChanges(leftoverIDs)
	if err != nil {
		return notify.EpisodeRenameApplyFailed(context.Background(), s.DB, 0, err.Error())
	}
	return nil
}
