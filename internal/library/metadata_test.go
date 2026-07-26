package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestRefreshListedDoesNotCreate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Meta",
		SourceURL:        "https://www.example.com/@meta",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID

	_, ok, err := s.RefreshListed(ser.ID, library.ListedVideo{
		RemoteID: "missing", Title: "Nope", SourceID: srcID,
	}, seedTaskID(t, s))
	if err != nil || ok {
		t.Fatalf("want skip unknown, ok=%v err=%v", ok, err)
	}

	created, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "Old Title", WebpageURL: "https://www.example.com/watch?v=vid1",
		SourceID: srcID,
	}, seedTaskID(t, s))
	if err != nil || !created.Created {
		t.Fatalf("create: %+v %v", created, err)
	}

	vid, ok, err := s.RefreshListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "New Title", Description: "fresh",
		WebpageURL: "https://www.example.com/watch?v=vid1",
		SourceID: srcID,
	}, seedTaskID(t, s))
	if err != nil || !ok || vid != created.VideoID {
		t.Fatalf("refresh: vid=%d ok=%v err=%v", vid, ok, err)
	}
	v, err := s.GetVideo(created.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	// Soft-fill: keep existing title; fill empty description.
	if v.Title != "Old Title" || v.Description != "fresh" {
		t.Fatalf("got title=%q desc=%q", v.Title, v.Description)
	}
	// Soft-fill again: non-empty description must not clobber.
	_, ok, err = s.RefreshListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "Other", Description: "newer",
		WebpageURL: "https://www.example.com/watch?v=vid1",
		SourceID: srcID,
	}, seedTaskID(t, s))
	if err != nil || !ok {
		t.Fatalf("refresh2: ok=%v err=%v", ok, err)
	}
	v, err = s.GetVideo(created.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Old Title" || v.Description != "fresh" {
		t.Fatalf("after second refresh title=%q desc=%q", v.Title, v.Description)
	}
	hist, err := s.ListVideoHistory(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hist {
		if h.Event == "rescan_metadata" || h.Event == "discovered" || h.Event == "updated" {
			t.Fatalf("list-pass events must not write video_history, got %q", h.Event)
		}
	}
}

func TestEnqueueMetadataRescan(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "MetaQ",
		SourceURL:        "https://www.example.com/@metaq",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueMetadataRescanSeries(ser.ID)
	if err != nil || id == 0 {
		t.Fatalf("enqueue series: %v id=%d", err, id)
	}
	_, err = s.EnqueueMetadataRescanSeries(ser.ID)
	if err == nil {
		t.Fatal("want conflict on second series rescan")
	}

	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vidTask, err := s.EnqueueMetadataRescanVideo(res.VideoID)
	if err != nil || vidTask == 0 {
		t.Fatalf("enqueue video: %v", err)
	}
}

func TestEnqueueRefreshSidecarsVideo(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "SideQ",
		SourceURL:        "https://www.example.com/@sideq",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "s1", Title: "T", WebpageURL: "https://www.example.com/watch?v=s1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueRefreshSidecarsVideo(res.VideoID); err == nil {
		t.Fatal("want error without pack anchor")
	}
	dir := filepath.Join(root.Path, "SideQ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, res.VideoID, media); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueRefreshSidecarsVideo(res.VideoID)
	if err != nil || id == 0 {
		t.Fatalf("enqueue: %v id=%d", err, id)
	}
	if _, err := s.EnqueueRefreshSidecarsVideo(res.VideoID); err == nil {
		t.Fatal("want conflict on second enqueue")
	}
}
