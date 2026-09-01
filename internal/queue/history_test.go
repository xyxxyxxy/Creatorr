package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestLastFinishedAt(t *testing.T) {
	s := openStore(t)
	_ = insertFinished(t, s, queue.KindDownload, queue.StatusDone, queue.SystemDomain)
	got, err := s.LastFinishedAt(queue.KindYtDlpUpdate, queue.SystemDomain, queue.StatusDone)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q want empty", got)
	}

	_, err = s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, domain, message, created_at, finished_at)
		VALUES (?, ?, ?, 'Already up to date', datetime('now'), '2026-08-19T10:00:00Z')
	`, queue.KindYtDlpUpdate, queue.StatusDone, queue.SystemDomain)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, domain, message, created_at, finished_at)
		VALUES (?, ?, ?, 'Already up to date', datetime('now'), '2026-09-01T12:00:00Z')
	`, queue.KindYtDlpUpdate, queue.StatusDone, queue.SystemDomain)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.LastFinishedAt(queue.KindYtDlpUpdate, queue.SystemDomain, queue.StatusDone)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-09-01T12:00:00Z" {
		t.Fatalf("got %q want newest finished_at", got)
	}
}

func insertFinished(t *testing.T, s *queue.Store, kind, status, domain string) int64 {
	t.Helper()
	res, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, domain, message, created_at, finished_at)
		VALUES (?, ?, ?, 'ok', datetime('now'), datetime('now'))
	`, kind, status, domain)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestHistoryFilterDomainKindStatus(t *testing.T) {
	s := openStore(t)
	id1 := insertFinished(t, s, queue.KindDownload, queue.StatusDone, "a.example")
	id2 := insertFinished(t, s, queue.KindScan, queue.StatusFailed, "b.example")
	_ = insertFinished(t, s, queue.KindDownload, queue.StatusCancelled, "b.example")
	_ = insertFinished(t, s, queue.KindSyncFiles, queue.StatusDone, queue.SystemDomain)
	_, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, domain, message, created_at)
		VALUES (?, ?, 'a.example', 'pending', datetime('now'))
	`, queue.KindDownload, queue.StatusPending)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CountHistory(queue.HistoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("all history count=%d want 4", n)
	}

	n, err = s.CountHistory(queue.HistoryFilter{Domain: "a.example"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("domain a count=%d want 1", n)
	}

	n, err = s.CountHistory(queue.HistoryFilter{Kind: queue.KindDownload, Statuses: []string{queue.StatusCancelled}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("download+cancelled count=%d want 1", n)
	}

	items, err := s.ListHistory(queue.HistoryFilter{Domain: "b.example", Kind: queue.KindScan}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != id2 {
		t.Fatalf("scan on b=%+v want id %d", items, id2)
	}

	items, err = s.ListHistory(queue.HistoryFilter{Statuses: []string{queue.StatusDone}, Domain: "a.example"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != id1 {
		t.Fatalf("done on a=%+v want id %d", items, id1)
	}

	domains, err := s.DistinctHistoryDomains()
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 3 || domains[0] != queue.SystemDomain || domains[1] != "a.example" || domains[2] != "b.example" {
		t.Fatalf("domains=%v want [%s a.example b.example]", domains, queue.SystemDomain)
	}
	kinds, err := s.DistinctHistoryKinds()
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 3 {
		t.Fatalf("kinds=%v", kinds)
	}
}

func TestHistoryFilterRange(t *testing.T) {
	s := openStore(t)
	_, err := s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, domain, message, created_at, finished_at) VALUES
		(?, ?, 'a.example', 'ok', '2026-07-24T10:00:00Z', '2026-07-24T11:00:00Z'),
		(?, ?, 'a.example', 'ok', '2026-07-25T09:00:00Z', '2026-07-25T10:30:00Z'),
		(?, ?, 'a.example', 'ok', '2026-07-25 22:00:00', '2026-07-25 23:15:00')
	`, queue.KindDownload, queue.StatusDone,
		queue.KindScan, queue.StatusDone,
		queue.KindDownload, queue.StatusFailed)
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CountHistory(queue.HistoryFilter{
		From: "2026-07-25T00:00:00Z",
		To:   "2026-07-25T23:59:59.999999999Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("day range count=%d want 2", n)
	}
	items, err := s.ListHistory(queue.HistoryFilter{
		From: "2026-07-24T00:00:00Z",
		To:   "2026-07-24T23:59:59.999999999Z",
	}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("jul24=%d", len(items))
	}
	n, err = s.CountHistory(queue.HistoryFilter{
		From:     "2026-07-25T00:00:00Z",
		To:       "2026-07-25T23:59:59.999999999Z",
		Statuses: []string{queue.StatusFailed},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("range+failed=%d", n)
	}
	n, err = s.CountHistory(queue.HistoryFilter{From: "2026-07-25T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("from-only=%d want 1", n)
	}
}
