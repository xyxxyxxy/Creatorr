package library

import (
	"database/sql"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"
)

// SetFileContentHash stores content_hash for a files row.
// Non-empty hash also stamps checked_at=ok_at (same instant). Clearing hash clears stamps.
func (s *Store) SetFileContentHash(fileID int64, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		_, err := s.DB.SQL.Exec(`
			UPDATE files SET content_hash = NULL,
			  content_hash_checked_at = NULL, content_hash_ok_at = NULL
			WHERE id = ?`, fileID)
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET content_hash = ?,
		  content_hash_checked_at = ?, content_hash_ok_at = ?
		WHERE id = ?`, hash, now, now, fileID)
	return err
}

// FileContentHash returns stored hash or ok=false when NULL/empty.
func (s *Store) FileContentHash(fileID int64) (hash string, ok bool, err error) {
	var ns sql.NullString
	err = s.DB.SQL.QueryRow(`SELECT content_hash FROM files WHERE id = ?`, fileID).Scan(&ns)
	if err != nil {
		return "", false, err
	}
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return "", false, nil
	}
	return ns.String, true, nil
}

// MarkFileHashOK stamps checked_at and ok_at to the same now (successful compare).
func (s *Store) MarkFileHashOK(fileID int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET content_hash_checked_at = ?, content_hash_ok_at = ?
		WHERE id = ?`, now, now, fileID)
	return err
}

// MarkFileHashAttempted stamps checked_at only (failed attempt; keep ok_at).
func (s *Store) MarkFileHashAttempted(fileID int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET content_hash_checked_at = ?
		WHERE id = ?`, now, fileID)
	return err
}

func (s *Store) ensureOrCompareFileHash(fileID int64, path string) (result, mismatch string, err error) {
	return integrity.EnsureOrCompareFileHash(s, fileID, path)
}

// FileIntegrityDerived is the never/ok/failed state from stamps.
func FileIntegrityDerived(checkedAt, okAt string) integrity.FileIntegrityState {
	return integrity.DeriveFileIntegrity(checkedAt, okAt)
}

// VideoHasFailedIntegrityFile reports whether any files row for videoID is derived-failed.
func (s *Store) VideoHasFailedIntegrityFile(videoID int64) (bool, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT content_hash_checked_at, content_hash_ok_at FROM files WHERE video_id = ?
	`, videoID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var checked, okAt sql.NullString
		if err := rows.Scan(&checked, &okAt); err != nil {
			return false, err
		}
		if FileIntegrityDerived(nullStr(checked), nullStr(okAt)) == integrity.FileIntegrityFailed {
			return true, nil
		}
	}
	return false, rows.Err()
}

// MediaContentHashOkAt returns the earliest non-empty media content_hash_ok_at for videoID.
func (s *Store) MediaContentHashOkAt(videoID int64) (okAt string, ok bool, err error) {
	var ns sql.NullString
	err = s.DB.SQL.QueryRow(`
		SELECT content_hash_ok_at FROM files
		WHERE video_id = ? AND kind = 'video'
		  AND content_hash_ok_at IS NOT NULL AND TRIM(content_hash_ok_at) != ''
		ORDER BY content_hash_ok_at ASC
		LIMIT 1
	`, videoID).Scan(&ns)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return "", false, nil
	}
	return ns.String, true, nil
}

func nullStr(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}
