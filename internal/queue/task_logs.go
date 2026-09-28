package queue

import (
	"strings"
	"sync"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

const taskLogCap = 200

// TaskLogs is an in-memory ring of progress() status lines per running task.
// Flushed to tasks.logs on failed Finish; cleared from memory on any finish/cancel.
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
// Password values in command-echo lines are redacted before storage.
func (l *TaskLogs) Append(id int64, line string) {
	if l == nil || id <= 0 {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	line = exectrace.RedactLine(line)
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
