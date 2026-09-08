package library

import (
	"database/sql"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"
)

// SetFileContentHash stores content_hash for a files row.
func (s *Store) SetFileContentHash(fileID int64, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		_, err := s.DB.SQL.Exec(`UPDATE files SET content_hash = NULL WHERE id = ?`, fileID)
		return err
	}
	_, err := s.DB.SQL.Exec(`UPDATE files SET content_hash = ? WHERE id = ?`, hash, fileID)
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

func (s *Store) ensureOrCompareFileHash(fileID int64, path string) (result, mismatch string, err error) {
	return integrity.EnsureOrCompareFileHash(s, fileID, path)
}
