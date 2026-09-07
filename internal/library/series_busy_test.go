package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestSeriesHasBusyMediaTasks(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	serA, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "A", SourceURL: "https://www.example.com/@a", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	serB, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "B", SourceURL: "https://www.example.com/@b", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resA, err := s.UpsertListed(serA.ID, library.ListedVideo{
		RemoteID: "a1", Title: "One", SourceID: serA.Sources[0].ID,
		WebpageURL: "https://www.example.com/watch?v=a1",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resB, err := s.UpsertListed(serB.ID, library.ListedVideo{
		RemoteID: "b1", Title: "Two", SourceID: serB.Sources[0].ID,
		WebpageURL: "https://www.example.com/watch?v=b1",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	assertBusy := func(seriesID int64, want bool) {
		t.Helper()
		got, err := s.SeriesHasBusyMediaTasks(seriesID)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("series %d busy=%v want %v", seriesID, got, want)
		}
	}

	assertBusy(serA.ID, false)
	assertBusy(serB.ID, false)

	_, err = s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindDownload, Domain: "example.com",
		SeriesID: serA.ID, VideoID: resA.VideoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, true)
	assertBusy(serB.ID, false)

	_, err = s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindDownload, Domain: "example.com",
		SeriesID: serB.ID, VideoID: resB.VideoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, true)
	assertBusy(serB.ID, true)

	// Clear A downloads; B still busy.
	if _, err := s.Queue.CancelDownloadsForVideo(resA.VideoID, queue.CancelReasonManual); err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, false)
	assertBusy(serB.ID, true)
	if _, err := s.Queue.CancelDownloadsForVideo(resB.VideoID, queue.CancelReasonManual); err != nil {
		t.Fatal(err)
	}
	assertBusy(serB.ID, false)

	_, err = s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindSponsorblockCut, Domain: queue.SystemDomain,
		SeriesID: serA.ID, VideoID: resA.VideoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, true)
	assertBusy(serB.ID, false)
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = ? AND video_id = ?`,
		queue.KindSponsorblockCut, resA.VideoID); err != nil {
		t.Fatal(err)
	}

	_, err = s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindIntegrityCheckInitial, Domain: queue.SystemDomain,
		SeriesID: serA.ID, VideoID: resA.VideoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, true)
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = ? AND video_id = ?`,
		queue.KindIntegrityCheckInitial, resA.VideoID); err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, false)

	// Non-media kinds on A do not lock.
	if _, err := s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual,
		Kind: queue.KindRenameEpisodes, Domain: queue.SystemDomain,
		SeriesID: serA.ID,
		Payload:  map[string]any{"series_id": serA.ID, "formats_by_root": map[string]string{}},
	}); err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, false)

	// Video-linked download with null series_id still locks via join.
	_, err = s.DB.SQL.Exec(`
		INSERT INTO tasks (kind, status, series_id, video_id, payload, domain, priority, origin, created_at)
		VALUES (?, 'pending', NULL, ?, '{}', 'example.com', 0, 'manual', datetime('now'))
	`, queue.KindDownload, resA.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	assertBusy(serA.ID, true)
	assertBusy(serB.ID, false)
}
