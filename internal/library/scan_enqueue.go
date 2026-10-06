package library

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// SeriesIDsMonitored returns series with series.monitored=1.
func (s *Store) SeriesIDsMonitored() ([]int64, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM series WHERE monitored = 1 ORDER BY id
	`)
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

// SeriesIDsWithMonitoredSources is kept for callers; prefers series.monitored.
func (s *Store) SeriesIDsWithMonitoredSources() ([]int64, error) {
	return s.SeriesIDsMonitored()
}

// SeriesIsMonitored reports whether series.monitored is on.
// When false, scheduled tip Scan and auto download-wanted stay off; manual tip
// Scan / full scan / Download now may still enqueue. Already-queued tasks are
// left alone except CancelPendingTipScansForSeries on unmonitor.
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
// Sources with a non-empty scan_cron on monitored series: tip Scan when
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
		WHERE ser.monitored = 1
		  AND TRIM(COALESCE(src.scan_cron, '')) != ''
		  AND LOWER(TRIM(src.scan_cron)) != 'never'
		ORDER BY src.id
	`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
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
		if _, err := s.EnqueueScanSource(r.id, queue.OriginScheduled); err != nil {
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
