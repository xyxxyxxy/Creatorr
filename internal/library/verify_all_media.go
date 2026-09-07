package library

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueVerifyAllMedia queues a resumable library integrity check pass.
func (s *Store) EnqueueVerifyAllMedia(origin string) (int64, error) {
	return s.EnqueueVerifyAllMediaScoped(nil, nil, origin)
}

// EnqueueVerifyAllMediaScoped queues integrity check for the whole library (empty scope),
// selected series, or selected videos. seriesIDs and videoIDs must not both be set.
func (s *Store) EnqueueVerifyAllMediaScoped(seriesIDs, videoIDs []int64, origin string) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	if origin == "" {
		origin = queue.OriginManual
	}
	seriesIDs = uniqInt64(seriesIDs)
	videoIDs = uniqInt64(videoIDs)
	if len(seriesIDs) > 0 && len(videoIDs) > 0 {
		return 0, fmt.Errorf("%w: series_ids and video_ids are mutually exclusive", ErrInvalid)
	}
	payload := map[string]any{
		"cursor":              0,
		"integrity_checked":   0,
		"partial":             0,
		"skipped":             0,
		"failed":              0,
		"skipped_busy":        0,
		"skipped_profile_off": 0,
		"skipped_no_media":    0,
	}
	msg := "Integrity check"
	if len(seriesIDs) > 0 {
		payload["series_ids"] = seriesIDs
		msg = "Integrity check (series)"
	} else if len(videoIDs) > 0 {
		payload["video_ids"] = videoIDs
		msg = "Integrity check (selected)"
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:  origin,
		Kind:    queue.KindIntegrityCheck,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: msg,
	})
}

type verifyAllMediaPayload struct {
	Cursor            int64                     `json:"cursor"`
	IntegrityChecked  int                       `json:"integrity_checked"`
	Verified          int                       `json:"verified"` // legacy dual-read
	Partial           int                       `json:"partial"`
	Skipped           int                       `json:"skipped"`
	Failed            int                       `json:"failed"`
	SkippedBusy       int                       `json:"skipped_busy"`
	SkippedProfileOff int                       `json:"skipped_profile_off"`
	SkippedNoMedia    int                       `json:"skipped_no_media"`
	SeriesIDs         []int64                   `json:"series_ids"`
	VideoIDs          []int64                   `json:"video_ids"`
	SkippedBusyIDs    []int64                   `json:"skipped_busy_ids"`
	SkippedProfileIDs []int64                   `json:"skipped_profile_off_ids"`
	SkippedNoMediaIDs []int64                   `json:"skipped_no_media_ids"`
	Checks            map[string]map[string]int `json:"checks"`
}

// VerifyAllMediaFail is one failed video from VerifyAllMediaPass (for notify).
type VerifyAllMediaFail struct {
	VideoID     int64
	SeriesTitle string
	VideoTitle  string
	Detail      string
}

// VerifyAllMediaPass runs integrity check on packed downloaded/integrity_check_failed media
// with a cursor for resume. Skips videos whose quality profile has File integrity off.
// onFail is optional; called after MarkVerifyFailed for each failure.
func (s *Store) VerifyAllMediaPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64), onFail func(VerifyAllMediaFail)) (*VerifyAllMediaResult, error) {
	var p verifyAllMediaPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	res := &VerifyAllMediaResult{
		IntegrityChecked:  p.IntegrityChecked,
		Partial:           p.Partial,
		Skipped:           p.Skipped,
		Failed:            p.Failed,
		SkippedBusy:       p.SkippedBusy,
		SkippedProfileOff: p.SkippedProfileOff,
		SkippedNoMedia:    p.SkippedNoMedia,
		SkippedBusyIDs:    append([]int64(nil), p.SkippedBusyIDs...),
		SkippedProfileIDs: append([]int64(nil), p.SkippedProfileIDs...),
		SkippedNoMediaIDs: append([]int64(nil), p.SkippedNoMediaIDs...),
		Checks:            p.Checks,
	}
	if res.IntegrityChecked == 0 && p.Verified > 0 {
		res.IntegrityChecked = p.Verified
	}
	if res.Checks == nil {
		res.Checks = map[string]map[string]int{}
	}

	persist := func() error {
		m := map[string]any{
			"cursor":              p.Cursor,
			"integrity_checked":   res.IntegrityChecked,
			"partial":             res.Partial,
			"skipped":             res.Skipped,
			"failed":              res.Failed,
			"skipped_busy":        res.SkippedBusy,
			"skipped_profile_off": res.SkippedProfileOff,
			"skipped_no_media":    res.SkippedNoMedia,
			"checks":              res.Checks,
		}
		if len(p.SeriesIDs) > 0 {
			m["series_ids"] = p.SeriesIDs
		}
		if len(p.VideoIDs) > 0 {
			m["video_ids"] = p.VideoIDs
		}
		if len(res.SkippedBusyIDs) > 0 {
			m["skipped_busy_ids"] = res.SkippedBusyIDs
		}
		if len(res.SkippedProfileIDs) > 0 {
			m["skipped_profile_off_ids"] = res.SkippedProfileIDs
		}
		if len(res.SkippedNoMediaIDs) > 0 {
			m["skipped_no_media_ids"] = res.SkippedNoMediaIDs
		}
		return s.Queue.UpdatePayload(task.ID, m)
	}

	q := `
		SELECT v.id
		FROM videos v
		WHERE v.status IN ('downloaded', 'integrity_check_failed')
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
		return res, qerr
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return res, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}

	total := len(ids)
	for i, id := range ids {
		select {
		case <-ctx.Done():
			_ = persist()
			return res, ctx.Err()
		default:
		}
		p.Cursor = id
		_ = persist()
		if progress != nil && total > 0 {
			pct := float64(i) / float64(total)
			progress(fmt.Sprintf("Integrity check %d/%d…", i+1, total), &pct)
		}

		busy, berr := s.videoBusyForRename(id, task.ID)
		if berr != nil {
			res.Failed++
			_ = persist()
			continue
		}
		if busy {
			res.Skipped++
			res.SkippedBusy++
			res.SkippedBusyIDs = appendCappedID(res.SkippedBusyIDs, id)
			_ = persist()
			continue
		}

		profileOn, perr := s.seriesProfileVerifyMedia(id)
		if perr != nil {
			res.Failed++
			_ = persist()
			continue
		}
		if !profileOn {
			res.Skipped++
			res.SkippedProfileOff++
			res.SkippedProfileIDs = appendCappedID(res.SkippedProfileIDs, id)
			_ = persist()
			continue
		}

		path, ok, herr := s.HasVideoFile(id)
		if herr != nil {
			res.Failed++
			_ = persist()
			continue
		}
		if !ok || path == "" {
			res.Skipped++
			res.SkippedNoMedia++
			res.SkippedNoMediaIDs = appendCappedID(res.SkippedNoMediaIDs, id)
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
		report, verr := s.RunIntegrityCheckVideo(ctx, id, perProgress, IntegrityCheckOpts{
			TaskID: task.ID,
		})
		if verr != nil {
			if ctx.Err() != nil {
				_ = persist()
				return res, ctx.Err()
			}
			res.Failed++
			bumpCheckAgg(res.Checks, report)
			_ = s.MarkVerifyFailed(id, task.ID, "Integrity check failed", report)
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
		if merr := s.MarkVerified(id, task.ID, report); merr != nil {
			res.Failed++
			_ = persist()
			continue
		}
		bumpCheckAgg(res.Checks, report)
		if report != nil && report.Outcome == IntegrityOutcomePartial {
			res.Partial++
		} else {
			res.IntegrityChecked++
		}
		_ = persist()
	}
	res.FinalizeOutcome()
	return res, nil
}

// VerifyAllMediaMessage formats the finish message for an integrity check batch.
func VerifyAllMediaMessage(checked, partial, skipped, failed int) string {
	return fmt.Sprintf("Integrity checked %d, partial %d, skipped %d, failed %d", checked, partial, skipped, failed)
}
