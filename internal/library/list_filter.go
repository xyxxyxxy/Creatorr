package library

import (
	"strings"
)

// List sort query values (empty = preset default).
const (
	SortUpload     = "upload"
	SortAdded      = "added"
	SortAcquired   = "acquired"
	SortTitle      = "title"
	SortLastUpload = "last_upload" // series: newest video upload_date
	SortDownloaded = "downloaded"  // series: downloaded video count
	SortWanted     = "wanted"      // series: wanted + wanted_archive count
	SortErrors     = "errors"      // series: download/integrity error count
	SortDuration   = "duration"    // video: duration_seconds
)

// Sort direction query values (empty = DefaultSortDir for the active sort).
const (
	SortDirAsc  = "asc"
	SortDirDesc = "desc"
)

// DefaultSortDir is asc for title, desc for date-like sorts (newest first).
func DefaultSortDir(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case SortTitle:
		return SortDirAsc
	default:
		return SortDirDesc
	}
}

// NormalizeSortDir returns asc|desc; empty or unknown uses DefaultSortDir(sort).
func NormalizeSortDir(sort, dir string) string {
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case SortDirAsc:
		return SortDirAsc
	case SortDirDesc:
		return SortDirDesc
	default:
		return DefaultSortDir(sort)
	}
}

// Text query field ids for q_field (default title).
const (
	QFieldTitle         = "title"
	QFieldSortTitle     = "sorttitle"
	QFieldOriginalTitle = "originaltitle"
	QFieldPlot          = "plot"
	QFieldTagline       = "tagline"
	QFieldNotes         = "notes"
)

// Presence field ids for empty= / not_empty= query params.
const (
	PresencePlot          = "plot"
	PresenceTagline       = "tagline"
	PresenceNotes         = "notes"
	PresenceStudio        = "studio"
	PresenceCountry       = "country"
	PresenceMPAA          = "mpaa"
	PresencePremiered     = "premiered"
	PresenceGenres        = "genres"
	PresenceTags          = "tags"
	PresenceActors        = "actors"
	PresenceSortTitle     = "sorttitle"
	PresenceOriginalTitle = "originaltitle"
	PresenceUploadDate    = "upload_date"
	PresenceMediaType     = "media_type"
	PresenceThumbnail     = "thumbnail"
)

// NormalizeQField returns a known text field id or title.
func NormalizeQField(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case QFieldSortTitle:
		return QFieldSortTitle
	case QFieldOriginalTitle:
		return QFieldOriginalTitle
	case QFieldPlot, "description":
		return QFieldPlot
	case QFieldTagline:
		return QFieldTagline
	case QFieldNotes:
		return QFieldNotes
	default:
		return QFieldTitle
	}
}

func videoTextColumn(qField string) string {
	switch NormalizeQField(qField) {
	case QFieldSortTitle:
		return "sorttitle"
	case QFieldOriginalTitle:
		return "originaltitle"
	case QFieldPlot:
		return "description"
	case QFieldTagline:
		return "tagline"
	case QFieldNotes:
		return "notes"
	default:
		return "title"
	}
}

func seriesTextColumn(qField string) string {
	switch NormalizeQField(qField) {
	case QFieldSortTitle:
		return "s.sorttitle"
	case QFieldOriginalTitle:
		return "s.originaltitle"
	case QFieldPlot:
		return "s.plot"
	case QFieldTagline:
		return "s.tagline"
	case QFieldNotes:
		return "s.notes"
	default:
		return "s.title"
	}
}

func videoOrderByClause(sort, dir string) string {
	sort = strings.ToLower(strings.TrimSpace(sort))
	if sort == "" {
		sort = SortUpload
	}
	desc := NormalizeSortDir(sort, dir) == SortDirDesc
	switch sort {
	case SortAdded:
		if desc {
			return `id DESC`
		}
		return `id ASC`
	case SortAcquired:
		if desc {
			return `(acquired_at IS NULL OR acquired_at = ''), acquired_at DESC, id DESC`
		}
		return `(acquired_at IS NULL OR acquired_at = ''), acquired_at ASC, id ASC`
	case SortTitle:
		if desc {
			return `title COLLATE NOCASE DESC, id DESC`
		}
		return `title COLLATE NOCASE ASC, id ASC`
	case SortDuration:
		// Unknown / zero duration after known lengths.
		if desc {
			return `(duration_seconds IS NULL OR duration_seconds <= 0), duration_seconds DESC, id DESC`
		}
		return `(duration_seconds IS NULL OR duration_seconds <= 0), duration_seconds ASC, id ASC`
	default: // upload
		if desc {
			return videoListOrderBy
		}
		return `(upload_date IS NULL OR upload_date = ''), upload_date ASC, id ASC`
	}
}

func seriesOrderByClause(sort, dir string) string {
	sort = strings.ToLower(strings.TrimSpace(sort))
	if sort == "" {
		sort = SortTitle
	}
	desc := NormalizeSortDir(sort, dir) == SortDirDesc
	switch sort {
	case SortAdded:
		if desc {
			return `s.added_at DESC, s.id DESC`
		}
		return `s.added_at ASC, s.id ASC`
	case SortLastUpload:
		if desc {
			return `vc.last_upload IS NULL, vc.last_upload DESC, s.id DESC`
		}
		return `vc.last_upload IS NULL, vc.last_upload ASC, s.id ASC`
	case SortDownloaded:
		if desc {
			return `COALESCE(vc.downloaded_count, 0) DESC, s.id DESC`
		}
		return `COALESCE(vc.downloaded_count, 0) ASC, s.id ASC`
	case SortWanted:
		if desc {
			return `COALESCE(vc.wanted_count, 0) DESC, s.id DESC`
		}
		return `COALESCE(vc.wanted_count, 0) ASC, s.id ASC`
	case SortErrors:
		if desc {
			return `COALESCE(vc.error_count, 0) DESC, s.id DESC`
		}
		return `COALESCE(vc.error_count, 0) ASC, s.id ASC`
	default: // title
		if desc {
			return `s.title COLLATE NOCASE DESC, s.id DESC`
		}
		return `s.title COLLATE NOCASE ASC, s.id ASC`
	}
}

// ponytail: json_each scan is fine for one library; side table if it gets slow.
func appendJSONStringListMatch(b *strings.Builder, args *[]any, col string, values []string) {
	vals := uniqueTrimmed(values)
	if len(vals) == 0 {
		return
	}
	b.WriteString(` AND EXISTS (SELECT 1 FROM json_each(` + col + `) j WHERE `)
	for i, v := range vals {
		if i > 0 {
			b.WriteString(` OR `)
		}
		b.WriteString(`j.value = ? COLLATE NOCASE`)
		*args = append(*args, v)
	}
	b.WriteString(`)`)
}

func appendJSONActorNameMatch(b *strings.Builder, args *[]any, col string, names []string) {
	vals := uniqueTrimmed(names)
	if len(vals) == 0 {
		return
	}
	b.WriteString(` AND EXISTS (SELECT 1 FROM json_each(` + col + `) j WHERE `)
	for i, v := range vals {
		if i > 0 {
			b.WriteString(` OR `)
		}
		b.WriteString(`json_extract(j.value, '$.name') = ? COLLATE NOCASE`)
		*args = append(*args, v)
	}
	b.WriteString(`)`)
}

func appendScalarEmpty(b *strings.Builder, col string, empty bool) {
	if empty {
		b.WriteString(` AND (` + col + ` IS NULL OR trim(` + col + `) = '')`)
	} else {
		b.WriteString(` AND ` + col + ` IS NOT NULL AND trim(` + col + `) != ''`)
	}
}

func appendJSONListEmpty(b *strings.Builder, col string, empty bool) {
	if empty {
		b.WriteString(` AND (` + col + ` IS NULL OR trim(` + col + `) = '' OR trim(` + col + `) = '[]')`)
	} else {
		b.WriteString(` AND ` + col + ` IS NOT NULL AND trim(` + col + `) != '' AND trim(` + col + `) != '[]'`)
	}
}

func uniqueTrimmed(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func presenceSet(fields []string) map[string]bool {
	out := map[string]bool{}
	for _, f := range uniqueTrimmed(fields) {
		out[strings.ToLower(f)] = true
	}
	return out
}

func appendVideoPresenceSQL(b *strings.Builder, empty, notEmpty []string) {
	em := presenceSet(empty)
	ne := presenceSet(notEmpty)
	for k := range em {
		if ne[k] {
			delete(em, k)
			delete(ne, k)
		}
	}
	apply := func(field string, isEmpty bool) {
		switch field {
		case PresencePlot:
			appendScalarEmpty(b, "description", isEmpty)
		case PresenceTagline:
			appendScalarEmpty(b, "tagline", isEmpty)
		case PresenceNotes:
			appendScalarEmpty(b, "notes", isEmpty)
		case PresenceStudio:
			appendScalarEmpty(b, "studio", isEmpty)
		case PresenceCountry:
			appendScalarEmpty(b, "country", isEmpty)
		case PresenceMPAA:
			appendScalarEmpty(b, "mpaa", isEmpty)
		case PresenceSortTitle:
			appendScalarEmpty(b, "sorttitle", isEmpty)
		case PresenceOriginalTitle:
			appendScalarEmpty(b, "originaltitle", isEmpty)
		case PresenceUploadDate:
			appendScalarEmpty(b, "upload_date", isEmpty)
		case PresenceMediaType:
			appendScalarEmpty(b, "media_type", isEmpty)
		case PresenceThumbnail:
			appendScalarEmpty(b, "thumbnail_url", isEmpty)
		case PresenceGenres:
			appendJSONListEmpty(b, "genres", isEmpty)
		case PresenceTags:
			appendJSONListEmpty(b, "tags", isEmpty)
		case PresenceActors:
			appendJSONListEmpty(b, "actors", isEmpty)
		}
	}
	for f := range em {
		apply(f, true)
	}
	for f := range ne {
		apply(f, false)
	}
}

func appendSeriesPresenceSQL(b *strings.Builder, empty, notEmpty []string) {
	em := presenceSet(empty)
	ne := presenceSet(notEmpty)
	for k := range em {
		if ne[k] {
			delete(em, k)
			delete(ne, k)
		}
	}
	apply := func(field string, isEmpty bool) {
		switch field {
		case PresencePlot:
			appendScalarEmpty(b, "s.plot", isEmpty)
		case PresenceTagline:
			appendScalarEmpty(b, "s.tagline", isEmpty)
		case PresenceNotes:
			appendScalarEmpty(b, "s.notes", isEmpty)
		case PresenceStudio:
			appendScalarEmpty(b, "s.studio", isEmpty)
		case PresenceCountry:
			appendScalarEmpty(b, "s.country", isEmpty)
		case PresenceMPAA:
			appendScalarEmpty(b, "s.mpaa", isEmpty)
		case PresencePremiered:
			appendScalarEmpty(b, "s.premiered", isEmpty)
		case PresenceSortTitle:
			appendScalarEmpty(b, "s.sorttitle", isEmpty)
		case PresenceOriginalTitle:
			appendScalarEmpty(b, "s.originaltitle", isEmpty)
		case PresenceGenres:
			appendJSONListEmpty(b, "s.genres", isEmpty)
		case PresenceTags:
			appendJSONListEmpty(b, "s.tags", isEmpty)
		case PresenceActors:
			appendJSONListEmpty(b, "s.actors", isEmpty)
		}
	}
	for f := range em {
		apply(f, true)
	}
	for f := range ne {
		apply(f, false)
	}
}
