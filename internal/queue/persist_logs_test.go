package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestPersistLogsOnFailedFinish(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", Message: "dl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	s.Logs.Append(id, "Running")
	s.Logs.Append(id, "ERROR: boom")
	var dbRaw string
	if err := s.DB.SQL.QueryRow(`SELECT COALESCE(logs,'') FROM tasks WHERE id = ?`, id).Scan(&dbRaw); err != nil {
		t.Fatal(err)
	}
	if dbRaw != "" && dbRaw != "[]" {
		t.Fatalf("expected DB logs empty while running, got %q", dbRaw)
	}
	if err := s.Finish(id, queue.StatusFailed, "fail", "DownloadFailed", "boom"); err != nil {
		t.Fatal(err)
	}
	if len(s.Logs.Snapshot(id)) != 0 {
		t.Fatal("expected memory logs cleared after Finish")
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v %#v", err, got)
	}
	if len(got.Logs) != 2 || got.Logs[0] != "Running" || got.Logs[1] != "ERROR: boom" {
		t.Fatalf("persisted logs=%v", got.Logs)
	}
}

func TestPersistLogsClearedOnDone(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", Message: "dl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	s.Logs.Append(id, "Running")
	if err := s.Finish(id, queue.StatusDone, "ok", "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if len(got.Logs) != 0 {
		t.Fatalf("done should clear logs, got %v", got.Logs)
	}
}
