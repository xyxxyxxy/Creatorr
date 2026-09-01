package queue

import (
	"strings"
	"sync"
)

const taskCommandsCap = 100

// TaskCommands is an in-memory list of shell-formatted external command lines per running task.
// Flushed to tasks.commands once on Finish/Cancel (kept for History); cleared from memory after.
type TaskCommands struct {
	mu   sync.Mutex
	byID map[int64][]string
}

func newTaskCommands() *TaskCommands {
	return &TaskCommands{byID: make(map[int64][]string)}
}

// Append adds a command line. Skips empty lines; no-op when at taskCommandsCap.
func (c *TaskCommands) Append(id int64, line string) {
	if c == nil || id <= 0 {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.byID[id]
	if len(buf) >= taskCommandsCap {
		return
	}
	c.byID[id] = append(buf, line)
}

// Snapshot returns a copy of buffered lines for task id (may be empty).
func (c *TaskCommands) Snapshot(id int64) []string {
	if c == nil || id <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.byID[id]
	if len(buf) == 0 {
		return nil
	}
	out := make([]string, len(buf))
	copy(out, buf)
	return out
}

// Clear drops the buffer for task id.
func (c *TaskCommands) Clear(id int64) {
	if c == nil || id <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byID, id)
}
