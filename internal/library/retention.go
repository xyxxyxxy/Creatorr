package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RetentionPurgePass deletes downloaded media past root retention TTL for taskID.
func (s *Store) RetentionPurgePass(taskID int64, progress ...ProgressFn) (int, error) {
	return s.RetentionPurgePassAt(time.Now().UTC(), taskID, progress...)
}

// RetentionPurgePassAt is RetentionPurgePass with an injectable clock (tests).
func (s *Store) RetentionPurgePassAt(now time.Time, taskID int64, progress ...ProgressFn) (int, error) {
	var prog ProgressFn
	if len(progress) > 0 {
		prog = progress[0]
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.reportTaskProgress(taskID, prog, "Scanning retention…", 0.1)
	retentionIDs, err := s.retentionPurge(now, taskID, prog)
	if err != nil {
		return 0, err
	}
	msg := "No changes"
	if len(retentionIDs) > 0 {
		msg = fmt.Sprintf("Retention purge: %d expired", len(retentionIDs))
		detailBytes, _ := json.Marshal(map[string]any{
			"retention_ids": retentionIDs,
			"video_ids":     retentionIDs,
		})
		if taskID > 0 && s.Queue != nil {
			_ = s.Queue.SetDetail(taskID, string(detailBytes))
		}
	}
	s.reportTaskProgress(taskID, prog, msg, 1)
	return len(retentionIDs), nil
}

func (s *Store) retentionPurge(now time.Time, taskID int64, progress ProgressFn) ([]int64, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, f.path, f.acquired_at, r.retention_ttl_seconds, r.path
		FROM videos v
		JOIN files f ON f.video_id = v.id AND f.kind = 'video'
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'integrity_check_failed')
		  AND r.retention_ttl_seconds IS NOT NULL
		  AND r.retention_ttl_seconds > 0
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type victim struct {
		videoID int64
		path    string
		root    string
		ttl     int64
		acq     string
	}
	var list []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.videoID, &v.path, &v.acq, &v.ttl, &v.root); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var ids []int64
	touchedRoots := map[string]struct{}{}
	online := map[string]bool{}
	n := len(list)
	for i, v := range list {
		if n > 0 && (i%25 == 0 || i == n-1) {
			s.reportTaskProgress(taskID, progress,
				fmt.Sprintf("Scanning retention… %d/%d", i+1, n),
				0.1+0.85*float64(i+1)/float64(n))
		}
		ok, cached := online[v.root]
		if !cached {
			ok = rootOnline(v.root)
			online[v.root] = ok
		}
		if !ok {
			continue
		}
		acq, err := parseTimeFlexible(v.acq)
		if err != nil {
			continue
		}
		expire := acq.Add(time.Duration(v.ttl) * time.Second)
		if now.Before(expire) {
			continue
		}
		deleteVideoArtifacts(v.path)
		if err := s.MarkDeleted(v.videoID, "retention", taskID); err != nil {
			return ids, err
		}
		ids = append(ids, v.videoID)
		touchedRoots[v.root] = struct{}{}
	}
	for root := range touchedRoots {
		pruneEmptyDirs(root)
	}
	return ids, nil
}

func deleteVideoArtifacts(mediaPath string) {
	dir := filepath.Dir(mediaPath)
	stem := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = os.Remove(mediaPath)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == stem || strings.HasPrefix(name, stem+"-thumb") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func pruneEmptyDirs(root string) {
	root = filepath.Clean(root)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		return nil
	})
	// bottom-up: collect dirs then try rmdir
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // only succeeds if empty
	}
}
