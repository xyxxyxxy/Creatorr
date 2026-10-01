package library

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueDownloadWanted enqueues downloads for wanted videos lacking a file.
// Requires series monitored and domain active. Videos with no source are skipped.
// Order: fair round-robin across series (fewest active downloads first, then
// oldest/never last download enqueue, then series_id), within each series oldest
// upload_date first (undated by lowest id).
// Per-domain max_download_queue caps that hostname only; other domains keep
// filling until their caps (or all active candidate domains are full).
func (s *Store) EnqueueDownloadWanted() (int, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	orderSQL := videoDownloadOrderOldest
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, COALESCE(v.source_url,''), v.source_id, COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1 AND v.status = 'wanted'
		ORDER BY v.series_id, ` + orderSQL + `
	`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	type row struct {
		id, seriesID int64
		url          string
		sourceID     sql.NullInt64
		sourceURL    string
		domain       string
	}
	bySeries := map[int64][]row{}
	var seriesIDs []int64
	seenSeries := map[int64]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.seriesID, &r.url, &r.sourceID, &r.sourceURL); err != nil {
			return 0, err
		}
		r.domain = queueDomain(r.url)
		if r.domain == "unknown" {
			r.domain = queueDomain(r.sourceURL)
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

	// Last download enqueue per series (any status). Missing = never served;
	// used as tie-break so finished low-id series do not reclaim slots forever.
	lastEnqueueBySeries := map[int64]string{}
	lrows, err := s.DB.SQL.Query(`
		SELECT series_id, MAX(created_at) FROM tasks
		WHERE kind = ? AND series_id IS NOT NULL
		GROUP BY series_id
	`, queue.KindDownload)
	if err != nil {
		return 0, err
	}
	for lrows.Next() {
		var sid int64
		var created string
		if err := lrows.Scan(&sid, &created); err != nil {
			_ = lrows.Close()
			return 0, err
		}
		lastEnqueueBySeries[sid] = created
	}
	if err := lrows.Err(); err != nil {
		_ = lrows.Close()
		return 0, err
	}
	_ = lrows.Close()

	sort.SliceStable(seriesIDs, func(i, j int) bool {
		ai, aj := activeBySeries[seriesIDs[i]], activeBySeries[seriesIDs[j]]
		if ai != aj {
			return ai < aj
		}
		li, lj := lastEnqueueBySeries[seriesIDs[i]], lastEnqueueBySeries[seriesIDs[j]]
		if li != lj {
			// Empty (never) sorts before any timestamp.
			return li < lj
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

	activeOK := map[string]bool{}
	pausedOK := map[string]bool{}
	seenActive := map[string]bool{}
	openDomainCount := 0
	for _, r := range list {
		ok, cached := activeOK[r.domain]
		if !cached {
			var err error
			ok, err = domains.IsActive(s.DB, r.domain)
			if err != nil {
				return 0, err
			}
			activeOK[r.domain] = ok
		}
		paused, cached := pausedOK[r.domain]
		if !cached {
			var err error
			paused, err = domains.IsPaused(s.DB, r.domain)
			if err != nil {
				return 0, err
			}
			pausedOK[r.domain] = paused
		}
		if ok && !paused && !seenActive[r.domain] {
			seenActive[r.domain] = true
			openDomainCount++
		}
	}

	fullDomains := map[string]bool{}
	fullCount := 0
	n := 0
	ids := make([]int64, len(list))
	for i, r := range list {
		ids[i] = r.id
	}
	hasMedia, err := s.videoIDsWithMediaFile(ids)
	if err != nil {
		return 0, err
	}
	pendingDL, err := s.videoIDsWithPendingDownload(ids)
	if err != nil {
		return 0, err
	}
	for _, r := range list {
		if !activeOK[r.domain] || pausedOK[r.domain] || fullDomains[r.domain] {
			continue
		}
		if hasMedia[r.id] {
			continue
		}
		if pendingDL[r.id] {
			continue
		}
		_, err = s.Queue.Enqueue(enqueueDownloadParams(r.id, r.seriesID, r.domain, queue.OriginScheduled))
		if err != nil {
			if errors.Is(err, queue.ErrQueueFull) {
				if !fullDomains[r.domain] {
					fullDomains[r.domain] = true
					fullCount++
					if fullCount == openDomainCount {
						break
					}
				}
				continue
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

// videoIDsWithMediaFile returns which of the given video IDs have an on-disk kind=video file.
func (s *Store) videoIDsWithMediaFile(videoIDs []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(videoIDs))
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT video_id, path FROM files
		WHERE kind = 'video' AND video_id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vid int64
		var path string
		if err := rows.Scan(&vid, &path); err != nil {
			return nil, err
		}
		if out[vid] {
			continue
		}
		if fileExists(path) {
			out[vid] = true
		}
	}
	return out, rows.Err()
}

// videoIDsWithPendingDownload returns video IDs among the set with pending/running download or sponsorblock_cut.
func (s *Store) videoIDsWithPendingDownload(videoIDs []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(videoIDs))
	args := make([]any, 0, 4+len(videoIDs))
	args = append(args, queue.KindDownload, queue.KindSponsorblockCut, queue.StatusPending, queue.StatusRunning)
	for i, id := range videoIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT video_id FROM tasks
		WHERE kind IN (?, ?) AND status IN (?, ?) AND video_id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vid int64
		if err := rows.Scan(&vid); err != nil {
			return nil, err
		}
		out[vid] = true
	}
	return out, rows.Err()
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
