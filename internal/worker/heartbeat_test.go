package worker

import (
	"testing"
	"time"
)

func TestHeartbeatStateTouchAt(t *testing.T) {
	var s HeartbeatState
	if !s.At().IsZero() {
		t.Fatal("expected zero before touch")
	}
	before := time.Now().Add(-time.Second)
	s.Touch()
	at := s.At()
	if at.Before(before) || at.After(time.Now().Add(time.Second)) {
		t.Fatalf("unexpected at=%v", at)
	}
}
