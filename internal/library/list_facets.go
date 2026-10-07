package library

import (
	"database/sql"
	"strings"
)

// DistinctVideoScalar returns sorted distinct non-empty values for a video text column.
// seriesID 0 = library-wide.
func (s *Store) DistinctVideoScalar(seriesID int64, col string) ([]string, error) {
	switch col {
	case "studio", "country", "mpaa", "media_type":
	default:
		return nil, nil
	}
	var b strings.Builder
	b.WriteString(`SELECT DISTINCT trim(` + col + `) FROM videos WHERE trim(` + col + `) != ''`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND series_id = ?`)
		args = append(args, seriesID)
	}
	b.WriteString(` ORDER BY 1 COLLATE NOCASE`)
	return s.scanDistinctStrings(b.String(), args...)
}

// DistinctVideoJSONStrings returns sorted distinct string values from a JSON string-array column.
func (s *Store) DistinctVideoJSONStrings(seriesID int64, col string) ([]string, error) {
	switch col {
	case "genres", "tags":
	default:
		return nil, nil
	}
	var b strings.Builder
	b.WriteString(`
		SELECT DISTINCT trim(j.value) FROM videos v, json_each(v.` + col + `) j
		WHERE trim(j.value) != ''`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND v.series_id = ?`)
		args = append(args, seriesID)
	}
	b.WriteString(` ORDER BY 1 COLLATE NOCASE`)
	return s.scanDistinctStrings(b.String(), args...)
}

// DistinctVideoActorNames returns sorted distinct actor names from videos.actors JSON.
func (s *Store) DistinctVideoActorNames(seriesID int64) ([]string, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT DISTINCT trim(json_extract(j.value, '$.name'))
		FROM videos v, json_each(v.actors) j
		WHERE trim(COALESCE(json_extract(j.value, '$.name'), '')) != ''`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND v.series_id = ?`)
		args = append(args, seriesID)
	}
	b.WriteString(` ORDER BY 1 COLLATE NOCASE`)
	return s.scanDistinctStrings(b.String(), args...)
}

// DistinctSeriesScalar returns sorted distinct non-empty series metadata values.
func (s *Store) DistinctSeriesScalar(col string) ([]string, error) {
	switch col {
	case "studio", "country", "mpaa":
	default:
		return nil, nil
	}
	q := `SELECT DISTINCT trim(` + col + `) FROM series WHERE trim(` + col + `) != '' ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSeriesJSONStrings returns sorted distinct values from a series JSON string-array column.
func (s *Store) DistinctSeriesJSONStrings(col string) ([]string, error) {
	switch col {
	case "genres", "tags":
	default:
		return nil, nil
	}
	q := `
		SELECT DISTINCT trim(j.value) FROM series s, json_each(s.` + col + `) j
		WHERE trim(j.value) != ''
		ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSeriesActorNames returns sorted distinct actor names from series.actors.
func (s *Store) DistinctSeriesActorNames() ([]string, error) {
	q := `
		SELECT DISTINCT trim(json_extract(j.value, '$.name'))
		FROM series s, json_each(s.actors) j
		WHERE trim(COALESCE(json_extract(j.value, '$.name'), '')) != ''
		ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSourceScalar returns sorted distinct non-empty source default-metadata scalars.
func (s *Store) DistinctSourceScalar(col string) ([]string, error) {
	switch col {
	case "studio", "country", "mpaa":
	default:
		return nil, nil
	}
	q := `SELECT DISTINCT trim(` + col + `) FROM sources WHERE trim(` + col + `) != '' ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSourceJSONStrings returns sorted distinct values from a source JSON string-array column.
func (s *Store) DistinctSourceJSONStrings(col string) ([]string, error) {
	switch col {
	case "genres", "tags":
	default:
		return nil, nil
	}
	q := `
		SELECT DISTINCT trim(j.value) FROM sources src, json_each(src.` + col + `) j
		WHERE trim(j.value) != ''
		ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSourceActorNames returns sorted distinct actor names from sources.actors.
func (s *Store) DistinctSourceActorNames() ([]string, error) {
	q := `
		SELECT DISTINCT trim(json_extract(j.value, '$.name'))
		FROM sources src, json_each(src.actors) j
		WHERE trim(COALESCE(json_extract(j.value, '$.name'), '')) != ''
		ORDER BY 1 COLLATE NOCASE`
	return s.scanDistinctStrings(q)
}

// DistinctSeriesPremieredYears returns distinct years from series.premiered (newest first).
// Undated series use presence empty=premiered, not a year slot.
func (s *Store) DistinctSeriesPremieredYears() (years []int, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT CAST(strftime('%Y', premiered) AS INTEGER) AS y
		FROM series
		WHERE premiered IS NOT NULL AND trim(premiered) != ''
		ORDER BY y DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		if y > 0 {
			years = append(years, y)
		}
	}
	return years, rows.Err()
}

func (s *Store) scanDistinctStrings(query string, args ...any) ([]string, error) {
	rows, err := s.DB.SQL.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if v.Valid && strings.TrimSpace(v.String) != "" {
			out = append(out, strings.TrimSpace(v.String))
		}
	}
	return out, rows.Err()
}
