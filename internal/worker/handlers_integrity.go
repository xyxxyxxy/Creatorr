package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func VerifyAllMediaHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "integrity check deps missing")
		}
		var recovered []notify.DigestItem
		res, err := d.Library.VerifyAllMediaPassExt(ctx, t, progress, func(f library.VerifyAllMediaFail) {
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, f.SeriesTitle, f.VideoTitle, f.Detail)
		}, func(f library.VerifyAllMediaSeriesMetaFail) {
			_ = notify.SeriesMetaVerifyFailed(ctx, d.Library.DB, t.ID, f.SeriesTitle, f.Kind, f.Detail)
		}, func(r library.VerifyAllMediaRecover) {
			recovered = append(recovered, notify.DigestItem{
				Series:  r.SeriesTitle,
				Title:   r.VideoTitle,
				VideoID: r.VideoID,
				Kind:    "recovered",
			})
		})
		if err != nil {
			return err
		}
		if res == nil {
			res = &library.VerifyAllMediaResult{}
		}
		if len(recovered) > 0 {
			_ = notify.IntegrityRecoveredDigest(ctx, d.Library.DB, t.ID, recovered)
		}
		msg := library.VerifyAllMediaMessage(res.IntegrityChecked, res.Partial, res.Skipped, res.Failed)
		progress(msg, ptrFloat(1))
		detail, _ := json.Marshal(res.DetailMap())
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}

// DeleteFilesHandler removes on-disk library files then MarkDeleted / DELETE series.

func MediaVerifyHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "integrity check deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity_check_initial missing video_id")
		}
		videoID := t.VideoID.Int64
		var payload struct {
			VideoID   int64  `json:"video_id"`
			MediaPath string `json:"media_path"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)

		path, ok, err := d.Library.HasVideoFile(videoID)
		if err != nil {
			return err
		}
		if !ok || path == "" {
			progress("Superseded (no media)", nil)
			return context.Canceled
		}
		if payload.MediaPath != "" && filepath.Clean(payload.MediaPath) != filepath.Clean(path) {
			progress("Superseded (media replaced)", nil)
			return context.Canceled
		}

		on, err := d.Library.SeriesProfileVerifyMedia(videoID)
		if err != nil {
			return err
		}
		if !on {
			progress("Skipped (File integrity off)", nil)
			return nil
		}

		wasFailed := false
		v, _ := d.Library.GetVideo(videoID)
		if v != nil && v.Status == "downloaded_integrity_failed" {
			wasFailed = true
		}

		report, err := d.Library.RunIntegrityCheckVideo(ctx, videoID, progress, library.IntegrityCheckOpts{
			TaskID: t.ID,
		})
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return context.Canceled
			}
			msg := err.Error()
			_ = d.Library.MarkVerifyFailed(videoID, t.ID, "Integrity check failed", report)
			if report != nil {
				_ = d.Library.Queue.MergeDetailJSON(t.ID, report.DetailMap())
			}
			seriesTitle := ""
			videoTitle := ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle, msg)
			return err
		}
		_ = d.Library.MarkVerified(videoID, t.ID, report)
		if report != nil {
			_ = d.Library.Queue.MergeDetailJSON(t.ID, report.DetailMap())
		}
		if wasFailed {
			seriesTitle, videoTitle := "", ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			_ = notify.IntegrityRecovered(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle)
		}
		return nil
	}
}

func FileHashCheckHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "file hash check deps missing")
		}
		var payload struct {
			VideoID  int64 `json:"video_id"`
			SeriesID int64 `json:"series_id"`
			FileID   int64 `json:"file_id"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		if payload.FileID <= 0 {
			return apperrors.New(apperrors.CodeIntegrityCheckFailed, "file_hash_check missing file_id")
		}
		f, gerr := d.Library.GetFile(payload.FileID)
		if gerr != nil {
			return apperrors.New(apperrors.CodeIntegrityCheckFailed, "file_hash_check file not found")
		}
		if f.IsSeriesMeta() {
			ok, detail, err := d.Library.RunSingleSeriesMetaFileIntegrityCheck(ctx, payload.FileID, progress)
			seriesTitle := ""
			if ser, serr := d.Library.GetSeries(f.SeriesID, false); serr == nil && ser != nil {
				seriesTitle = ser.Title
			}
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, context.Canceled) {
					return context.Canceled
				}
				_ = notify.SeriesMetaVerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, f.Kind, err.Error())
				return err
			}
			if !ok {
				msg := detail
				if msg == "" {
					msg = "Integrity check failed"
				}
				_ = notify.SeriesMetaVerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, f.Kind, msg)
				return apperrors.WithDetail(
					apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"),
					msg,
				)
			}
			stillFailed, ferr := d.Library.SeriesHasFailedIntegrityFile(f.SeriesID)
			if ferr == nil && !stillFailed {
				_ = notify.SeriesIntegrityRecovered(ctx, d.Library.DB, t.ID, seriesTitle)
			}
			progress("File hash ok", ptrFloat(1))
			return nil
		}
		videoID := payload.VideoID
		if videoID <= 0 && t.VideoID.Valid {
			videoID = t.VideoID.Int64
		}
		if videoID <= 0 && f.VideoID.Valid {
			videoID = f.VideoID.Int64
		}
		if videoID <= 0 {
			return apperrors.New(apperrors.CodeIntegrityCheckFailed, "file_hash_check missing video_id/file_id")
		}
		ok, detail, err := d.Library.RunSingleFileIntegrityCheck(ctx, videoID, payload.FileID, progress)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return context.Canceled
			}
			_ = d.Library.MarkVerifyFailed(videoID, t.ID, "Integrity check failed", nil)
			v, _ := d.Library.GetVideo(videoID)
			seriesTitle, videoTitle := "", ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle, err.Error())
			return err
		}
		if !ok {
			_ = d.Library.MarkVerifyFailed(videoID, t.ID, "Integrity check failed", nil)
			v, _ := d.Library.GetVideo(videoID)
			seriesTitle, videoTitle := "", ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			msg := detail
			if msg == "" {
				msg = "Integrity check failed"
			}
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle, msg)
			return apperrors.WithDetail(
				apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"),
				msg,
			)
		}
		recovered, aerr := d.Library.ApplyFileIntegrityVideoStatus(videoID, t.ID)
		if aerr != nil {
			return aerr
		}
		if recovered {
			v, _ := d.Library.GetVideo(videoID)
			seriesTitle, videoTitle := "", ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			_ = notify.IntegrityRecovered(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle)
		}
		progress("File hash ok", ptrFloat(1))
		return nil
	}
}

func ptrFloat(v float64) *float64 { return &v }
