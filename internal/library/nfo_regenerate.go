package library

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueRegenerateNFO queues a resumable library-wide NFO rewrite.
func (s *Store) EnqueueRegenerateNFO() (int64, error) {
	return s.EnqueueRegenerateNFOScoped(nil, nil)
}

// EnqueueRegenerateNFOScoped queues NFO rewrite for the whole library (empty scope),
// selected series, or selected videos. seriesIDs and videoIDs must not both be set.
func (s *Store) EnqueueRegenerateNFOScoped(seriesIDs, videoIDs []int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	seriesIDs = uniqInt64(seriesIDs)
	videoIDs = uniqInt64(videoIDs)
	if len(seriesIDs) > 0 && len(videoIDs) > 0 {
		return 0, fmt.Errorf("%w: series_ids and video_ids are mutually exclusive", ErrInvalid)
	}
	payload := map[string]any{
		"video_cursor":  0,
		"series_cursor": 0,
		"phase":         "videos",
		"rewrote":       0,
		"skipped":       0,
		"failed":        0,
	}
	msg := "Regenerate NFO files"
	if len(seriesIDs) > 0 {
		payload["series_ids"] = seriesIDs
		msg = "Regenerate series NFO"
	} else if len(videoIDs) > 0 {
		payload["video_ids"] = videoIDs
		msg = "Regenerate selected NFO"
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind:    queue.KindRegenerateNFO,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: msg,
	})
}

type nfoRegenPayload struct {
	VideoCursor  int64   `json:"video_cursor"`
	SeriesCursor int64   `json:"series_cursor"`
	Phase        string  `json:"phase"` // videos | series
	Rewrote      int     `json:"rewrote"`
	Skipped      int     `json:"skipped"`
	Failed       int     `json:"failed"`
	SeriesIDs    []int64 `json:"series_ids"`
	VideoIDs     []int64 `json:"video_ids"`
}

func (p nfoRegenPayload) persistMap(rewrote, skipped, failed int) map[string]any {
	m := map[string]any{
		"video_cursor":  p.VideoCursor,
		"series_cursor": p.SeriesCursor,
		"phase":         p.Phase,
		"rewrote":       rewrote,
		"skipped":       skipped,
		"failed":        failed,
	}
	if len(p.SeriesIDs) > 0 {
		m["series_ids"] = p.SeriesIDs
	}
	if len(p.VideoIDs) > 0 {
		m["video_ids"] = p.VideoIDs
	}
	return m
}

// NFORegeneratePass rewrites episode/series NFOs with payload cursors for restart resume.
func (s *Store) NFORegeneratePass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (rewrote, skipped, failed int, err error) {
	var p nfoRegenPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	if p.Phase == "" {
		p.Phase = "videos"
	}
	rewrote, skipped, failed = p.Rewrote, p.Skipped, p.Failed

	persist := func() error {
		return s.Queue.UpdatePayload(task.ID, p.persistMap(rewrote, skipped, failed))
	}

	if p.Phase == "videos" {
		q := `SELECT DISTINCT f.video_id FROM files f`
		args := []any{}
		where := ` WHERE f.kind = 'video' AND f.video_id > ?`
		args = append(args, p.VideoCursor)
		if len(p.VideoIDs) > 0 {
			where += ` AND f.video_id IN (` + sqlIntPlaceholders(len(p.VideoIDs)) + `)`
			for _, id := range p.VideoIDs {
				args = append(args, id)
			}
		} else if len(p.SeriesIDs) > 0 {
			q += ` JOIN videos v ON v.id = f.video_id`
			where += ` AND v.series_id IN (` + sqlIntPlaceholders(len(p.SeriesIDs)) + `)`
			for _, id := range p.SeriesIDs {
				args = append(args, id)
			}
		}
		q += where + ` ORDER BY f.video_id`
		rows, qerr := s.DB.SQL.Query(q, args...)
		if qerr != nil {
			return rewrote, skipped, failed, qerr
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return rewrote, skipped, failed, err
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
		for i, id := range ids {
			select {
			case <-ctx.Done():
				_ = persist()
				return rewrote, skipped, failed, ctx.Err()
			default:
			}
			changed, err := s.RewriteVideoNFO(id, task.ID)
			if err != nil {
				failed++
			} else if changed {
				rewrote++
			} else {
				skipped++
			}
			p.VideoCursor = id
			_ = persist()
			if progress != nil {
				pct := float64(i+1) / float64(len(ids)+1) * 0.9
				progress(fmt.Sprintf("NFO videos: rewrote=%d skipped=%d failed=%d", rewrote, skipped, failed), &pct)
			}
		}
		p.Phase = "series"
		_ = persist()
	}

	// Video-only scope: rewrite tvshow.nfo for distinct series of those videos once.
	var seriesIDs []int64
	if len(p.VideoIDs) > 0 {
		rows, qerr := s.DB.SQL.Query(`
			SELECT DISTINCT series_id FROM videos
			WHERE id IN (`+sqlIntPlaceholders(len(p.VideoIDs))+`) AND series_id > ?
			ORDER BY series_id
		`, append(int64Args(p.VideoIDs), p.SeriesCursor)...)
		if qerr != nil {
			return rewrote, skipped, failed, qerr
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return rewrote, skipped, failed, err
			}
			seriesIDs = append(seriesIDs, id)
		}
		_ = rows.Close()
	} else if len(p.SeriesIDs) > 0 {
		for _, id := range p.SeriesIDs {
			if id > p.SeriesCursor {
				seriesIDs = append(seriesIDs, id)
			}
		}
		sort.Slice(seriesIDs, func(i, j int) bool { return seriesIDs[i] < seriesIDs[j] })
	} else {
		rows, qerr := s.DB.SQL.Query(`SELECT id FROM series WHERE id > ? ORDER BY id`, p.SeriesCursor)
		if qerr != nil {
			return rewrote, skipped, failed, qerr
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return rewrote, skipped, failed, err
			}
			seriesIDs = append(seriesIDs, id)
		}
		_ = rows.Close()
	}

	for i, sid := range seriesIDs {
		select {
		case <-ctx.Done():
			_ = persist()
			return rewrote, skipped, failed, ctx.Err()
		default:
		}
		changed, err := s.RewriteSeriesNFOIfChanged(sid)
		if err != nil {
			failed++
		} else if changed {
			rewrote++
		} else {
			skipped++
		}
		p.SeriesCursor = sid
		_ = persist()
		if progress != nil {
			pct := 0.9 + float64(i+1)/float64(len(seriesIDs)+1)*0.1
			progress(fmt.Sprintf("NFO series: rewrote=%d skipped=%d failed=%d", rewrote, skipped, failed), &pct)
		}
	}
	return rewrote, skipped, failed, nil
}

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// NFORegenerateMessage formats the finish message.
func NFORegenerateMessage(rewrote, skipped, failed int) string {
	msg := fmt.Sprintf("NFO regenerate: rewrote %d, skipped %d", rewrote, skipped)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	return msg
}

// RecordNFORegenerateActivity stores the outcome message and detail on the task.
func (s *Store) RecordNFORegenerateActivity(taskID int64, rewrote, skipped, failed int) {
	msg := NFORegenerateMessage(rewrote, skipped, failed)
	if s.Queue != nil {
		p := 1.0
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
	detail, _ := json.Marshal(map[string]any{
		"rewrote": rewrote, "skipped": skipped, "failed": failed,
	})
	if s.Queue != nil {
		_ = s.Queue.SetDetail(taskID, string(detail))
	}
}
