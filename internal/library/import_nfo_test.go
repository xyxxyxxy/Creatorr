package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestParseEpisodeNFOFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ep.nfo")
	body := `<?xml version="1.0"?>
<episodedetails>
  <title>Hello</title>
  <sorttitle>Hello, The</sorttitle>
  <plot>Plot line</plot>
  <studio>Studio X</studio>
  <genre>Action</genre>
  <genre>Drama</genre>
  <tag>a</tag>
  <aired>2024-03-15</aired>
  <uniqueid type="yt-dlp" default="true">abc</uniqueid>
  <actor><name>Ann</name><role>Host</role><order>0</order></actor>
</episodedetails>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, aired, err := library.ParseEpisodeNFOFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Hello" || p.Plot != "Plot line" || p.Studio != "Studio X" {
		t.Fatalf("meta=%+v", p)
	}
	if len(p.Genres) != 2 || p.Genres[0] != "Action" {
		t.Fatalf("genres=%v", p.Genres)
	}
	if p.UniqueIDType != "yt-dlp" || p.UniqueIDValue != "abc" {
		t.Fatalf("uniqueid=%s/%s", p.UniqueIDType, p.UniqueIDValue)
	}
	if len(p.Actors) != 1 || p.Actors[0].Name != "Ann" {
		t.Fatalf("actors=%v", p.Actors)
	}
	if aired != "2024-03-15" {
		t.Fatalf("aired=%q", aired)
	}
}

func TestApplyImportNFOUpdatesDBAndRegenerates(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NFO Show", SourceURL: "https://example.com/nfo", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, description)
		VALUES (?, 'nfo1', 'Old Title', 'downloaded', 'old plot')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'nfo1'`).Scan(&videoID)

	dir := filepath.Join(root.Path, "NFO Show")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "Ep.mkv")
	_ = os.WriteFile(media, []byte("media"), 0o644)
	if err := s.CompleteImport(videoID, media, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(dir, "Ep.nfo")
	src := `<?xml version="1.0"?><episodedetails>
  <title>From NFO</title>
  <plot>Imported plot</plot>
  <studio>Import Studio</studio>
  <uniqueid type="yt-dlp" default="true">nfo1</uniqueid>
</episodedetails>`
	if err := os.WriteFile(nfo, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	taskID := seedTaskID(t, s)
	if err := s.ApplyImportNFO(videoID, nfo, taskID); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "From NFO" || v.Description != "Imported plot" || v.Studio != "Import Studio" {
		t.Fatalf("video=%+v", v)
	}
	got, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "Imported plot") == false {
		t.Fatalf("regenerated nfo missing plot: %s", got)
	}
	// Regenerated NFO should be Creatorr format, not raw source-only tags.
	if !strings.Contains(string(got), "<episodedetails>") || !strings.Contains(string(got), "<showtitle>") {
		t.Fatalf("want Creatorr episode nfo, got %s", got)
	}
}
