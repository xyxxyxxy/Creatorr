package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func (s *Store) backfillVideoSizeBytes(videoID, size int64) error {
	_, err := s.DB.SQL.Exec(`
		UPDATE files SET size_bytes = ? WHERE video_id = ? AND kind = 'video'
	`, size, videoID)
	return err
}

// sidecarMissingSizeSentinel marks a registered sidecar path as known-missing on disk.
// Keeps the row (path preserved for restore) without flipping video status.
const sidecarMissingSizeSentinel int64 = -1

// FileSyncSidecarIssue is one sidecar file change from FileSyncPass.
type FileSyncSidecarIssue struct {
	VideoID int64
	FileID  int64
	Kind    string
	Path    string
}

// MarkSidecarMissing keeps the files row, sets size_bytes to the missing sentinel,
// and records history. Does not change video status.
func (s *Store) MarkSidecarMissing(videoID, fileID, taskID int64, kind, path string) error {
	if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, sidecarMissingSizeSentinel, fileID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "sidecar_missing", "Sidecar file missing on disk", map[string]any{
		"reason":  "sync_files",
		"kind":    kind,
		"path":    path,
		"file_id": fileID,
	}, taskID)
}

// RestoreSidecar clears the missing sentinel, stores on-disk size, and records history.
func (s *Store) RestoreSidecar(videoID, fileID, taskID, diskSize int64, kind, path string) error {
	if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, diskSize, fileID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "sidecar_restored", "Sidecar file found again", map[string]any{
		"reason":  "sync_files",
		"kind":    kind,
		"path":    path,
		"file_id": fileID,
		"size":    diskSize,
	}, taskID)
}

// RestoreSidecarNFO restores a missing NFO without storing size (NFO never size-checked).
func (s *Store) RestoreSidecarNFO(videoID, fileID, taskID int64, path string) error {
	if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = NULL WHERE id = ?`, fileID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "sidecar_restored", "Sidecar file found again", map[string]any{
		"reason":  "sync_files",
		"kind":    "nfo",
		"path":    path,
		"file_id": fileID,
	}, taskID)
}

func (s *Store) clearSidecarSizeBytes(fileID int64) error {
	_, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = NULL WHERE id = ?`, fileID)
	return err
}

// MarkSidecarExternallyChanged updates size_bytes when a present sidecar's size drifts.
// Does not change video status (media downloaded_integrity_failed is media-only).
func (s *Store) MarkSidecarExternallyChanged(videoID, fileID, taskID, oldSize, newSize int64, kind, path string) error {
	if _, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, newSize, fileID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, "sidecar_externally_changed", "Sidecar file size changed on disk", map[string]any{
		"reason":   "sync_files",
		"kind":     kind,
		"path":     path,
		"file_id":  fileID,
		"old_size": oldSize,
		"new_size": newSize,
	}, taskID)
}

func (s *Store) backfillSidecarSizeBytes(fileID, size int64) error {
	_, err := s.DB.SQL.Exec(`UPDATE files SET size_bytes = ? WHERE id = ?`, size, fileID)
	return err
}

// ProgressFn reports task message + optional fraction in [0,1].
type ProgressFn func(msg string, pct *float64)

func (s *Store) reportTaskProgress(taskID int64, progress ProgressFn, msg string, pct float64) {
	p := pct
	if progress != nil {
		progress(msg, &p)
		return
	}
	if taskID > 0 && s.Queue != nil {
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
}

// FileSyncResult is the outcome of one FileSyncPass.
type FileSyncResult struct {
	MissingIDs           []int64
	RestoredIDs          []int64
	ExternallyChangedIDs []int64
	SidecarMissing       []FileSyncSidecarIssue
	SidecarRestored      []FileSyncSidecarIssue
	SidecarChanged       []FileSyncSidecarIssue
}

// Total returns count of videos/files that changed in any tracked way.
func (r FileSyncResult) Total() int {
	return len(r.MissingIDs) + len(r.RestoredIDs) + len(r.ExternallyChangedIDs) +
		len(r.SidecarMissing) + len(r.SidecarRestored) + len(r.SidecarChanged)
}

// FileSyncPass runs missing/restore/size/beginning reconciliation for taskID.
// Optional progress reports mid-pass percentages for the queue UI.
func (s *Store) FileSyncPass(taskID int64, progress ...ProgressFn) (FileSyncResult, error) {
	var out FileSyncResult
	var prog ProgressFn
	if len(progress) > 0 {
		prog = progress[0]
	}
	s.reportTaskProgress(taskID, prog, "Checking library files…", 0.1)
	missingIDs, restoredIDs, changedIDs, err := s.fileSyncMissingAndRestore(taskID, prog)
	if err != nil {
		return out, err
	}
	out.MissingIDs = missingIDs
	out.RestoredIDs = restoredIDs
	out.ExternallyChangedIDs = changedIDs

	s.reportTaskProgress(taskID, prog, "Checking sidecar files…", 0.48)
	sidecarMiss, sidecarRest, sidecarChg, err := s.fileSyncSidecars(taskID, prog)
	if err != nil {
		return out, err
	}
	out.SidecarMissing = sidecarMiss
	out.SidecarRestored = sidecarRest
	out.SidecarChanged = sidecarChg

	total := out.Total()
	msg := "No changes"
	if total > 0 {
		parts := make([]string, 0, 8)
		if n := len(out.MissingIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d missing on disk", n))
		}
		if n := len(out.RestoredIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d restored", n))
		}
		if n := len(out.ExternallyChangedIDs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d size changed", n))
		}
		if n := len(out.SidecarMissing); n > 0 {
			parts = append(parts, fmt.Sprintf("%d sidecar missing", n))
		}
		if n := len(out.SidecarRestored); n > 0 {
			parts = append(parts, fmt.Sprintf("%d sidecar restored", n))
		}
		if n := len(out.SidecarChanged); n > 0 {
			parts = append(parts, fmt.Sprintf("%d sidecar size changed", n))
		}
		msg = "File sync: " + strings.Join(parts, ", ")
		allIDs := append(append([]int64{}, out.MissingIDs...), out.RestoredIDs...)
		allIDs = append(allIDs, out.ExternallyChangedIDs...)
		for _, it := range out.SidecarMissing {
			allIDs = append(allIDs, it.VideoID)
		}
		for _, it := range out.SidecarRestored {
			allIDs = append(allIDs, it.VideoID)
		}
		for _, it := range out.SidecarChanged {
			allIDs = append(allIDs, it.VideoID)
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"missing_ids":            out.MissingIDs,
			"restored_ids":           out.RestoredIDs,
			"externally_changed_ids": out.ExternallyChangedIDs,
			"sidecar_missing":        out.SidecarMissing,
			"sidecar_restored":       out.SidecarRestored,
			"sidecar_changed":        out.SidecarChanged,
			"video_ids":              allIDs,
		})
		if taskID > 0 && s.Queue != nil {
			_ = s.Queue.SetDetail(taskID, string(detailBytes))
		}
	}
	s.reportTaskProgress(taskID, prog, msg, 1)
	return out, nil
}

func rootOnline(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func (s *Store) fileSyncMissingAndRestore(taskID int64, progress ProgressFn) (missingIDs, restoredIDs, changedIDs []int64, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.status, f.path, r.path, f.size_bytes
		FROM videos v
		JOIN files f ON f.video_id = v.id AND f.kind = 'video'
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'downloaded_integrity_failed', 'missing')
	`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	type hit struct {
		id        int64
		status    string
		path      string
		root      string
		sizeBytes sql.NullInt64
	}
	var list []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.status, &h.path, &h.root, &h.sizeBytes); err != nil {
			return nil, nil, nil, err
		}
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}

	n := len(list)
	online := map[string]bool{}
	for i, h := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Checking library files… %d/%d", i+1, n),
				0.1+0.4*float64(i+1)/float64(n))
		}
		ok, cached := online[h.root]
		if !cached {
			ok = rootOnline(h.root)
			online[h.root] = ok
		}
		if !ok {
			continue // root offline - do not mark missing or restore
		}
		st, statErr := os.Stat(h.path)
		exists := statErr == nil && !st.IsDir()
		switch h.status {
		case "downloaded", "downloaded_integrity_failed":
			if !exists {
				if err := s.MarkMissing(h.id, taskID); err != nil {
					return missingIDs, restoredIDs, changedIDs, err
				}
				missingIDs = append(missingIDs, h.id)
				continue
			}
			diskSize := st.Size()
			if !h.sizeBytes.Valid {
				if err := s.backfillVideoSizeBytes(h.id, diskSize); err != nil {
					return missingIDs, restoredIDs, changedIDs, err
				}
				continue
			}
			if h.sizeBytes.Int64 != diskSize {
				if err := s.MarkExternallyChanged(h.id, taskID, h.sizeBytes.Int64, diskSize); err != nil {
					return missingIDs, restoredIDs, changedIDs, err
				}
				changedIDs = append(changedIDs, h.id)
			}
		case "missing":
			if exists {
				if err := s.RestoreDownloaded(h.id, taskID); err != nil {
					return missingIDs, restoredIDs, changedIDs, err
				}
				restoredIDs = append(restoredIDs, h.id)
			}
		}
	}
	return missingIDs, restoredIDs, changedIDs, nil
}

// fileSyncSidecars reconciles registered non-video files (nfo/json/thumb/sub/…).
// Missing / size drift alerts without flipping video status. size_bytes = -1 means
// already known missing (idempotent until path returns).
func (s *Store) fileSyncSidecars(taskID int64, progress ProgressFn) (missing, restored, changed []FileSyncSidecarIssue, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT f.id, f.video_id, f.path, f.kind, f.size_bytes, r.path
		FROM files f
		JOIN videos v ON v.id = f.video_id
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE f.kind != 'video'
		  AND v.status IN ('downloaded', 'downloaded_integrity_failed', 'missing')
	`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	type hit struct {
		fileID    int64
		videoID   int64
		path      string
		kind      string
		root      string
		sizeBytes sql.NullInt64
	}
	var list []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.fileID, &h.videoID, &h.path, &h.kind, &h.sizeBytes, &h.root); err != nil {
			return nil, nil, nil, err
		}
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}

	n := len(list)
	online := map[string]bool{}
	for i, h := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Checking sidecar files… %d/%d", i+1, n),
				0.48+0.08*float64(i+1)/float64(n))
		}
		ok, cached := online[h.root]
		if !cached {
			ok = rootOnline(h.root)
			online[h.root] = ok
		}
		if !ok {
			continue
		}
		st, statErr := os.Stat(h.path)
		exists := statErr == nil && !st.IsDir()
		knownMissing := h.sizeBytes.Valid && h.sizeBytes.Int64 == sidecarMissingSizeSentinel
		issue := FileSyncSidecarIssue{
			VideoID: h.videoID,
			FileID:  h.fileID,
			Kind:    h.kind,
			Path:    h.path,
		}
		switch {
		case !exists && knownMissing:
			// already reported
		case !exists:
			if err := s.MarkSidecarMissing(h.videoID, h.fileID, taskID, h.kind, h.path); err != nil {
				return missing, restored, changed, err
			}
			missing = append(missing, issue)
		case exists && knownMissing:
			if h.kind == "nfo" {
				if err := s.RestoreSidecarNFO(h.videoID, h.fileID, taskID, h.path); err != nil {
					return missing, restored, changed, err
				}
			} else if err := s.RestoreSidecar(h.videoID, h.fileID, taskID, st.Size(), h.kind, h.path); err != nil {
				return missing, restored, changed, err
			}
			restored = append(restored, issue)
		case exists && h.kind == "nfo":
			// Never size-check NFO (integrity check owns content). Keep size NULL when present.
			if h.sizeBytes.Valid && h.sizeBytes.Int64 != sidecarMissingSizeSentinel {
				if err := s.clearSidecarSizeBytes(h.fileID); err != nil {
					return missing, restored, changed, err
				}
			}
		case exists && !h.sizeBytes.Valid:
			if err := s.backfillSidecarSizeBytes(h.fileID, st.Size()); err != nil {
				return missing, restored, changed, err
			}
		case exists && h.sizeBytes.Int64 != st.Size():
			diskSize := st.Size()
			if err := s.MarkSidecarExternallyChanged(h.videoID, h.fileID, taskID, h.sizeBytes.Int64, diskSize, h.kind, h.path); err != nil {
				return missing, restored, changed, err
			}
			changed = append(changed, issue)
		}
	}
	return missing, restored, changed, nil
}

func parseTimeFlexible(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, s)
}
