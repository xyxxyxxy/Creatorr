package library_test

import (
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestMarkDownloadFailedDoesNotHoldSiblings(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ErrOnly", SourceURL: "https://www.example.com/@eo", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID

	var ids []int64
	for _, rid := range []string{"e1", "e2", "e3"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	for _, id := range ids[:2] {
		if err := s.MarkDownloadFailed(id, seedTaskID(t, s), apperrors.CodeDownloadFailed, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	v3, _ := s.GetVideo(ids[2])
	if v3.Status != "wanted" {
		t.Fatalf("sibling stays wanted, got %s", v3.Status)
	}
}

func TestRetrySourceErrorsClearsDownloadErrors(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Retry", SourceURL: "https://www.example.com/@rt", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "r1", Title: "R1", WebpageURL: "https://www.example.com/watch?v=r1",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeRemuxFailed, "ffmpeg"); err != nil {
		t.Fatal(err)
	}
	n, err := s.RetrySourceErrors(srcID)
	if err != nil || n != 1 {
		t.Fatalf("retry n=%d err=%v", n, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != "wanted" {
		t.Fatalf("want wanted after retry, got %s", v.Status)
	}
}
