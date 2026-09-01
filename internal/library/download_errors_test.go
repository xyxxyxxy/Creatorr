package library_test

import (
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestAgeRestrictedDoesNotTriggerSourceHold(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "AgeGate", SourceURL: "https://www.example.com/@ag", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_ = settings.Set(s.DB, settings.KeySourceDownloadErrorThreshold, "2")

	var ids []int64
	for _, rid := range []string{"a1", "a2", "a3"} {
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
		if err := s.MarkDownloadFailed(id, seedTaskID(t, s), apperrors.CodeAgeRestricted, "verify your age"); err != nil {
			t.Fatal(err)
		}
	}
	v3, _ := s.GetVideo(ids[2])
	if v3.Status != "wanted" {
		t.Fatalf("two age-restricted errors must not hold siblings, got %s", v3.Status)
	}
	n, err := s.CountDownloadErrors(srcID)
	if err != nil || n != 0 {
		t.Fatalf("CountDownloadErrors=%d err=%v want 0", n, err)
	}
}

func TestAgeRestrictedPlusDownloadErrorTriggersHold(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "AgeMix", SourceURL: "https://www.example.com/@am", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_ = settings.Set(s.DB, settings.KeySourceDownloadErrorThreshold, "2")

	var ids []int64
	for _, rid := range []string{"m1", "m2", "m3", "m4"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	if err := s.MarkDownloadFailed(ids[0], seedTaskID(t, s), apperrors.CodeAgeRestricted, "verify your age"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDownloadFailed(ids[1], seedTaskID(t, s), apperrors.CodeDownloadFailed, "boom1"); err != nil {
		t.Fatal(err)
	}
	v4, _ := s.GetVideo(ids[3])
	if v4.Status != "wanted" {
		t.Fatalf("one countable error must not hold siblings yet, got %s", v4.Status)
	}
	if err := s.MarkDownloadFailed(ids[2], seedTaskID(t, s), apperrors.CodeDownloadFailed, "boom2"); err != nil {
		t.Fatal(err)
	}
	v4, _ = s.GetVideo(ids[3])
	if v4.Status != "wanted_source_error" {
		t.Fatalf("two countable errors at threshold want wanted_source_error, got %s", v4.Status)
	}
	n, err := s.CountDownloadErrors(srcID)
	if err != nil || n != 2 {
		t.Fatalf("CountDownloadErrors=%d err=%v want 2", n, err)
	}
}
