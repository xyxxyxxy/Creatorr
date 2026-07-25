package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func queueDomain(rawURL string) string {
	return queue.DomainFromURL(rawURL)
}

// NamingDomain returns a hostname for {domain}, or empty when unknown.
func NamingDomain(rawURL string) string {
	return namingDomain(rawURL)
}

// namingDomain returns a hostname for {domain}, or empty when unknown.
func namingDomain(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	d := queueDomain(rawURL)
	if d == "" || d == "unknown" {
		return ""
	}
	return d
}

func enqueueDownloadParams(videoID, seriesID int64, domain string) queue.EnqueueParams {
	return queue.EnqueueParams{
		Kind:     queue.KindDownload,
		Domain:   domain,
		SeriesID: seriesID,
		VideoID:  videoID,
		Message:  "Download",
		Payload:  map[string]any{"video_id": videoID},
	}
}

func enqueueDownloadNowParams(videoID, seriesID int64, domain string) queue.EnqueueParams {
	p := enqueueDownloadParams(videoID, seriesID, domain)
	p.Message = "Download now"
	p.Priority = queue.PriorityDownloadNow
	p.BypassDownloadCap = true
	return p
}

// EnqueueTipScanDownloads enqueues downloads (or pack_stream) for newly created tip-scan
// videos with status wanted, ordered by download_wanted_order. Stops on domain queue full.
// Caller should only invoke after tip scan (not full scan) when download_new_on_scan is on.
func (s *Store) EnqueueTipScanDownloads(seriesID int64, createdIDs []int64) (int, error) {
	if s.Queue == nil || len(createdIDs) == 0 {
		return 0, nil
	}
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return 0, err
	}
	if !ser.Monitored {
		return 0, nil
	}

	orderSQL := videoDownloadOrderOldest
	if raw, err := settings.Get(s.DB, settings.KeyDownloadWantedOrder); err == nil {
		if settings.NormalizeDownloadWantedOrder(raw) == settings.DownloadWantedOrderNewest {
			orderSQL = videoDownloadOrderNewest
		}
	}

	placeholders := make([]string, len(createdIDs))
	args := make([]any, 0, len(createdIDs)+1)
	for i, id := range createdIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, "wanted")
	q := `
		SELECT v.id FROM videos v
		WHERE v.id IN (` + strings.Join(placeholders, ",") + `) AND v.status = ?
		ORDER BY ` + orderSQL
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	for _, id := range ids {
		if ser.IsStream() {
			_, err = s.EnqueuePackStream(id, false)
		} else {
			_, err = s.EnqueueDownload(id)
		}
		if err != nil {
			if errors.Is(err, queue.ErrQueueFull) || errors.Is(err, ErrConflict) {
				// Cap full or already queued / mapped conflict — stop on queue full.
				if errors.Is(err, queue.ErrQueueFull) || strings.Contains(err.Error(), "download queue full") {
					return n, nil
				}
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}


func (s *Store) sourceDomainActive(src Source) (bool, error) {
	return domains.IsActive(s.DB, queueDomain(src.URL))
}

// EnqueueScan queues full scans for sources that still need archive indexing.
// Does not require series.monitored (full scan always runs). Tip Scan is separate.
func (s *Store) EnqueueScan(seriesID int64) (firstTaskID int64, err error) {
	n, first, err := s.EnqueueFullScansForSeries(seriesID)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("%w: no scan tasks enqueued", ErrInvalid)
	}
	return first, nil
}

// EnqueueFullScansForSeries enqueues one full scan per source with unfinished archive index.
// Series monitored is not required. Feed sources with full scan done use
// EnqueueScansForSeries (tip Scan) instead.
func (s *Store) EnqueueFullScansForSeries(seriesID int64) (count int, firstTaskID int64, err error) {
	if s.Queue == nil {
		return 0, 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return 0, 0, err
	}
	sources, err := s.listSources(seriesID)
	if err != nil {
		return 0, 0, err
	}
	if len(sources) == 0 {
		return 0, 0, fmt.Errorf("%w: add a source first", ErrInvalid)
	}
	for _, src := range sources {
		if src.FullScanDone {
			continue
		}
		ok, err := s.sourceDomainActive(src)
		if err != nil {
			return count, firstTaskID, err
		}
		if !ok {
			continue
		}
		id, err := s.EnqueueScanSource(src.ID)
		if err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			return count, firstTaskID, err
		}
		count++
		if firstTaskID == 0 {
			firstTaskID = id
		}
	}
	if count == 0 {
		var nDone, nInactive, nNeed int
		for _, src := range sources {
			if src.FullScanDone {
				nDone++
				continue
			}
			nNeed++
			ok, _ := s.sourceDomainActive(src)
			if !ok {
				nInactive++
			}
		}
		if nDone == len(sources) {
			return 0, 0, fmt.Errorf("%w: full scan already done; use Scan or Restart full scan", ErrInvalid)
		}
		if nNeed > 0 && nInactive == nNeed {
			return 0, 0, fmt.Errorf("%w: all source domains inactive", ErrInvalid)
		}
		return 0, 0, fmt.Errorf("%w: scan already queued", ErrConflict)
	}
	return count, firstTaskID, nil
}

// EnqueueScansForSeries enqueues tip Scan for feed sources with full scan done.
// Manual / series kick: allowed even when scan_cron is never.
func (s *Store) EnqueueScansForSeries(seriesID int64) (count int, firstTaskID int64, err error) {
	if s.Queue == nil {
		return 0, 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return 0, 0, err
	}
	if !ser.Monitored {
		return 0, 0, fmt.Errorf("%w: series unmonitored", ErrInvalid)
	}
	sources, err := s.listSources(seriesID)
	if err != nil {
		return 0, 0, err
	}
	if len(sources) == 0 {
		return 0, 0, fmt.Errorf("%w: add a source first", ErrInvalid)
	}
	var eligible int
	for _, src := range sources {
		if src.IsSingle() || !src.FullScanDone {
			continue
		}
		ok, err := s.sourceDomainActive(src)
		if err != nil {
			return count, firstTaskID, err
		}
		if !ok {
			continue
		}
		eligible++
		id, err := s.EnqueueScanSource(src.ID)
		if err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			return count, firstTaskID, err
		}
		count++
		if firstTaskID == 0 {
			firstTaskID = id
		}
	}
	if count == 0 {
		if eligible == 0 {
			return 0, 0, fmt.Errorf("%w: no indexed feed sources ready for Scan", ErrInvalid)
		}
		return 0, 0, fmt.Errorf("%w: scan already queued", ErrConflict)
	}
	return count, firstTaskID, nil
}

// EnqueueScanSource queues one scan for a single source (index-only).
// Full scan runs whenever the domain is active - series.monitored not required.
// Tip Scan still requires series monitored ∧ domain active.
func (s *Store) EnqueueScanSource(sourceID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	src, err := s.GetSourceByID(sourceID)
	if err != nil {
		return 0, err
	}
	domOK, err := s.sourceDomainActive(*src)
	if err != nil {
		return 0, err
	}
	if !domOK {
		return 0, fmt.Errorf("%w: domain inactive", ErrInvalid)
	}
	if src.IsSingle() && src.FullScanDone {
		return 0, fmt.Errorf("%w: single source skips Scan; use Restart full scan", ErrInvalid)
	}
	if src.FullScanDone {
		ok, err := s.SeriesIsMonitored(src.SeriesID)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("%w: series unmonitored", ErrInvalid)
		}
	}
	busy, err := s.HasActiveScanForSource(sourceID)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: scan already queued or running", ErrConflict)
	}
	domain := queueDomain(src.URL)
	mode := "full"
	msg := "Full scan"
	if src.FullScanDone {
		mode = "scan"
		msg = "Scan"
	}
	payload := map[string]any{
		"series_id": seriesIDJSON(src.SeriesID),
		"source_id": sourceID,
		"mode":      mode,
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindScan,
		Domain:   domain,
		SeriesID: src.SeriesID,
		Message:  msg,
		Payload:  payload,
	})
	if errors.Is(err, queue.ErrDuplicate) {
		return 0, fmt.Errorf("%w: scan already queued or running", ErrConflict)
	}
	return id, err
}

func seriesIDJSON(id int64) int64 { return id }

func (s *Store) hasPendingScanForSource(sourceID int64) (bool, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, payload FROM tasks
		WHERE kind = ? AND status = ?
	`, queue.KindScan, queue.StatusPending)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return false, err
		}
		var p struct {
			SourceID int64 `json:"source_id"`
		}
		if json.Unmarshal([]byte(payload), &p) == nil && p.SourceID == sourceID {
			return true, nil
		}
	}
	return false, rows.Err()
}

// HasPendingScanForSource reports if a scan is queued (pending) for the source.
func (s *Store) HasPendingScanForSource(sourceID int64) (bool, error) {
	return s.hasPendingScanForSource(sourceID)
}

// HasActiveScanForSource reports pending or running scan for the source (UI stall warning).
func (s *Store) HasActiveScanForSource(sourceID int64) (bool, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, payload FROM tasks
		WHERE kind = ? AND status IN (?, ?)
	`, queue.KindScan, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return false, err
		}
		var p struct {
			SourceID int64 `json:"source_id"`
		}
		if json.Unmarshal([]byte(payload), &p) == nil && p.SourceID == sourceID {
			return true, nil
		}
	}
	return false, rows.Err()
}

// GetSourceByID loads a source without series scope.
func (s *Store) GetSourceByID(sourceID int64) (*Source, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT `+sourceSelectCols+`
		FROM sources WHERE id = ?
	`, sourceID)
	src, err := scanSource(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &src, nil
}

// ResetFullScan clears full-scan done state (Restart full scan). Keeps indexed videos.
func (s *Store) ResetFullScan(sourceID int64) error {
	res, err := s.DB.SQL.Exec(`
		UPDATE sources SET full_scan_done = 0 WHERE id = ?
	`, sourceID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// FullRescanSource resets full scan and enqueues a full scan for one source.
func (s *Store) FullRescanSource(sourceID int64) (int64, error) {
	active, err := s.HasActiveScanForSource(sourceID)
	if err != nil {
		return 0, err
	}
	if active {
		return 0, fmt.Errorf("%w: scan already queued or running", ErrConflict)
	}
	if err := s.ResetFullScan(sourceID); err != nil {
		return 0, err
	}
	return s.EnqueueScanSource(sourceID)
}

// FullRescanSeries resets full scan on all sources and enqueues full scans.
func (s *Store) FullRescanSeries(seriesID int64) (int, int64, error) {
	srcs, err := s.listSources(seriesID)
	if err != nil {
		return 0, 0, err
	}
	if len(srcs) == 0 {
		return 0, 0, fmt.Errorf("%w: add a source first", ErrInvalid)
	}
	for _, src := range srcs {
		if err := s.ResetFullScan(src.ID); err != nil {
			return 0, 0, err
		}
	}
	return s.EnqueueFullScansForSeries(seriesID)
}

// MarkFullScanDone marks archive indexing complete for a source.
func (s *Store) MarkFullScanDone(sourceID int64) error {
	_, err := s.DB.SQL.Exec(`
		UPDATE sources SET full_scan_done = 1 WHERE id = ?
	`, sourceID)
	return err
}

// VideoExistsByRemote reports whether series already has this remote id.
func (s *Store) VideoExistsByRemote(seriesID int64, remoteID string) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos
		WHERE series_id = ? AND remote_id = ?
	`, seriesID, remoteID).Scan(&n)
	return n > 0, err
}
