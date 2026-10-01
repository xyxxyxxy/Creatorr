package library

import (
	"regexp"
	"strings"
)

var (
	mediaExts = map[string]bool{
		".mkv": true, ".mp4": true, ".mov": true, ".webm": true, ".m4v": true,
		".mka": true, ".m4a": true, ".mp3": true, ".opus": true, ".ogg": true, ".flac": true,
	}
	uniqueIDTyped = regexp.MustCompile(`(?i)<uniqueid[^>]*type="([^"]*)"[^>]*>([^<]+)</uniqueid>`)
	uniqueIDAny   = regexp.MustCompile(`(?i)<uniqueid[^>]*>([^<]+)</uniqueid>`)
	bracketID     = regexp.MustCompile(`\[([^\]]+)\]`)
	stripBracket  = regexp.MustCompile(`\[.*?\]`)
	stripSE       = regexp.MustCompile(`(?i)S\d+E\d+-?\d*\s*`)
)

// ListImportPickerSeries returns id/title/root for every series (no sources, no disk I/O).
func (s *Store) ListImportPickerSeries() ([]ImportPickerSeries, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, title, root_id FROM series ORDER BY title COLLATE NOCASE, id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ImportPickerSeries
	for rows.Next() {
		var ser ImportPickerSeries
		if err := rows.Scan(&ser.ID, &ser.Title, &ser.RootID); err != nil {
			return nil, err
		}
		out = append(out, ser)
	}
	return out, rows.Err()
}

// ListImportPickerVideos returns a capped video list for Import / Maintenance pickers.
// With no SeriesID, Q, IDs, or HasMedia filter, returns an empty slice (no full-library dump).
func (s *Store) ListImportPickerVideos(q ImportPickerVideoQuery) ([]ImportPickerVideo, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	ids := make([]int64, 0, len(q.IDs))
	seen := map[int64]struct{}{}
	for _, id := range q.IDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		if len(ids) >= 200 {
			break
		}
	}

	needle := strings.TrimSpace(q.Q)
	hasFilter := q.SeriesID != nil || needle != "" || len(ids) > 0 || q.HasMedia != nil
	if !hasFilter {
		return nil, nil
	}

	var b strings.Builder
	args := make([]any, 0, 8)
	b.WriteString(`
		SELECT v.id, v.series_id, v.title, s.title, v.status, COALESCE(v.special_feature,''),
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video') AS has_media,
		  EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'thumb') AS has_thumb
		FROM videos v
		JOIN series s ON s.id = v.series_id
		WHERE 1=1`)
	if len(ids) > 0 {
		b.WriteString(` AND v.id IN (`)
		for i, id := range ids {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteByte('?')
			args = append(args, id)
		}
		b.WriteByte(')')
		limit = len(ids)
	} else {
		if q.SeriesID != nil {
			b.WriteString(` AND v.series_id = ?`)
			args = append(args, *q.SeriesID)
		}
		if needle != "" {
			b.WriteString(` AND (v.title LIKE ? COLLATE NOCASE OR s.title LIKE ? COLLATE NOCASE)`)
			like := "%" + needle + "%"
			args = append(args, like, like)
		}
	}
	if q.HasMedia != nil && *q.HasMedia {
		b.WriteString(` AND EXISTS(SELECT 1 FROM files f WHERE f.video_id = v.id AND f.kind = 'video')`)
	}
	b.WriteString(` ORDER BY s.title COLLATE NOCASE, v.title COLLATE NOCASE, v.id LIMIT ?`)
	args = append(args, limit)

	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ImportPickerVideo
	for rows.Next() {
		var v ImportPickerVideo
		var hasMedia, hasThumb int
		if err := rows.Scan(&v.ID, &v.SeriesID, &v.Title, &v.SeriesTitle, &v.Status, &v.PackRole, &hasMedia, &hasThumb); err != nil {
			return nil, err
		}
		v.PackRole = NormalizePackRole(v.PackRole)
		v.HasMedia = hasMedia != 0
		v.HasThumb = hasThumb != 0
		out = append(out, v)
	}
	return out, rows.Err()
}
