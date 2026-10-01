package library

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SeriesNFO is input for WriteSeriesNFO.
type SeriesNFO struct {
	Title         string
	SortTitle     string
	OriginalTitle string
	Plot          string
	Studio        string
	Genres        []string
	Tags          []string
	UniqueIDType  string
	UniqueIDValue string
	Actors        []SeriesActor
	Tagline       string
	Country       string
	MPAA          string
	Premiered     string // YYYY-MM-DD
	Monitored     bool
}

// WriteSeriesNFO writes Kodi/Emby tvshow.nfo at path.
func WriteSeriesNFO(path string, meta SeriesNFO) error {
	return os.WriteFile(path, FormatSeriesNFO(meta), 0o644)
}

// FormatSeriesNFO returns tvshow XML bytes.
func FormatSeriesNFO(meta SeriesNFO) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString("<tvshow>\n")
	b.WriteString("  <title>" + xmlEscape(meta.Title) + "</title>\n")
	if st := omitWhenEqualTitle(meta.SortTitle, meta.Title); st != "" {
		b.WriteString("  <sorttitle>" + xmlEscape(st) + "</sorttitle>\n")
	}
	if ot := omitWhenEqualTitle(meta.OriginalTitle, meta.Title); ot != "" {
		b.WriteString("  <originaltitle>" + xmlEscape(ot) + "</originaltitle>\n")
	}
	if meta.Plot != "" {
		b.WriteString("  <plot>" + xmlEscape(meta.Plot) + "</plot>\n")
	}
	if meta.Tagline != "" {
		b.WriteString("  <tagline>" + xmlEscape(meta.Tagline) + "</tagline>\n")
	}
	if meta.Studio != "" {
		b.WriteString("  <studio>" + xmlEscape(meta.Studio) + "</studio>\n")
	}
	for _, g := range meta.Genres {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		b.WriteString("  <genre>" + xmlEscape(g) + "</genre>\n")
	}
	for _, t := range meta.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		b.WriteString("  <tag>" + xmlEscape(t) + "</tag>\n")
	}
	if meta.Premiered != "" {
		day := UploadCalendarDate(meta.Premiered)
		if day == "" {
			day = meta.Premiered
		}
		b.WriteString("  <premiered>" + xmlEscape(day) + "</premiered>\n")
		if len(day) >= 4 {
			b.WriteString("  <year>" + xmlEscape(day[:4]) + "</year>\n")
		}
	}
	status := "Ended"
	if meta.Monitored {
		status = "Continuing"
	}
	b.WriteString("  <status>" + status + "</status>\n")
	if meta.Country != "" {
		b.WriteString("  <country>" + xmlEscape(meta.Country) + "</country>\n")
	}
	if meta.MPAA != "" {
		b.WriteString("  <mpaa>" + xmlEscape(meta.MPAA) + "</mpaa>\n")
	}
	if meta.UniqueIDValue != "" {
		typ := meta.UniqueIDType
		if typ == "" {
			typ = "creatorr"
		}
		fmt.Fprintf(&b, `  <uniqueid type="%s" default="true">%s</uniqueid>`+"\n",
			xmlEscape(typ), xmlEscape(meta.UniqueIDValue))
	}
	for i, a := range meta.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		order := a.Order
		if order == 0 {
			order = i
		}
		b.WriteString("  <actor>\n")
		b.WriteString("    <name>" + xmlEscape(name) + "</name>\n")
		if role := strings.TrimSpace(a.Role); role != "" {
			b.WriteString("    <role>" + xmlEscape(role) + "</role>\n")
		}
		fmt.Fprintf(&b, "    <order>%d</order>\n", order)
		b.WriteString("  </actor>\n")
	}
	b.WriteString("</tvshow>\n")
	return []byte(b.String())
}

// coalesceUniqueID keeps existing NFO uniqueid when the editor does not supply one
// (uniqueid is not operator-edited in Metadata modals; Fetch/prefetch may still set it).
func coalesceUniqueID(inType, inVal, keepType, keepVal string) (string, string) {
	inType = strings.TrimSpace(inType)
	inVal = strings.TrimSpace(inVal)
	if inType == "" && inVal == "" {
		return strings.TrimSpace(keepType), strings.TrimSpace(keepVal)
	}
	if inType == "" {
		inType = strings.TrimSpace(keepType)
	}
	if inVal == "" {
		inVal = strings.TrimSpace(keepVal)
	}
	return inType, inVal
}

func (s *Store) writeSeriesNFOFor(ser *Series, rootPath string) error {
	dir := SeriesDir(rootPath, ser.Title)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	premiered := ser.Meta.Premiered
	if premiered == "" {
		if day, err := s.OldestVideoUploadDay(ser.ID); err == nil {
			premiered = day
		}
	}
	return WriteSeriesNFO(filepath.Join(dir, "tvshow.nfo"), SeriesNFO{
		Title:         ser.Title,
		SortTitle:     ser.Meta.SortTitle,
		OriginalTitle: ser.Meta.OriginalTitle,
		Plot:          ser.Meta.Plot,
		Studio:        ser.Meta.Studio,
		Genres:        ser.Meta.Genres,
		Tags:          ser.Meta.Tags,
		UniqueIDType:  ser.Meta.UniqueIDType,
		UniqueIDValue: ser.Meta.UniqueIDValue,
		Actors:        ser.Meta.Actors,
		Tagline:       ser.Meta.Tagline,
		Country:       ser.Meta.Country,
		MPAA:          ser.Meta.MPAA,
		Premiered:     premiered,
		Monitored:     ser.Monitored,
	})
}

// WriteSeriesNFODisk rewrites tvshow.nfo for a series when the series folder already exists
// (or creates it). Prefer EnsureSeriesNFO for monitor/title hooks that should only touch existing dirs.
func (s *Store) WriteSeriesNFODisk(seriesID int64) error {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	return s.writeSeriesNFOFor(ser, root.Path)
}

// RewriteSeriesNFOIfPresent rewrites tvshow.nfo only when the series folder already exists.
func (s *Store) RewriteSeriesNFOIfPresent(seriesID int64) error {
	_, err := s.RewriteSeriesNFOIfChanged(seriesID)
	return err
}

// RewriteSeriesNFOIfChanged rewrites tvshow.nfo when the series folder exists and content differs.
// Returns changed=false when folder missing or bytes already match.
func (s *Store) RewriteSeriesNFOIfChanged(seriesID int64) (changed bool, err error) {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return false, err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return false, err
	}
	dir := SeriesDir(root.Path, ser.Title)
	if !dirExists(dir) {
		return false, nil
	}
	premiered := ser.Meta.Premiered
	if premiered == "" {
		if day, err := s.OldestVideoUploadDay(ser.ID); err == nil {
			premiered = day
		}
	}
	meta := SeriesNFO{
		Title:         ser.Title,
		SortTitle:     ser.Meta.SortTitle,
		OriginalTitle: ser.Meta.OriginalTitle,
		Plot:          ser.Meta.Plot,
		Studio:        ser.Meta.Studio,
		Genres:        ser.Meta.Genres,
		Tags:          ser.Meta.Tags,
		UniqueIDType:  ser.Meta.UniqueIDType,
		UniqueIDValue: ser.Meta.UniqueIDValue,
		Actors:        ser.Meta.Actors,
		Tagline:       ser.Meta.Tagline,
		Country:       ser.Meta.Country,
		MPAA:          ser.Meta.MPAA,
		Premiered:     premiered,
		Monitored:     ser.Monitored,
	}
	path := filepath.Join(dir, "tvshow.nfo")
	want := FormatSeriesNFO(meta)
	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, want) {
		return false, nil
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// OldestVideoUploadDay returns YYYY-MM-DD of the oldest non-empty upload_date, or "".
func (s *Store) OldestVideoUploadDay(seriesID int64) (string, error) {
	var raw string
	err := s.DB.SQL.QueryRow(`
		SELECT upload_date FROM videos
		WHERE series_id = ? AND upload_date IS NOT NULL AND trim(upload_date) != ''
		ORDER BY upload_date ASC LIMIT 1
	`, seriesID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	day := UploadCalendarDate(raw)
	if day == "" {
		return strings.TrimSpace(raw), nil
	}
	return day, nil
}

// RecomputeSeriesPremiered updates stored premiered from oldest video; rewrites NFO if changed.
// Returns whether premiered changed.
func (s *Store) RecomputeSeriesPremiered(seriesID int64) (changed bool, err error) {
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return false, err
	}
	day, err := s.OldestVideoUploadDay(seriesID)
	if err != nil {
		return false, err
	}
	if day == ser.Meta.Premiered {
		return false, nil
	}
	if _, err := s.DB.SQL.Exec(`UPDATE series SET premiered = ? WHERE id = ?`, day, seriesID); err != nil {
		return false, err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return true, err
	}
	ser.Meta.Premiered = day
	if err := s.writeSeriesNFOFor(ser, root.Path); err != nil {
		return true, err
	}
	return true, nil
}

// RewriteSeriesEpisodeNFOs rewrites episode NFOs for all videos with media in the series.
// taskID is required when any episode NFO bytes change.
func (s *Store) RewriteSeriesEpisodeNFOs(seriesID, taskID int64) (rewrote, failed int, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT f.video_id FROM files f
		JOIN videos v ON v.id = f.video_id
		WHERE v.series_id = ? AND f.kind = 'video'
		ORDER BY f.video_id
	`, seriesID)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		wrote, err := s.RewriteVideoNFO(id, taskID)
		if err != nil {
			failed++
			continue
		}
		if wrote {
			rewrote++
		}
	}
	return rewrote, failed, nil
}
