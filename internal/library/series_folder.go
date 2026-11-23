package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ErrSeriesBusy is returned when title/root change is blocked by media, rename, or series_move tasks for this series.
var ErrSeriesBusy = fmt.Errorf("%w: series has pending or running download, SponsorBlock cut, media verify, episode rename, or folder move tasks - wait or cancel first", ErrInvalid)

// seriesFolderRenamer renames/moves a directory. Tests swap this to force EXDEV.
var seriesFolderRenamer = os.Rename

// MoveSeriesFolder renames/moves the on-disk series directory and rewrites files.path prefixes.
// oldTitle/oldRootID describe the previous location; ser reflects the new title/root already in DB.
// Cross-device rename (EXDEV) falls back to a streaming copy then remove of the source.
func (s *Store) MoveSeriesFolder(ctx context.Context, ser *Series, oldTitle string, oldRootID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	oldRoot, err := s.GetRoot(oldRootID)
	if err != nil {
		return err
	}
	newRoot, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	_ = s.EnsureSeriesDirCapped(oldRoot.Path, oldTitle)
	oldDir := SeriesDir(oldRoot.Path, oldTitle)
	// Compat: uncapped historical folder still on disk.
	if !dirExists(oldDir) {
		uncapped := filepath.Join(oldRoot.Path, sanitizeName(oldTitle, 0))
		if dirExists(uncapped) {
			oldDir = uncapped
		}
	}
	newDir := SeriesDir(newRoot.Path, ser.Title)
	if oldDir == newDir {
		return nil
	}
	if !dirExists(oldDir) {
		// Nothing on disk yet - ok (NFO may be written later under new path).
		return nil
	}
	// Orphan/incomplete prior attempt: dest exists while source still present.
	if fileExists(newDir) || dirExists(newDir) {
		if dirExists(oldDir) {
			if err := os.RemoveAll(newDir); err != nil {
				return fmt.Errorf("remove incomplete series folder: %w", err)
			}
		} else {
			return fmt.Errorf("%w: target series folder already exists: %s", ErrConflict, newDir)
		}
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return err
	}
	if err := seriesFolderRenamer(oldDir, newDir); err != nil {
		if !isCrossDevice(err) {
			return fmt.Errorf("rename series folder: %w", err)
		}
		if err := copyDirThenRemove(ctx, oldDir, newDir); err != nil {
			return fmt.Errorf("copy series folder (cross-device): %w", err)
		}
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE files SET path = ? || substr(path, ?)
		WHERE video_id IN (SELECT id FROM videos WHERE series_id = ?)
		  AND (path = ? OR path LIKE ?)
	`, newDir, len(oldDir)+1, ser.ID, oldDir, oldDir+string(filepath.Separator)+"%")
	if err != nil {
		return fmt.Errorf("update file paths: %w", err)
	}
	return nil
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	var link *os.LinkError
	if errors.As(err, &link) && link.Err == syscall.EXDEV {
		return true
	}
	return errors.Is(err, syscall.EXDEV)
}

// copyDirThenRemove copies srcDir to dstDir (regular files and directories only), then removes srcDir.
// On any failure or cancel, dstDir is removed and srcDir is left intact.
func copyDirThenRemove(ctx context.Context, srcDir, dstDir string) error {
	if err := copyDirTree(ctx, srcDir, dstDir); err != nil {
		_ = os.RemoveAll(dstDir)
		return err
	}
	if err := os.RemoveAll(srcDir); err != nil {
		return fmt.Errorf("remove source after copy: %w", err)
	}
	return nil
}

func copyDirTree(ctx context.Context, srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(dstDir, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			return os.MkdirAll(dest, mode.Perm())
		case mode.IsRegular():
			return copyFileStream(path, dest, mode.Perm())
		default:
			return fmt.Errorf("unsupported file type %s: %s", mode.Type(), path)
		}
	})
}

func copyFileStream(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
