package queue

import (
	"database/sql"
	"sync"
)

// LiveState holds the latest message and progress fraction for running tasks.
// Not persisted; cleared when the task finishes or is cancelled (same lifetime as TaskLogs).
type LiveState struct {
	mu   sync.Mutex
	byID map[int64]liveEntry
}

type liveEntry struct {
	message  string
	progress *float64 // nil = message-only / indeterminate spinner
}

func newLiveState() *LiveState {
	return &LiveState{byID: make(map[int64]liveEntry)}
}

// Set stores the latest live message and optional 0..1 progress for task id.
func (l *LiveState) Set(id int64, message string, progress *float64) {
	if l == nil || id <= 0 {
		return
	}
	var pct *float64
	if progress != nil {
		v := *progress
		pct = &v
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byID[id] = liveEntry{message: message, progress: pct}
}

// Get returns the live snapshot for task id.
func (l *LiveState) Get(id int64) (message string, progress *float64, ok bool) {
	if l == nil || id <= 0 {
		return "", nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.byID[id]
	if !ok {
		return "", nil, false
	}
	if e.progress != nil {
		v := *e.progress
		return e.message, &v, true
	}
	return e.message, nil, true
}

// Clear drops live state for task id.
func (l *LiveState) Clear(id int64) {
	if l == nil || id <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byID, id)
}

// applyLive overlays in-memory message/progress onto a running task row.
func (s *Store) applyLive(t *Task) {
	if s == nil || t == nil || t.Status != StatusRunning {
		return
	}
	msg, pct, ok := s.Live.Get(t.ID)
	if !ok {
		return
	}
	t.Message = msg
	if pct == nil {
		t.Progress = sql.NullFloat64{}
	} else {
		t.Progress = sql.NullFloat64{Float64: *pct, Valid: true}
	}
}

// clearLive drops Logs, Live, and Commands buffers for a finished/cancelled task.
func (s *Store) clearLive(id int64) {
	if s == nil {
		return
	}
	s.Logs.Clear(id)
	s.Live.Clear(id)
	s.Commands.Clear(id)
}

// applyCommands overlays in-memory command lines onto a running task row.
func (s *Store) applyCommands(t *Task) {
	if s == nil || t == nil || t.Status != StatusRunning {
		return
	}
	if lines := s.Commands.Snapshot(t.ID); len(lines) > 0 {
		t.Commands = lines
	}
}
