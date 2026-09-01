package worker

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// HeartbeatState is an in-process worker liveness clock for /api/health.
// Not persisted; restart starts empty until the first Touch.
type HeartbeatState struct {
	unixNano atomic.Int64
}

// Touch records the current time as the last heartbeat.
func (s *HeartbeatState) Touch() {
	if s == nil {
		return
	}
	s.unixNano.Store(time.Now().UnixNano())
}

// At returns the last heartbeat time, or zero if never touched.
func (s *HeartbeatState) At() time.Time {
	if s == nil {
		return time.Time{}
	}
	n := s.unixNano.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// Heartbeat periodically updates HeartbeatState so /api/health can probe the worker.
type Heartbeat struct {
	State    *HeartbeatState
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
	if h.State == nil {
		log.Error("worker heartbeat missing state")
		return
	}
	t := time.NewTicker(h.Interval)
	defer t.Stop()
	h.State.Touch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.State.Touch()
		}
	}
}
