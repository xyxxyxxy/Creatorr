package library

import (
	"database/sql"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// Source list sort values (empty = series).
const (
	SortSourceSeries      = "series"
	SortSourceLabel       = "name" // Explorer UI; DB column remains sources.label
	SortSourceDomain      = "domain"
	SortSourceLastScanned = "last_scanned"
)

// Source list filter values.
const (
	SourceFullScanDone       = "done"
	SourceFullScanIncomplete = "incomplete"
	SourceScheduleOn         = "on"
	SourceScheduleOff        = "off"
	SourceDiscoveredWanted   = "wanted"
	SourceDiscoveredIgnored  = "ignored"
)

// SourceFilterFacets are distinct Source Filter values present in a series scope.
// Used to hide Filter menu entries that cannot change the result (pre-filtered lists).
type SourceFilterFacets struct {
	Domains               []string
	HasFullScanDone       bool
	HasFullScanIncomplete bool
	HasScheduleOn         bool
	HasScheduleOff        bool
	HasDiscoveredWanted   bool
	HasDiscoveredIgnored  bool
}

// SourceFilterFacetsForSeries returns Filter facets for one series (empty if seriesID < 1).
func (s *Store) SourceFilterFacetsForSeries(seriesID int64) (SourceFilterFacets, error) {
	var out SourceFilterFacets
	if seriesID < 1 {
		return out, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT url, scan_cron, full_scan_done, index_as_ignored FROM sources WHERE series_id = ?`, seriesID)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	domains := map[string]struct{}{}
	for rows.Next() {
		var rawURL, cron string
		var fullDone, indexAsIgnored int
		if err := rows.Scan(&rawURL, &cron, &fullDone, &indexAsIgnored); err != nil {
			return out, err
		}
		if d := queue.DomainFromURL(rawURL); d != "" && d != "unknown" {
			domains[d] = struct{}{}
		}
		if fullDone != 0 {
			out.HasFullScanDone = true
		} else {
			out.HasFullScanIncomplete = true
		}
		src := Source{ScanCron: cron}
		if src.ScanCronNever() {
			out.HasScheduleOff = true
		} else {
			out.HasScheduleOn = true
		}
		if indexAsIgnored != 0 {
			out.HasDiscoveredIgnored = true
		} else {
			out.HasDiscoveredWanted = true
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for d := range domains {
		out.Domains = append(out.Domains, d)
	}
	sort.Strings(out.Domains)
	return out, nil
}

// lastScannedAtSQL is newest scanned/scan_error created_at for a source (sticky Status clock).
const lastScannedAtSQL = `(
	SELECT sh.created_at FROM source_history sh
	WHERE sh.source_id = src.id AND sh.event IN ('scanned','scan_error')
	ORDER BY sh.id DESC LIMIT 1
)`

// sourceDomainSortSQL approximates DomainFromURL for ORDER BY (scheme/www stripped, host before path).
const sourceDomainSortSQL = `lower(replace(
	CASE
		WHEN instr(
			CASE WHEN instr(src.url, '://') > 0 THEN substr(src.url, instr(src.url, '://') + 3) ELSE src.url END,
			'/'
		) > 0
		THEN substr(
			CASE WHEN instr(src.url, '://') > 0 THEN substr(src.url, instr(src.url, '://') + 3) ELSE src.url END,
			1,
			instr(
				CASE WHEN instr(src.url, '://') > 0 THEN substr(src.url, instr(src.url, '://') + 3) ELSE src.url END,
				'/'
			) - 1
		)
		ELSE CASE WHEN instr(src.url, '://') > 0 THEN substr(src.url, instr(src.url, '://') + 3) ELSE src.url END
	END,
	'www.',
	''
))`

// Source text query field ids for q_field (default name; DB column sources.label).
const (
	QFieldSourceURL    = "url"
	QFieldSourceLabel  = "name"
	QFieldSourceSeries = "series"
)

// SourceListFilter narrows library-wide or series-scoped source lists.
type SourceListFilter struct {
	SeriesID        int64    // series-detail lock (series_id); browser multi uses SeriesIDs
	SeriesIDs       []int64  // browser series= multi when SeriesID==0
	Q               string   // case-insensitive substring against QField
	QField          string   // url|name|series; empty = name; legacy label accepted
	Domains         []string // hostname facets; OR of url LIKE
	Studios         []string // default video metadata; OR
	Countries       []string
	MPAAs           []string
	Genres          []string
	Tags            []string
	Actors          []string // actor names in default metadata
	HasError        *bool    // nil = any; true = last event scan_error; false = not
	FullScanDone    *bool    // nil = any; true = done; false = incomplete
	ScheduleOn      *bool    // nil = any; true = non-empty cron; false = Never
	IndexAsIgnored  *bool    // nil = any; discovered Wanted/Ignored flag
	SeriesMonitored *bool    // nil = any
	Sort            string
	SortDir         string
}

// SourceListRow is a source plus series title for Explorer rows.
type SourceListRow struct {
	Source
	SeriesTitle     string
	SeriesMonitored bool
	LastScannedAt   string // sticky last scanned/scan_error time; empty = never
}

// MenuActive reports whether any Filter-menu constraint is set (not search, not sort).
// SeriesID counts when used as a browser Series filter (series-detail lock is gated in the UI).
func (f SourceListFilter) MenuActive() bool {
	return f.SeriesID > 0 ||
		len(uniqPositiveInt64s(f.SeriesIDs)) > 0 ||
		len(trimNonEmptyStrings(f.Domains)) > 0 ||
		len(trimNonEmptyStrings(f.Studios)) > 0 ||
		len(trimNonEmptyStrings(f.Countries)) > 0 ||
		len(trimNonEmptyStrings(f.MPAAs)) > 0 ||
		len(trimNonEmptyStrings(f.Genres)) > 0 ||
		len(trimNonEmptyStrings(f.Tags)) > 0 ||
		len(trimNonEmptyStrings(f.Actors)) > 0 ||
		f.HasError != nil ||
		f.FullScanDone != nil ||
		f.ScheduleOn != nil ||
		f.IndexAsIgnored != nil ||
		f.SeriesMonitored != nil
}

// Active reports whether search or any Filter-menu constraint is set.
func (f SourceListFilter) Active() bool {
	return strings.TrimSpace(f.Q) != "" || f.MenuActive()
}

// NormalizeSourceQField returns a known Sources text field id or name.
func NormalizeSourceQField(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case QFieldSourceURL:
		return QFieldSourceURL
	case QFieldSourceSeries:
		return QFieldSourceSeries
	case QFieldSourceLabel, "label": // "label" = legacy Explorer query value
		return QFieldSourceLabel
	default:
		return QFieldSourceLabel
	}
}

func (f SourceListFilter) where() (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString(` FROM sources src JOIN series s ON s.id = src.series_id WHERE 1=1`)
	if f.SeriesID > 0 {
		b.WriteString(` AND src.series_id = ?`)
		args = append(args, f.SeriesID)
	} else {
		appendInt64In(&b, &args, "src.series_id", f.SeriesIDs)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + q + "%"
		switch NormalizeSourceQField(f.QField) {
		case QFieldSourceLabel:
			b.WriteString(` AND IFNULL(src.label,'') LIKE ?`)
			args = append(args, like)
		case QFieldSourceSeries:
			b.WriteString(` AND s.title LIKE ?`)
			args = append(args, like)
		default:
			b.WriteString(` AND src.url LIKE ?`)
			args = append(args, like)
		}
	}
	domains := trimNonEmptyStrings(f.Domains)
	if len(domains) > 0 {
		var parts []string
		for range domains {
			parts = append(parts, `src.url LIKE ?`)
		}
		appendAndOrGroup(&b, parts)
		for _, d := range domains {
			args = append(args, "%"+d+"%")
		}
	}
	appendStringsIn(&b, &args, "src.studio", f.Studios, true)
	appendStringsIn(&b, &args, "src.country", f.Countries, true)
	appendStringsIn(&b, &args, "src.mpaa", f.MPAAs, true)
	appendJSONStringListMatch(&b, &args, "src.genres", f.Genres)
	appendJSONStringListMatch(&b, &args, "src.tags", f.Tags)
	appendJSONActorNameMatch(&b, &args, "src.actors", f.Actors)
	if f.HasError != nil {
		sub := `(
				SELECT sh.event FROM source_history sh
				WHERE sh.source_id = src.id AND sh.event IN ('scanned','scan_error')
				ORDER BY sh.id DESC LIMIT 1
			)`
		if *f.HasError {
			b.WriteString(` AND ` + sub + ` = 'scan_error'`)
		} else {
			b.WriteString(` AND (` + sub + ` IS NULL OR ` + sub + ` != 'scan_error')`)
		}
	}
	if f.FullScanDone != nil {
		if *f.FullScanDone {
			b.WriteString(` AND src.full_scan_done = 1`)
		} else {
			b.WriteString(` AND src.full_scan_done = 0`)
		}
	}
	if f.ScheduleOn != nil {
		cronOff := `(TRIM(IFNULL(src.scan_cron,'')) = '' OR LOWER(TRIM(src.scan_cron)) = 'never')`
		if *f.ScheduleOn {
			b.WriteString(` AND NOT ` + cronOff)
		} else {
			b.WriteString(` AND ` + cronOff)
		}
	}
	if f.IndexAsIgnored != nil {
		if *f.IndexAsIgnored {
			b.WriteString(` AND src.index_as_ignored = 1`)
		} else {
			b.WriteString(` AND src.index_as_ignored = 0`)
		}
	}
	if f.SeriesMonitored != nil {
		if *f.SeriesMonitored {
			b.WriteString(` AND s.monitored = 1`)
		} else {
			b.WriteString(` AND s.monitored = 0`)
		}
	}
	return b.String(), args
}

func (f SourceListFilter) orderBy() string {
	sort := strings.ToLower(strings.TrimSpace(f.Sort))
	if sort == "" {
		sort = SortSourceSeries
	}
	desc := NormalizeSortDir(sort, f.SortDir) == SortDirDesc
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	switch sort {
	case SortTitle, SortSourceSeries:
		return "s.title COLLATE NOCASE " + dir + ", src.id ASC"
	case "url", SortSourceLabel, "label": // "label" = legacy sort query value
		return "IFNULL(src.label, src.url) COLLATE NOCASE " + dir + ", src.id ASC"
	case SortSourceDomain:
		return sourceDomainSortSQL + " " + dir + ", src.id ASC"
	case SortSourceLastScanned, "scanned":
		// Never-scanned last. Empty dir uses DefaultSortDir (desc = newest first).
		if desc {
			return lastScannedAtSQL + ` IS NULL, ` + lastScannedAtSQL + ` DESC, src.id ASC`
		}
		return lastScannedAtSQL + ` IS NULL, ` + lastScannedAtSQL + ` ASC, src.id ASC`
	default:
		return "s.title COLLATE NOCASE ASC, src.id ASC"
	}
}

// CountSourcesFiltered counts sources matching filter.
func (s *Store) CountSourcesFiltered(filter SourceListFilter) (int, error) {
	where, args := filter.where()
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*)`+where, args...).Scan(&n)
	return n, err
}

func scanSourceListRow(scanner interface {
	Scan(dest ...any) error
}) (SourceListRow, error) {
	var row SourceListRow
	var label, titleInclude, titleExclude sql.NullString
	var indexAsIgnored, fullScanDone int
	var genresRaw, tagsRaw, actorsRaw string
	var specialFeature sql.NullString
	var lastScanned sql.NullString
	err := scanner.Scan(
		&row.ID, &row.SeriesID, &row.URL, &label, &row.ScanCron, &indexAsIgnored,
		&titleInclude, &titleExclude, &row.FullScanLimit, &fullScanDone,
		&row.Studio, &row.Country, &row.MPAA,
		&genresRaw, &tagsRaw, &actorsRaw, &specialFeature,
		&row.SeriesTitle, &row.SeriesMonitored, &lastScanned,
	)
	if err != nil {
		return row, err
	}
	row.Label = label
	row.IndexAsIgnored = indexAsIgnored != 0
	row.FullScanDone = fullScanDone != 0
	row.TitleRegexpInclude = titleInclude.String
	row.TitleRegexpExclude = titleExclude.String
	row.Studio = strings.TrimSpace(row.Studio)
	row.Country = strings.TrimSpace(row.Country)
	row.MPAA = strings.TrimSpace(row.MPAA)
	row.Genres = decodeStringSlice(genresRaw)
	row.Tags = decodeStringSlice(tagsRaw)
	row.Actors = decodeActors(actorsRaw)
	row.SpecialFeature = scanNullPackRole(specialFeature)
	if lastScanned.Valid {
		row.LastScannedAt = lastScanned.String
	}
	return row, nil
}

// ListSourceIDsFiltered returns all source ids matching filter (Select all bulk).
func (s *Store) ListSourceIDsFiltered(filter SourceListFilter) ([]int64, error) {
	where, args := filter.where()
	q := `SELECT src.id` + where + ` ORDER BY ` + filter.orderBy()
	rows, err := s.DB.SQL.Query(q, args...)
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

// ListSourcesFiltered returns a page of sources with series titles.
func (s *Store) ListSourcesFiltered(filter SourceListFilter, limit, offset int) ([]SourceListRow, error) {
	if limit < 1 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	where, args := filter.where()
	q := `SELECT src.id, src.series_id, src.url, src.label, src.scan_cron, src.index_as_ignored,
		src.title_regexp_include, src.title_regexp_exclude, src.full_scan_limit, src.full_scan_done,
		COALESCE(src.studio,''), COALESCE(src.country,''), COALESCE(src.mpaa,''),
		COALESCE(src.genres,'[]'), COALESCE(src.tags,'[]'), COALESCE(src.actors,'[]'), src.special_feature,
		s.title, s.monitored, ` + lastScannedAtSQL + where + ` ORDER BY ` + filter.orderBy() + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SourceListRow
	for rows.Next() {
		row, err := scanSourceListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
