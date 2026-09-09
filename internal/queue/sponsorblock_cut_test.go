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
		Origin: queue.OriginManual,
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimNext(); err != nil {
		t.Fatal(err)
	}

	_, err = s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid, SeriesID: seriesID,
		Message: "cut",
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

func TestSponsorblockCutFIFOAfterEarlierEnqueue(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys2")

	_, err := s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Message: "cut",
		Payload: map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != queue.KindSponsorblockCut {
		t.Fatalf("want sponsorblock_cut first (FIFO), got %#v", got)
	}
}

func TestUpdateProgressNilClears(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "sys3")
	id, err := s.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com", VideoID: vid})
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
		Origin: queue.OriginManual,
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Payload:  map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain, VideoID: vid,
		Payload:  map[string]any{"video_id": vid, "media_path": "/x"},
	})
	if err == nil {
		t.Fatal("expected duplicate reject")
	}
}
