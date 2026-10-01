package library

import (
	"database/sql"
	"strings"
)

const (
	SeriesListStatusMonitored   = "monitored"
	SeriesListStatusUnmonitored = "unmonitored"
	SeriesListStatusComplete    = "complete"
	SeriesListStatusIncomplete  = "incomplete"
	SeriesListStatusHasErrors   = "has_errors"
)

// SeriesListFilter scopes the series admin list (title, root, quality, delivery, status).
type SeriesListFilter struct {
	Title            string // case-insensitive substring; empty = any
	RootID           int64  // 0 = any
	QualityProfileID int64  // 0 = any
	DeliveryMode     string // video|audio; empty = any
	Status           string // SeriesListStatus*; empty = any
}

// Active reports whether any series list filter constraint is set.
func (f SeriesListFilter) Active() bool {
	return strings.TrimSpace(f.Title) != "" || f.RootID > 0 || f.QualityProfileID > 0 ||
		f.DeliveryMode == DeliveryVideo || f.DeliveryMode == DeliveryAudio ||
		seriesListStatusActive(f.Status)
}

func seriesListStatusActive(status string) bool {
	switch status {
	case SeriesListStatusMonitored, SeriesListStatusUnmonitored,
		SeriesListStatusComplete, SeriesListStatusIncomplete, SeriesListStatusHasErrors:
		return true
	default:
		return false
	}
}

// seriesProgressOpenStatuses: still-open for list progress + Incomplete (wanted, archive wait, download error, verify fail).
const seriesProgressOpenStatuses = `'wanted', 'wanted_archive', 'wanted_download_error', 'integrity_check_failed'`

// seriesListSelectCols + seriesListFromJoins use one video/source aggregate join instead of
// five correlated COUNT subqueries per series row.
const seriesListSelectCols = `s.id, s.title, s.root_id, s.quality_profile_id, s.monitored, s.delivery_mode, s.added_at,
		       r.name, q.name,
		       COALESCE(vc.video_count, 0),
		       COALESCE(vc.downloaded_count, 0),
		       COALESCE(vc.wanted_count, 0),
		       COALESCE(vc.pending_count, 0),
		       COALESCE(sc.source_count, 0)`

const seriesListFromJoins = `
		FROM series s
		JOIN root_folders r ON r.id = s.root_id
		JOIN quality_profiles q ON q.id = s.quality_profile_id
		LEFT JOIN (
			SELECT series_id,
				COUNT(*) AS video_count,
				SUM(CASE WHEN status = 'downloaded' THEN 1 ELSE 0 END) AS downloaded_count,
				SUM(CASE WHEN status IN ('wanted', 'wanted_archive') THEN 1 ELSE 0 END) AS wanted_count,
				SUM(CASE WHEN status IN (` + seriesProgressOpenStatuses + `) THEN 1 ELSE 0 END) AS pending_count
			FROM videos
			GROUP BY series_id
		) vc ON vc.series_id = s.id
		LEFT JOIN (
			SELECT series_id, COUNT(*) AS source_count
			FROM sources
			GROUP BY series_id
		) sc ON sc.series_id = s.id`

func appendSeriesListFilterSQL(b *strings.Builder, args *[]any, f SeriesListFilter) {
	if title := strings.TrimSpace(f.Title); title != "" {
		b.WriteString(` AND s.title LIKE ? ESCAPE '\' COLLATE NOCASE`)
		*args = append(*args, likeContainsPattern(title))
	}
	if f.RootID > 0 {
		b.WriteString(` AND s.root_id = ?`)
		*args = append(*args, f.RootID)
	}
	if f.QualityProfileID > 0 {
		b.WriteString(` AND s.quality_profile_id = ?`)
		*args = append(*args, f.QualityProfileID)
	}
	if f.DeliveryMode == DeliveryVideo || f.DeliveryMode == DeliveryAudio {
		b.WriteString(` AND s.delivery_mode = ?`)
		*args = append(*args, f.DeliveryMode)
	}
	switch f.Status {
	case SeriesListStatusMonitored:
		b.WriteString(` AND s.monitored = 1`)
	case SeriesListStatusUnmonitored:
		b.WriteString(` AND s.monitored = 0`)
	case SeriesListStatusComplete:
		// Match list progress: has downloaded or open work, and no open work left.
		b.WriteString(` AND (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status IN ('downloaded', ` + seriesProgressOpenStatuses + `)) > 0
			AND NOT EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN (` + seriesProgressOpenStatuses + `)
			)`)
	case SeriesListStatusIncomplete:
		b.WriteString(` AND EXISTS (
			SELECT 1 FROM videos v
			WHERE v.series_id = s.id AND v.status IN (` + seriesProgressOpenStatuses + `)
		)`)
	case SeriesListStatusHasErrors:
		b.WriteString(` AND (
			EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN ('wanted_download_error', 'integrity_check_failed')
			)
			OR EXISTS (
				SELECT 1 FROM sources src
				WHERE src.series_id = s.id
				  AND (
				    SELECT sh.event FROM source_history sh
				    WHERE sh.source_id = src.id AND sh.event IN (?, ?)
				    ORDER BY sh.id DESC LIMIT 1
				  ) = ?
			)
		)`)
		*args = append(*args, SourceHistScanned, SourceHistScanError, SourceHistScanError)
	}
}

func scanSeriesListRow(rows *sql.Rows) (Series, error) {
	var ser Series
	var mon int
	if err := rows.Scan(
		&ser.ID, &ser.Title, &ser.RootID, &ser.QualityProfileID, &mon, &ser.DeliveryMode, &ser.AddedAt,
		&ser.RootName, &ser.QualityProfileName,
		&ser.VideoCount, &ser.DownloadedCount, &ser.WantedCount, &ser.PendingCount,
		&ser.SourceCount,
	); err != nil {
		return Series{}, err
	}
	ser.Monitored = mon != 0
	ser.DeliveryMode = NormalizeDeliveryMode(ser.DeliveryMode)
	return ser, nil
}

func (s *Store) ListSeries() ([]Series, error) {
	out, err := s.ListSeriesFiltered(SeriesListFilter{}, 0, 0)
	if err != nil {
		return nil, err
	}
	// Load sources after closing the series rows - MaxOpenConns(1) deadlocks on nested queries.
	for i := range out {
		srcs, err := s.listSources(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Sources = srcs
	}
	return out, nil
}

// CountSeriesFiltered returns how many series match the filter.
func (s *Store) CountSeriesFiltered(filter SeriesListFilter) (int, error) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM series s WHERE 1=1`)
	args := []any{}
	appendSeriesListFilterSQL(&b, &args, filter)
	var n int
	err := s.DB.SQL.QueryRow(b.String(), args...).Scan(&n)
	return n, err
}

// ListSeriesFiltered returns series matching filter, newest title order.
// limit <= 0 means no LIMIT (all matches). offset ignored when limit <= 0.
func (s *Store) ListSeriesFiltered(filter SeriesListFilter, limit, offset int) ([]Series, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT ` + seriesListSelectCols + seriesListFromJoins + `
		WHERE 1=1`)
	args := []any{}
	appendSeriesListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY s.title COLLATE NOCASE`)
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		b.WriteString(` LIMIT ? OFFSET ?`)
		args = append(args, limit, offset)
	}
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Series
	for rows.Next() {
		ser, err := scanSeriesListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ser)
	}
	return out, rows.Err()
}
