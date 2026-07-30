package queue_test

import (
	"database/sql"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestLiveProgressNotWrittenToDB(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "live1")
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.ClaimNext()
	if err != nil || task == nil {
		t.Fatalf("claim: %v %#v", err, task)
	}
	p := 0.42
	if err := s.UpdateProgress(id, "Downloading…", &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Message != "Downloading…" {
		t.Fatalf("message=%q", got.Message)
	}
	if !got.Progress.Valid || got.Progress.Float64 != p {
		t.Fatalf("progress=%v", got.Progress)
	}
	var dbMsg sql.NullString
	var dbProg sql.NullFloat64
	if err := s.DB.SQL.QueryRow(`SELECT message, progress FROM tasks WHERE id = ?`, id).Scan(&dbMsg, &dbProg); err != nil {
		t.Fatal(err)
	}
	if dbProg.Valid {
		t.Fatalf("expected DB progress NULL, got %v", dbProg.Float64)
	}
	if dbMsg.Valid && dbMsg.String == "Downloading…" {
		t.Fatalf("expected DB message not updated to live text, got %q", dbMsg.String)
	}
}

func TestRequeueStaleRunningClearsProgress(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "live2")
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`UPDATE tasks SET progress = 0.5 WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.RequeueStaleRunning()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("requeued=%d", n)
	}
	var prog sql.NullFloat64
	var status, message string
	if err := s.DB.SQL.QueryRow(`SELECT status, message, progress FROM tasks WHERE id = ?`, id).Scan(&status, &message, &prog); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("status=%q", status)
	}
	if message != "Requeued after restart" {
		t.Fatalf("message=%q", message)
	}
	if prog.Valid {
		t.Fatalf("expected progress NULL, got %v", prog.Float64)
	}
}

func TestLiveStateClearedOnFinish(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "live3")
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	p := 0.9
	_ = s.UpdateProgress(id, "almost", &p)
	if err := s.Finish(id, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
	_, _, ok := s.Live.Get(id)
	if ok {
		t.Fatal("expected live state cleared")
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Message != "Done" {
		t.Fatalf("message=%q", got.Message)
	}
	if got.Progress.Valid {
		t.Fatalf("finished progress should not overlay, got %v", got.Progress.Float64)
	}
}
