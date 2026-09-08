package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// BulkEditSeriesHandler applies bulk series settings/metadata updates.
func BulkEditSeriesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "bulk edit series deps missing")
		}
		updated, skipped, failed, err := d.Library.BulkEditSeriesPass(ctx, t, progress)
		if err != nil {
			return err
		}
		msg := library.BulkEditSeriesMessage(updated, skipped, failed)
		progress(msg, ptrFloat(1))
		detail, _ := json.Marshal(map[string]any{
			"updated": updated, "skipped": skipped, "failed": failed,
		})
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}

// BulkEditVideosHandler applies bulk video catalog metadata updates.
func BulkEditVideosHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "bulk edit videos deps missing")
		}
		updated, skipped, failed, err := d.Library.BulkEditVideosPass(ctx, t, progress)
		if err != nil {
			return err
		}
		msg := library.BulkEditSeriesMessage(updated, skipped, failed)
		progress(msg, ptrFloat(1))
		detail, _ := json.Marshal(map[string]any{
			"updated": updated, "skipped": skipped, "failed": failed,
		})
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}

// RegenerateNFOHandler rewrites episode/series NFOs (resumable).
func RegenerateNFOHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		rewrote, skipped, failed, err := d.Library.NFORegeneratePass(ctx, t, progress)
		if err != nil {
			return err
		}
		d.Library.RecordNFORegenerateActivity(t.ID, rewrote, skipped, failed)
		return nil
	}
}

// VerifyAllMediaHandler null-decodes all packed downloaded/integrity_check_failed media (resumable).
func DeleteFilesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if err := d.Library.FileDeletePass(ctx, t, progress); err != nil {
			return err
		}
		d.Library.RecordFileDeleteActivity(t)
		return nil
	}
}

// YtDlpUpdateHandler fetches and installs yt-dlp from GitHub when newer.
func YtDlpUpdateHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.YtDlp == nil || d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "yt-dlp update deps missing")
		}
		channel, err := settings.Get(d.Library.DB, settings.KeyYtDlpUpdateChannel)
		if err != nil {
			return err
		}
		channel = settings.NormalizeYtDlpUpdateChannel(channel)
		origin := strings.TrimSpace(t.Origin)
		if origin == "" {
			origin = queue.OriginManual
		}
		managed := d.YtDlp.BinPath()
		res, err := ytdlp.Update(ctx, ytdlp.UpdateOpts{
			ManagedPath: managed,
			Channel:     channel,
			Progress:    func(msg string) { progress(msg, nil) },
		})
		if err != nil {
			return err
		}
		var detail map[string]any
		if res.Skipped {
			detail = map[string]any{
				"version": res.ToVersion,
				"channel": res.Channel,
				"origin":  origin,
				"skipped": true,
			}
			_ = d.Library.Queue.MergeDetailJSON(t.ID, detail)
			return nil
		}
		detail = map[string]any{
			"from":    res.FromVersion,
			"to":      res.ToVersion,
			"channel": res.Channel,
			"origin":  origin,
		}
		d.YtDlp.SetBin(managed)
		if err := settings.RecordYtDlpInstall(d.Library.DB, res.ToVersion); err != nil {
			return err
		}
		progress("Updated to "+res.ToVersion, nil)
		_ = d.Library.Queue.MergeDetailJSON(t.ID, detail)
		return nil
	}
}

// SyncFilesHandler runs FileSyncPass on the system lane.
func SyncFilesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "sync files deps missing")
		}
		res, err := d.Library.FileSyncPass(t.ID, progress)
		if err != nil {
			return err
		}
		if len(res.MissingIDs) == 0 && len(res.ExternallyChangedIDs) == 0 &&
			len(res.SidecarMissing) == 0 && len(res.SidecarChanged) == 0 {
			return nil
		}
		missing := fileSyncIssueItems(d.Library, res.MissingIDs)
		missing = append(missing, fileSyncSidecarIssueItems(d.Library, res.SidecarMissing)...)
		changed := fileSyncIssueItems(d.Library, res.ExternallyChangedIDs)
		changed = append(changed, fileSyncSidecarIssueItems(d.Library, res.SidecarChanged)...)
		_ = notify.FileSyncIssues(ctx, d.Library.DB, t.ID, missing, changed)
		return nil
	}
}

func fileSyncIssueItems(lib *library.Store, ids []int64) []notify.FileSyncIssueItem {
	out := make([]notify.FileSyncIssueItem, 0, len(ids))
	for _, id := range ids {
		it := notify.FileSyncIssueItem{}
		v, err := lib.GetVideo(id)
		if err == nil && v != nil {
			it.Title = v.Title
			if ser, serr := lib.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
				it.Series = ser.Title
			}
		}
		out = append(out, it)
	}
	return out
}

func fileSyncSidecarIssueItems(lib *library.Store, issues []library.FileSyncSidecarIssue) []notify.FileSyncIssueItem {
	out := make([]notify.FileSyncIssueItem, 0, len(issues))
	for _, si := range issues {
		it := notify.FileSyncIssueItem{Detail: sidecarIssueDetail(si.Kind, si.Path)}
		v, err := lib.GetVideo(si.VideoID)
		if err == nil && v != nil {
			it.Title = v.Title
			if ser, serr := lib.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
				it.Series = ser.Title
			}
		}
		out = append(out, it)
	}
	return out
}

func sidecarIssueDetail(kind, path string) string {
	kind = strings.TrimSpace(kind)
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return kind
	}
	if kind == "" {
		return base
	}
	return kind + ": " + base
}

// RetentionDeleteHandler runs RetentionPurgePass on the system lane.
func RetentionDeleteHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		_ = ctx
		_, err := d.Library.RetentionPurgePass(t.ID, progress)
		return err
	}
}

// RenameEpisodesHandler renames packed episode files to the snapshot formats.
func RenameEpisodesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		renamed, skipped, failed, err := d.Library.ApplyEpisodeNamingPass(ctx, t, progress)
		if err != nil {
			return err
		}
		msg := library.ApplyNamingMessage(renamed, skipped, failed)
		progress(msg, ptrFloat(1))
		detail, _ := json.Marshal(map[string]any{
			"renamed": renamed, "skipped_busy": skipped, "failed": failed,
		})
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}

// ImportHandler installs a file from the import inbox into the series library folder,
