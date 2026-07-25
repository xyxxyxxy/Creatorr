package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestSystemLaneAlwaysSerial(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys1")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)

	_, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}

	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid, SeriesID: seriesID,
		Message: "cut", Priority: queue.PrioritySponsorblockCut,
		Payload: map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected no claim while system task running, got %#v", got)
	}
}

func TestSponsorblockCutPriorityBehindFileSync(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys2")

	_, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Message: "cut", Priority: queue.PrioritySponsorblockCut,
		Payload: map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync",
		Priority: queue.PrioritySyncFilesDue,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != queue.KindSyncFiles {
		t.Fatalf("want sync_files first, got %#v", got)
	}
}

func TestUpdateProgressNilClears(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys3")
	id, err := s.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.ClaimNext()
	if err != nil || task == nil {
		t.Fatalf("claim: %v %#v", err, task)
	}
	p := 0.4
	if err := s.UpdateProgress(id, "downloading", &p); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProgress(id, "remux", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Progress.Valid {
		t.Fatalf("expected progress cleared, got %v", got.Progress.Float64)
	}
	if got.Message != "remux" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestSponsorblockCutDupPerVideo(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys4")
	_, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Priority: queue.PrioritySponsorblockCut,
		Payload:  map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Priority: queue.PrioritySponsorblockCut,
		Payload:  map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err == nil {
		t.Fatal("expected duplicate reject")
	}
}
