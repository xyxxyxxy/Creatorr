package library_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestCreateIndexedVideoIgnoredAndConflict(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", SourceURL: "https://example.com/show",
		RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Ep1",
		RemoteID:   "abc123",
		UploadDate: "2024-06-01",
		SourceURL:  "https://example.com/watch/abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "ignored" {
		t.Fatalf("status=%q want ignored", v.Status)
	}
	if v.SourceID.Valid {
		t.Fatalf("source_id should be NULL, got %v", v.SourceID.Int64)
	}
	if v.RemoteID != "abc123" || v.Title != "Ep1" {
		t.Fatalf("got remote=%q title=%q", v.RemoteID, v.Title)
	}
	if !v.SourceURL.Valid || v.SourceURL.String != "https://example.com/watch/abc123" {
		t.Fatalf("source_url=%v", v.SourceURL)
	}
	_, err = s.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Dup",
		RemoteID:   "abc123",
		UploadDate: "2024-06-02",
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestCreateIndexedVideoGeneratesRemoteID(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.CreateIndexedVideo(library.CreateIndexedVideoParams{
		SeriesID:   ser.ID,
		Title:      "Manual",
		UploadDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.RemoteID, "video-") {
		t.Fatalf("remote_id=%q", v.RemoteID)
	}
}

func TestAddVideoDraftRoundTripAndFromEntry(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	draft := library.BuildAddVideoDraftFromEntry(ytdlp.Entry{
		ID:          "vid1",
		Title:       "Hello",
		UploadDate:  "2024-03-04T00:00:00Z",
		WebpageURL:  "https://example.com/w/vid1",
		Description: "plot",
	}, "https://example.com/w/vid1")
	if draft.RemoteID != "vid1" || draft.Title != "Hello" || draft.SourceURL == "" {
		t.Fatalf("%+v", draft)
	}
	token := "tokabcdef0123456"
	if err := s.WriteAddVideoDraft(token, draft); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadAddVideoDraft(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != draft.Title || got.RemoteID != draft.RemoteID {
		t.Fatalf("%+v", got)
	}
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.CreateIndexedVideoFromAddDraft(ser.ID, token, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "ignored" || v.RemoteID != "vid1" || v.Title != "Hello" {
		t.Fatalf("%+v", v)
	}
}

func TestEnqueueAddVideoPrefetch(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueAddVideoPrefetch("https://example.com/w/1", "tokabcdef0123456", 0)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil || task == nil {
		t.Fatalf("%v %+v", err, task)
	}
	if task.Kind != queue.KindPrefetchAddVideo {
		t.Fatalf("kind=%q", task.Kind)
	}
	if !queue.IsInteractiveKind(queue.KindPrefetchAddVideo) {
		t.Fatal("expected interactive")
	}
}
