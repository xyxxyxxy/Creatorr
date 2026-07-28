package queue_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestClaimInteractiveWhileDomainBusy(t *testing.T) {
	s := openStore(t)
	_ = domains.EnsureHost(s.DB, "example.com")
	vid := seedVideo(t, s, "v1")

	dlID, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", VideoID: vid, Message: "dl",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNext()
	if err != nil || claimed == nil || claimed.ID != dlID {
		t.Fatalf("claim download: %v %+v", err, claimed)
	}

	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)
	prefID, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindPrefetchSeriesMeta, Domain: "example.com", SeriesID: seriesID,
		Payload: map[string]any{"url": "https://example.com/x"}, Message: "prefetch",
	})
	if err != nil {
		t.Fatal(err)
	}

	again, err := s.ClaimNext()
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("ClaimNext should wait for domain: %+v", again)
	}

	inter, err := s.ClaimInteractive()
	if err != nil || inter == nil || inter.ID != prefID {
		t.Fatalf("ClaimInteractive: %v %+v", err, inter)
	}
}

func TestClaimInteractiveIgnoresPause(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "v-pause")
	var seriesID int64
	_ = s.DB.SQL.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, vid).Scan(&seriesID)
	prefID, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindPrefetchSeriesMeta, Domain: "example.com", SeriesID: seriesID,
		Payload: map[string]any{"url": "https://example.com/x"}, Message: "prefetch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := domains.SetPaused(s.DB, "example.com", true); err != nil {
		t.Fatal(err)
	}
	if task, err := s.ClaimNext(); err != nil || task != nil {
		t.Fatalf("ClaimNext should skip paused: %v %+v", err, task)
	}
	inter, err := s.ClaimInteractive()
	if err != nil || inter == nil || inter.ID != prefID {
		t.Fatalf("ClaimInteractive should ignore pause: %v %+v", err, inter)
	}
}
