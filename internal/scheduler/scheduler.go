package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Scheduler turns implicit settings cron schedules into tasks / maintenance passes.
type Scheduler struct {
	Library *library.Store
	Log     *slog.Logger
	// Tick is how often to re-read settings and check due work (default 30s).
	Tick time.Duration
	Now  func() time.Time

	// startedAt is set by Run. Scan catch-up uses it so fires missed while the
	// process was down wait for the next cron after boot (not immediate).
	startedAt           time.Time
	lastDownload        time.Time
	lastSyncFiles       time.Time
	lastRetentionDelete time.Time
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Tick <= 0 {
		s.Tick = 30 * time.Second
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	// Anchor global crons to boot: zero last would mean "due now" (missed catch-up).
	now := s.Now().UTC()
	s.startedAt = now
	s.lastDownload = now
	s.lastSyncFiles = now
	s.lastRetentionDelete = now
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	s.TickOnce(ctx, log)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.TickOnce(ctx, log)
		}
	}
}

// TickOnce evaluates cron schedules and runs due work.
func (s *Scheduler) TickOnce(ctx context.Context, log *slog.Logger) {
	if s.Library == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := s.Now().UTC()
	database := s.Library.DB

	n, err := s.Library.EnqueueScansDue(now, s.startedAt)
	if err != nil {
		log.Error("schedule scan", "err", err)
	} else if n > 0 {
		log.Info("scheduled scan", "enqueued", n)
	}

	if cronDue(database, settings.KeyDownloadWantedCron, s.lastDownload, now, log, "download wanted") {
		n, err := s.Library.EnqueueDownloadWanted()
		if err != nil {
			log.Error("schedule download wanted", "err", err)
		} else {
			if n > 0 {
				log.Info("scheduled download wanted", "enqueued", n)
			}
			s.lastDownload = now
		}
		// Same cron: pack .strm for stream-mode series wanted videos.
		if pn, err := s.Library.EnqueuePackStreamWanted(); err != nil {
			log.Error("schedule pack stream wanted", "err", err)
		} else if pn > 0 {
			log.Info("scheduled pack stream wanted", "enqueued", pn)
		}
		if mn, sn, err := s.Library.EnqueueMaturityDue(); err != nil {
			log.Error("schedule maturity", "err", err)
		} else if mn > 0 || sn > 0 {
			log.Info("scheduled maturity", "media", mn, "sidecars", sn)
		}
	}

	if cronDue(database, settings.KeySyncFilesCron, s.lastSyncFiles, now, log, "file sync") {
		id, err := s.Library.EnqueueSyncFiles(queue.PrioritySyncFilesDue)
		if err != nil {
			log.Debug("schedule file sync enqueue", "err", err)
		} else if id > 0 {
			log.Info("scheduled file sync", "task", id)
		}
		s.lastSyncFiles = now
	}

	if cronDue(database, settings.KeyRetentionDeleteCron, s.lastRetentionDelete, now, log, "retention purge") {
		id, err := s.Library.EnqueueRetentionDelete(queue.PriorityRetentionDeleteDue)
		if err != nil {
			log.Debug("schedule retention purge enqueue", "err", err)
		} else if id > 0 {
			log.Info("scheduled retention purge", "task", id)
		}
		s.lastRetentionDelete = now
	}
}

func cronDue(database *db.DB, key string, last, now time.Time, log *slog.Logger, label string) bool {
	expr, _ := settings.Get(database, key)
	ok, err := cronexpr.Due(expr, last, now)
	if err != nil {
		log.Error(label+" cron", "err", err)
		return false
	}
	return ok
}
