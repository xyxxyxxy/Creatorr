package queue

import (
	"strings"
	"sync"
)

const taskLogCap = 200

// TaskLogs is an in-memory ring of progress() status lines per running task.
// Not persisted; cleared when the task finishes or is cancelled.
type TaskLogs struct {
	mu   sync.Mutex
	byID map[int64]*taskLogBuf
}

type taskLogBuf struct {
	lines []string
}

func newTaskLogs() *TaskLogs {
	return &TaskLogs{byID: make(map[int64]*taskLogBuf)}
}

// Append adds a progress line for task id. Skips empty and consecutive duplicates.
func (l *TaskLogs) Append(id int64, line string) {
	if l == nil || id <= 0 {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	buf := l.byID[id]
	if buf == nil {
		buf = &taskLogBuf{}
		l.byID[id] = buf
	}
	if n := len(buf.lines); n > 0 && buf.lines[n-1] == line {
		return
	}
	buf.lines = append(buf.lines, line)
	if len(buf.lines) > taskLogCap {
		buf.lines = append([]string(nil), buf.lines[len(buf.lines)-taskLogCap:]...)
	}
}

// Snapshot returns a copy of current lines for task id (may be empty).
func (l *TaskLogs) Snapshot(id int64) []string {
	if l == nil || id <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	buf := l.byID[id]
	if buf == nil || len(buf.lines) == 0 {
		return nil
	}
	out := make([]string, len(buf.lines))
	copy(out, buf.lines)
	return out
}

// Clear drops the buffer for task id.
func (l *TaskLogs) Clear(id int64) {
	if l == nil || id <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byID, id)
}
