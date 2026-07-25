package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueRenameEpisodes queues a library-wide rename using a format snapshot.
func (s *Store) EnqueueRenameEpisodes() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	cfg := LoadNamingConfig(s.DB)
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRenameEpisodes,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"episode_format": cfg.EpisodeFormat,
			"cursor":         0,
		},
		Message: "Apply episode format",
	})
}

// EnqueueSyncFiles queues a system-lane file sync pass.
// No-op (returns 0, nil) when the library has no videos.
func (s *Store) EnqueueSyncFiles(priority int) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ok, err := s.anyVideo()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindSyncFiles,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "File sync",
	})
}

// EnqueueRetentionDelete queues a system-lane retention TTL purge.
// No-op (returns 0, nil) when no root has retention_ttl_seconds set.
func (s *Store) EnqueueRetentionDelete(priority int) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ok, err := s.anyRootRetentionTTL()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindRetentionDelete,
		Domain:   queue.SystemDomain,
		Priority: priority,
		Message:  "Retention purge",
	})
}

type applyNamingPayload struct {
	EpisodeFormat string `json:"episode_format"`
	Cursor        int64  `json:"cursor"`
}

func (p applyNamingPayload) namingConfig() NamingConfig {
	fmtStr := strings.TrimSpace(p.EpisodeFormat)
	if fmtStr == "" {
		fmtStr = DefaultEpisodeFormat
	}
	return NamingConfig{EpisodeFormat: fmtStr}
}

// ApplyEpisodeNamingPass renames packed episodes to the snapshot formats.
func (s *Store) ApplyEpisodeNamingPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (renamed, skippedBusy, failed int, err error) {
	var p applyNamingPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	cfg := p.namingConfig()

	type row struct {
		ID          int64
		Title       string
		RemoteID    string
		Season      int
		Episode     int
		SeriesTitle string
		RootPath    string
		UploadDate  sql.NullString
		SourceURL   string
	}
	qrows, err := s.DB.SQL.Query(`
		SELECT v.id, v.title, v.remote_id,
		       COALESCE(v.season, 1), COALESCE(v.episode, 1),
		       s.title, r.path, v.upload_date, COALESCE(v.source_url, '')
		FROM videos v
		JOIN series s ON s.id = v.series_id
		JOIN root_folders r ON r.id = s.root_id
		WHERE v.status IN ('downloaded', 'streamable', 'verify_failed')
		  AND v.id > ?
		  AND EXISTS (
		    SELECT 1 FROM files f
		    WHERE f.video_id = v.id AND f.kind IN ('video', 'strm')
		  )
		ORDER BY v.id ASC
	`, p.Cursor)
	if err != nil {
		return 0, 0, 0, err
	}
	var list []row
	for qrows.Next() {
		var r row
		if err := qrows.Scan(&r.ID, &r.Title, &r.RemoteID, &r.Season, &r.Episode, &r.SeriesTitle, &r.RootPath, &r.UploadDate, &r.SourceURL); err != nil {
			_ = qrows.Close()
			return 0, 0, 0, err
		}
		list = append(list, r)
	}
	_ = qrows.Close()
	if err := qrows.Err(); err != nil {
		return 0, 0, 0, err
	}

	total := len(list)
	for i, r := range list {
		if err := ctx.Err(); err != nil {
			return renamed, skippedBusy, failed, err
		}
		p.Cursor = r.ID
		payload := map[string]any{
			"cursor":         p.Cursor,
			"episode_format": p.namingConfig().EpisodeFormat,
		}
		_ = s.Queue.UpdatePayload(task.ID, payload)
		if progress != nil && total > 0 {
			pct := float64(i) / float64(total)
			progress(fmt.Sprintf("Renaming %d/%d…", i+1, total), &pct)
		}

		busy, err := s.videoBusyForRename(r.ID)
		if err != nil {
			failed++
			continue
		}
		if busy {
			skippedBusy++
			continue
		}

		aired := ""
		if r.UploadDate.Valid {
			aired = r.UploadDate.String
		}
		ok, skip, fail := s.renameVideoEpisodeSet(task.ID, r.ID, r.SeriesTitle, r.Title, r.RemoteID, r.Season, r.Episode, aired, namingDomain(r.SourceURL), r.RootPath, cfg)
		if skip {
			skippedBusy++
		} else if fail {
			failed++
		} else if ok {
			renamed++
		}
	}
	return renamed, skippedBusy, failed, nil
}

func (s *Store) videoBusyForRename(videoID int64) (bool, error) {
	var one int
	err := s.DB.SQL.QueryRow(`
		SELECT 1 FROM tasks
		WHERE video_id = ? AND kind IN (?, ?, ?, ?, ?, ?) AND status IN (?, ?)
		LIMIT 1
	`, videoID, queue.KindDownload, queue.KindPackStream, queue.KindCacheBeginning, queue.KindSponsorblockCut, queue.KindMediaVerify, queue.KindStreamPlay,
		queue.StatusPending, queue.StatusRunning).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) renameVideoEpisodeSet(taskID, videoID int64, seriesTitle, title, remoteID string, season, episode int, aired, domain, root string, cfg NamingConfig) (renamed, skipped, failed bool) {
	busy, _ := s.videoBusyForRename(videoID)
	if busy {
		return false, true, false
	}

	fileRows, err := s.DB.SQL.Query(`SELECT id, kind, path FROM files WHERE video_id = ?`, videoID)
	if err != nil {
		return false, false, true
	}
	type frow struct {
		ID   int64
		Kind string
		Path string
	}
	var files []frow
	var primary string
	for fileRows.Next() {
		var f frow
		if err := fileRows.Scan(&f.ID, &f.Kind, &f.Path); err != nil {
			_ = fileRows.Close()
			return false, false, true
		}
		files = append(files, f)
		if (f.Kind == "video" || f.Kind == "strm") && primary == "" {
			primary = f.Path
		}
	}
	_ = fileRows.Close()
	if primary == "" || !fileExists(primary) {
		return false, false, false
	}

	meta := EpisodeNFO{
		SeriesTitle: seriesTitle,
		Title:       title,
		Season:      season,
		Episode:     episode,
		Aired:       aired,
		UniqueID:    remoteID,
		Domain:      domain,
	}
	dest, err := BuildEpisodePaths(root, meta, cfg)
	if err != nil {
		return false, false, true
	}

	oldBase := strings.TrimSuffix(primary, filepath.Ext(primary))
	newBase := dest.PrimaryBase
	if filepath.Clean(oldBase) == filepath.Clean(newBase) {
		return false, false, false
	}

	currentPaths := make([]string, 0, len(files))
	for _, f := range files {
		currentPaths = append(currentPaths, f.Path)
	}

	type move struct {
		from, to string
		id       int64
	}
	var moves []move
	for _, f := range files {
		if !strings.HasPrefix(f.Path, oldBase) {
			return false, false, true
		}
		rel := strings.TrimPrefix(f.Path, oldBase)
		to := newBase + rel
		if DestinationOccupied(to, currentPaths) {
			return false, false, true
		}
		moves = append(moves, move{from: f.Path, to: to, id: f.ID})
	}

	if err := os.MkdirAll(dest.EpisodeDir, 0o755); err != nil {
		return false, false, true
	}

	var done []move
	for _, m := range moves {
		if m.from == m.to {
			done = append(done, m)
			continue
		}
		if err := moveFile(m.from, m.to); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				d := done[i]
				if d.from != d.to {
					_ = moveFile(d.to, d.from)
				}
			}
			return false, false, true
		}
		done = append(done, m)
	}

	for _, m := range done {
		_, _ = s.DB.SQL.Exec(`UPDATE files SET path = ? WHERE id = ?`, m.to, m.id)
	}

	oldDir := filepath.Dir(primary)
	_ = PruneEmptyDir(oldDir)
	parent := filepath.Dir(oldDir)
	if parent != "" && filepath.Clean(parent) != filepath.Clean(root) {
		_ = PruneEmptyDir(parent)
	}

	_ = s.AddVideoHistory(videoID, "renamed", "Episode files renamed", map[string]any{
		"previous": filepath.Base(oldBase),
		"new":      filepath.Base(newBase),
		"previous_path": oldBase,
		"new_path":      newBase,
	}, taskID)

	return true, false, false
}

// ApplyNamingMessage formats the finish message.
func ApplyNamingMessage(renamed, skippedBusy, failed int) string {
	return fmt.Sprintf("Renamed %d, skipped busy %d, failed %d", renamed, skippedBusy, failed)
}
