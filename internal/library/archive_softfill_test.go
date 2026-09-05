package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSoftFillArchiveMetaFromInfoJSONPlaceholderTitle(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ArchMeta", SourceURL: "https://www.youtube.com/@am",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rid := "dQw4w9WgXcQ"
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: rid, Title: rid, WebpageURL: "https://www.youtube.com/watch?v=" + rid,
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(t.TempDir(), "info.json")
	body := `{
  "id": "` + rid + `",
  "title": "Real Title From Archive",
  "description": "Archive plot",
  "upload_date": "20200115",
  "thumbnail": "https://cdn.example.com/t.jpg"
}`
	if err := os.WriteFile(info, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftFillArchiveMetaFromInfoJSON(res.VideoID, info); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Real Title From Archive" {
		t.Fatalf("title=%q", v.Title)
	}
	if v.Description != "Archive plot" {
		t.Fatalf("description=%q", v.Description)
	}
	if !v.ThumbnailURL.Valid || v.ThumbnailURL.String != "https://cdn.example.com/t.jpg" {
		t.Fatalf("thumb=%v", v.ThumbnailURL)
	}
	if !v.UploadDate.Valid || library.UploadCalendarDate(v.UploadDate.String) != "2020-01-15" {
		t.Fatalf("upload=%v", v.UploadDate)
	}
}
