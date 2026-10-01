package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSoftFillVideoMetaFromSourcePreset(t *testing.T) {
	preset := library.Source{
		Studio:         "StudioA",
		Country:        "US",
		MPAA:           "TV-14",
		Genres:         []string{"News"},
		Tags:           []string{"example.com", "extra"},
		Actors:         []library.SeriesActor{{Name: "Ann", Role: "Host"}},
		SpecialFeature: "extras",
	}
	studio, country, mpaa, role, genres, tags, actors, changed := library.SoftFillVideoMetaFromSourcePreset(
		"", "", "", "", nil, nil, nil, preset,
	)
	if !changed {
		t.Fatal("expected change")
	}
	if studio != "StudioA" || country != "US" || mpaa != "TV-14" || role != "extras" {
		t.Fatalf("singles: %q %q %q %q", studio, country, mpaa, role)
	}
	if len(genres) != 1 || genres[0] != "News" {
		t.Fatalf("genres=%v", genres)
	}
	if len(tags) != 2 || tags[0] != "example.com" {
		t.Fatalf("tags=%v", tags)
	}
	if len(actors) != 1 || actors[0].Name != "Ann" || actors[0].Role != "Host" {
		t.Fatalf("actors=%v", actors)
	}

	// Existing values not clobbered; lists union; actor role fill-if-empty.
	studio, country, mpaa, role, genres, tags, actors, changed = library.SoftFillVideoMetaFromSourcePreset(
		"Keep", "CA", "TV-MA", "special_episode",
		[]string{"Drama"}, []string{"keep"},
		[]library.SeriesActor{{Name: "Ann", Role: ""}},
		preset,
	)
	if !changed {
		t.Fatal("expected union/role change")
	}
	if studio != "Keep" || country != "CA" || mpaa != "TV-MA" || role != "special_episode" {
		t.Fatalf("kept singles: %q %q %q %q", studio, country, mpaa, role)
	}
	if len(genres) < 2 {
		t.Fatalf("genres union=%v", genres)
	}
	if len(tags) < 2 {
		t.Fatalf("tags union=%v", tags)
	}
	if actors[0].Role != "Host" {
		t.Fatalf("actor role fill=%q", actors[0].Role)
	}
}

func TestApplySourceMetadataPresetOnInsert(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "PresetApply",
		SourceURL:        "https://www.example.com/@p",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src := ser.Sources[0]
	studio := "PresetStudio"
	tags := []string{"example.com", "preset-tag"}
	genres := []string{"Tech"}
	_, err = s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{
		Studio: &studio,
		Tags:   &tags,
		Genres: &genres,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "p1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=p1",
		SourceID: src.ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Studio != "PresetStudio" {
		t.Fatalf("studio=%q", v.Studio)
	}
	foundTag := false
	for _, tag := range v.Tags {
		if tag == "preset-tag" {
			foundTag = true
		}
	}
	if !foundTag {
		t.Fatalf("tags=%v want preset-tag", v.Tags)
	}
	foundGenre := false
	for _, g := range v.Genres {
		if g == "Tech" {
			foundGenre = true
		}
	}
	if !foundGenre {
		t.Fatalf("genres=%v want Tech", v.Genres)
	}
}
