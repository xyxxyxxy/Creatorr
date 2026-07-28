package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/events"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// Runner claims and executes tasks from the domain queue.
type Runner struct {
	Queue    *queue.Store
	Library  *library.Store
	Log      *slog.Logger
	Interval time.Duration
	Events   *events.Hub
	// Handlers map task kind -> executor. Missing kinds fail with unknown kind.
	Handlers map[string]TaskHandler

	digestOnce sync.Once
	digestBuf  *digestState
}

// TaskHandler runs one claimed task.
type TaskHandler func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	if r.Interval <= 0 {
		r.Interval = 500 * time.Millisecond
	}
	log := r.Log
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx, log)
		}
	}
}

func taskIDs(t *queue.Task) (seriesID, videoID int64) {
	if t.SeriesID.Valid {
		seriesID = t.SeriesID.Int64
	}
	if t.VideoID.Valid {
		videoID = t.VideoID.Int64
	}
	return seriesID, videoID
}

func (r *Runner) tick(ctx context.Context, log *slog.Logger) {
	// Interactive first - never waits behind ClaimNext work.
	if task, err := r.Queue.ClaimInteractive(); err != nil {
		log.Error("claim interactive", "err", err)
	} else if task != nil {
		go r.execute(ctx, log, task)
	}

	// Fill parallel slots in one tick when cooldown allows (esp. cooldown=0).
	for {
		task, err := r.Queue.ClaimNext()
		if err != nil {
			log.Error("claim task", "err", err)
			return
		}
		if task == nil {
			return
		}
		// System + hostname run concurrent: never block the claim loop on a long task.
		go r.execute(ctx, log, task)
	}
}

func (r *Runner) execute(ctx context.Context, log *slog.Logger, task *queue.Task) {
	log.Info("task started", "id", task.ID, "kind", task.Kind, "domain", task.Domain)
	sid, vid := taskIDs(task)
	r.Queue.Logs.Append(task.ID, "Running")
	r.Events.TaskUpdated(task.ID, task.Kind, task.Domain, queue.StatusRunning, "Running", sid, vid, nil)
	if snap, err := domains.SnapshotDomainAccess(r.Queue.DB, task.Domain); err != nil {
		log.Warn("domain-access snapshot", "task", task.ID, "err", err)
	} else if snap != nil {
		if err := r.Queue.MergeDetailJSON(task.ID, map[string]any{domains.DetailKeyDomainAccess: snap}); err != nil {
			log.Warn("domain-access persist", "task", task.ID, "err", err)
		}
	}

	handler := r.Handlers[task.Kind]
	progress := func(msg string, pct *float64) {
		if err := r.Queue.UpdateProgress(task.ID, msg, pct); err != nil {
			log.Warn("progress update", "task", task.ID, "err", err)
		} else {
			r.Queue.Logs.Append(task.ID, msg)
		}
		r.Events.TaskUpdated(task.ID, task.Kind, task.Domain, queue.StatusRunning, msg, sid, vid, pct)
	}

	taskCtx, cancel := context.WithCancel(ctx)
	taskCtx = exectrace.With(taskCtx, func(line string) {
		if err := r.Queue.AppendCommand(task.ID, line); err != nil {
			log.Warn("append command", "task", task.ID, "err", err)
		}
		r.Queue.Logs.Append(task.ID, "$ "+line)
	})
	persistPOT := func(st ytdlp.POTStatus) {
		if st.State == "" {
			return
		}
		if err := r.Queue.MergeDetailJSON(task.ID, map[string]any{ytdlp.DetailKeyPOToken: st}); err != nil {
			log.Warn("persist pot status", "task", task.ID, "err", err)
		}
	}
	taskCtx = ytdlp.ContextWithPOTTracker(taskCtx,
		func(detail string) {
			if err := notify.POTProvider(context.WithoutCancel(taskCtx), r.Queue.DB, task.ID, task.Domain, detail); err != nil {
				log.Warn("notify pot_provider", "task", task.ID, "err", err)
			}
		},
		persistPOT,
	)
	r.Queue.RegisterRunning(task.ID, cancel)
	defer func() {
		cancel()
		r.Queue.UnregisterRunning(task.ID)
		r.releaseFlareIfIdle(task.Domain)
	}()

	var runErr error
	if handler == nil {
		runErr = fmt.Errorf("%w: %s", errUnknownKind, task.Kind)
	} else {
		runErr = handler(taskCtx, task, progress)
	}
	if st := ytdlp.POTStatusFromContext(taskCtx); st.State != "" {
		persistPOT(st)
	}

	st, _ := r.Queue.TaskStatus(task.ID)
	cancelled := st == queue.StatusCancelled || errors.Is(runErr, context.Canceled)
	if cancelled {
		msg := "Cancelled"
		if t, err := r.Queue.GetTask(task.ID); err == nil && t != nil && strings.TrimSpace(t.Message) != "" {
			msg = t.Message
			task = t
		}
		if st != queue.StatusCancelled {
			_ = r.Queue.Finish(task.ID, queue.StatusCancelled, msg, "Cancelled", "")
		}
		if r.Library != nil {
			if err := r.Library.RecordTaskCancelled(task); err != nil {
				log.Warn("record cancelled history", "task", task.ID, "err", err)
			}
		}
		if r.Events != nil {
			r.Events.TaskFailed(task.ID, task.Kind, task.Domain, msg, "Cancelled", sid, vid)
		}
		if mediaKind(task.Kind) {
			r.maybeScheduleDigest(ctx, log)
		}
		log.Info("task cancelled", "id", task.ID, "kind", task.Kind)
		return
	}

	if runErr != nil {
		code, msg := classify(runErr)
		if code == apperrors.CodeLiveBroadcastSkipped &&
			task.Kind == queue.KindDownload &&
			task.VideoID.Valid && r.Library != nil {
			doneMsg := "Skipped (currently live)"
			_ = r.Queue.Finish(task.ID, queue.StatusDone, doneMsg, code, runErr.Error())
			_ = r.Queue.SetDetail(task.ID, runErr.Error())
			if err := r.Library.RecordLiveBroadcastSkipped(task.VideoID.Int64, task.ID); err != nil {
				log.Warn("record live_skipped", "video", task.VideoID.Int64, "err", err)
			}
			seriesTitle, videoTitle := "", ""
			if v, err := r.Library.GetVideo(task.VideoID.Int64); err == nil && v != nil {
				videoTitle = v.Title
				if ser, err := r.Library.GetSeries(v.SeriesID, false); err == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			if err := notify.LiveSkipped(ctx, r.Queue.DB, task.ID, seriesTitle, videoTitle); err != nil {
				log.Warn("live_skipped notify", "task", task.ID, "err", err)
			}
			r.Events.TaskDone(task.ID, task.Kind, task.Domain, doneMsg, sid, vid)
			if mediaKind(task.Kind) {
				r.maybeScheduleDigest(ctx, log)
			}
			log.Info("task done (live broadcast skipped)", "id", task.ID, "kind", task.Kind)
			return
		}
		if code == apperrors.CodeMediaTypeExcluded && task.Kind == queue.KindDownload && task.VideoID.Valid && r.Library != nil {
			doneMsg := "Ignored (excluded media type)"
			_ = r.Queue.Finish(task.ID, queue.StatusDone, doneMsg, code, runErr.Error())
			_ = r.Queue.SetDetail(task.ID, runErr.Error())
			if err := r.Library.MarkIgnoredMediaType(task.VideoID.Int64, task.ID, ""); err != nil {
				log.Warn("mark ignored media_type", "video", task.VideoID.Int64, "err", err)
			}
			r.Events.TaskDone(task.ID, task.Kind, task.Domain, doneMsg, sid, vid)
			if mediaKind(task.Kind) {
				r.maybeScheduleDigest(ctx, log)
			}
			log.Info("task done (media type excluded)", "id", task.ID, "kind", task.Kind)
			return
		}
		_ = r.Queue.Finish(task.ID, queue.StatusFailed, msg, code, runErr.Error())
		_ = r.Queue.SetDetail(task.ID, runErr.Error())
		r.Events.TaskFailed(task.ID, task.Kind, task.Domain, msg, code, sid, vid)
		if task.Kind == queue.KindDownload && task.VideoID.Valid && r.Library != nil {
			if err := r.Library.MarkDownloadFailed(task.VideoID.Int64, task.ID, code, msg); err != nil {
				log.Warn("mark wanted_download_error", "video", task.VideoID.Int64, "err", err)
			}
		} else {
			r.maybeHoldSourceOnYtDlp(log, task, code)
		}
		r.maybeNotifyFailure(ctx, log, task, code, runErr)
		if mediaKind(task.Kind) {
			r.maybeScheduleDigest(ctx, log)
		}
		log.Warn("task failed", "id", task.ID, "kind", task.Kind, "err", runErr)
		return
	}

	doneMsg := "Done"
	if t, err := r.Queue.GetTask(task.ID); err == nil && t != nil && strings.TrimSpace(t.Message) != "" && t.Message != "Running" {
		doneMsg = t.Message
	}
	_ = r.Queue.Finish(task.ID, queue.StatusDone, doneMsg, "", "")
	r.Events.TaskDone(task.ID, task.Kind, task.Domain, doneMsg, sid, vid)
	if mediaKind(task.Kind) {
		r.noteDigestSuccess(task)
		r.maybeScheduleDigest(ctx, log)
	}
	log.Info("task done", "id", task.ID, "kind", task.Kind)
}

func (r *Runner) maybeHoldSourceOnYtDlp(log *slog.Logger, task *queue.Task, code string) {
	if r.Library == nil || !apperrors.IsYtDlpPauseCode(code) {
		return
	}
	srcID := queue.SourceIDFromPayload(task.Payload)
	if srcID <= 0 && task.VideoID.Valid {
		if v, err := r.Library.GetVideo(task.VideoID.Int64); err == nil && v != nil && v.SourceID.Valid {
			srcID = v.SourceID.Int64
		}
	}
	if srcID <= 0 {
		return
	}
	if err := r.Library.HoldSourceOnYtDlpError(srcID, task.ID); err != nil {
		log.Warn("hold source on yt-dlp error", "source", srcID, "err", err)
	}
}

func (r *Runner) maybeNotifyFailure(ctx context.Context, log *slog.Logger, task *queue.Task, code string, runErr error) {
	if task.Domain == "" || task.Domain == "unknown" || task.Domain == "system" {
		return
	}
	switch code {
	case apperrors.CodeCookieInvalid, apperrors.CodeRateLimited,
		apperrors.CodeRemuxFailed, apperrors.CodePackFailed, apperrors.CodeMediaVerifyFailed,
		apperrors.CodeMediaTypeExcluded, apperrors.CodeLiveBroadcastSkipped:
		// keep classified code (do not re-detect remux/pack/verify into pause)
	default:
		if d := apperrors.DetectPauseCode(runErr.Error()); d != "" {
			code = d
		}
	}
	notify.SoftPauseAndAlert(ctx, r.Queue.DB, log, task.ID, task.Domain, code, runErr.Error())
}

func (r *Runner) releaseFlareIfIdle(domain string) {
	if r == nil || r.Queue == nil {
		return
	}
	domain = strings.TrimSpace(domain)
	if domain == "" || domain == "unknown" || domain == queue.SystemDomain {
		return
	}
	busy, err := r.Queue.HasPendingOrRunningDomain(domain)
	if err != nil || busy {
		return
	}
	ytdlp.ReleaseFlareSession(context.Background(), domain)
}

var errUnknownKind = errors.New("unknown task kind")

func classify(err error) (code, message string) {
	var ae *apperrors.AppError
	if errors.As(err, &ae) && ae != nil {
		code = apperrors.UpgradeCode(ae.Code, ae.Error())
		if code == apperrors.CodeCookieInvalid || code == apperrors.CodeRateLimited {
			return code, apperrors.PauseMessage(code)
		}
		return code, ae.Message
	}
	if errors.Is(err, errUnknownKind) {
		return apperrors.CodeInternal, "Unknown task kind"
	}
	if d := apperrors.DetectPauseCode(err.Error()); d != "" {
		return d, apperrors.PauseMessage(d)
	}
	return apperrors.CodeInternal, "Task failed"
}

// StubHandlers returns no-op success handlers for maintenance kinds and tests.
func StubHandlers() map[string]TaskHandler {
	stub := func(kind string) TaskHandler {
		return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
			progress(fmt.Sprintf("stub %s", kind), nil)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
				return nil
			}
		}
	}
	return map[string]TaskHandler{
		queue.KindScan:               stub(queue.KindScan),
		queue.KindDownload:           stub(queue.KindDownload),
		queue.KindRescanMetadata:     stub(queue.KindRescanMetadata),
		queue.KindRefreshSidecars:    stub(queue.KindRefreshSidecars),
		queue.KindImport:             stub(queue.KindImport),
		queue.KindPrefetchSeriesMeta: stub(queue.KindPrefetchSeriesMeta),
		queue.KindPrefetchVideoMeta:  stub(queue.KindPrefetchVideoMeta),
		queue.KindPrefetchAddSeries:  stub(queue.KindPrefetchAddSeries),
		queue.KindPrefetchAddVideo:   stub(queue.KindPrefetchAddVideo),
		queue.KindSyncFiles:          stub(queue.KindSyncFiles),
		queue.KindRetentionDelete:    stub(queue.KindRetentionDelete),
		queue.KindRenameEpisodes:     stub(queue.KindRenameEpisodes),
		queue.KindRegenerateNFO:      stub(queue.KindRegenerateNFO),
		queue.KindDeleteFiles:        stub(queue.KindDeleteFiles),
		queue.KindSponsorblockCut:    stub(queue.KindSponsorblockCut),
		queue.KindMediaVerify:        stub(queue.KindMediaVerify),
	}
}
