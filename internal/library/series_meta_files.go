package library

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// SeriesMetaFileKinds are files.kind values for series-folder metadata (video_id NULL).
var SeriesMetaFileKinds = map[string]bool{
	SeriesMetaFileRoleNFO: true,
	ArtPoster:             true,
	ArtBanner:             true,
	ArtFanart:             true,
	ArtClearlogo:          true,
}

// IsSeriesMetaKind reports whether kind is a series-folder metadata role.
func IsSeriesMetaKind(kind string) bool {
	return SeriesMetaFileKinds[strings.TrimSpace(kind)]
}

// RegisterSeriesMetaFile upserts one series-meta files row (video_id NULL).
// Preserves content_hash stamps when path is unchanged.
func (s *Store) RegisterSeriesMetaFile(seriesID int64, path, kind string) error {
	path = strings.TrimSpace(path)
	kind = strings.TrimSpace(kind)
	if seriesID <= 0 || path == "" || !IsSeriesMetaKind(kind) {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	var size any
	if kind != SeriesMetaFileRoleNFO {
		size = st.Size()
	}
	var existingID int64
	var existingPath string
	err = s.DB.SQL.QueryRow(`
		SELECT id, path FROM files
		WHERE series_id = ? AND video_id IS NULL AND kind = ?
		LIMIT 1
	`, seriesID, kind).Scan(&existingID, &existingPath)
	if err == nil && existingID > 0 {
		if existingPath == path {
			_, err = s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, size, existingID)
			return err
		}
		_, _ = s.DB.SQL.Exec(`DELETE FROM files WHERE id = ?`, existingID)
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}
	acquired := nowRFC3339()
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (series_id, video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, NULL, ?, ?, ?, ?)
	`, seriesID, path, kind, acquired, size)
	return err
}

// SyncSeriesMetaFiles registers on-disk series meta and marks stale DB rows missing.
// Does not enqueue integrity checks.
func (s *Store) SyncSeriesMetaFiles(seriesID int64) error {
	if seriesID <= 0 {
		return nil
	}
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	dir := SeriesDir(root.Path, ser.Title)
	onDisk := ListSeriesMetaFiles(dir)
	seen := map[string]string{} // kind -> path
	for _, f := range onDisk {
		seen[f.Role] = f.Path
		if err := s.RegisterSeriesMetaFile(seriesID, f.Path, f.Role); err != nil {
			return fmt.Errorf("register series meta %s: %w", f.Role, err)
		}
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, kind FROM files
		WHERE series_id = ? AND video_id IS NULL
	`, seriesID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return err
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, sidecarMissingSizeSentinel, id); err != nil {
			return err
		}
	}
	return rows.Err()
}

// SyncAllSeriesMetaFiles registers series meta for every series (migrate / full sync).
func (s *Store) SyncAllSeriesMetaFiles() error {
	rows, err := s.DB.SQL.Query(`SELECT id FROM series ORDER BY id`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if err := s.SyncSeriesMetaFiles(id); err != nil {
			return err
		}
	}
	return rows.Err()
}
