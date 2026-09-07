package library

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// sha256File returns lowercase hex SHA-256 of path contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

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

// ensureOrCompareFileHash fills NULL hash or compares when set.
// Returns mismatch detail when hash differs; empty string when OK.
func (s *Store) ensureOrCompareFileHash(fileID int64, path string) (mismatch string, err error) {
	sum, err := sha256File(path)
	if err != nil {
		return "", err
	}
	stored, ok, err := s.FileContentHash(fileID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", s.SetFileContentHash(fileID, sum)
	}
	if stored != sum {
		return fmt.Sprintf("content hash mismatch (stored %s… disk %s…)", truncHash(stored), truncHash(sum)), nil
	}
	return "", nil
}

func truncHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}
