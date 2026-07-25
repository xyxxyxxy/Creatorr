package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestCancelScanWritesSourceHistory(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CancelScan", SourceURL: "https://www.example.com/@cancelscan",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID

	// CreateSeries already enqueues a full scan; cancel that pending task.
	active, err := s.Queue.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	var tid int64
	for _, tsk := range active {
		if tsk.Kind == queue.KindScan && queue.SourceIDFromPayload(tsk.Payload) == srcID {
			tid = tsk.ID
			break
		}
	}
	if tid == 0 {
		t.Fatal("expected pending scan from CreateSeries")
	}
	if _, err := s.Queue.CancelWithMessage(tid, "Cancelled"); err != nil {
		t.Fatal(err)
	}

	hist, err := s.ListSourceHistoryPage(srcID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Event != library.SourceHistCancelled || hist[0].TaskID != tid {
		t.Fatalf("source history=%+v", hist)
	}
	st, err := s.LatestSourceScanStatus(srcID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Event != "" {
		t.Fatalf("cancelled must not sticky-status, got %+v", st)
	}

	// Idempotent: second record is a no-op.
	task, err := s.Queue.GetTask(tid)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	if err := s.RecordTaskCancelled(task); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountSourceHistory(srcID)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
}

func TestCancelDownloadWritesVideoHistory(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CancelDL", SourceURL: "https://www.example.com/@canceldl",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	tidBook := seedTaskID(t, s)
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "One", SourceID: srcID,
	}, tidBook)
	if err != nil {
		t.Fatal(err)
	}

	tid, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com",
		SeriesID: ser.ID, VideoID: res.VideoID,
		Payload: map[string]any{"url": "https://www.example.com/watch?v=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.IgnoreVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cancelled) != 1 || cancelled[0].ID != tid {
		t.Fatalf("cancelled=%+v", cancelled)
	}

	hist, err := s.ListVideoHistory(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range hist {
		if e.Event == library.VideoHistCancelled && e.TaskID.Valid && e.TaskID.Int64 == tid {
			found = true
			if e.Message != "Cancelled (video ignored)" {
				t.Fatalf("message=%q", e.Message)
			}
		}
	}
	if !found {
		t.Fatalf("expected cancelled video history, got %+v", hist)
	}
}
