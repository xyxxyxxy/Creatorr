package queue_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestRequeueStaleRunningIncrementsInterruptCount(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	s := queue.NewStore(d)
	id, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindScan, Domain: "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, queue.StatusRunning, id); err != nil {
		t.Fatal(err)
	}
	n, err := s.RequeueStaleRunning()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("requeued=%d", n)
	}
	var count int
	if err := d.SQL.QueryRow(`SELECT interrupt_count FROM tasks WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("interrupt_count=%d want 1", count)
	}
	items, err := s.ListTasks(queue.TaskListFilter{Statuses: []string{queue.StatusPending}}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].InterruptCount != 1 {
		t.Fatalf("list=%+v", items)
	}
}
