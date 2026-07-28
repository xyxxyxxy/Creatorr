package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestAddVideoPrefetchIsInteractiveAndIgnoresPause(t *testing.T) {
	s := openStore(t)
	id, err := s.Enqueue(queue.EnqueueParams{
		Kind:   queue.KindPrefetchAddVideo,
		Domain: "example.com",
		Payload: map[string]any{
			"url":         "https://example.com/w/1",
			"draft_token": "tok123abc",
		},
		Message: "add video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queue.IsInteractiveKind(queue.KindPrefetchAddVideo) {
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
