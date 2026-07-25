package events_test

import (
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/events"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := events.NewHub()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	h.Publish(events.Event{Type: events.TypeTaskDone, TaskID: 7, Kind: "scan"})
	select {
	case e := <-ch:
		if e.Type != events.TypeTaskDone || e.TaskID != 7 {
			t.Fatalf("%+v", e)
		}
		if e.At == "" {
			t.Fatal("missing at")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHubDropSlowSubscriber(t *testing.T) {
	h := events.NewHub()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)
	// Fill buffer without reading.
	for i := 0; i < 40; i++ {
		h.Publish(events.Event{Type: events.TypeTaskUpdated, TaskID: int64(i)})
	}
	// Must not block.
	done := make(chan struct{})
	go func() {
		h.Publish(events.Event{Type: events.TypeTaskFailed, TaskID: 99})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked")
	}
}
