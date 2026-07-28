package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
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

func TestEnsureVideoDomainTag(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "DomainTag",
		SourceURL:        "https://www.example.com/@d",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "d1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=d1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.EnsureVideoDomainTag(res.VideoID, "https://www.example.com/watch?v=d1")
	if err != nil || !ok {
		t.Fatalf("ensure: ok=%v err=%v", ok, err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tags) != 1 || v.Tags[0] != "example.com" {
		t.Fatalf("tags=%v", v.Tags)
	}

	if err := settings.Set(s.DB, settings.KeyMetadataDomainTag, "0"); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET tags = '[]' WHERE id = ?`, res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err = s.EnsureVideoDomainTag(res.VideoID, "https://www.example.com/watch?v=d1")
	if err != nil || ok {
		t.Fatalf("disabled: ok=%v err=%v", ok, err)
	}
}
