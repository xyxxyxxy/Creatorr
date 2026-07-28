package library

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// RetentionSecondsPerDay converts UI retention days to stored seconds.
const RetentionSecondsPerDay int64 = 24 * 60 * 60

// RetentionDaysFromSeconds maps stored TTL seconds to whole days (ceil; 0 stays 0).
func RetentionDaysFromSeconds(sec int64) int64 {
	if sec <= 0 {
		return 0
	}
	return (sec + RetentionSecondsPerDay - 1) / RetentionSecondsPerDay
}

// RetentionSecondsFromDays maps UI days to stored TTL seconds (0 stays 0).
func RetentionSecondsFromDays(days int64) int64 {
	if days <= 0 {
		return 0
	}
	return days * RetentionSecondsPerDay
}

// RootFolder is a named download root with optional retention TTL.
type RootFolder struct {
	ID                  int64
	Name                string
	Path                string
	RetentionTTLSeconds sql.NullInt64
}

func (s *Store) ListRoots() ([]RootFolder, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, name, path, retention_ttl_seconds FROM root_folders ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RootFolder
	for rows.Next() {
		var r RootFolder
		if err := rows.Scan(&r.ID, &r.Name, &r.Path, &r.RetentionTTLSeconds); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// anyRootRetentionTTL reports whether any root has a positive retention TTL.
func (s *Store) anyRootRetentionTTL() (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM root_folders
		WHERE retention_ttl_seconds IS NOT NULL AND retention_ttl_seconds > 0
	`).Scan(&n)
	return n > 0, err
}

func (s *Store) GetRoot(id int64) (*RootFolder, error) {
	var r RootFolder
	err := s.DB.SQL.QueryRow(`
		SELECT id, name, path, retention_ttl_seconds FROM root_folders WHERE id = ?
	`, id).Scan(&r.ID, &r.Name, &r.Path, &r.RetentionTTLSeconds)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CreateRoot(name, path string, retention *int64) (*RootFolder, error) {
	path = strings.TrimSpace(path)
	if err := requireAbsoluteRootPath(path); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	var ttl any
	if retention != nil {
		ttl = *retention
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO root_folders (name, path, retention_ttl_seconds) VALUES (?, ?, ?)
	`, name, path, ttl)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id, _ := res.LastInsertId()
	return s.GetRoot(id)
}

func (s *Store) UpdateRoot(id int64, name, path *string, retention *int64, clearRetention bool) (*RootFolder, error) {
	cur, err := s.GetRoot(id)
	if err != nil {
		return nil, err
	}
	n, p := cur.Name, cur.Path
	ttl := cur.RetentionTTLSeconds
	if path != nil {
		cleaned := strings.TrimSpace(*path)
		if err := requireAbsoluteRootPath(cleaned); err != nil {
			return nil, err
		}
		p = cleaned
	}
	if name != nil {
		n = strings.TrimSpace(*name)
	}
	if clearRetention {
		ttl = sql.NullInt64{}
	} else if retention != nil {
		ttl = sql.NullInt64{Int64: *retention, Valid: true}
	}
	var ttlVal any
	if ttl.Valid {
		ttlVal = ttl.Int64
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE root_folders SET name = ?, path = ?, retention_ttl_seconds = ? WHERE id = ?
	`, n, p, ttlVal, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.GetRoot(id)
}

func requireAbsoluteRootPath(path string) error {
	if err := requireNonEmpty("path", path); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: path must be absolute", ErrInvalid)
	}
	return nil
}
