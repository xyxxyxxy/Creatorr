package library

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueVerifyAllMedia queues a resumable library-wide media verify pass.
func (s *Store) EnqueueVerifyAllMedia() (int64, error) {
	return s.EnqueueVerifyAllMediaScoped(nil, nil)
}

// EnqueueVerifyAllMediaScoped queues verify for the whole library (empty scope),
// selected series, or selected videos. seriesIDs and videoIDs must not both be set.
func (s *Store) EnqueueVerifyAllMediaScoped(seriesIDs, videoIDs []int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	seriesIDs = uniqInt64(seriesIDs)
	videoIDs = uniqInt64(videoIDs)
	if len(seriesIDs) > 0 && len(videoIDs) > 0 {
		return 0, fmt.Errorf("%w: series_ids and video_ids are mutually exclusive", ErrInvalid)
	}
	payload := map[string]any{
		"cursor":   0,
		"verified": 0,
		"skipped":  0,
		"failed":   0,
	}
	msg := "Verify all downloaded media"
	if len(seriesIDs) > 0 {
		payload["series_ids"] = seriesIDs
		msg = "Verify series media"
	} else if len(videoIDs) > 0 {
		payload["video_ids"] = videoIDs
		msg = "Verify selected media"
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindVerifyAllMedia,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: msg,
	})
}

type verifyAllMediaPayload struct {
	Cursor    int64   `json:"cursor"`
	Verified  int     `json:"verified"`
	Skipped   int     `json:"skipped"`
	Failed    int     `json:"failed"`
	SeriesIDs []int64 `json:"series_ids"`
	VideoIDs  []int64 `json:"video_ids"`
}

// VerifyAllMediaFail is one failed video from VerifyAllMediaPass (for notify).
type VerifyAllMediaFail struct {
	VideoID     int64
	SeriesTitle string
	VideoTitle  string
	Detail      string
}

// VerifyAllMediaPass null-decodes packed downloaded/verify_failed media with a cursor for resume.
// onFail is optional; called after MarkVerifyFailed for each failure.
func (s *Store) VerifyAllMediaPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64), onFail func(VerifyAllMediaFail)) (verified, skipped, failed int, err error) {
	var p verifyAllMediaPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	verified, skipped, failed = p.Verified, p.Skipped, p.Failed

	persist := func() error {
		m := map[string]any{
			"cursor":   p.Cursor,
			"verified": verified,
			"skipped":  skipped,
			"failed":   failed,
		}
		if len(p.SeriesIDs) > 0 {
			m["series_ids"] = p.SeriesIDs
		}
		if len(p.VideoIDs) > 0 {
			m["video_ids"] = p.VideoIDs
		}
		return s.Queue.UpdatePayload(task.ID, m)
	}

	q := `
		SELECT v.id
		FROM videos v
		WHERE v.status IN ('downloaded', 'verify_failed')
		  AND v.id > ?
		  AND EXISTS (
		    SELECT 1 FROM files f
		    WHERE f.video_id = v.id AND f.kind = 'video'
		  )`
	args := []any{p.Cursor}
	if len(p.SeriesIDs) > 0 {
		q += ` AND v.series_id IN (` + sqlIntPlaceholders(len(p.SeriesIDs)) + `)`
		for _, id := range p.SeriesIDs {
			args = append(args, id)
		}
	}
	if len(p.VideoIDs) > 0 {
		q += ` AND v.id IN (` + sqlIntPlaceholders(len(p.VideoIDs)) + `)`
		for _, id := range p.VideoIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY v.id ASC`

	rows, qerr := s.DB.SQL.Query(q, args...)
	if qerr != nil {
		return verified, skipped, failed, qerr
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return verified, skipped, failed, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return verified, skipped, failed, err
	}

	total := len(ids)
	for i, id := range ids {
		select {
		case <-ctx.Done():
			_ = persist()
			return verified, skipped, failed, ctx.Err()
		default:
		}
		p.Cursor = id
		_ = persist()
		if progress != nil && total > 0 {
			pct := float64(i) / float64(total)
			progress(fmt.Sprintf("Verifying %d/%d…", i+1, total), &pct)
		}

		busy, berr := s.videoBusyForRename(id, task.ID)
		if berr != nil {
			failed++
			_ = persist()
			continue
		}
		if busy {
			skipped++
			_ = persist()
			continue
		}

		path, ok, herr := s.HasVideoFile(id)
		if herr != nil {
			failed++
			_ = persist()
			continue
		}
		if !ok || path == "" {
			skipped++
			_ = persist()
			continue
		}

		perProgress := func(msg string, pct *float64) {
			if progress == nil {
				return
			}
			base := 0.0
			if total > 0 {
				base = float64(i) / float64(total)
			}
			span := 1.0 / float64(total+1)
			if pct != nil {
				f := base + *pct*span
				progress(msg, &f)
			} else {
				progress(msg, &base)
			}
		}
		if verr := VerifyDownloadedMedia(ctx, path, perProgress); verr != nil {
			if ctx.Err() != nil {
				_ = persist()
				return verified, skipped, failed, ctx.Err()
			}
			failed++
			_ = s.MarkVerifyFailed(id, task.ID, "Media verify failed")
			if onFail != nil {
				seriesTitle := ""
				videoTitle := ""
				if v, gerr := s.GetVideo(id); gerr == nil && v != nil {
					videoTitle = v.Title
					if ser, serr := s.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
						seriesTitle = ser.Title
					}
				}
				onFail(VerifyAllMediaFail{
					VideoID: id, SeriesTitle: seriesTitle, VideoTitle: videoTitle, Detail: verr.Error(),
				})
			}
			_ = persist()
			continue
		}
		if merr := s.MarkVerified(id, task.ID); merr != nil {
			failed++
			_ = persist()
			continue
		}
		verified++
		_ = persist()
	}
	return verified, skipped, failed, nil
}

// VerifyAllMediaMessage formats the finish message.
func VerifyAllMediaMessage(verified, skipped, failed int) string {
	msg := fmt.Sprintf("Verify all media: verified %d, skipped %d", verified, skipped)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	return msg
}
