package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestMergeDomainTag(t *testing.T) {
	got := library.MergeDomainTag(nil, "https://www.example.com/watch?v=1")
	if len(got) != 1 || got[0] != "example.com" {
		t.Fatalf("prepend: %v", got)
	}
	got = library.MergeDomainTag([]string{"News", "example.com"}, "https://www.example.com/v")
	if len(got) != 2 || got[0] != "example.com" || got[1] != "News" {
		t.Fatalf("move first: %v", got)
	}
	got = library.MergeDomainTag([]string{"example.com", "News"}, "https://www.example.com/v")
	if len(got) != 2 || got[0] != "example.com" {
		t.Fatalf("already first: %v", got)
	}
	got = library.MergeDomainTag([]string{"News"}, "")
	if len(got) != 1 || got[0] != "News" {
		t.Fatalf("empty url: %v", got)
	}
}

func TestAddSourceSeedsDomainTag(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "DomainSeed",
		SourceURL:        "https://www.example.com/@d",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src := ser.Sources[0]
	if len(src.Tags) != 1 || src.Tags[0] != "example.com" {
		t.Fatalf("create series source tags=%v", src.Tags)
	}
	added, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://cdn.example.org/feed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(added.Tags) != 1 || added.Tags[0] != "cdn.example.org" {
		t.Fatalf("add source tags=%v", added.Tags)
	}
}

func TestUpdateSourceWatchURLCanDropDomainTag(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "WatchDrop",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/watch?v=abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	only := []string{"only"}
	updated, err := s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{Tags: &only})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "only" {
		t.Fatalf("tags=%v want operator-only list", updated.Tags)
	}
}

func TestUpdateSourceFeedCanDropDomainTag(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "FeedDrop",
		SourceURL:        "https://www.example.com/@f",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src := ser.Sources[0]
	empty := []string{"only"}
	updated, err := s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{Tags: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "only" {
		t.Fatalf("feed tags=%v", updated.Tags)
	}
}
