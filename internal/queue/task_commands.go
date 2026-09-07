package queue

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

// Head/tail unique command templates kept in memory (and usually persisted).
const (
	taskCommandsHead = 10
	taskCommandsTail = 10
	// taskCommandsFfprobeMax is max distinct ffprobe templates per task.
	taskCommandsFfprobeMax = 3
)

// TaskCommands is an in-memory list of shell-formatted external command lines per running task.
// Lines are template-deduped (path args → $PATH); slots use head+tail. Flushed to tasks.commands
// on Finish/Cancel when policy allows; cleared from memory after.
type TaskCommands struct {
	mu   sync.Mutex
	byID map[int64]*taskCmdBuf
}

type taskCmdEntry struct {
	Line  string // first sample (real paths)
	Key   string // fingerprint
	Class string // ytdlp | ffmpeg | ffprobe | other
	Count int
}

type taskCmdBuf struct {
	entries []taskCmdEntry
	omitted int // unique templates dropped (middle eviction or ffprobe overflow)
}

func newTaskCommands() *TaskCommands {
	return &TaskCommands{byID: make(map[int64]*taskCmdBuf)}
}

// Append records bin+line with template dedupe and head/tail / ffprobe caps.
func (c *TaskCommands) Append(id int64, bin, line string) {
	if c == nil || id <= 0 {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	class := commandBinaryClass(bin)
	key := exectrace.Fingerprint(line)
	if key == "" {
		key = line
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.byID[id]
	if buf == nil {
		buf = &taskCmdBuf{}
		c.byID[id] = buf
	}
	for i := range buf.entries {
		if buf.entries[i].Key == key {
			buf.entries[i].Count++
			return
		}
	}
	if class == "ffprobe" && countClass(buf.entries, "ffprobe") >= taskCommandsFfprobeMax {
		buf.omitted++
		return
	}
	capN := taskCommandsHead + taskCommandsTail
	if len(buf.entries) >= capN {
		// Evict oldest entry past the head window (keep head+tail).
		buf.omitted++
		buf.entries = append(buf.entries[:taskCommandsHead], buf.entries[taskCommandsHead+1:]...)
	}
	buf.entries = append(buf.entries, taskCmdEntry{
		Line: line, Key: key, Class: class, Count: 1,
	})
}

func countClass(entries []taskCmdEntry, class string) int {
	n := 0
	for _, e := range entries {
		if e.Class == class {
			n++
		}
	}
	return n
}

func commandBinaryClass(bin string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(bin)))
	base = strings.TrimSuffix(base, ".exe")
	switch {
	case strings.Contains(base, "yt-dlp") || base == "ytdlp":
		return "ytdlp"
	case base == "ffprobe" || strings.HasPrefix(base, "ffprobe"):
		return "ffprobe"
	case base == "ffmpeg" || strings.HasPrefix(base, "ffmpeg"):
		return "ffmpeg"
	default:
		return "other"
	}
}

// Snapshot returns display lines (sample + ×N similar) plus an omitted marker when needed.
func (c *TaskCommands) Snapshot(id int64) []string {
	if c == nil || id <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.byID[id]
	if buf == nil || len(buf.entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(buf.entries)+1)
	for _, e := range buf.entries {
		out = append(out, formatCmdEntry(e))
	}
	if buf.omitted > 0 {
		out = append(out, fmt.Sprintf("… %d similar command templates omitted …", buf.omitted))
	}
	return out
}

func formatCmdEntry(e taskCmdEntry) string {
	if e.Count <= 1 {
		return e.Line
	}
	return fmt.Sprintf("%s  (×%d similar)", e.Line, e.Count)
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

// PersistCommandsOnStatus reports whether buffered commands should be written to SQLite
// for this task kind and finish status. Noisy system kinds keep commands only on failure.
func PersistCommandsOnStatus(kind, status string) bool {
	if status == StatusFailed {
		return true
	}
	switch kind {
	case KindRenameEpisodes, KindSyncFiles, KindIntegrityCheck, KindRegenerateNFO,
		KindRetentionDelete, KindBulkEditSeries, KindBulkEditVideos:
		return false
	default:
		return true // done / cancelled for download, scan, etc.
	}
}
