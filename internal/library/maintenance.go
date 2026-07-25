package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// SeriesIDsMonitored returns series with series.monitored=1.
func (s *Store) SeriesIDsMonitored() ([]int64, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM series WHERE monitored = 1 ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

// SeriesIDsWithMonitoredSources is kept for callers; prefers series.monitored.
func (s *Store) SeriesIDsWithMonitoredSources() ([]int64, error) {
	return s.SeriesIDsMonitored()
}

// SeriesIsMonitored reports whether series.monitored is on.
// When false, new scan/download tasks must not be enqueued; already-queued tasks are left alone.
func (s *Store) SeriesIsMonitored(seriesID int64) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`SELECT monitored FROM series WHERE id = ?`, seriesID).Scan(&n)
	if err == sql.ErrNoRows {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return n != 0, nil
}

// SeriesHasMonitoredSource reports series.monitored (name kept for call sites).
func (s *Store) SeriesHasMonitoredSource(seriesID int64) (bool, error) {
	return s.SeriesIsMonitored(seriesID)
}

// EnqueueScansDue is the scheduled Scan pass at wall clock now.
// Feed sources with a non-empty scan_cron on monitored series: tip Scan when
// full_scan_done, else full scan (same EnqueueScanSource mode switch).
//
// notBefore (usually process start): if the source was already overdue at that
// instant, wait for the next cron after notBefore instead of catching up missed
// fires from downtime. Zero notBefore keeps plain due-check catch-up (tests /
// mid-process ticks without a boot anchor).
func (s *Store) EnqueueScansDue(now, notBefore time.Time) (int, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	rows, err := s.DB.SQL.Query(`
		SELECT src.id, src.scan_cron, src.url
		FROM sources src
		JOIN series ser ON ser.id = src.series_id
		WHERE ser.monitored = 1 AND src.kind = 'feed'
		  AND TRIM(COALESCE(src.scan_cron, '')) != ''
		ORDER BY src.id
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type scanDueRow struct {
		id       int64
		scanCron string
		url      string
	}
	var pending []scanDueRow
	for rows.Next() {
		var r scanDueRow
		if err := rows.Scan(&r.id, &r.scanCron, &r.url); err != nil {
			return 0, err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range pending {
		ok, err := domains.IsActive(s.DB, queueDomain(r.url))
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		last, err := s.LatestTipScannedAt(r.id)
		if err != nil {
			return n, err
		}
		if !notBefore.IsZero() {
			if last.IsZero() {
				last = notBefore
			} else {
				overdueAtBoot, err := cronexpr.Due(r.scanCron, last, notBefore)
				if err != nil {
					continue
				}
				if overdueAtBoot {
					last = notBefore
				}
			}
		}
		due, err := cronexpr.Due(r.scanCron, last, now)
		if err != nil || !due {
			continue
		}
		if _, err := s.EnqueueScanSource(r.id); err != nil {
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}

// EnqueueFullScansForMonitored enqueues unfinished full scans for monitored series.
// Scheduled Scan uses EnqueueScansDue (tip or full). This helper is for manual/API kicks.
func (s *Store) EnqueueFullScansForMonitored() (int, error) {
	ids, err := s.SeriesIDsWithMonitoredSources()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		c, _, err := s.EnqueueFullScansForSeries(id)
		if err != nil {
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
				continue
			}
			return n, err
		}
		n += c
	}
	return n, nil
}

// EnqueueDownloadWanted enqueues downloads for wanted videos lacking a file.
// Requires series monitored and domain active. Videos with no source are skipped.
// Order: fair round-robin across series (fewest active downloads first), within
// each series by download_wanted_order (upload_date; undated by id). Per-domain max_download_queue cap.
func (s *Store) EnqueueDownloadWanted() (int, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	orderSQL := videoDownloadOrderOldest
	if raw, err := settings.Get(s.DB, settings.KeyDownloadWantedOrder); err == nil {
		if settings.NormalizeDownloadWantedOrder(raw) == settings.DownloadWantedOrderNewest {
			orderSQL = videoDownloadOrderNewest
		}
	}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, COALESCE(v.source_url,''), v.source_id, COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1 AND ser.delivery_mode = 'download' AND v.status = 'wanted'
		ORDER BY v.series_id, `+orderSQL+`
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct {
		id, seriesID int64
		url          string
		sourceID     sql.NullInt64
		sourceURL    string
	}
	bySeries := map[int64][]row{}
	var seriesIDs []int64
	seenSeries := map[int64]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.seriesID, &r.url, &r.sourceID, &r.sourceURL); err != nil {
			return 0, err
		}
		if !seenSeries[r.seriesID] {
			seenSeries[r.seriesID] = true
			seriesIDs = append(seriesIDs, r.seriesID)
		}
		bySeries[r.seriesID] = append(bySeries[r.seriesID], r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	activeBySeries := map[int64]int{}
	arows, err := s.DB.SQL.Query(`
		SELECT series_id, COUNT(*) FROM tasks
		WHERE kind = ? AND status IN (?, ?) AND series_id IS NOT NULL
		GROUP BY series_id
	`, queue.KindDownload, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return 0, err
	}
	for arows.Next() {
		var sid int64
		var n int
		if err := arows.Scan(&sid, &n); err != nil {
			_ = arows.Close()
			return 0, err
		}
		activeBySeries[sid] = n
	}
	if err := arows.Err(); err != nil {
		_ = arows.Close()
		return 0, err
	}
	_ = arows.Close()

	sort.SliceStable(seriesIDs, func(i, j int) bool {
		ai, aj := activeBySeries[seriesIDs[i]], activeBySeries[seriesIDs[j]]
		if ai != aj {
			return ai < aj
		}
		return seriesIDs[i] < seriesIDs[j]
	})

	// Round-robin interleave in least-loaded series order.
	var list []row
	cursors := map[int64]int{}
	for {
		added := false
		for _, sid := range seriesIDs {
			group := bySeries[sid]
			c := cursors[sid]
			if c >= len(group) {
				continue
			}
			list = append(list, group[c])
			cursors[sid] = c + 1
			added = true
		}
		if !added {
			break
		}
	}

	n := 0
	for _, r := range list {
		if _, ok, err := s.HasVideoFile(r.id); err != nil {
			return n, err
		} else if ok {
			continue
		}
		busy, err := s.hasPendingDownload(r.id)
		if err != nil {
			return n, err
		}
		if busy {
			continue
		}
		domain := queueDomain(r.url)
		if domain == "unknown" {
			domain = queueDomain(r.sourceURL)
		}
		ok, err := domains.IsActive(s.DB, domain)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		_, err = s.Queue.Enqueue(enqueueDownloadParams(r.id, r.seriesID, domain))
		if err != nil {
			if errors.Is(err, queue.ErrQueueFull) {
				return n, nil
			}
			if errors.Is(err, queue.ErrDuplicate) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) hasPendingDownload(videoID int64) (bool, error) {
	var id sql.NullInt64
	err := s.DB.SQL.QueryRow(`
		SELECT id FROM tasks
		WHERE kind IN (?, ?) AND video_id = ? AND status IN (?, ?)
		LIMIT 1
	`, queue.KindDownload, queue.KindSponsorblockCut, videoID, queue.StatusPending, queue.StatusRunning).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return id.Valid, nil
}

// MarkDeleted clears file rows, sets status deleted, writes history.
// Used for intentional removals (retention, user delete) - not for transient missing paths.
func (s *Store) MarkDeleted(videoID int64, reason string, taskID int64) error {
	_ = s.ClearBeginning(videoID)
	_ = s.ClearPlaybackCache(videoID)
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ?`, videoID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE videos SET status = 'deleted' WHERE id = ?
	`, videoID); err != nil {
		return err
	}
	detail := fmt.Sprintf(`{"reason":%q}`, reason)
	if taskID <= 0 {
		return fmt.Errorf("%w: task_id required for file_deleted history", ErrInvalid)
	}
	if _, err := tx.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'file_deleted', ?, ?, ?)
	`, videoID, nowRFC3339(), "Files removed ("+reason+")", detail, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkMissing sets status missing but keeps file rows (path preserved for restore).
// taskID links history to the sync_files task when > 0.
func (s *Store) MarkMissing(videoID, taskID int64) error {
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'missing' WHERE id = ?`, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "file_missing", "Media file missing on disk", map[string]any{
		"reason": "sync_files",
	}, taskID)
}

// RestoreDownloaded sets status downloaded when a previously missing file is back.
// taskID links history to the sync_files task when > 0.
func (s *Store) RestoreDownloaded(videoID, taskID int64) error {
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "file_restored", "Media file found again", map[string]any{
		"reason": "sync_files",
	}, taskID)
}

// ProgressFn reports task message + optional fraction in [0,1].
type ProgressFn func(msg string, pct *float64)

func (s *Store) reportTaskProgress(taskID int64, progress ProgressFn, msg string, pct float64) {
	p := pct
	if progress != nil {
		progress(msg, &p)
		return
	}
	if taskID > 0 && s.Queue != nil {
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
}

// FileSyncPass runs missing/restore/beginning reconciliation for taskID.
// Optional progress reports mid-pass percentages for the queue UI.
func (s *Store) FileSyncPass(taskID int64, progress ...ProgressFn) (int, error) {
	var prog ProgressFn
	if len(progress) > 0 {
		prog = progress[0]
	}
	s.reportTaskProgress(taskID, prog, "Checking library files…", 0.1)
	missingIDs, restoredIDs, err := s.fileSyncMissingAndRestore(taskID, prog)
	if err != nil {
		return 0, err
	}
	s.reportTaskProgress(taskID, prog, "Checking beginning caches…", 0.55)
	beginLostIDs, beginRestoredIDs, err := s.fileSyncBeginnings(taskID, prog)
	if err != nil {
		return len(missingIDs) + len(restoredIDs), err
	}
	s.reportTaskProgress(taskID, prog, "Enforcing progressive cache budget…", 0.85)
	_ = s.EnforcePlaybackCacheBudget(0)

	total := len(missingIDs) + len(restoredIDs) + len(beginLostIDs) + len(beginRestoredIDs)
	msg := "No changes"
	if total > 0 {
		parts := make([]string, 0, 4)
		if n := len(missingIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d missing on disk", n))
		}
		if n := len(restoredIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d restored", n))
		}
		if n := len(beginLostIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d beginning cache missing", n))
		}
		if n := len(beginRestoredIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d beginning cache restored", n))
		}
		msg = "File sync: " + strings.Join(parts, ", ")
		allIDs := append(append([]int64{}, missingIDs...), restoredIDs...)
		allIDs = append(append(allIDs, beginLostIDs...), beginRestoredIDs...)
		detailBytes, _ := json.Marshal(map[string]any{
			"missing_ids":            missingIDs,
			"restored_ids":           restoredIDs,
			"beginning_missing_ids":  beginLostIDs,
			"beginning_restored_ids": beginRestoredIDs,
			"video_ids":              allIDs,
		})
		if taskID > 0 && s.Queue != nil {
			_ = s.Queue.SetDetail(taskID, string(detailBytes))
		}
	}
	s.reportTaskProgress(taskID, prog, msg, 1)
	return total, nil
}

// RetentionPurgePass deletes downloaded media past root retention TTL for taskID.
func (s *Store) RetentionPurgePass(taskID int64, progress ...ProgressFn) (int, error) {
	return s.RetentionPurgePassAt(time.Now().UTC(), taskID, progress...)
}

// RetentionPurgePassAt is RetentionPurgePass with an injectable clock (tests).
func (s *Store) RetentionPurgePassAt(now time.Time, taskID int64, progress ...ProgressFn) (int, error) {
	var prog ProgressFn
	if len(progress) > 0 {
		prog = progress[0]
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.reportTaskProgress(taskID, prog, "Scanning retention…", 0.1)
	retentionIDs, err := s.retentionPurge(now, taskID, prog)
	if err != nil {
		return 0, err
	}
	msg := "No changes"
	if len(retentionIDs) > 0 {
		msg = fmt.Sprintf("Retention purge: %d expired", len(retentionIDs))
		detailBytes, _ := json.Marshal(map[string]any{
			"retention_ids": retentionIDs,
			"video_ids":     retentionIDs,
		})
		if taskID > 0 && s.Queue != nil {
			_ = s.Queue.SetDetail(taskID, string(detailBytes))
		}
	}
	s.reportTaskProgress(taskID, prog, msg, 1)
	return len(retentionIDs), nil
}

func rootOnline(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func (s *Store) fileSyncMissingAndRestore(taskID int64, progress ProgressFn) (missingIDs, restoredIDs []int64, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.status, f.path, r.path
		FROM videos v
		JOIN files f ON f.video_id = v.id AND f.kind = 'video'
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'verify_failed', 'missing')
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type hit struct {
		id     int64
		status string
		path   string
		root   string
	}
	var list []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.status, &h.path, &h.root); err != nil {
			return nil, nil, err
		}
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}

	n := len(list)
	online := map[string]bool{}
	for i, h := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Checking library files… %d/%d", i+1, n),
				0.1+0.4*float64(i+1)/float64(n))
		}
		ok, cached := online[h.root]
		if !cached {
			ok = rootOnline(h.root)
			online[h.root] = ok
		}
		if !ok {
			continue // root offline - do not mark missing or restore
		}
		exists := fileExists(h.path)
		switch h.status {
		case "downloaded", "verify_failed":
			if !exists {
				if err := s.MarkMissing(h.id, taskID); err != nil {
					return missingIDs, restoredIDs, err
				}
				missingIDs = append(missingIDs, h.id)
			}
		case "missing":
			if exists {
				if err := s.RestoreDownloaded(h.id, taskID); err != nil {
					return missingIDs, restoredIDs, err
				}
				restoredIDs = append(restoredIDs, h.id)
			}
		}
	}
	return missingIDs, restoredIDs, nil
}

// fileSyncBeginnings reconciles download-beginning cache vs stream_beginning_cached.
// Video status stays streamable; beginning loss/restore is recorded on video activity.
// Lost beginning: best-effort requeue cache_beginning (setting > 0, domain active, pipe-only -
// same gates as EnqueueCacheBeginning). Skips entirely when CacheDir is offline.
func (s *Store) fileSyncBeginnings(taskID int64, progress ProgressFn) (lostIDs, restoredIDs []int64, err error) {
	cacheRoot := strings.TrimSpace(s.CacheDir)
	if cacheRoot == "" {
		cacheRoot = filepath.Join("var", "cache")
	}
	if !rootOnline(cacheRoot) {
		return nil, nil, nil
	}

	rows, err := s.DB.SQL.Query(`
		SELECT id, COALESCE(stream_urls_kind,''), stream_beginning_cached
		FROM videos
		WHERE status = 'streamable'
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type hit struct {
		id      int64
		kind    string
		flagged bool
	}
	var list []hit
	for rows.Next() {
		var h hit
		var flag int
		if err := rows.Scan(&h.id, &h.kind, &flag); err != nil {
			return nil, nil, err
		}
		h.flagged = flag != 0
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}

	n := len(list)
	for i, h := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Checking beginning caches… %d/%d", i+1, n),
				0.55+0.4*float64(i+1)/float64(n))
		}
		if StreamCDNDirect(h.kind) {
			continue
		}
		onDisk := s.HasBeginning(h.id)
		switch {
		case h.flagged && !onDisk:
			if err := s.SetStreamBeginningCached(h.id, false); err != nil {
				return lostIDs, restoredIDs, err
			}
			detail := map[string]any{"reason": "sync_files"}
			if tid, enqErr := s.EnqueueCacheBeginning(h.id); enqErr == nil && tid > 0 {
				detail["task_id"] = tid
			}
			_ = s.AddVideoHistory(h.id, "beginning_missing", "Download beginning cache missing on disk", detail, taskID)
			lostIDs = append(lostIDs, h.id)
		case !h.flagged && onDisk:
			if err := s.SetStreamBeginningCached(h.id, true); err != nil {
				return lostIDs, restoredIDs, err
			}
			_ = s.AddVideoHistory(h.id, "beginning_restored", "Download beginning cache found again", map[string]any{
				"reason": "sync_files",
			}, taskID)
			restoredIDs = append(restoredIDs, h.id)
		}
	}
	return lostIDs, restoredIDs, nil
}

func (s *Store) retentionPurge(now time.Time, taskID int64, progress ProgressFn) ([]int64, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, f.path, f.acquired_at, r.retention_ttl_seconds, r.path
		FROM videos v
		JOIN files f ON f.video_id = v.id AND f.kind = 'video'
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'verify_failed')
		  AND r.retention_ttl_seconds IS NOT NULL
		  AND r.retention_ttl_seconds > 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type victim struct {
		videoID int64
		path    string
		root    string
		ttl     int64
		acq     string
	}
	var list []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.videoID, &v.path, &v.acq, &v.ttl, &v.root); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var ids []int64
	touchedRoots := map[string]struct{}{}
	online := map[string]bool{}
	n := len(list)
	for i, v := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Scanning retention… %d/%d", i+1, n),
				0.1+0.85*float64(i+1)/float64(n))
		}
		ok, cached := online[v.root]
		if !cached {
			ok = rootOnline(v.root)
			online[v.root] = ok
		}
		if !ok {
			continue
		}
		acq, err := parseTimeFlexible(v.acq)
		if err != nil {
			continue
		}
		expire := acq.Add(time.Duration(v.ttl) * time.Second)
		if now.Before(expire) {
			continue
		}
		deleteVideoArtifacts(v.path)
		if err := s.MarkDeleted(v.videoID, "retention", taskID); err != nil {
			return ids, err
		}
		ids = append(ids, v.videoID)
		touchedRoots[v.root] = struct{}{}
	}
	for root := range touchedRoots {
		pruneEmptyDirs(root)
	}
	return ids, nil
}

func parseTimeFlexible(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, s)
}

func deleteVideoArtifacts(mediaPath string) {
	dir := filepath.Dir(mediaPath)
	stem := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = os.Remove(mediaPath)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == stem || strings.HasPrefix(name, stem+"-thumb") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func pruneEmptyDirs(root string) {
	root = filepath.Clean(root)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		return nil
	})
	// bottom-up: collect dirs then try rmdir
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // only succeeds if empty
	}
}
