package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueRegenerateStrm queues a resumable rewrite of all on-disk .strm URL lines.
func (s *Store) EnqueueRegenerateStrm() (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindRegenerateStrm,
		Domain: queue.SystemDomain,
		Payload: map[string]any{
			"video_cursor": 0,
			"rewrote":      0,
			"skipped":      0,
			"failed":       0,
		},
		Message: "Regenerate strm files",
	})
}

type strmRegenPayload struct {
	VideoCursor int64 `json:"video_cursor"`
	Rewrote     int   `json:"rewrote"`
	Skipped     int   `json:"skipped"`
	Failed      int   `json:"failed"`
}

// RewriteStreamFile rewrites the .strm beside a video to the current external URL + token.
// Returns true when bytes changed.
func (s *Store) RewriteStreamFile(videoID int64) (changed bool, err error) {
	base := s.EffectivePublicBaseURL()
	if base == "" {
		return false, fmt.Errorf("%w: external Creatorr URL required", ErrInvalid)
	}
	tok, err := EnsureStreamToken(s.DB)
	if err != nil {
		return false, err
	}
	want, err := StreamURL(base, videoID, tok)
	if err != nil {
		return false, err
	}
	wantBody := want + "\n"

	var path string
	err = s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'strm' ORDER BY id LIMIT 1
	`, videoID).Scan(&path)
	if err != nil {
		return false, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false, fmt.Errorf("empty strm path")
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if string(cur) == wantBody {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(wantBody), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// StrmRegeneratePass rewrites .strm files with payload cursors for restart resume.
func (s *Store) StrmRegeneratePass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (rewrote, skipped, failed int, err error) {
	if strings.TrimSpace(s.EffectivePublicBaseURL()) == "" {
		return 0, 0, 0, fmt.Errorf("%w: external Creatorr URL required", ErrInvalid)
	}
	if _, err := EnsureStreamToken(s.DB); err != nil {
		return 0, 0, 0, err
	}

	var p strmRegenPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	rewrote, skipped, failed = p.Rewrote, p.Skipped, p.Failed

	persist := func() error {
		return s.Queue.UpdatePayload(task.ID, map[string]any{
			"video_cursor": p.VideoCursor,
			"rewrote":      rewrote,
			"skipped":      skipped,
			"failed":       failed,
		})
	}

	rows, qerr := s.DB.SQL.Query(`
		SELECT DISTINCT video_id FROM files WHERE kind = 'strm' AND video_id > ? ORDER BY video_id
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
		changed, err := s.RewriteStreamFile(id)
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
			pct := float64(i+1) / float64(len(ids)+1)
			progress(fmt.Sprintf("strm: rewrote=%d skipped=%d failed=%d", rewrote, skipped, failed), &pct)
		}
	}
	return rewrote, skipped, failed, nil
}

// StrmRegenerateMessage formats the finish message.
func StrmRegenerateMessage(rewrote, skipped, failed int) string {
	msg := fmt.Sprintf("strm regenerate: rewrote %d, skipped %d", rewrote, skipped)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	return msg
}

// RecordStrmRegenerateActivity stores the outcome message and detail on the task.
func (s *Store) RecordStrmRegenerateActivity(taskID int64, rewrote, skipped, failed int) {
	msg := StrmRegenerateMessage(rewrote, skipped, failed)
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
