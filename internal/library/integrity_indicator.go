package library

import (
	"database/sql"
	"strings"
	"time"
)

// VideoMediaHasContentHash reports whether the primary kind=video file has a non-empty content_hash.
func (s *Store) VideoMediaHasContentHash(videoID int64) (bool, error) {
	var ns sql.NullString
	err := s.DB.SQL.QueryRow(`
		SELECT content_hash FROM files
		WHERE video_id = ? AND kind = 'video'
		ORDER BY id LIMIT 1
	`, videoID).Scan(&ns)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ns.Valid && strings.TrimSpace(ns.String) != "", nil
}

// LastIntegrityCheckAt returns the newest integrity-related video_history timestamp.
// Events: integrity_checked, integrity_check_failed, legacy verified.
func (s *Store) LastIntegrityCheckAt(videoID int64) (time.Time, bool, error) {
	var raw string
	err := s.DB.SQL.QueryRow(`
		SELECT created_at FROM video_history
		WHERE video_id = ?
		  AND event IN (?, ?, ?)
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, videoID, VideoHistIntegrityChecked, VideoHistVerifyFailed, VideoHistVerified).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, ok := ParseUploadTime(raw)
	if !ok {
		return time.Time{}, false, nil
	}
	return t, true, nil
}
