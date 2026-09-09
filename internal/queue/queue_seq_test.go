package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestClaimImmediateDownloadNow(t *testing.T) {
	s := openStore(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "1", false)
	v1 := seedVideo(t, s, "imm1")
	v2 := seedVideo(t, s, "imm2")

	idNorm, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: v1,
		Message: "normal",
	})
	if err != nil {
		t.Fatal(err)
	}
	idNow, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: v2,
		Message: "Download now", Immediate: true, BypassDownloadCap: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ClaimImmediate()
	if err != nil || got == nil || got.ID != idNow {
		t.Fatalf("ClaimImmediate want download-now id=%d got %#v err=%v", idNow, got, err)
	}
	blocked, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if blocked != nil {
		t.Fatalf("ClaimNext should wait for parallel slot while download-now runs, got %#v", blocked)
	}
	if err := s.Finish(idNow, queue.StatusDone, "Done", "", ""); err != nil {
		t.Fatal(err)
	}
	again, err := s.ClaimNext()
	if err != nil || again == nil || again.ID != idNorm {
		t.Fatalf("ClaimNext want normal id=%d got %#v err=%v", idNorm, again, err)
	}
}

func TestMoveToFrontAndClearCooldown(t *testing.T) {
	s := openStore(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "1", false)
	v1 := seedVideo(t, s, "mf1")
	v2 := seedVideo(t, s, "mf2")

	first, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: v1,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: v2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveToFront(second); err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNext()
	if err != nil || got == nil || got.ID != second {
		t.Fatalf("want second at front, got %#v err=%v (first=%d)", got, err, first)
	}

	s.StartCooldownForDomains([]string{"example.com"})
	if s.CooldownUntil("example.com").IsZero() {
		t.Fatal("expected cooldown armed")
	}
	if !s.ClearCooldown("example.com") {
		t.Fatal("expected ClearCooldown true")
	}
	if !s.CooldownUntil("example.com").IsZero() {
		t.Fatal("cooldown should be cleared")
	}
	// Clear again when idle is false
	if s.ClearCooldown("example.com") {
		t.Fatal("expected ClearCooldown false when idle")
	}
}
