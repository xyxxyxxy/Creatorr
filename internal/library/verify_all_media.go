package library

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"

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
	SeriesMetaDone    []int64                   `json:"series_meta_done"` // series IDs whose art was checked once
	Checks            map[string]map[string]int `json:"checks"`
}

// VerifyAllMediaFail is one failed video from VerifyAllMediaPass (for notify).
type VerifyAllMediaFail = integrity.VerifyAllMediaFail

// VerifyAllMediaRecover is one video restored from downloaded_integrity_failed.
type VerifyAllMediaRecover struct {
	VideoID     int64
	SeriesTitle string
	VideoTitle  string
}

// VerifyAllMediaSeriesMetaFail is one series-art failure from the bulk pass.
type VerifyAllMediaSeriesMetaFail = SeriesMetaIntegrityFail

// VerifyAllMediaPass runs integrity check on packed downloaded/downloaded_integrity_failed media
// with a cursor for resume. Skips videos whose quality profile has File integrity off.
// Series art is checked once per series in scope (skips tvshow.nfo).
// onFail is optional; called after MarkVerifyFailed for each failure.
// onRecover is optional; called after MarkVerified when status was downloaded_integrity_failed.
// onSeriesMetaFail is optional; called for each failed series-meta art file.
func (s *Store) VerifyAllMediaPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64), onFail func(VerifyAllMediaFail), onRecover ...func(VerifyAllMediaRecover)) (*VerifyAllMediaResult, error) {
	return s.VerifyAllMediaPassExt(ctx, task, progress, onFail, nil, onRecover...)
}

// VerifyAllMediaPassExt is VerifyAllMediaPass with an optional series-meta fail callback.
func (s *Store) VerifyAllMediaPassExt(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64), onFail func(VerifyAllMediaFail), onSeriesMetaFail func(VerifyAllMediaSeriesMetaFail), onRecover ...func(VerifyAllMediaRecover)) (*VerifyAllMediaResult, error) {
	var recoverFn func(VerifyAllMediaRecover)
	if len(onRecover) > 0 {
		recoverFn = onRecover[0]
	}
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
		if len(p.SeriesMetaDone) > 0 {
			m["series_meta_done"] = p.SeriesMetaDone
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

	// Cumulative across resumes (payload counters), not this-run-only.
	reportLive := func() {
		if progress == nil {
			return
		}
		done := res.IntegrityChecked + res.Partial + res.Skipped + res.Failed
		pct := float64(done%1000) / 1000.0
		progress(fmt.Sprintf("Integrity check %d…", done), &pct)
	}
	if res.IntegrityChecked+res.Partial+res.Skipped+res.Failed > 0 {
		reportLive()
	}

	// Series art once per series in scope (before video loop; resume via series_meta_done).
	metaIDs, merr := s.seriesIDsForIntegrityMeta(p.SeriesIDs, p.VideoIDs)
	if merr != nil {
		return res, merr
	}
	doneMeta := map[int64]struct{}{}
	for _, id := range p.SeriesMetaDone {
		doneMeta[id] = struct{}{}
	}
	for _, sid := range metaIDs {
		if _, ok := doneMeta[sid]; ok {
			continue
		}
		select {
		case <-ctx.Done():
			_ = persist()
			return res, ctx.Err()
		default:
		}
		fails, cerr := s.RunSeriesMetaIntegrityCheck(ctx, sid, progress)
		if cerr != nil {
			if ctx.Err() != nil {
				_ = persist()
				return res, ctx.Err()
			}
			res.Failed++
		}
		for _, f := range fails {
			res.Failed++
			if onSeriesMetaFail != nil {
				onSeriesMetaFail(f)
			}
		}
		p.SeriesMetaDone = append(p.SeriesMetaDone, sid)
		doneMeta[sid] = struct{}{}
		_ = persist()
		reportLive()
	}

	const batchSize = 50
	const persistEvery = 10
	for {
		select {
		case <-ctx.Done():
			_ = persist()
			return res, ctx.Err()
		default:
		}

		q := `
		SELECT v.id
		FROM videos v
		WHERE v.status IN ('downloaded', 'downloaded_integrity_failed')
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
		q += ` ORDER BY v.id ASC LIMIT ?`
		args = append(args, batchSize)

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
		if len(ids) == 0 {
			break
		}

		for i, id := range ids {
			select {
			case <-ctx.Done():
				_ = persist()
				return res, ctx.Err()
			default:
			}
			dirty := false

			busy, berr := s.videoBusyForRename(id, task.ID)
			if berr != nil {
				res.Failed++
				dirty = true
			} else if busy {
				res.Skipped++
				res.SkippedBusy++
				res.SkippedBusyIDs = appendCappedID(res.SkippedBusyIDs, id)
				dirty = true
			} else {
				profileOn, perr := s.seriesProfileVerifyMedia(id)
				if perr != nil {
					res.Failed++
					dirty = true
				} else if !profileOn {
					res.Skipped++
					res.SkippedProfileOff++
					res.SkippedProfileIDs = appendCappedID(res.SkippedProfileIDs, id)
					dirty = true
				} else {
					path, ok, herr := s.HasVideoFile(id)
					if herr != nil {
						res.Failed++
						dirty = true
					} else if !ok || path == "" {
						res.Skipped++
						res.SkippedNoMedia++
						res.SkippedNoMediaIDs = appendCappedID(res.SkippedNoMediaIDs, id)
						dirty = true
					} else {
						perProgress := func(msg string, pct *float64) {
							if progress == nil {
								return
							}
							base := float64(i) / float64(len(ids)+1)
							span := 1.0 / float64(len(ids)+1)
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
								// Leave cursor before this id so resume retries the interrupted video.
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
							dirty = true
						} else {
							wasFailed := false
							seriesTitle, videoTitle := "", ""
							if v, gerr := s.GetVideo(id); gerr == nil && v != nil {
								wasFailed = v.Status == "downloaded_integrity_failed"
								videoTitle = v.Title
								if ser, serr := s.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
									seriesTitle = ser.Title
								}
							}
							if merr := s.MarkVerified(id, task.ID, report); merr != nil {
								res.Failed++
								dirty = true
							} else {
								bumpCheckAgg(res.Checks, report)
								if report != nil && report.Outcome == IntegrityOutcomePartial {
									res.Partial++
								} else {
									res.IntegrityChecked++
								}
								if wasFailed && recoverFn != nil {
									recoverFn(VerifyAllMediaRecover{
										VideoID: id, SeriesTitle: seriesTitle, VideoTitle: videoTitle,
									})
								}
								dirty = true
							}
						}
					}
				}
			}
			// Advance cursor only after this id is fully handled (skip/ok/fail).
			p.Cursor = id
			reportLive()
			done := res.IntegrityChecked + res.Partial + res.Skipped + res.Failed
			if dirty && (done%persistEvery == 0 || i == len(ids)-1) {
				_ = persist()
			}
		}
		if len(ids) < batchSize {
			break
		}
	}
	_ = persist()
	res.FinalizeOutcome()
	return res, nil
}

// seriesIDsForIntegrityMeta lists series whose art should be checked once in this pass.
// Empty series+video scope = all series; video-only scope = distinct series of those videos.
func (s *Store) seriesIDsForIntegrityMeta(seriesIDs, videoIDs []int64) ([]int64, error) {
	seriesIDs = uniqInt64(seriesIDs)
	videoIDs = uniqInt64(videoIDs)
	if len(seriesIDs) > 0 {
		return seriesIDs, nil
	}
	if len(videoIDs) > 0 {
		args := make([]any, len(videoIDs))
		for i, id := range videoIDs {
			args[i] = id
		}
		rows, err := s.DB.SQL.Query(`
			SELECT DISTINCT series_id FROM videos
			WHERE id IN (`+sqlIntPlaceholders(len(args))+`)
			ORDER BY series_id
		`, args...)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		var out []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
	rows, err := s.DB.SQL.Query(`SELECT id FROM series ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// VerifyAllMediaMessage formats the finish message for an integrity check batch.
func VerifyAllMediaMessage(checked, partial, skipped, failed int) string {
	return integrity.VerifyAllMediaMessage(checked, partial, skipped, failed)
}
