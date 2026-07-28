package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// Heartbeat periodically updates worker_state so /api/health can probe the worker.
type Heartbeat struct {
	DB       *db.DB
	Interval time.Duration
	Log      *slog.Logger
}

// Run blocks until ctx is cancelled.
func (h *Heartbeat) Run(ctx context.Context) {
	if h.Interval <= 0 {
		h.Interval = 5 * time.Second
	}
	log := h.Log
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(h.Interval)
	defer t.Stop()
	h.touch(log)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.touch(log)
		}
	}
}

func (h *Heartbeat) touch(log *slog.Logger) {
	if err := h.DB.TouchWorkerHeartbeat(time.Now()); err != nil {
		log.Warn("worker heartbeat failed", "err", err)
	}
}
