package library

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueDeleteFiles queues worker-owned disk (+ DB) deletion for series and/or videos.
func (s *Store) EnqueueDeleteFiles(seriesIDs, videoIDs []int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	seriesIDs = uniqPositive(seriesIDs)
	videoIDs = uniqPositive(videoIDs)
	if len(seriesIDs) == 0 && len(videoIDs) == 0 {
		return 0, fmt.Errorf("%w: series_ids or video_ids required", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindDeleteFiles,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"series_ids":   seriesIDs,
			"video_ids":    videoIDs,
			"video_index":  0,
			"series_index": 0,
			"phase":        "videos",
		},
		Message: "Delete files",
	})
}

func uniqPositive(ids []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

type fileDeletePayload struct {
	SeriesIDs   []int64 `json:"series_ids"`
	VideoIDs    []int64 `json:"video_ids"`
	VideoIndex  int     `json:"video_index"`
	SeriesIndex int     `json:"series_index"`
	Phase       string  `json:"phase"` // videos | series
}

// ActiveFileDeleteTargets returns series/video ids targeted by pending/running delete_files tasks.
func (s *Store) ActiveFileDeleteTargets() (seriesIDs map[int64]struct{}, videoIDs map[int64]struct{}, err error) {
	seriesIDs = map[int64]struct{}{}
	videoIDs = map[int64]struct{}{}
	if s.Queue == nil {
		return seriesIDs, videoIDs, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT payload FROM tasks
		WHERE kind = ? AND status IN (?, ?)
	`, queue.KindDeleteFiles, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, nil, err
		}
		var p fileDeletePayload
		_ = json.Unmarshal([]byte(raw), &p)
		for _, sid := range p.SeriesIDs {
			if sid > 0 {
				seriesIDs[sid] = struct{}{}
			}
		}
		for _, vid := range p.VideoIDs {
			if vid > 0 {
				videoIDs[vid] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	// Expand series → videos so list rows for those episodes show deleting state.
	for sid := range seriesIDs {
		vrows, err := s.DB.SQL.Query(`SELECT id FROM videos WHERE series_id = ?`, sid)
		if err != nil {
			return nil, nil, err
		}
		for vrows.Next() {
			var vid int64
			if err := vrows.Scan(&vid); err != nil {
				_ = vrows.Close()
				return nil, nil, err
			}
			videoIDs[vid] = struct{}{}
		}
		_ = vrows.Close()
	}
	// Expand video → series so series rows show deleting when a video purge is queued.
	for vid := range videoIDs {
		var sid int64
		if err := s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&sid); err != nil {
			continue
		}
		if sid > 0 {
			seriesIDs[sid] = struct{}{}
		}
	}
	return seriesIDs, videoIDs, nil
}

// VideoQueuedForDelete reports whether videoID is in an active delete_files task.
func (s *Store) VideoQueuedForDelete(videoID int64) (bool, error) {
	_, vids, err := s.ActiveFileDeleteTargets()
	if err != nil {
		return false, err
	}
	_, ok := vids[videoID]
	return ok, nil
}

// SeriesQueuedForDelete reports whether seriesID is in an active delete_files task.
func (s *Store) SeriesQueuedForDelete(seriesID int64) (bool, error) {
	sids, _, err := s.ActiveFileDeleteTargets()
	if err != nil {
		return false, err
	}
	_, ok := sids[seriesID]
	return ok, nil
}

// FileDeletePass deletes on-disk artifacts then MarkDeleted / DELETE series.
func (s *Store) FileDeletePass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) error {
	var p fileDeletePayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	if p.Phase == "" {
		p.Phase = "videos"
	}
	persist := func() error {
		return s.Queue.UpdatePayload(task.ID, map[string]any{
			"series_ids":   p.SeriesIDs,
			"video_ids":    p.VideoIDs,
			"video_index":  p.VideoIndex,
			"series_index": p.SeriesIndex,
			"phase":        p.Phase,
		})
	}

	seriesSet := map[int64]struct{}{}
	for _, sid := range p.SeriesIDs {
		seriesSet[sid] = struct{}{}
	}

	if p.Phase == "videos" {
		for p.VideoIndex < len(p.VideoIDs) {
			select {
			case <-ctx.Done():
				_ = persist()
				return ctx.Err()
			default:
			}
			vid := p.VideoIDs[p.VideoIndex]
			if err := s.deleteVideoDiskArtifacts(vid); err != nil {
				return err
			}
			if err := s.MarkDeleted(vid, "manual", task.ID); err != nil {
				return err
			}
			p.VideoIndex++
			_ = persist()
			if progress != nil {
				pct := float64(p.VideoIndex) / float64(len(p.VideoIDs)+len(p.SeriesIDs)+1)
				progress(fmt.Sprintf("Deleted video %d/%d", p.VideoIndex, len(p.VideoIDs)), &pct)
			}
		}
		p.Phase = "series"
		_ = persist()
	}

	for p.SeriesIndex < len(p.SeriesIDs) {
		select {
		case <-ctx.Done():
			_ = persist()
			return ctx.Err()
		default:
		}
		sid := p.SeriesIDs[p.SeriesIndex]
		ser, err := s.GetSeries(sid, false)
		if err != nil {
			return err
		}
		root, err := s.GetRoot(ser.RootID)
		if err != nil {
			return err
		}
		vrows, err := s.DB.SQL.Query(`SELECT id FROM videos WHERE series_id = ? ORDER BY id`, sid)
		if err != nil {
			return err
		}
		var vids []int64
		for vrows.Next() {
			var vid int64
			if err := vrows.Scan(&vid); err != nil {
				_ = vrows.Close()
				return err
			}
			vids = append(vids, vid)
		}
		_ = vrows.Close()
		for _, vid := range vids {
			select {
			case <-ctx.Done():
				_ = persist()
				return ctx.Err()
			default:
			}
			if err := s.deleteVideoDiskArtifacts(vid); err != nil {
				return err
			}
		}
		if _, err := s.DB.SQL.Exec(`DELETE FROM series WHERE id = ?`, sid); err != nil {
			return err
		}
		pruneEmptyDirsUnder(root.Path, SeriesDir(root.Path, ser.Title))
		p.SeriesIndex++
		_ = persist()
		if progress != nil {
			pct := 0.5 + float64(p.SeriesIndex)/float64(len(p.SeriesIDs)+1)*0.5
			progress(fmt.Sprintf("Deleted series %d/%d", p.SeriesIndex, len(p.SeriesIDs)), &pct)
		}
	}
	return nil
}

func (s *Store) deleteVideoDiskArtifacts(videoID int64) error {
	rows, err := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ?`, videoID)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return err
		}
		if p == "" {
			continue
		}
		dir := filepath.Dir(p)
		stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		key := dir + "\x00" + stem
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deleteVideoArtifacts(p)
	}
	return rows.Err()
}

func pruneEmptyDirsUnder(root, seriesDir string) {
	_ = PruneEmptyDir(seriesDir)
	parent := filepath.Dir(seriesDir)
	if parent != "" && filepath.Clean(parent) != filepath.Clean(root) {
		_ = PruneEmptyDir(parent)
	}
}

// FileDeleteMessage formats the finish message.
func FileDeleteMessage(p fileDeletePayload) string {
	return fmt.Sprintf("Deleted %d video(s), %d series", len(p.VideoIDs), len(p.SeriesIDs))
}

// RecordFileDeleteActivity stores the outcome message and detail on the task.
func (s *Store) RecordFileDeleteActivity(task *queue.Task) {
	var p fileDeletePayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	msg := FileDeleteMessage(p)
	if s.Queue != nil {
		done := 1.0
		_ = s.Queue.UpdateProgress(task.ID, msg, &done)
	}
	detail, _ := json.Marshal(map[string]any{
		"series_ids": p.SeriesIDs,
		"video_ids":  p.VideoIDs,
	})
	if s.Queue != nil {
		_ = s.Queue.SetDetail(task.ID, string(detail))
	}
}
