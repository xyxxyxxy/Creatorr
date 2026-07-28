package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrSeriesBusy is returned when title/root change is blocked by media tasks.
var ErrSeriesBusy = fmt.Errorf("%w: series has pending or running download/pack tasks - wait or cancel first", ErrInvalid)

// MoveSeriesFolder renames/moves the on-disk series directory and rewrites files.path prefixes.
// oldTitle/oldRootID describe the previous location; ser reflects the new title/root already in DB.
func (s *Store) MoveSeriesFolder(ser *Series, oldTitle string, oldRootID int64) error {
	oldRoot, err := s.GetRoot(oldRootID)
	if err != nil {
		return err
	}
	newRoot, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	oldDir := SeriesDir(oldRoot.Path, oldTitle)
	newDir := SeriesDir(newRoot.Path, ser.Title)
	if oldDir == newDir {
		return nil
	}
	if !dirExists(oldDir) {
		// Nothing on disk yet - ok (NFO may be written later under new path).
		return nil
	}
	if fileExists(newDir) || dirExists(newDir) {
		return fmt.Errorf("%w: target series folder already exists: %s", ErrConflict, newDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("rename series folder: %w", err)
	}
	oldPrefix := oldDir
	newPrefix := newDir
	if !strings.HasSuffix(oldPrefix, string(filepath.Separator)) {
		oldPrefix += string(filepath.Separator)
	}
	if !strings.HasSuffix(newPrefix, string(filepath.Separator)) {
		newPrefix += string(filepath.Separator)
	}
	// Also rewrite exact dir matches stored without trailing sep (unlikely for files).
	_, err = s.DB.SQL.Exec(`
		UPDATE files SET path = ? || substr(path, ?)
		WHERE video_id IN (SELECT id FROM videos WHERE series_id = ?)
		  AND (path = ? OR path LIKE ?)
	`, newDir, len(oldDir)+1, ser.ID, oldDir, oldDir+string(filepath.Separator)+"%")
	if err != nil {
		return fmt.Errorf("update file paths: %w", err)
	}
	_ = oldPrefix
	_ = newPrefix
	return nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
