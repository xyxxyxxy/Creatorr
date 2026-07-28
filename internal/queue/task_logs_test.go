package queue

import (
	"fmt"
	"testing"
)

func TestTaskLogsAppendSnapshotClear(t *testing.T) {
	l := newTaskLogs()
	l.Append(1, "Running")
	l.Append(1, "Running") // duplicate skipped
	l.Append(1, "Listing…")
	got := l.Snapshot(1)
	if len(got) != 2 || got[0] != "Running" || got[1] != "Listing…" {
		t.Fatalf("snapshot=%v", got)
	}
	l.Clear(1)
	if len(l.Snapshot(1)) != 0 {
		t.Fatal("expected empty after clear")
	}
}

func TestTaskLogsCap(t *testing.T) {
	l := newTaskLogs()
	for i := 0; i < taskLogCap+50; i++ {
		l.Append(2, fmt.Sprintf("line %d", i))
	}
	got := l.Snapshot(2)
	if len(got) != taskLogCap {
		t.Fatalf("len=%d want %d", len(got), taskLogCap)
	}
	if got[0] != fmt.Sprintf("line %d", 50) || got[len(got)-1] != fmt.Sprintf("line %d", taskLogCap+49) {
		t.Fatalf("ring window wrong: first=%q last=%q", got[0], got[len(got)-1])
	}
}
