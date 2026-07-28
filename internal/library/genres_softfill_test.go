package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
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
	if err != nil || !ok {
		t.Fatalf("merge fill: ok=%v err=%v", ok, err)
	}
	v, err = s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Genres) != 3 || v.Genres[0] != "News" || v.Genres[1] != "Education" || v.Genres[2] != "Science" {
		t.Fatalf("genres merged: %v", v.Genres)
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

func TestSaveVideoMetadataClearedGenresStayCleared(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearGenres", SourceURL: "https://www.example.com/@cg",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "cg1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=cg1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "ClearGenres")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	info := filepath.Join(dir, "ep.info.json")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(info, []byte(`{"categories":["Science & Technology"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, res.VideoID, media); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'json', datetime('now'))`, res.VideoID, info); err != nil {
		t.Fatal(err)
	}
	ok, err := s.SoftFillVideoGenresFromPackedInfo(res.VideoID)
	if err != nil || !ok {
		t.Fatalf("seed fill: ok=%v err=%v", ok, err)
	}

	if err := settings.Set(s.DB, settings.KeyMetadataGenresFromCategories, "0"); err != nil {
		t.Fatal(err)
	}

	_, err = s.SaveVideoMetadata(res.VideoID, library.SaveVideoMetadataParams{
		Title: "Ep", Genres: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Genres) != 0 {
		t.Fatalf("cleared genres came back: %v", v.Genres)
	}
}
