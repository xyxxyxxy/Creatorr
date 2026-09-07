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
		res, err := d.Library.VerifyAllMediaPass(ctx, t, progress, func(f library.VerifyAllMediaFail) {
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, f.SeriesTitle, f.VideoTitle, f.Detail)
		})
		if err != nil {
			return err
		}
		if res == nil {
			res = &library.VerifyAllMediaResult{}
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
			v, _ := d.Library.GetVideo(videoID)
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
		return nil
	}
}

func ptrFloat(v float64) *float64 { return &v }
