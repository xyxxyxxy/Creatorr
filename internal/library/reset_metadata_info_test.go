package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestResetVideoMetadataFromInfoJSON(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ResetMeta", SourceURL: "https://www.example.com/@rm",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "rm1", Title: "Operator Title", WebpageURL: "https://www.example.com/watch?v=rm1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID

	_, err = s.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{
		Title: "Operator Title", Plot: "Operator plot", Studio: "OpStudio",
		SortTitle: "Sort Op", OriginalTitle: "Orig Op",
		Genres: []string{"OperatorGenre", "KeepMe"}, Tags: []string{"OpTag"},
		Actors: []library.SeriesActor{{Name: "Alice", Role: "Host", Order: 0}},
		Tagline: "Op tagline", Country: "US", MPAA: "TV-14",
		UniqueIDType: "custom", UniqueIDValue: "abc",
		UploadDate: "2020-01-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateVideoNotes(vid, "keep these notes"); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root.Path, "ResetMeta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	info := filepath.Join(dir, "ep.info.json")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{
  "id": "rm1",
  "title": "From Info Title",
  "description": "From info plot",
  "upload_date": "20210601",
  "thumbnail": "https://cdn.example.com/t.jpg",
  "categories":["Education"],
  "tags":["info-tag"],
  "duration": 125.4,
  "width": 1920,
  "height": 1080,
  "fps": 30,
  "media_type": "video"
}`
	if err := os.WriteFile(info, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, vid, media); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'json', datetime('now'))`, vid, info); err != nil {
		t.Fatal(err)
	}

	out, err := s.ResetVideoMetadataFromInfoJSON(vid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Skipped {
		t.Fatal("expected reset, got skipped")
	}

	v, err := s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "From Info Title" {
		t.Fatalf("title=%q", v.Title)
	}
	if v.Description != "From info plot" {
		t.Fatalf("description=%q", v.Description)
	}
	if v.Notes != "keep these notes" {
		t.Fatalf("notes wiped: %q", v.Notes)
	}
	if v.Studio != "" || v.SortTitle != "" || v.OriginalTitle != "" || v.Tagline != "" || v.Country != "" || v.MPAA != "" {
		t.Fatalf("extras not wiped: studio=%q sort=%q orig=%q tagline=%q country=%q mpaa=%q",
			v.Studio, v.SortTitle, v.OriginalTitle, v.Tagline, v.Country, v.MPAA)
	}
	if v.UniqueIDType != "" || v.UniqueIDValue != "" {
		t.Fatalf("uniqueid not cleared: %q/%q", v.UniqueIDType, v.UniqueIDValue)
	}
	if len(v.Actors) != 0 {
		t.Fatalf("actors not wiped: %v", v.Actors)
	}
	if len(v.Genres) != 1 || v.Genres[0] != "Education" {
		t.Fatalf("genres want replace [Education], got %v", v.Genres)
	}
	if len(v.Tags) != 1 || v.Tags[0] != "info-tag" {
		t.Fatalf("tags want replace [info-tag], got %v", v.Tags)
	}
	if !v.UploadDate.Valid || library.UploadCalendarDate(v.UploadDate.String) != "2021-06-01" {
		t.Fatalf("upload=%v", v.UploadDate)
	}
	if !v.ThumbnailURL.Valid || v.ThumbnailURL.String != "https://cdn.example.com/t.jpg" {
		t.Fatalf("thumb=%v", v.ThumbnailURL)
	}
	if !v.DurationSeconds.Valid || v.DurationSeconds.Int64 != 125 {
		t.Fatalf("duration=%v", v.DurationSeconds)
	}
	if library.NormalizePackRole(v.PackRole) != library.PackRoleRegular {
		t.Fatalf("pack role=%q", v.PackRole)
	}
}

func TestResetVideoMetadataFromInfoJSONSkipNoJSON(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ResetSkip", SourceURL: "https://www.example.com/@rs",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "rs1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=rs1",
		SourceID: ser.Sources[0].ID,
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ResetVideoMetadataFromInfoJSON(res.VideoID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Skipped {
		t.Fatal("expected skip without info.json")
	}
}

func TestResetVideoMetadataFromInfoJSONClearUploadDate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ResetClearDate", SourceURL: "https://www.example.com/@rcd",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "rcd1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=rcd1",
		SourceID: ser.Sources[0].ID, UploadDate: "2020-03-01T12:00:00Z",
	}, seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	dir := filepath.Join(root.Path, "ResetClearDate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "ep.mkv")
	info := filepath.Join(dir, "ep.info.json")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No upload_date in info.json → clear.
	if err := os.WriteFile(info, []byte(`{"id":"rcd1","title":"Ep","categories":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))`, vid, media); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'json', datetime('now'))`, vid, info); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ResetVideoMetadataFromInfoJSON(vid, 0); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(vid)
	if err != nil {
		t.Fatal(err)
	}
	if v.UploadDate.Valid {
		t.Fatalf("upload_date should clear, got %v", v.UploadDate)
	}
	if v.Season.Valid || v.Episode.Valid {
		t.Fatalf("season/episode should clear: season=%v episode=%v", v.Season, v.Episode)
	}
}
