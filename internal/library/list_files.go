package library

import (
	"strings"
)

// File list sort keys.
const (
	SortFilePath     = "path"
	SortFileKind     = "kind"
	SortFileSize     = "size"
	SortFileAcquired = "acquired"
	SortFileSeries   = "series"
)

// File Explorer Status filter values (match Integrity column labels).
const (
	FileStatusFailed    = "failed"
	FileStatusOK        = "ok"
	FileStatusUnchecked = "unchecked"
	FileStatusInactive  = "inactive"
	FileStatusNA        = "na"
)

// FileListFilter narrows DB-only files Explorer lists (no os.Stat).
type FileListFilter struct {
	SeriesID int64    // 0 = all; scope lock or browser filter
	VideoID  int64    // 0 = all; video-detail embed
	Kinds    []string // OR via IN
	Statuses []string // failed|ok|unchecked|inactive|na; OR of status predicates
	Q        string   // path substring
	Sort     string
	SortDir  string
}

// FileListRow is one files row plus titles for Explorer.
type FileListRow struct {
	VideoFile
	SeriesTitle string
	VideoTitle  string // empty for series-meta
}

// MenuActive reports whether any Filter-menu constraint is set (not search, not
// series/video scope locks).
func (f FileListFilter) MenuActive() bool {
	return len(trimNonEmptyStrings(f.Kinds)) > 0 ||
		len(normalizeFileStatuses(f.Statuses)) > 0
}

// Active reports whether search, Filter-menu, or a scope lock is set.
func (f FileListFilter) Active() bool {
	return f.SeriesID > 0 ||
		f.VideoID > 0 ||
		strings.TrimSpace(f.Q) != "" ||
		f.MenuActive()
}

func normalizeFileStatuses(raw []string) []string {
	var out []string
	for _, s := range raw {
		if n := NormalizeFileStatus(s); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// NormalizeFileStatus returns a known Status filter value or "".
func NormalizeFileStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case FileStatusFailed, FileStatusOK, FileStatusUnchecked, FileStatusInactive, FileStatusNA:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func fileIntegrityFailedSQL() string {
	return `(
			f.content_hash_checked_at IS NOT NULL AND TRIM(f.content_hash_checked_at) != ''
			AND (
				f.content_hash_ok_at IS NULL OR TRIM(f.content_hash_ok_at) = ''
				OR f.content_hash_checked_at > f.content_hash_ok_at
			)
		)`
}

func (f FileListFilter) where() (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString(`
		FROM files f
		LEFT JOIN series s ON s.id = f.series_id
		LEFT JOIN videos v ON v.id = f.video_id
		WHERE 1=1`)
	if f.SeriesID > 0 {
		b.WriteString(` AND f.series_id = ?`)
		args = append(args, f.SeriesID)
	}
	if f.VideoID > 0 {
		b.WriteString(` AND f.video_id = ?`)
		args = append(args, f.VideoID)
	}
	appendStringsIn(&b, &args, "f.kind", f.Kinds, false)
	failedSQL := fileIntegrityFailedSQL()
	missingSQL := `f.size_bytes = ?`
	presentSQL := `(f.size_bytes IS NULL OR f.size_bytes != ?)`
	hasOKSQL := `(f.content_hash_ok_at IS NOT NULL AND TRIM(f.content_hash_ok_at) != '')`
	statuses := normalizeFileStatuses(f.Statuses)
	if len(statuses) > 0 {
		var parts []string
		for _, st := range statuses {
			switch st {
			case FileStatusFailed:
				parts = append(parts, failedSQL)
			case FileStatusOK:
				parts = append(parts, hasOKSQL+` AND NOT `+failedSQL)
			case FileStatusNA:
				parts = append(parts, `f.kind = 'nfo' AND NOT `+failedSQL+` AND NOT `+hasOKSQL)
			case FileStatusInactive:
				parts = append(parts, missingSQL+` AND f.kind != 'nfo' AND NOT `+failedSQL+` AND NOT `+hasOKSQL)
				args = append(args, sidecarMissingSizeSentinel)
			case FileStatusUnchecked:
				parts = append(parts, `f.kind != 'nfo' AND `+presentSQL+` AND NOT `+failedSQL+` AND NOT `+hasOKSQL)
				args = append(args, sidecarMissingSizeSentinel)
			}
		}
		appendAndOrGroup(&b, parts)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		b.WriteString(` AND f.path LIKE ?`)
		args = append(args, "%"+q+"%")
	}
	return b.String(), args
}

func (f FileListFilter) orderBy() string {
	sort := strings.ToLower(strings.TrimSpace(f.Sort))
	if sort == "" {
		sort = SortFileKind
	}
	desc := NormalizeSortDir(sort, f.SortDir) == SortDirDesc
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	switch sort {
	case SortFilePath:
		return "f.path COLLATE NOCASE " + dir + ", f.id ASC"
	case SortFileSize:
		return "f.size_bytes IS NULL, f.size_bytes " + dir + ", f.id ASC"
	case SortFileAcquired:
		return "f.acquired_at " + dir + ", f.id ASC"
	case SortFileSeries:
		return "IFNULL(s.title,'') COLLATE NOCASE " + dir + ", f.id ASC"
	default: // kind
		return "f.kind COLLATE NOCASE " + dir + ", f.path COLLATE NOCASE ASC, f.id ASC"
	}
}

// CountFilesFiltered counts files matching filter (DB only).
func (s *Store) CountFilesFiltered(filter FileListFilter) (int, error) {
	where, args := filter.where()
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*)`+where, args...).Scan(&n)
	return n, err
}

// ListFileIDsFiltered returns file ids matching filter (same ORDER BY as the list).
func (s *Store) ListFileIDsFiltered(filter FileListFilter) ([]int64, error) {
	where, args := filter.where()
	q := `SELECT f.id` + where + ` ORDER BY ` + filter.orderBy()
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

// ListFilesFiltered returns a page of files (DB only; missing = size_bytes -1).
func (s *Store) ListFilesFiltered(filter FileListFilter, limit, offset int) ([]FileListRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where, args := filter.where()
	q := `
		SELECT f.id, COALESCE(f.series_id, 0), f.video_id, f.path, f.kind, f.acquired_at,
		       f.size_bytes, f.content_hash, f.content_hash_checked_at, f.content_hash_ok_at,
		       IFNULL(s.title, ''), IFNULL(v.title, '')
	` + where + ` ORDER BY ` + filter.orderBy() + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FileListRow
	for rows.Next() {
		var r FileListRow
		if err := rows.Scan(
			&r.ID, &r.SeriesID, &r.VideoID, &r.Path, &r.Kind, &r.AcquiredAt,
			&r.SizeBytes, &r.ContentHash, &r.ContentHashCheckedAt, &r.ContentHashOkAt,
			&r.SeriesTitle, &r.VideoTitle,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeriesHasFailedIntegrityFile reports any derived-failed files row for series_id.
func (s *Store) SeriesHasFailedIntegrityFile(seriesID int64) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM files
		WHERE series_id = ?
		  AND content_hash_checked_at IS NOT NULL AND TRIM(content_hash_checked_at) != ''
		  AND (
			content_hash_ok_at IS NULL OR TRIM(content_hash_ok_at) = ''
			OR content_hash_checked_at > content_hash_ok_at
		  )
	`, seriesID).Scan(&n)
	return n > 0, err
}

// CountSeriesFailedIntegrityFiles returns derived-failed file count for series.
func (s *Store) CountSeriesFailedIntegrityFiles(seriesID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM files
		WHERE series_id = ?
		  AND content_hash_checked_at IS NOT NULL AND TRIM(content_hash_checked_at) != ''
		  AND (
			content_hash_ok_at IS NULL OR TRIM(content_hash_ok_at) = ''
			OR content_hash_checked_at > content_hash_ok_at
		  )
	`, seriesID).Scan(&n)
	return n, err
}

// SeriesFailedIntegrityFileCounts returns failed-file counts keyed by series_id.
func (s *Store) SeriesFailedIntegrityFileCounts(seriesIDs []int64) (map[int64]int, error) {
	out := map[int64]int{}
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
		SELECT series_id, COUNT(*) FROM files
		WHERE series_id IN (`+sqlIntPlaceholders(len(args))+`)
		  AND content_hash_checked_at IS NOT NULL AND TRIM(content_hash_checked_at) != ''
		  AND (
			content_hash_ok_at IS NULL OR TRIM(content_hash_ok_at) = ''
			OR content_hash_checked_at > content_hash_ok_at
		  )
		GROUP BY series_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
