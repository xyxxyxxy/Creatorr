package integrity

import "fmt"

// HashStore persists and reads files.content_hash.
type HashStore interface {
	FileContentHash(fileID int64) (hash string, ok bool, err error)
	SetFileContentHash(fileID int64, hash string) error
}

// EnsureOrCompareFileHash fills NULL hash or compares when set.
// result is IntegrityResultOK, IntegrityResultFilled, or IntegrityResultFailed.
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
		return IntegrityResultFailed,
			fmt.Sprintf("content hash mismatch (stored %s… disk %s…)", TruncHash(stored), TruncHash(sum)),
			nil
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
