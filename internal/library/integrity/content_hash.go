package integrity

import (
	"fmt"
	"strings"
	"time"
)

// FileIntegrityState is derived from content_hash_checked_at / content_hash_ok_at.
type FileIntegrityState string

const (
	FileIntegrityNever  FileIntegrityState = ""
	FileIntegrityOK     FileIntegrityState = "ok"
	FileIntegrityFailed FileIntegrityState = "failed"
)

// DeriveFileIntegrity returns never / ok / failed from stamp strings (RFC3339).
// Success paths must write the same instant to both columns.
func DeriveFileIntegrity(checkedAt, okAt string) FileIntegrityState {
	checkedAt = strings.TrimSpace(checkedAt)
	okAt = strings.TrimSpace(okAt)
	if checkedAt == "" {
		return FileIntegrityNever
	}
	if okAt == "" {
		return FileIntegrityFailed
	}
	if checkedAt == okAt {
		return FileIntegrityOK
	}
	tc, ok1 := parseStamp(checkedAt)
	to, ok2 := parseStamp(okAt)
	if ok1 && ok2 {
		if tc.After(to) {
			return FileIntegrityFailed
		}
		if tc.Equal(to) {
			return FileIntegrityOK
		}
		// ok_at after checked_at should not happen; treat as ok if equal-ish success write.
		return FileIntegrityOK
	}
	if checkedAt > okAt {
		return FileIntegrityFailed
	}
	return FileIntegrityOK
}

func parseStamp(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// HashStore persists and reads files.content_hash plus integrity stamps.
type HashStore interface {
	FileContentHash(fileID int64) (hash string, ok bool, err error)
	SetFileContentHash(fileID int64, hash string) error
	MarkFileHashOK(fileID int64) error
	MarkFileHashAttempted(fileID int64) error
}

// EnsureOrCompareFileHash fills NULL hash or compares when set.
// result is IntegrityResultOK, IntegrityResultFilled, or IntegrityResultFailed.
// Stamps: fill/OK set checked_at=ok_at; mismatch advances checked_at only.
func EnsureOrCompareFileHash(s HashStore, fileID int64, path string) (result, mismatch string, err error) {
	sum, err := SHA256File(path)
	if err != nil {
		return "", "", err
	}
	stored, ok, err := s.FileContentHash(fileID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		if err := s.SetFileContentHash(fileID, sum); err != nil {
			return "", "", err
		}
		return IntegrityResultFilled, "", nil
	}
	if stored != sum {
		_ = s.MarkFileHashAttempted(fileID)
		return IntegrityResultFailed,
			fmt.Sprintf("content hash mismatch (stored %s… disk %s…)", TruncHash(stored), TruncHash(sum)),
			nil
	}
	if err := s.MarkFileHashOK(fileID); err != nil {
		return "", "", err
	}
	return IntegrityResultOK, "", nil
}

// TruncHash shortens a hex digest for operator-facing messages.
func TruncHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}
