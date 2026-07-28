package library

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueRegenerateNFO queues a resumable library-wide NFO rewrite.
func (s *Store) EnqueueRegenerateNFO() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRegenerateNFO,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"video_cursor":  0,
			"series_cursor": 0,
			"phase":         "videos",
			"rewrote":       0,
			"skipped":       0,
			"failed":        0,
		},
		Message: "Regenerate NFO files",
	})
}

type nfoRegenPayload struct {
	VideoCursor  int64  `json:"video_cursor"`
	SeriesCursor int64  `json:"series_cursor"`
	Phase        string `json:"phase"` // videos | series
	Rewrote      int    `json:"rewrote"`
	Skipped      int    `json:"skipped"`
	Failed       int    `json:"failed"`
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
		return s.Queue.UpdatePayload(task.ID, map[string]any{
			"video_cursor":  p.VideoCursor,
			"series_cursor": p.SeriesCursor,
			"phase":         p.Phase,
			"rewrote":       rewrote,
			"skipped":       skipped,
			"failed":        failed,
		})
	}

	if p.Phase == "videos" {
		rows, qerr := s.DB.SQL.Query(`
			SELECT DISTINCT video_id FROM files WHERE kind = 'video' AND video_id > ? ORDER BY video_id
		`, p.VideoCursor)
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

	rows, qerr := s.DB.SQL.Query(`SELECT id FROM series WHERE id > ? ORDER BY id`, p.SeriesCursor)
	if qerr != nil {
		return rewrote, skipped, failed, qerr
	}
	var seriesIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return rewrote, skipped, failed, err
		}
		seriesIDs = append(seriesIDs, id)
	}
	_ = rows.Close()
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
