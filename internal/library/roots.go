package library

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
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

const rootSelectCols = `id, name, path, retention_ttl_seconds, episode_format, special_episode_format, special_feature_format`

// RootFolder is a named download root with optional retention TTL and per-root episode formats.
type RootFolder struct {
	ID                    int64
	Name                  string
	Path                  string
	RetentionTTLSeconds   sql.NullInt64
	EpisodeFormat         string
	SpecialEpisodeFormat  string
	SpecialFeatureFormat  string
}

func scanRoot(scanner interface {
	Scan(dest ...any) error
}) (RootFolder, error) {
	var r RootFolder
	err := scanner.Scan(&r.ID, &r.Name, &r.Path, &r.RetentionTTLSeconds,
		&r.EpisodeFormat, &r.SpecialEpisodeFormat, &r.SpecialFeatureFormat)
	if err != nil {
		return r, err
	}
	r.EpisodeFormat = settings.NormalizeEpisodeFormat(r.EpisodeFormat)
	r.SpecialEpisodeFormat = settings.NormalizeSpecialEpisodeFormat(r.SpecialEpisodeFormat)
	r.SpecialFeatureFormat = settings.NormalizeSpecialFeatureFormat(r.SpecialFeatureFormat)
	return r, nil
}

func (s *Store) ListRoots() ([]RootFolder, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT ` + rootSelectCols + ` FROM root_folders ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RootFolder
	for rows.Next() {
		r, err := scanRoot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AnyRootRetentionTTL reports whether any root has a positive retention TTL.
func (s *Store) AnyRootRetentionTTL() (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM root_folders
		WHERE retention_ttl_seconds IS NOT NULL AND retention_ttl_seconds > 0
	`).Scan(&n)
	return n > 0, err
}

func (s *Store) GetRoot(id int64) (*RootFolder, error) {
	r, err := scanRoot(s.DB.SQL.QueryRow(`
		SELECT `+rootSelectCols+` FROM root_folders WHERE id = ?
	`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) CreateRoot(name, path, episodeFormat string, retention *int64) (*RootFolder, error) {
	return s.CreateRootWithFormats(name, path, episodeFormat, "", "", retention)
}

// CreateRootWithFormats creates a root with optional special format overrides (empty = default).
func (s *Store) CreateRootWithFormats(name, path, episodeFormat, specialEpisodeFormat, specialFeatureFormat string, retention *int64) (*RootFolder, error) {
	path = strings.TrimSpace(path)
	if err := requireAbsoluteRootPath(path); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	episodeFormat = settings.NormalizeEpisodeFormat(episodeFormat)
	if err := settings.ValidateEpisodeFormat(episodeFormat); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	specialEpisodeFormat = settings.NormalizeSpecialEpisodeFormat(specialEpisodeFormat)
	if err := settings.ValidateSpecialEpisodeFormat(specialEpisodeFormat); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	specialFeatureFormat = settings.NormalizeSpecialFeatureFormat(specialFeatureFormat)
	if err := settings.ValidateSpecialFeatureFormat(specialFeatureFormat); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var ttl any
	if retention != nil {
		ttl = *retention
	}
	res, err := s.DB.SQL.Exec(`
		INSERT INTO root_folders (name, path, retention_ttl_seconds, episode_format, special_episode_format, special_feature_format)
		VALUES (?, ?, ?, ?, ?, ?)
	`, name, path, ttl, episodeFormat, specialEpisodeFormat, specialFeatureFormat)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id, _ := res.LastInsertId()
	return s.GetRoot(id)
}

func (s *Store) UpdateRoot(id int64, name, path, episodeFormat *string, retention *int64, clearRetention bool) (*RootFolder, error) {
	return s.UpdateRootFormats(id, name, path, episodeFormat, nil, nil, retention, clearRetention)
}

// UpdateRootFormats updates a root including optional special format fields.
func (s *Store) UpdateRootFormats(id int64, name, path, episodeFormat, specialEpisodeFormat, specialFeatureFormat *string, retention *int64, clearRetention bool) (*RootFolder, error) {
	cur, err := s.GetRoot(id)
	if err != nil {
		return nil, err
	}
	n, p, ep := cur.Name, cur.Path, cur.EpisodeFormat
	seFmt, sfFmt := cur.SpecialEpisodeFormat, cur.SpecialFeatureFormat
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
	if episodeFormat != nil {
		ep = settings.NormalizeEpisodeFormat(*episodeFormat)
		if err := settings.ValidateEpisodeFormat(ep); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if specialEpisodeFormat != nil {
		seFmt = settings.NormalizeSpecialEpisodeFormat(*specialEpisodeFormat)
		if err := settings.ValidateSpecialEpisodeFormat(seFmt); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	if specialFeatureFormat != nil {
		sfFmt = settings.NormalizeSpecialFeatureFormat(*specialFeatureFormat)
		if err := settings.ValidateSpecialFeatureFormat(sfFmt); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
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
		UPDATE root_folders SET name = ?, path = ?, retention_ttl_seconds = ?,
			episode_format = ?, special_episode_format = ?, special_feature_format = ?
		WHERE id = ?
	`, n, p, ttlVal, ep, seFmt, sfFmt, id)
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

// CountSeriesUsingRoot returns how many series reference this root folder.
func (s *Store) CountSeriesUsingRoot(id int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM series WHERE root_id = ?
	`, id).Scan(&n)
	return n, err
}

// SeriesCountsByRoot returns series counts keyed by root_id.
func (s *Store) SeriesCountsByRoot() (map[int64]int, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT root_id, COUNT(*) FROM series GROUP BY root_id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64]int)
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

// DeleteRoot removes a root folder. Fails with ErrConflict when series still use it.
func (s *Store) DeleteRoot(id int64) error {
	if _, err := s.GetRoot(id); err != nil {
		return err
	}
	n, err := s.CountSeriesUsingRoot(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: root folder is used by %d series", ErrConflict, n)
	}
	res, err := s.DB.SQL.Exec(`DELETE FROM root_folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
