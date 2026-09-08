package queue_test

import (
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestCancelReasonMessageRejectsEmptyUnknown(t *testing.T) {
	for _, code := range []string{"", "  ", "operator", "free text"} {
		if _, err := queue.CancelReasonMessage(code); err == nil {
			t.Fatalf("code %q: want error", code)
		}
	}
}

func TestCancelReasonMessageManualShutdown(t *testing.T) {
	msg, err := queue.CancelReasonMessage(queue.CancelReasonManual)
	if err != nil || msg != "Cancelled (manual)" {
		t.Fatalf("manual: %q %v", msg, err)
	}
	msg, err = queue.CancelReasonMessage(queue.CancelReasonShutdown)
	if err != nil || msg != "Cancelled (shutdown)" {
		t.Fatalf("shutdown: %q %v", msg, err)
	}
}

func TestCancelWithReasonRejectsUnknown(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "cr1")
	id, err := s.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelWithReason(id, "nope"); err == nil {
		t.Fatal("want reject unknown reason")
	}
	st, err := s.TaskStatus(id)
	if err != nil || st != queue.StatusPending {
		t.Fatalf("status=%q err=%v want pending", st, err)
	}
	if _, err := s.CancelWithReason(id, queue.CancelReasonManual); err != nil {
		t.Fatal(err)
	}
	st, err = s.TaskStatus(id)
	if err != nil || st != queue.StatusCancelled {
		t.Fatalf("status=%q err=%v", st, err)
	}
	got, err := s.GetTask(id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Message != "Cancelled (manual)" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestKindResumableOnShutdown(t *testing.T) {
	if !queue.KindResumableOnShutdown(queue.KindDownload) {
		t.Fatal("download should be resumable")
	}
	for _, kind := range []string{
		queue.KindPrefetchSeriesMeta, queue.KindPrefetchVideoMeta,
		queue.KindPrefetchAddSeries, queue.KindPrefetchAddVideo,
	} {
		if queue.KindResumableOnShutdown(kind) {
			t.Fatalf("%s should not be resumable", kind)
		}
		if !strings.HasPrefix(kind, "prefetch_") {
			t.Fatalf("unexpected kind %s", kind)
		}
	}
	if queue.KindResumableOnShutdown(queue.KindProbeSourceTitle) {
		t.Fatal("probe_source_title should not be resumable")
	}
}
