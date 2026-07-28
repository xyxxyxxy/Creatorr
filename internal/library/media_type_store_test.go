package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestUpsertListedAutoIgnoreMediaTypes(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MT", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		AutoIgnoreMediaTypes: []string{"short"},
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@mt",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	miss, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "s1", Title: "A Short", SourceID: src.ID, MediaType: "short",
	}, taskID)
	if err != nil || !miss.Created || miss.Status != "ignored" || miss.IgnoreReason != library.IgnoreReasonMediaType {
		t.Fatalf("short: %+v err=%v", miss, err)
	}
	empty, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "e1", Title: "No Type", SourceID: src.ID, MediaType: "",
	}, taskID)
	if err != nil || !empty.Created || empty.Status != "wanted" || empty.IgnoreReason != "" {
		t.Fatalf("empty type: %+v err=%v", empty, err)
	}
	ok, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "Normal", SourceID: src.ID, MediaType: "video",
	}, taskID)
	if err != nil || !ok.Created || ok.Status != "wanted" {
		t.Fatalf("video: %+v err=%v", ok, err)
	}
}

func TestAutoIgnoreMediaTypesBeatsIndexAsIgnored(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MT2", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		AutoIgnoreMediaTypes: []string{"short"},
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@mt2", IndexAsIgnored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "s2", Title: "Short", SourceID: src.ID, MediaType: "short",
	}, seedTaskID(t, s))
	if err != nil || res.IgnoreReason != library.IgnoreReasonMediaType {
		t.Fatalf("want media_type reason: %+v err=%v", res, err)
	}
}

func TestListAutoIgnoreMediaTypeSuggestionsFromSeries(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Sug", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		AutoIgnoreMediaTypes: []string{"custom_type"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAutoIgnoreMediaTypeSuggestions()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range got {
		if v == "custom_type" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("custom_type missing from suggestions: %v", got)
	}
}

func TestVideoListFilterMediaType(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MTF", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@mtf"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	for _, li := range []library.ListedVideo{
		{RemoteID: "a", Title: "Short", SourceID: src.ID, MediaType: "short"},
		{RemoteID: "b", Title: "Video", SourceID: src.ID, MediaType: "video"},
		{RemoteID: "c", Title: "None", SourceID: src.ID, MediaType: ""},
	} {
		if _, err := s.UpsertListed(ser.ID, li, taskID); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{}, 50, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all: %d err=%v", len(all), err)
	}
	shorts, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{MediaType: "short"}, 50, 0)
	if err != nil || len(shorts) != 1 || shorts[0].MediaType != "short" {
		t.Fatalf("short filter: %+v err=%v", shorts, err)
	}
	types, err := s.ListSeriesMediaTypes(ser.ID)
	if err != nil || len(types) != 2 || types[0] != "short" || types[1] != "video" {
		t.Fatalf("series types: %v err=%v", types, err)
	}
}
