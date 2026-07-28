package library

import (
	"encoding/json"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// SeriesWarnLevel is a virtual series health status for list/detail UI.
// Higher severity overwrites lower: error > incomplete > none.
type SeriesWarnLevel string

const (
	SeriesWarnNone       SeriesWarnLevel = ""
	SeriesWarnIncomplete SeriesWarnLevel = "incomplete" // full scan stalled, no tip schedule
	SeriesWarnError      SeriesWarnLevel = "error"      // video download hold or source scan_error
)

// SeriesWarnLevels returns the strongest warn level per series ID.
// incomplete: a source has unfinished full scan, no pending/running scan task, and no tip
// schedule (single or empty scan_cron) - scheduled incomplete is left to the next cron tick.
// error: any video in wanted_download_error / wanted_source_error / verify_failed, or a source whose
// latest scan-related history event is scan_error.
func (s *Store) SeriesWarnLevels(seriesIDs []int64) (map[int64]SeriesWarnLevel, error) {
	out := make(map[int64]SeriesWarnLevel, len(seriesIDs))
	if len(seriesIDs) == 0 {
		return out, nil
	}
	want := make(map[int64]struct{}, len(seriesIDs))
	for _, id := range seriesIDs {
		if id > 0 {
			want[id] = struct{}{}
			out[id] = SeriesWarnNone
		}
	}
	if len(want) == 0 {
		return out, nil
	}

	activeScanSources, err := s.activeScanSourceIDs()
	if err != nil {
		return nil, err
	}

	args := make([]any, 0, len(want))
	for id := range want {
		args = append(args, id)
	}
	ph := sqlIntPlaceholders(len(args))

	rows, err := s.DB.SQL.Query(`
		SELECT id, series_id, full_scan_done, kind, COALESCE(scan_cron, '')
		FROM sources
		WHERE series_id IN (`+ph+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var srcID, seriesID int64
		var done int
		var kind, scanCron string
		if err := rows.Scan(&srcID, &seriesID, &done, &kind, &scanCron); err != nil {
			return nil, err
		}
		if done != 0 {
			continue
		}
		if activeScanSources[srcID] {
			continue
		}
		// Scheduled feeds resume via tip cron; do not escalate to series status.
		if kind != SourceKindSingle && strings.TrimSpace(scanCron) != "" {
			continue
		}
		if out[seriesID] == SeriesWarnNone {
			out[seriesID] = SeriesWarnIncomplete
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	errRows, err := s.DB.SQL.Query(`
		SELECT DISTINCT series_id FROM videos
		WHERE series_id IN (`+ph+`)
		  AND status IN ('wanted_download_error', 'wanted_source_error', 'verify_failed')
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = errRows.Close() }()
	for errRows.Next() {
		var seriesID int64
		if err := errRows.Scan(&seriesID); err != nil {
			return nil, err
		}
		out[seriesID] = SeriesWarnError
	}
	if err := errRows.Err(); err != nil {
		return nil, err
	}

	scanErrSeries, err := s.SeriesIDsWithSourceScanError(argsToInt64(args))
	if err != nil {
		return nil, err
	}
	for seriesID := range scanErrSeries {
		out[seriesID] = SeriesWarnError
	}
	return out, nil
}

func argsToInt64(args []any) []int64 {
	out := make([]int64, 0, len(args))
	for _, a := range args {
		if id, ok := a.(int64); ok {
			out = append(out, id)
		}
	}
	return out
}

// SeriesVideoErrorFlags reports whether a series has any videos in error statuses.
type SeriesVideoErrorFlags struct {
	HasSourceError     bool // wanted_source_error
	SourceErrorCount   int
	HasDownloadError   bool // wanted_download_error
	DownloadErrorCount int
	HasVerifyFailed    bool // verify_failed
	VerifyFailedCount  int
}

// SeriesVideoErrorFlagsMap returns error flags per series ID (missing keys = no errors).
func (s *Store) SeriesVideoErrorFlagsMap(seriesIDs []int64) (map[int64]SeriesVideoErrorFlags, error) {
	out := make(map[int64]SeriesVideoErrorFlags, len(seriesIDs))
	if len(seriesIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(seriesIDs))
	for _, id := range seriesIDs {
		if id > 0 {
			args = append(args, id)
		}
	}
	if len(args) == 0 {
		return out, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT series_id,
		       SUM(CASE WHEN status = 'wanted_source_error' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN status = 'wanted_download_error' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN status = 'verify_failed' THEN 1 ELSE 0 END)
		FROM videos
		WHERE series_id IN (`+sqlIntPlaceholders(len(args))+`)
		  AND status IN ('wanted_source_error', 'wanted_download_error', 'verify_failed')
		GROUP BY series_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seriesID int64
		var srcErr, dlErr, verifyErr int
		if err := rows.Scan(&seriesID, &srcErr, &dlErr, &verifyErr); err != nil {
			return nil, err
		}
		out[seriesID] = SeriesVideoErrorFlags{
			HasSourceError:     srcErr > 0,
			SourceErrorCount:   srcErr,
			HasDownloadError:   dlErr > 0,
			DownloadErrorCount: dlErr,
			HasVerifyFailed:    verifyErr > 0,
			VerifyFailedCount:  verifyErr,
		}
	}
	return out, rows.Err()
}

// CountSeriesWithError returns how many series have SeriesWarnError health
// (video wanted_download_error / wanted_source_error / verify_failed, or a source
// whose latest scan-related history is scan_error). Incomplete full-scan is excluded.
func (s *Store) CountSeriesWithError() (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT series_id FROM videos
			WHERE status IN ('wanted_download_error', 'wanted_source_error', 'verify_failed')
			UNION
			SELECT DISTINCT src.series_id
			FROM sources src
			WHERE (
				SELECT sh.event FROM source_history sh
				WHERE sh.source_id = src.id AND sh.event IN (?, ?)
				ORDER BY sh.id DESC LIMIT 1
			) = ?
		)
	`, SourceHistScanned, SourceHistScanError, SourceHistScanError).Scan(&n)
	return n, err
}

// SeriesWarnLevel returns the strongest warn level for one series.
func (s *Store) SeriesWarnLevel(seriesID int64) (SeriesWarnLevel, error) {
	m, err := s.SeriesWarnLevels([]int64{seriesID})
	if err != nil {
		return SeriesWarnNone, err
	}
	return m[seriesID], nil
}

func (s *Store) activeScanSourceIDs() (map[int64]bool, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT payload FROM tasks
		WHERE kind = ? AND status IN (?, ?)
	`, queue.KindScan, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]bool{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var p struct {
			SourceID int64 `json:"source_id"`
		}
		if json.Unmarshal([]byte(payload), &p) == nil && p.SourceID > 0 {
			out[p.SourceID] = true
		}
	}
	return out, rows.Err()
}
