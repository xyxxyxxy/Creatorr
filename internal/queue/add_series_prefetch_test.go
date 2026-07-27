package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestAddSeriesPrefetchIsInteractiveAndIgnoresPause(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindPrefetchAddSeries,
		Domain: "example.com",
		Payload: map[string]any{
			"url":         "https://example.com/c",
			"draft_token": "tok123abc",
		},
		Message: "add series",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queue.IsInteractiveKind(queue.KindPrefetchAddSeries) {
		t.Fatal("expected interactive")
	}
	if err := domains.SetPaused(s.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	if task, err := s.ClaimNext(); err != nil || task != nil {
		t.Fatalf("ClaimNext should skip: %v %+v", err, task)
	}
	inter, err := s.ClaimInteractive()
	if err != nil || inter == nil || inter.ID != id {
		t.Fatalf("ClaimInteractive: %v %+v", err, inter)
	}
	if queue.DraftTokenFromPayload(inter.Payload) != "tok123abc" {
		t.Fatalf("token=%q", queue.DraftTokenFromPayload(inter.Payload))
	}
}

func TestInteractiveFinishSkipsCooldown(t *testing.T) {
	s := openStore(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 30, 8, 1, "10M", "1", false)
	_ = domains.EnsureHost(s.DB, "example.com")
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindPrefetchAddSeries,
		Domain:  "example.com",
		Payload: map[string]any{"url": "https://example.com/c", "draft_token": "tok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimInteractive(); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(id, queue.StatusDone, "ok", "", ""); err != nil {
		t.Fatal(err)
	}
	if !s.CooldownUntil("example.com").IsZero() {
		t.Fatal("interactive finish must not start domain cooldown")
	}
}
