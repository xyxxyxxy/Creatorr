package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/testutil"
)

func TestListTasksQFilter(t *testing.T) {
	d := testutil.OpenDB(t)
	s := queue.NewStore(d)
	keep, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindScan, Domain: "example.com", Origin: queue.OriginManual,
		Message: "alpha scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindScan, Domain: "other.example", Origin: queue.OriginManual,
		Message: "beta scan",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListTasks(queue.TaskListFilter{Q: "alpha"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != keep {
		t.Fatalf("q=alpha: %+v want id=%d", got, keep)
	}

	byDomain, err := s.ListTasks(queue.TaskListFilter{Q: "other.example"}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDomain) != 1 || byDomain[0].Domain != "other.example" {
		t.Fatalf("q=domain: %+v", byDomain)
	}

	n, err := s.CountTasks(queue.TaskListFilter{Q: "no-such-task-text"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("count empty q match=%d", n)
	}
}
