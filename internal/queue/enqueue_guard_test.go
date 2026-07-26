package queue_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestEnqueueRejectsDuplicateDownload(t *testing.T) {
	s := openStore(t)
	vid := seedVideo(t, s, "dup1")
	p := queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", SeriesID: 1, VideoID: vid,
		Message: "Download",
	}
	if _, err := s.Enqueue(p); err != nil {
		t.Fatal(err)
	}
	_, err := s.Enqueue(p)
	if !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestEnqueueRejectsDuplicateScanSource(t *testing.T) {
	s := openStore(t)
	p := queue.EnqueueParams{
		Kind: queue.KindScan, Domain: "example.com",
		Payload: map[string]any{"source_id": int64(7), "mode": "scan"},
	}
	if _, err := s.Enqueue(p); err != nil {
		t.Fatal(err)
	}
	_, err := s.Enqueue(p)
	if !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestEnqueueDownloadQueueCap(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "q3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 2, 1, "10M", "off", "0", false)
	s := queue.NewStore(d)
	v1 := seedVideo(t, s, "c1")
	v2 := seedVideo(t, s, "c2")
	v3 := seedVideo(t, s, "c3")
	for _, vid := range []int64{v1, v2} {
		if _, err := s.Enqueue(queue.EnqueueParams{
			Kind: queue.KindDownload, Domain: "example.com", SeriesID: 1, VideoID: vid,
		}); err != nil {
			t.Fatalf("enqueue %d: %v", vid, err)
		}
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", SeriesID: 1, VideoID: v3,
	})
	if !errors.Is(err, queue.ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "other.example", SeriesID: 1, VideoID: v3,
	})
	if err != nil {
		t.Fatalf("other domain should have its own cap, got %v", err)
	}
}

func TestEnqueueCacheBeginningSharesCap(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "q-begin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 2, 1, "10M", "off", "0", false)
	s := queue.NewStore(d)
	v1 := seedVideo(t, s, "b1")
	v2 := seedVideo(t, s, "b2")
	v3 := seedVideo(t, s, "b3")
	if _, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", SeriesID: 1, VideoID: v1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindCacheBeginning, Domain: "example.com", SeriesID: 1, VideoID: v2,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(queue.EnqueueParams{
		Kind: queue.KindCacheBeginning, Domain: "example.com", SeriesID: 1, VideoID: v3,
	})
	if !errors.Is(err, queue.ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
}
