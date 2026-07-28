package library

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// VideoSizeBytes returns size_bytes for the video media file, or ok=false when unset/missing.
func (s *Store) VideoSizeBytes(videoID int64) (n int64, ok bool, err error) {
	var size sql.NullInt64
	err = s.DB.SQL.QueryRow(`
		SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'video' LIMIT 1
	`, videoID).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !size.Valid {
		return 0, false, nil
	}
	return size.Int64, true, nil
}

// VideoSizeBytesMap returns size_bytes for kind=video rows among the given video IDs.
// Missing or NULL sizes are omitted from the map.
func (s *Store) VideoSizeBytesMap(videoIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT video_id, size_bytes FROM files
		WHERE kind = 'video' AND size_bytes IS NOT NULL
		  AND video_id IN (`+sqlIntPlaceholders(len(videoIDs))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			return nil, err
		}
		out[id] = size
	}
	return out, rows.Err()
}

// VideoThumbPath returns the on-disk thumb path when a kind=thumb file row exists and the path is present.
func (s *Store) VideoThumbPath(videoID int64) (path string, ok bool, err error) {
	err = s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'thumb' LIMIT 1
	`, videoID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	if _, err := os.Stat(path); err != nil {
		return "", false, nil
	}
	return path, true, nil
}

// VideoThumbPathMap returns on-disk thumb paths for videos that have a kind=thumb file present.
func (s *Store) VideoThumbPathMap(videoIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT video_id, path FROM files
		WHERE kind = 'thumb' AND path IS NOT NULL AND path != ''
		  AND video_id IN (`+sqlIntPlaceholders(len(videoIDs))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out[id] = path
	}
	return out, rows.Err()
}

// VideoJSONPathMap returns on-disk info.json paths for videos that have a kind=json file present.
func (s *Store) VideoJSONPathMap(videoIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT video_id, path FROM files
		WHERE kind = 'json' AND path IS NOT NULL AND path != ''
		  AND video_id IN (`+sqlIntPlaceholders(len(videoIDs))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out[id] = path
	}
	return out, rows.Err()
}

// VideoFile is one row from the files table for a video.
type VideoFile struct {
	ID         int64
	Path       string
	Kind       string
	AcquiredAt string
	SizeBytes  sql.NullInt64
}

// SidecarKinds are known non-media companion roles (not packed video media).
var SidecarKinds = map[string]bool{
	"nfo": true, "json": true, "thumb": true, "sub": true, "sponsorblock": true,
}

// ListVideoMediaFiles returns kind=video rows for a video.
func (s *Store) ListVideoMediaFiles(videoID int64) ([]VideoFile, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, path, kind, acquired_at, size_bytes
		FROM files
		WHERE video_id = ? AND kind = 'video'
		ORDER BY path
	`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VideoFile
	for rows.Next() {
		var f VideoFile
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &f.AcquiredAt, &f.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListVideoSidecars returns companion files beside the packed media whose basename
// starts with the media stem (filename without final extension). Merges on-disk
// matches with files-table rows (DB wins for id/kind). Media (video) excluded.
// Ordered: nfo, json, thumb, sub, sponsorblock, then other, then path.
func (s *Store) ListVideoSidecars(videoID int64) ([]VideoFile, error) {
	mediaPath, mediaBases, err := s.videoMediaStemContext(videoID)
	if err != nil {
		return nil, err
	}
	if mediaPath == "" {
		return nil, nil
	}
	stem := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	if stem == "" {
		return nil, nil
	}

	byPath, err := s.listVideoFilesByPath(videoID)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var out []VideoFile

	dir := filepath.Dir(mediaPath)
	if entries, rerr := os.ReadDir(dir); rerr == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, stem) {
				continue
			}
			if _, skip := mediaBases[name]; skip {
				continue
			}
			path := filepath.Join(dir, name)
			seen[path] = struct{}{}
			if f, ok := byPath[path]; ok {
				if f.Kind == "video" {
					continue
				}
				out = append(out, f)
				continue
			}
			out = append(out, VideoFile{
				Path: path,
				Kind: InferEpisodeSidecarKind(name),
			})
		}
	}

	for path, f := range byPath {
		if f.Kind == "video" {
			continue
		}
		base := filepath.Base(path)
		if !strings.HasPrefix(base, stem) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		out = append(out, f)
	}

	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := sidecarKindOrder(out[i].Kind), sidecarKindOrder(out[j].Kind)
		if pi != pj {
			return pi < pj
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func sidecarKindOrder(kind string) int {
	switch kind {
	case "nfo":
		return 1
	case "json":
		return 2
	case "thumb":
		return 3
	case "sub":
		return 4
	case "sponsorblock":
		return 5
	default:
		return 9
	}
}

// InferEpisodeSidecarKind guesses files.kind from an episode companion basename.
func InferEpisodeSidecarKind(name string) string {
	lower := strings.ToLower(filepath.Base(name))
	switch {
	case strings.HasSuffix(lower, ".sponsorblock.json"):
		return "sponsorblock"
	case strings.HasSuffix(lower, ".info.json"):
		return "json"
	case strings.HasSuffix(lower, ".nfo"):
		return "nfo"
	case strings.Contains(lower, "-thumb.") || strings.Contains(lower, ".thumb."):
		return "thumb"
	}
	ext := strings.ToLower(filepath.Ext(lower))
	switch ext {
	case ".srt", ".vtt", ".ass", ".ssa", ".sub":
		return "sub"
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return "thumb"
	default:
		return "other"
	}
}

// videoMediaStemContext returns a packed media path (kind=video)
// for stem derivation, plus basenames of all video rows to exclude from sidecars.
func (s *Store) videoMediaStemContext(videoID int64) (mediaPath string, mediaBases map[string]struct{}, err error) {
	rows, err := s.DB.SQL.Query(`
		SELECT path, kind FROM files
		WHERE video_id = ? AND kind = 'video'
		ORDER BY id
	`, videoID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	mediaBases = map[string]struct{}{}
	for rows.Next() {
		var path, kind string
		if err := rows.Scan(&path, &kind); err != nil {
			return "", nil, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		mediaBases[filepath.Base(path)] = struct{}{}
		if mediaPath == "" || kind == "video" {
			mediaPath = path
		}
	}
	return mediaPath, mediaBases, rows.Err()
}

func (s *Store) listVideoFilesByPath(videoID int64) (map[string]VideoFile, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, path, kind, acquired_at, size_bytes
		FROM files WHERE video_id = ?
	`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VideoFile{}
	for rows.Next() {
		var f VideoFile
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &f.AcquiredAt, &f.SizeBytes); err != nil {
			return nil, err
		}
		f.Path = strings.TrimSpace(f.Path)
		if f.Path == "" {
			continue
		}
		out[f.Path] = f
	}
	return out, rows.Err()
}

// GetVideoFile loads one files row for a video (any kind).
func (s *Store) GetVideoFile(videoID, fileID int64) (*VideoFile, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT id, path, kind, acquired_at, size_bytes
		FROM files
		WHERE id = ? AND video_id = ?
	`, fileID, videoID)
	var f VideoFile
	err := row.Scan(&f.ID, &f.Path, &f.Kind, &f.AcquiredAt, &f.SizeBytes)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeletableSidecarKind reports whether an operator may delete this files.kind
// individually (sub, thumb, other). Generated/provenance kinds are excluded.
func DeletableSidecarKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "sub", "thumb", "other":
		return true
	default:
		return false
	}
}

// DeleteVideoSidecar unlinks one registered sidecar and drops its files row.
// Sync (like series art clear): not delete_files. History links a finished
// system delete_sidecar bookkeeping task.
func (s *Store) DeleteVideoSidecar(videoID, fileID int64) error {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	f, err := s.GetVideoFile(videoID, fileID)
	if err != nil {
		return err
	}
	if !DeletableSidecarKind(f.Kind) {
		return fmt.Errorf("%w: cannot delete %s files individually", ErrInvalid, f.Kind)
	}
	path := strings.TrimSpace(f.Path)
	name := filepath.Base(path)
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove sidecar: %w", err)
		}
	}
	res, err := s.DB.SQL.Exec(`DELETE FROM files WHERE id = ? AND video_id = ?`, fileID, videoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	var taskID int64
	if s.Queue != nil {
		tid, qerr := s.Queue.InsertRunning(queue.EnqueueParams{
			Kind:     queue.KindDeleteSidecar,
			Domain:   queue.SystemDomain,
			SeriesID: v.SeriesID,
			VideoID:  videoID,
			Message:  fmt.Sprintf("Delete sidecar %s", name),
			Payload:  map[string]any{"file_id": fileID, "kind": f.Kind, "path": path},
		})
		if qerr != nil {
			return qerr
		}
		taskID = tid
		_ = s.Queue.Finish(tid, queue.StatusDone, fmt.Sprintf("Deleted %s", name), "", "")
	}
	if taskID > 0 {
		_ = s.AddVideoHistory(videoID, "sidecar_deleted", fmt.Sprintf("Deleted sidecar %s", name), map[string]any{
			"kind": f.Kind, "path": path, "name": name, "file_id": fileID,
		}, taskID)
	}
	return nil
}

// FormatBytes formats n as an IEC byte string (KiB/MiB/GiB/TiB).
func FormatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	prefixes := "KMGTPE"
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), prefixes[exp])
}

// RegisterFileKind inserts or replaces a files row for videoID/kind.
func (s *Store) RegisterFileKind(videoID int64, path, kind string) error {
	path = strings.TrimSpace(path)
	kind = strings.TrimSpace(kind)
	if path == "" || kind == "" || !fileExists(path) {
		return nil
	}
	acquired := nowRFC3339()
	_, _ = s.DB.SQL.Exec(`DELETE FROM files WHERE video_id = ? AND kind = ?`, videoID, kind)
	_, err := s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, ?, ?, NULL)
	`, videoID, path, kind, acquired)
	return err
}

