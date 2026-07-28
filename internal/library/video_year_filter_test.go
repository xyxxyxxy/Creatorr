package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestVideoListFilterYear(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "YearFilt", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@yearfilt"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	for _, li := range []library.ListedVideo{
		{RemoteID: "y24", Title: "A", SourceID: src.ID, UploadDate: "2024-06-15T12:00:00Z"},
		{RemoteID: "y25a", Title: "B", SourceID: src.ID, UploadDate: "2025-01-01T00:00:00Z"},
		{RemoteID: "y25b", Title: "C", SourceID: src.ID, UploadDate: "2025-12-31T23:59:59Z"},
		{RemoteID: "none", Title: "D", SourceID: src.ID, UploadDate: ""},
	} {
		if _, err := s.UpsertListed(ser.ID, li, taskID); err != nil {
			t.Fatal(err)
		}
	}
	years, unknown, err := s.DistinctVideoYears(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 2 || years[0] != 2025 || years[1] != 2024 {
		t.Fatalf("years=%v want [2025 2024]", years)
	}
	if !unknown {
		t.Fatal("want unknown year option when undated videos exist")
	}
	got, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Year: 2025}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("2025 filter: %d videos", len(got))
	}
	got24, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Year: 2024}, 50, 0)
	if err != nil || len(got24) != 1 || got24[0].RemoteID != "y24" {
		t.Fatalf("2024 filter: %+v err=%v", got24, err)
	}
	gotNone, err := s.ListVideosPageFiltered(ser.ID, library.VideoListFilter{Year: library.VideoYearUnknown}, 50, 0)
	if err != nil || len(gotNone) != 1 || gotNone[0].RemoteID != "none" {
		t.Fatalf("unknown filter: %+v err=%v", gotNone, err)
	}
	n, err := s.CountVideosFiltered(ser.ID, library.VideoListFilter{Year: 2025})
	if err != nil || n != 2 {
		t.Fatalf("count 2025=%d err=%v", n, err)
	}
	nUnk, err := s.CountVideosFiltered(ser.ID, library.VideoListFilter{Year: library.VideoYearUnknown})
	if err != nil || nUnk != 1 {
		t.Fatalf("count unknown=%d err=%v", nUnk, err)
	}
}
