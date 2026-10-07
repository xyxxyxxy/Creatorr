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

// SeriesListFilter scopes the series admin list (text, catalog metadata, root, quality, delivery, status).
type SeriesListFilter struct {
	Title              string // case-insensitive substring against QField
	QField             string
	RootIDs            []int64
	QualityProfileIDs  []int64
	DeliveryModes      []string // video|audio; OR match
	Statuses           []string // SeriesListStatus*; OR of status predicates
	Studios            []string
	Countries          []string
	MPAAs              []string
	PremieredYears     []int // UTC calendar years; 0 entries ignored
	Genres             []string
	Tags               []string
	Actors             []string
	Empty              []string
	NotEmpty           []string
	Sort               string // title|added; empty = title
	SortDir            string // asc|desc; empty = DefaultSortDir(Sort)
}

// MenuActive reports whether any Filter-menu constraint is set (not search, not sort).
func (f SeriesListFilter) MenuActive() bool {
	return len(uniqPositiveInt64s(f.RootIDs)) > 0 ||
		len(uniqPositiveInt64s(f.QualityProfileIDs)) > 0 ||
		len(seriesDeliveryModes(f.DeliveryModes)) > 0 ||
		seriesListStatusesActive(f.Statuses) ||
		len(trimNonEmptyStrings(f.Studios)) > 0 || len(trimNonEmptyStrings(f.Countries)) > 0 ||
		len(trimNonEmptyStrings(f.MPAAs)) > 0 ||
		len(uniqNonZeroInts(f.PremieredYears)) > 0 || len(f.Genres) > 0 || len(f.Tags) > 0 || len(f.Actors) > 0 ||
		len(f.Empty) > 0 || len(f.NotEmpty) > 0
}

// Active reports whether search or any Filter-menu constraint is set (not sort).
func (f SeriesListFilter) Active() bool {
	return strings.TrimSpace(f.Title) != "" || f.MenuActive()
}

func seriesListStatusesActive(statuses []string) bool {
	for _, st := range statuses {
		if seriesListStatusActive(st) {
			return true
		}
	}
	return false
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

func seriesDeliveryModes(modes []string) []string {
	var out []string
	for _, m := range modes {
		m = NormalizeDeliveryMode(strings.TrimSpace(m))
		if m == DeliveryVideo || m == DeliveryAudio {
			out = append(out, m)
		}
	}
	return out
}

// seriesProgressOpenStatuses: still-open for list progress + Incomplete (wanted, archive wait, download error, verify fail).
const seriesProgressOpenStatuses = `'wanted', 'wanted_archive', 'wanted_download_error', 'downloaded_integrity_failed'`

// seriesListSelectCols + seriesListFromJoins use one video/source aggregate join instead of
// five correlated COUNT subqueries per series row.
const seriesListSelectCols = `s.id, s.title, s.root_id, s.quality_profile_id, s.monitored, s.delivery_mode, s.added_at,
		       r.name, q.name,
		       COALESCE(vc.video_count, 0),
		       COALESCE(vc.downloaded_count, 0),
		       COALESCE(vc.wanted_count, 0),
		       COALESCE(vc.pending_count, 0),
		       COALESCE(sc.source_count, 0),
		       COALESCE(sz.size_bytes, 0)`

const seriesListFromJoins = `
		FROM series s
		JOIN root_folders r ON r.id = s.root_id
		JOIN quality_profiles q ON q.id = s.quality_profile_id
		LEFT JOIN (
			SELECT series_id,
				COUNT(*) AS video_count,
				SUM(CASE WHEN status = 'downloaded' THEN 1 ELSE 0 END) AS downloaded_count,
				SUM(CASE WHEN status IN ('wanted', 'wanted_archive') THEN 1 ELSE 0 END) AS wanted_count,
				SUM(CASE WHEN status IN (` + seriesProgressOpenStatuses + `) THEN 1 ELSE 0 END) AS pending_count,
				SUM(CASE WHEN status IN ('wanted_download_error', 'downloaded_integrity_failed') THEN 1 ELSE 0 END) AS error_count,
				MAX(CASE WHEN upload_date IS NOT NULL AND trim(upload_date) != '' THEN upload_date END) AS last_upload
			FROM videos
			GROUP BY series_id
		) vc ON vc.series_id = s.id
		LEFT JOIN (
			SELECT series_id, COUNT(*) AS source_count
			FROM sources
			GROUP BY series_id
		) sc ON sc.series_id = s.id
		LEFT JOIN (
			SELECT v.series_id, COALESCE(SUM(f.size_bytes), 0) AS size_bytes
			FROM videos v
			JOIN files f ON f.video_id = v.id AND f.kind = 'video' AND f.size_bytes IS NOT NULL
			GROUP BY v.series_id
		) sz ON sz.series_id = s.id`

func appendSeriesListFilterSQL(b *strings.Builder, args *[]any, f SeriesListFilter) {
	if title := strings.TrimSpace(f.Title); title != "" {
		col := seriesTextColumn(f.QField)
		b.WriteString(` AND ` + col + ` LIKE ? ESCAPE '\' COLLATE NOCASE`)
		*args = append(*args, likeContainsPattern(title))
	}
	appendInt64In(b, args, "s.root_id", f.RootIDs)
	appendInt64In(b, args, "s.quality_profile_id", f.QualityProfileIDs)
	appendStringsIn(b, args, "s.delivery_mode", seriesDeliveryModes(f.DeliveryModes), false)
	appendStringsIn(b, args, "s.studio", f.Studios, true)
	appendStringsIn(b, args, "s.country", f.Countries, true)
	appendStringsIn(b, args, "s.mpaa", f.MPAAs, true)
	years := uniqNonZeroInts(f.PremieredYears)
	if len(years) > 0 {
		b.WriteString(` AND s.premiered IS NOT NULL AND trim(s.premiered) != ''`)
		appendIntsIn(b, args, `CAST(strftime('%Y', s.premiered) AS INTEGER)`, years)
	}
	appendJSONStringListMatch(b, args, "s.genres", f.Genres)
	appendJSONStringListMatch(b, args, "s.tags", f.Tags)
	appendJSONActorNameMatch(b, args, "s.actors", f.Actors)
	appendSeriesPresenceSQL(b, f.Empty, f.NotEmpty)
	appendSeriesListStatusesSQL(b, args, f.Statuses)
}

func appendSeriesListStatusesSQL(b *strings.Builder, args *[]any, statuses []string) {
	var parts []string
	for _, st := range statuses {
		st = strings.TrimSpace(st)
		switch st {
		case SeriesListStatusMonitored:
			parts = append(parts, `s.monitored = 1`)
		case SeriesListStatusUnmonitored:
			parts = append(parts, `s.monitored = 0`)
		case SeriesListStatusComplete:
			parts = append(parts, `(SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status IN ('downloaded', `+seriesProgressOpenStatuses+`)) > 0
			AND NOT EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN (`+seriesProgressOpenStatuses+`)
			)`)
		case SeriesListStatusIncomplete:
			parts = append(parts, `EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN (`+seriesProgressOpenStatuses+`)
			)`)
		case SeriesListStatusHasErrors:
			parts = append(parts, `(
			EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN ('wanted_download_error', 'downloaded_integrity_failed')
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
	appendAndOrGroup(b, parts)
}

func scanSeriesListRow(rows *sql.Rows) (Series, error) {
	var ser Series
	var mon int
	if err := rows.Scan(
		&ser.ID, &ser.Title, &ser.RootID, &ser.QualityProfileID, &mon, &ser.DeliveryMode, &ser.AddedAt,
		&ser.RootName, &ser.QualityProfileName,
		&ser.VideoCount, &ser.DownloadedCount, &ser.WantedCount, &ser.PendingCount,
		&ser.SourceCount, &ser.SizeBytes,
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

// ListMostWantedSeries returns series ordered like Browser Series sort=wanted
// (wanted + wanted_archive count, highest first), limited to one gallery row.
func (s *Store) ListMostWantedSeries(limit int) ([]Series, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.ListSeriesFiltered(SeriesListFilter{
		Sort:    SortWanted,
		SortDir: SortDirDesc,
	}, limit, 0)
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
	b.WriteString(` ORDER BY ` + seriesOrderByClause(filter.Sort, filter.SortDir))
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
