package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSoftFillVideoGenresFromCategories(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "GenresFill",
		SourceURL:        "https://www.example.com/@g",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "g1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=g1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}

	ok, err := s.SoftFillVideoGenresFromCategories(res.VideoID, []string{"Education", " Science ", "education"})
	if err != nil || !ok {
		t.Fatalf("first fill: ok=%v err=%v", ok, err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Genres) != 2 || v.Genres[0] != "Education" || v.Genres[1] != "Science" {
		t.Fatalf("genres=%v", v.Genres)
	}

	ok, err = s.SoftFillVideoGenresFromCategories(res.VideoID, []string{"News"})
	if err != nil || ok {
		t.Fatalf("second fill must no-op: ok=%v err=%v", ok, err)
	}
	v, err = s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Genres) != 2 || v.Genres[0] != "Education" {
		t.Fatalf("genres clobbered: %v", v.Genres)
	}
}

func TestSoftFillVideoGenresFromInfoJSON(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "GenresJSON",
		SourceURL:        "https://www.example.com/@gj",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "gj1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=gj1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	info := filepath.Join(dir, "ep.info.json")
	if err := os.WriteFile(info, []byte(`{"categories":["Education","Tech"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := s.SoftFillVideoGenresFromInfoJSON(res.VideoID, info)
	if err != nil || !ok {
		t.Fatalf("fill from json: ok=%v err=%v", ok, err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Genres) != 2 || v.Genres[0] != "Education" || v.Genres[1] != "Tech" {
		t.Fatalf("genres=%v", v.Genres)
	}
}
