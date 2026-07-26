package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestStreamPlayOccupiesParallelSlot(t *testing.T) {
	s := openStore(t)
	_ = domains.EnsureHost(s.DB, "example.com")
	vid := seedVideo(t, s, "v-stream")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)

	dlID, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid, Message: "dl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}

	streamID, err := s.InsertRunning(queue.EnqueueParams{
		Kind: queue.KindStreamPlay, Domain: "example.com", SeriesID: seriesID, VideoID: vid,
		Message: "Streaming playback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamID <= 0 {
		t.Fatal("expected stream_play id")
	}

	// Peer download still running; stream_play also running - ClaimNext should stay nil (max=1).
	if task, err := s.ClaimNext(); err != nil || task != nil {
		t.Fatalf("ClaimNext while stream_play+download: %v %+v", err, task)
	}

	// Finishing download frees nothing while stream_play still holds the slot.
	if err := s.Finish(dlID, queue.StatusDone, "done", "", ""); err != nil {
		t.Fatal(err)
	}
	if task, err := s.ClaimNext(); err != nil || task != nil {
		t.Fatalf("ClaimNext while stream_play only: %v %+v", err, task)
	}

	if err := s.Finish(streamID, queue.StatusDone, "Stream session ended", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestStreamPlayFinishSkipsCooldown(t *testing.T) {
	s := openStore(t)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "off", "0", false)
	_ = domains.EnsureHost(s.DB, "example.com")
	vid := seedVideo(t, s, "v-cd")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)

	tid, err := s.InsertRunning(queue.EnqueueParams{
		Kind: queue.KindStreamPlay, Domain: "example.com", SeriesID: seriesID, VideoID: vid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(tid, queue.StatusDone, "Stream session ended", "", ""); err != nil {
		t.Fatal(err)
	}
	if until := s.CooldownUntil("example.com"); !until.IsZero() {
		t.Fatalf("stream_play Finish should not arm cooldown, until=%v", until)
	}
}

func TestStreamPlayInsertWhilePaused(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "v-paused-play")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)
	if err := domains.SetPaused(s.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	tid, err := s.InsertRunning(queue.EnqueueParams{
		Kind: queue.KindStreamPlay, Domain: "example.com", SeriesID: seriesID, VideoID: vid,
		Message: "Streaming playback",
	})
	if err != nil || tid <= 0 {
		t.Fatalf("InsertRunning while paused: %v id=%d", err, tid)
	}
	if _, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid, Message: "dl",
	}); err != nil {
		t.Fatal(err)
	}
	if task, err := s.ClaimNext(); err != nil || task != nil {
		t.Fatalf("ClaimNext while paused: %v %+v", err, task)
	}
}

func TestCancelStaleStreamPlayAndRequeueOthers(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "v-boot")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)

	if _, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext()
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}

	streamID, err := s.InsertRunning(queue.EnqueueParams{
		Kind: queue.KindStreamPlay, Domain: "example.com", SeriesID: seriesID, VideoID: vid,
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CancelStaleStreamPlay()
	if err != nil || n != 1 {
		t.Fatalf("CancelStaleStreamPlay: n=%d err=%v", n, err)
	}
	st, _ := s.TaskStatus(streamID)
	if st != queue.StatusCancelled {
		t.Fatalf("stream status=%s", st)
	}

	n, err = s.RequeueStaleRunning()
	if err != nil || n != 1 {
		t.Fatalf("RequeueStaleRunning: n=%d err=%v (want download only)", n, err)
	}
	st, _ = s.TaskStatus(claimed.ID)
	if st != queue.StatusPending {
		t.Fatalf("download after requeue status=%s", st)
	}
}

func TestIsInteractiveKindStreamPlay(t *testing.T) {
	if !queue.IsInteractiveKind(queue.KindStreamPlay) {
		t.Fatal("stream_play should be interactive")
	}
	if queue.IsPrefetchKind(queue.KindStreamPlay) {
		t.Fatal("stream_play should not be prefetch")
	}
}
