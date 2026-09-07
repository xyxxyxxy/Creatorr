package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// EnqueueRenameEpisodes queues a library-wide rename using a per-root format snapshot.
func (s *Store) EnqueueRenameEpisodes() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if err := s.rejectFullRenameEpisodesActive(); err != nil {
		return 0, err
	}
	formats, err := s.snapshotFormatsByRoot()
	if err != nil {
		return 0, err
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRenameEpisodes,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"formats_by_root": formats,
			"cursor":          0,
		},
		Message: "Apply episode format",
	})
}

// EnqueueRenameEpisodesVideos queues a scoped Apply for the given video IDs.
// Merges into an existing pending video-scoped rename_episodes when present.
func (s *Store) EnqueueRenameEpisodesVideos(videoIDs []int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ids := uniqInt64(videoIDs)
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: video_ids required", ErrInvalid)
	}
	formats, err := s.snapshotFormatsByRoot()
	if err != nil {
		return 0, err
	}
	if tid, ok, err := s.mergePendingRenameVideoIDs(ids, formats); err != nil {
		return 0, err
	} else if ok {
		return tid, nil
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRenameEpisodes,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"formats_by_root": formats,
			"cursor":          0,
			"video_ids":       ids,
		},
		Message: "Rename episodes (scoped)",
	})
}

// EnqueueRenameEpisodesSeries queues a scoped Apply for one series, optionally
// with folder-move prelude fields (old_title / old_root_id) after Edit series.
func (s *Store) EnqueueRenameEpisodesSeries(seriesID int64, oldTitle string, oldRootID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if seriesID <= 0 {
		return 0, fmt.Errorf("%w: series_id required", ErrInvalid)
	}
	formats, err := s.snapshotFormatsByRoot()
	if err != nil {
		return 0, err
	}
	if tid, ok, err := s.findPendingRenameSeries(seriesID); err != nil {
		return 0, err
	} else if ok {
		return tid, nil
	}
	payload := map[string]any{
		"formats_by_root": formats,
		"cursor":          0,
		"series_id":       seriesID,
	}
	if strings.TrimSpace(oldTitle) != "" {
		payload["old_title"] = strings.TrimSpace(oldTitle)
	}
	if oldRootID > 0 {
		payload["old_root_id"] = oldRootID
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRenameEpisodes,
		Domain:   queue.SystemDomain,
		SeriesID: seriesID,
		Payload:  payload,
		Message:  "Rename episodes (series)",
	})
}

// EnqueueRenameEpisodesSeriesIDs queues a scoped Apply for one or more series (Maintenance).
// A single id uses EnqueueRenameEpisodesSeries. Multiple ids share one task with series_ids.
func (s *Store) EnqueueRenameEpisodesSeriesIDs(seriesIDs []int64) (int64, error) {
	ids := uniqInt64(seriesIDs)
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: series_ids required", ErrInvalid)
	}
	if len(ids) == 1 {
		return s.EnqueueRenameEpisodesSeries(ids[0], "", 0)
	}
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	formats, err := s.snapshotFormatsByRoot()
	if err != nil {
		return 0, err
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRenameEpisodes,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"formats_by_root": formats,
			"cursor":          0,
			"series_ids":      ids,
		},
		Message: "Rename episodes (series)",
	})
}

func (s *Store) snapshotFormatsByRoot() (map[string]string, error) {
	roots, err := s.ListRoots()
	if err != nil {
		return nil, err
	}
	formats := make(map[string]string, len(roots))
	for _, r := range roots {
		formats[strconv.FormatInt(r.ID, 10)] = settings.NormalizeEpisodeFormat(r.EpisodeFormat)
	}
	return formats, nil
}

func (s *Store) rejectFullRenameEpisodesActive() error {
	rows, err := s.listActiveRenamePayloads()
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var p applyNamingPayload
		_ = json.Unmarshal([]byte(raw), &p)
		if p.isFullLibrary() {
			return queue.ErrDuplicate
		}
	}
	return nil
}

func (s *Store) listActiveRenamePayloads() ([]string, error) {
	qrows, err := s.DB.SQL.Query(`
		SELECT payload FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, queue.SystemDomain, queue.KindRenameEpisodes, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = qrows.Close() }()
	var out []string
	for qrows.Next() {
		var payload string
		if err := qrows.Scan(&payload); err != nil {
			return nil, err
		}
		out = append(out, payload)
	}
	return out, qrows.Err()
}

func (s *Store) mergePendingRenameVideoIDs(ids []int64, formats map[string]string) (taskID int64, merged bool, err error) {
	qrows, err := s.DB.SQL.Query(`
		SELECT id, payload FROM tasks
		WHERE domain = ? AND kind = ? AND status = ?
	`, queue.SystemDomain, queue.KindRenameEpisodes, queue.StatusPending)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = qrows.Close() }()
	for qrows.Next() {
		var id int64
		var raw string
		if err := qrows.Scan(&id, &raw); err != nil {
			return 0, false, err
		}
		var p applyNamingPayload
		_ = json.Unmarshal([]byte(raw), &p)
		if p.SeriesID > 0 || p.isFullLibrary() {
			continue
		}
		if len(p.VideoIDs) == 0 {
			continue
		}
		p.VideoIDs = uniqInt64(append(p.VideoIDs, ids...))
		if len(formats) > 0 {
			p.FormatsByRoot = formats
		}
		if err := s.Queue.UpdatePayload(id, p.payloadMap()); err != nil {
			return 0, false, err
		}
		return id, true, nil
	}
	return 0, false, qrows.Err()
}

func (s *Store) findPendingRenameSeries(seriesID int64) (taskID int64, found bool, err error) {
	qrows, err := s.DB.SQL.Query(`
		SELECT id, payload FROM tasks
		WHERE domain = ? AND kind = ? AND status = ?
	`, queue.SystemDomain, queue.KindRenameEpisodes, queue.StatusPending)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = qrows.Close() }()
	for qrows.Next() {
		var id int64
		var raw string
		if err := qrows.Scan(&id, &raw); err != nil {
			return 0, false, err
		}
		var p applyNamingPayload
		_ = json.Unmarshal([]byte(raw), &p)
		if p.SeriesID == seriesID {
			return id, true, nil
		}
	}
	return 0, false, qrows.Err()
}

// EnqueueSyncFiles queues a system-lane file sync pass.
// No-op (returns 0, nil) when the library has no videos.
func (s *Store) EnqueueSyncFiles(priority int) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ok, err := s.anyVideo()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindSyncFiles,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "File sync",
	})
}

// EnqueueRetentionDelete queues a system-lane retention TTL purge.
// No-op (returns 0, nil) when no root has retention_ttl_seconds set.
func (s *Store) EnqueueRetentionDelete(priority int) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ok, err := s.AnyRootRetentionTTL()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRetentionDelete,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "Retention purge",
	})
}

type applyNamingPayload struct {
	EpisodeFormat string            `json:"episode_format"`
	FormatsByRoot map[string]string `json:"formats_by_root"`
	Cursor        int64             `json:"cursor"`
	SeriesID      int64             `json:"series_id"`
	SeriesIDs     []int64           `json:"series_ids"`
	VideoIDs      []int64           `json:"video_ids"`
	OldTitle      string            `json:"old_title"`
	OldRootID     int64             `json:"old_root_id"`
	FolderMoved   bool              `json:"folder_moved"`
}

func (p applyNamingPayload) isFullLibrary() bool {
	return p.SeriesID <= 0 && len(p.SeriesIDs) == 0 && len(p.VideoIDs) == 0
}

func (p applyNamingPayload) namingConfigForRoot(rootID int64) NamingConfig {
	if len(p.FormatsByRoot) > 0 {
		if fmtStr, ok := p.FormatsByRoot[strconv.FormatInt(rootID, 10)]; ok {
			fmtStr = strings.TrimSpace(fmtStr)
			if fmtStr == "" {
				fmtStr = DefaultEpisodeFormat
			}
			return NamingConfig{EpisodeFormat: fmtStr}
		}
	}
	// Legacy single-format payload (in-flight across upgrade).
	fmtStr := strings.TrimSpace(p.EpisodeFormat)
	if fmtStr == "" {
		fmtStr = DefaultEpisodeFormat
	}
	return NamingConfig{EpisodeFormat: fmtStr}
}

func (p applyNamingPayload) payloadMap() map[string]any {
	out := map[string]any{"cursor": p.Cursor}
	if len(p.FormatsByRoot) > 0 {
		out["formats_by_root"] = p.FormatsByRoot
	}
	if strings.TrimSpace(p.EpisodeFormat) != "" {
		out["episode_format"] = strings.TrimSpace(p.EpisodeFormat)
	}
	if p.SeriesID > 0 {
		out["series_id"] = p.SeriesID
	}
	if len(p.SeriesIDs) > 0 {
		out["series_ids"] = p.SeriesIDs
	}
	if len(p.VideoIDs) > 0 {
		out["video_ids"] = p.VideoIDs
	}
	if strings.TrimSpace(p.OldTitle) != "" {
		out["old_title"] = strings.TrimSpace(p.OldTitle)
	}
	if p.OldRootID > 0 {
		out["old_root_id"] = p.OldRootID
	}
	if p.FolderMoved {
		out["folder_moved"] = true
	}
	return out
}

// ApplyEpisodeNamingPass renames packed episodes to the snapshot formats.
// Optional series_id / video_ids scope the pass. Series tasks may prelude MoveSeriesFolder.
// Reindexes UTC years in scope first, then two-phase renames under a per-series mutex.
func (s *Store) ApplyEpisodeNamingPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (renamed, skippedBusy, failed int, err error) {
	var p applyNamingPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)

	if err := s.applySeriesFolderPrelude(task.ID, &p); err != nil {
		return 0, 0, 0, err
	}

	q, args := buildApplyNamingQuery(p)
	qrows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return 0, 0, 0, err
	}
	type row struct {
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
	var list []row
	yearsBySeries := map[int64]map[int]bool{}
	for qrows.Next() {
		var r row
		if err := qrows.Scan(&r.ID, &r.Title, &r.RemoteID, &r.Season, &r.Episode, &r.SeriesTitle, &r.RootID, &r.RootPath, &r.UploadDate, &r.SourceURL); err != nil {
			_ = qrows.Close()
			return 0, 0, 0, err
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
	_ = qrows.Close()
	if err := qrows.Err(); err != nil {
		return 0, 0, 0, err
	}

	for seriesID, years := range yearsBySeries {
		for y := range years {
			if _, err := s.ReindexSeriesUTCYear(seriesID, y); err != nil {
				return 0, 0, 0, err
			}
		}
	}

	// Re-fetch season/episode after reindex.
	bySeries := map[int64][]int64{}
	var touched []int64
	for _, r := range list {
		touched = append(touched, r.ID)
		bySeries[r.SeriesID] = append(bySeries[r.SeriesID], r.ID)
	}

	total := len(list)
	i := 0
	for seriesID, ids := range bySeries {
		if err := ctx.Err(); err != nil {
			return renamed, skippedBusy, failed, err
		}
		if progress != nil && total > 0 {
			pct := float64(i) / float64(total)
			progress(fmt.Sprintf("Renaming series %d…", seriesID), &pct)
		}
		unlock := lockSeriesRename(seriesID)
		nRenamed, leftovers := s.twoPhaseRepackEpisodeNumbers(ids, task.ID)
		unlock()
		renamed += nRenamed
		for _, id := range leftovers {
			busy, berr := s.videoBusyForRename(id, task.ID)
			if berr != nil {
				failed++
			} else if busy {
				skippedBusy++
			} else {
				failed++
			}
		}

		i += len(ids)
		if len(ids) > 0 {
			p.Cursor = ids[len(ids)-1]
			_ = s.Queue.UpdatePayload(task.ID, p.payloadMap())
		}
	}

	_ = s.warnRemainingPathCollisions(ctx, task.ID, p, touched)
	return renamed, skippedBusy, failed, nil
}

func buildApplyNamingQuery(p applyNamingPayload) (string, []any) {
	base := `
		SELECT v.id, v.title, v.remote_id,
		       COALESCE(v.season, 0), COALESCE(v.episode, 0),
		       s.title, s.root_id, r.path, v.upload_date, COALESCE(v.source_url, '')
		FROM videos v
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'verify_failed')
		  AND v.id > ?
		  AND EXISTS (
		    SELECT 1 FROM files f
		    WHERE f.video_id = v.id AND f.kind = 'video'
		  )`
	args := []any{p.Cursor}
	if p.SeriesID > 0 {
		base += ` AND v.series_id = ?`
		args = append(args, p.SeriesID)
	} else if len(p.SeriesIDs) > 0 {
		base += ` AND v.series_id IN (` + sqlIntPlaceholders(len(p.SeriesIDs)) + `)`
		for _, id := range p.SeriesIDs {
			args = append(args, id)
		}
	}
	if len(p.VideoIDs) > 0 {
		base += ` AND v.id IN (` + sqlIntPlaceholders(len(p.VideoIDs)) + `)`
		for _, id := range p.VideoIDs {
			args = append(args, id)
		}
	}
	base += ` ORDER BY v.id ASC`
	return base, args
}

func (s *Store) applySeriesFolderPrelude(taskID int64, p *applyNamingPayload) error {
	if p.SeriesID <= 0 || p.FolderMoved {
		return nil
	}
	if strings.TrimSpace(p.OldTitle) == "" && p.OldRootID <= 0 {
		return nil
	}
	ser, err := s.GetSeries(p.SeriesID, false)
	if err != nil {
		return err
	}
	oldTitle := strings.TrimSpace(p.OldTitle)
	if oldTitle == "" {
		oldTitle = ser.Title
	}
	oldRoot := p.OldRootID
	if oldRoot <= 0 {
		oldRoot = ser.RootID
	}
	if err := s.MoveSeriesFolder(ser, oldTitle, oldRoot); err != nil {
		_, _ = s.DB.SQL.Exec(`
			UPDATE series SET title = ?, root_id = ? WHERE id = ?
		`, oldTitle, oldRoot, p.SeriesID)
		return fmt.Errorf("folder move failed (reverted title/root): %w", err)
	}
	_ = s.WriteSeriesNFODisk(p.SeriesID)
	_, _, _ = s.RewriteSeriesEpisodeNFOs(p.SeriesID, taskID)
	p.FolderMoved = true
	_ = s.Queue.UpdatePayload(taskID, p.payloadMap())
	return nil
}

// ApplySeriesEpisodeNaming runs ideal-path Apply for one series (bulk / SyncDisk).
func (s *Store) ApplySeriesEpisodeNaming(ctx context.Context, seriesID, taskID int64) (renamed, skippedBusy, failed int, err error) {
	formats, err := s.snapshotFormatsByRoot()
	if err != nil {
		return 0, 0, 0, err
	}
	payload, _ := json.Marshal(applyNamingPayload{
		FormatsByRoot: formats,
		SeriesID:      seriesID,
	}.payloadMap())
	task := &queue.Task{ID: taskID, Payload: string(payload)}
	return s.ApplyEpisodeNamingPass(ctx, task, nil)
}

func (s *Store) warnRemainingPathCollisions(ctx context.Context, taskID int64, p applyNamingPayload, videoIDs []int64) error {
	if s.DB == nil || taskID <= 0 {
		return nil
	}
	ids := uniqInt64(videoIDs)
	if len(ids) == 0 && (p.SeriesID > 0 || len(p.SeriesIDs) > 0) {
		q := `SELECT id FROM videos WHERE status IN ('downloaded', 'verify_failed')`
		var args []any
		if p.SeriesID > 0 {
			q += ` AND series_id = ?`
			args = append(args, p.SeriesID)
		} else {
			q += ` AND series_id IN (` + sqlIntPlaceholders(len(p.SeriesIDs)) + `)`
			for _, id := range p.SeriesIDs {
				args = append(args, id)
			}
		}
		rows, err := s.DB.SQL.Query(q, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
	}
	if len(ids) == 0 && p.isFullLibrary() {
		rows, err := s.DB.SQL.Query(`
			SELECT id FROM videos WHERE status IN ('downloaded', 'verify_failed')
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
	}
	type leftover struct {
		VideoID int64
		Path    string
		Ideal   string
	}
	var left []leftover
	for _, vid := range ids {
		actual, ideal, ok, err := s.episodeCollisionSuffixState(vid, p)
		if err != nil || !ok {
			continue
		}
		left = append(left, leftover{VideoID: vid, Path: actual, Ideal: ideal})
	}
	if len(left) == 0 {
		return nil
	}
	samples := make([]notify.PathCollisionSample, len(left))
	for i, L := range left {
		samples[i] = notify.PathCollisionSample{VideoID: L.VideoID, Path: L.Path, Ideal: L.Ideal}
	}
	return notify.PathCollisionRemaining(ctx, s.DB, taskID, samples)
}

func (s *Store) episodeCollisionSuffixState(videoID int64, p applyNamingPayload) (actualBase, idealBase string, hasSuffix bool, err error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return "", "", false, err
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return "", "", false, err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return "", "", false, err
	}
	var primary string
	err = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video' LIMIT 1`, videoID).Scan(&primary)
	if err != nil || primary == "" || !fileExists(primary) {
		return "", "", false, err
	}
	season, episode := 0, 0
	if v.Season.Valid {
		season = int(v.Season.Int64)
	}
	if v.Episode.Valid {
		episode = int(v.Episode.Int64)
	}
	aired := ""
	domain := ""
	if v.UploadDate.Valid {
		aired = v.UploadDate.String
	}
	if v.SourceURL.Valid {
		domain = namingDomain(v.SourceURL.String)
	}
	cfg := p.namingConfigForRoot(ser.RootID)
	dest, err := BuildEpisodePaths(root.Path, EpisodeNFO{
		SeriesTitle: ser.Title,
		Title:       v.Title,
		Season:      season,
		Episode:     episode,
		Aired:       aired,
		UniqueID:    v.RemoteID,
		Domain:      domain,
	}, cfg)
	if err != nil {
		return "", "", false, err
	}
	actualBase = strings.TrimSuffix(primary, filepath.Ext(primary))
	idealBase = dest.PrimaryBase
	_, hasSuffix = CollisionSuffixN(actualBase, idealBase)
	return actualBase, idealBase, hasSuffix, nil
}

func (s *Store) videoBusyForRename(videoID, exceptTaskID int64) (bool, error) {
	var one int
	err := s.DB.SQL.QueryRow(`
		SELECT 1 FROM tasks
		WHERE video_id = ? AND kind IN (?, ?, ?) AND status IN (?, ?)
		  AND (? = 0 OR id != ?)
		LIMIT 1
	`, videoID, queue.KindDownload, queue.KindSponsorblockCut, queue.KindMediaVerify,
		queue.StatusPending, queue.StatusRunning, exceptTaskID, exceptTaskID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ApplyNamingMessage formats the finish message.
func ApplyNamingMessage(renamed, skippedBusy, failed int) string {
	return fmt.Sprintf("Renamed %d, skipped busy %d, failed %d", renamed, skippedBusy, failed)
}
