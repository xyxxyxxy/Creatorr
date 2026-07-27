package queue_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func openStore(t *testing.T) *queue.Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	_ = settings.SetDomainDefault(d, 0, 100, 1, "10M", "0", false)
	return queue.NewStore(d)
}

func seedVideo(t *testing.T, s *queue.Store, remote string) int64 {
	t.Helper()
	_, _ = s.DB.SQL.Exec(`INSERT INTO root_folders (name, path) VALUES (?, ?)`, "r"+remote, "/tmp/"+remote)
	var rootID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM root_folders WHERE name = ?`, "r"+remote).Scan(&rootID)
	_, _ = s.DB.SQL.Exec(`INSERT INTO quality_profiles (name, format_selector) VALUES (?, 'best')`, "p"+remote)
	var profID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM quality_profiles WHERE name = ?`, "p"+remote).Scan(&profID)
	res, err := s.DB.SQL.Exec(`INSERT INTO series (title, root_id, quality_profile_id, added_at) VALUES (?, ?, ?, datetime('now'))`, "S"+remote, rootID, profID)
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := res.LastInsertId()
	res, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, ?, ?, 'wanted')
	`, sid, remote, "T"+remote)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	return vid
}

func TestEnqueueClaimFinish(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "v1")
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid,
		Payload: map[string]any{"url": "https://example.com/watch?v=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.ID != id {
		t.Fatalf("claim: %+v", task)
	}
	// second claim same domain while running -> nil
	task2, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if task2 != nil {
		t.Fatalf("expected no claim while running, got %+v", task2)
	}
	if err := s.Finish(id, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestClaimNextAllowsParallelTasks(t *testing.T) {
	s := openStore(t)
	_ = settings.SetDomainDefault(s.DB, 0, 8, 2, "10M", "0", false)
	v1 := seedVideo(t, s, "p1")
	v2 := seedVideo(t, s, "p2")
	id1, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: v1})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: v2})
	if err != nil {
		t.Fatal(err)
	}
	t1, err := s.ClaimNext()
	if err != nil || t1 == nil || t1.ID != id1 {
		t.Fatalf("first claim: %+v %v", t1, err)
	}
	t2, err := s.ClaimNext()
	if err != nil || t2 == nil || t2.ID != id2 {
		t.Fatalf("second claim with parallel=2: %+v %v", t2, err)
	}
	t3, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if t3 != nil {
		t.Fatalf("expected no third claim, got %+v", t3)
	}
}

func TestCooldownUntil(t *testing.T) {
	s := openStore(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "1", false)
	_ = domains.EnsureHost(s.DB, "example.com")
	vid := seedVideo(t, s, "cd1")
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid,
		Payload: map[string]any{"url": "https://example.com/watch?v=cd1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(id, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
	until := s.CooldownUntil("example.com")
	if until.IsZero() {
		t.Fatal("expected cooldown after finish")
	}
	if !s.CooldownUntil("other.example.com").IsZero() {
		t.Fatal("other domain should not cool down")
	}
}

func TestSystemLaneNoCooldown(t *testing.T) {
	s := openStore(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "1", false)
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}
	if !s.CooldownUntil(queue.SystemDomain).IsZero() {
		t.Fatal("system must not cool down after claim")
	}
	if err := s.Finish(id, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
	if !s.CooldownUntil(queue.SystemDomain).IsZero() {
		t.Fatal("system must not cool down after finish")
	}
	id2, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync2",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != id2 {
		t.Fatalf("expected immediate claim of next system task, got %#v", got)
	}
}

func TestUnmonitoredBlocksClaim(t *testing.T) {
	s := openStore(t)
	_, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindScan, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := domains.Deactivate(s.DB, "example.com"); err != nil {
		t.Fatal(err)
	}
	task, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Fatalf("expected unmonitored domain to block, got %+v", task)
	}
	if err := domains.SetActive(s.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	task, err = s.ClaimNext()
	if err != nil || task == nil {
		t.Fatalf("claim after remonitor: %v %+v", err, task)
	}
}

func TestPausedBlocksClaimKeepsPending(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindScan, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := domains.SetPaused(s.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	var nDomains int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM domains WHERE domain = ?`, "example.com").Scan(&nDomains)
	if nDomains != 0 {
		t.Fatalf("pause must not create domains row: %d", nDomains)
	}
	task, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Fatalf("expected paused domain to block claim, got %+v", task)
	}
	var status string
	if err := s.DB.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("pending should stay queued, got %s", status)
	}
	if err := domains.SetPaused(s.DB, "example.com", false); err != nil {
		t.Fatal(err)
	}
	task, err = s.ClaimNext()
	if err != nil || task == nil || task.ID != id {
		t.Fatalf("claim after resume: %v %+v", err, task)
	}
}

func TestListActivePositions(t *testing.T) {
	s := openStore(t)
	v1 := seedVideo(t, s, "a1")
	v2 := seedVideo(t, s, "a2")
	_, _ = s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "a.example", VideoID: v1, Priority: 0})
	_, _ = s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "a.example", VideoID: v2, Priority: 10})
	list, err := s.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len=%d", len(list))
	}
	// higher priority first -> position 1
	if list[0].Priority != 10 || list[0].QueuePos != 1 {
		t.Fatalf("first=%+v", list[0])
	}
	if list[1].QueuePos != 2 {
		t.Fatalf("second=%+v", list[1])
	}
}

func TestCancelDomainPendingAndRunning(t *testing.T) {
	s := openStore(t)
	v1 := seedVideo(t, s, "cd1")
	v2 := seedVideo(t, s, "cd2")
	id1, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "cancel.example", VideoID: v1})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "cancel.example", VideoID: v2})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext()
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %+v", err, claimed)
	}
	if claimed.ID != id1 && claimed.ID != id2 {
		t.Fatalf("unexpected claim id %d", claimed.ID)
	}
	out, err := s.CancelDomain("cancel.example", "Domain deactivated")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("cancelled=%d want 2", len(out))
	}
	for _, id := range []int64{id1, id2} {
		st, err := s.TaskStatus(id)
		if err != nil || st != queue.StatusCancelled {
			t.Fatalf("id %d status=%q err=%v", id, st, err)
		}
	}
}

func TestDomainFromURL(t *testing.T) {
	if got := queue.DomainFromURL("https://www.Example.com/watch?v=x"); got != "example.com" {
		t.Fatalf("got %q", got)
	}
	if got := queue.DomainFromURL("https://example.com/watch?v=x"); got != "example.com" {
		t.Fatalf("bare host got %q", got)
	}
	if got := queue.DomainFromURL("www.example.com"); got != "example.com" {
		t.Fatalf("bare domain got %q", got)
	}
}

func TestAppendCommand(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendCommand(id, "yt-dlp -J https://example.com/v"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendCommand(id, `ffmpeg -i "a b.mkv" out.mkv`); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(id, queue.StatusDone, "ok", "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v %#v", err, got)
	}
	if len(got.Commands) != 2 {
		t.Fatalf("commands=%v", got.Commands)
	}
	if got.Commands[0] != "yt-dlp -J https://example.com/v" {
		t.Fatalf("cmd0=%q", got.Commands[0])
	}
	if got.Commands[1] != `ffmpeg -i "a b.mkv" out.mkv` {
		t.Fatalf("cmd1=%q", got.Commands[1])
	}
	if err := s.AppendCommand(id, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(id)
	if len(got.Commands) != 2 {
		t.Fatalf("empty append changed len: %v", got.Commands)
	}
}
