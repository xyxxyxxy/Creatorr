package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/testutil/fakemedia"
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
	p, aired, durationSec, err := library.ParseEpisodeNFOFile(path)
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
	if durationSec != 0 {
		t.Fatalf("durationSec=%d want 0", durationSec)
	}
}

func TestParseEpisodeNFOFileDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ep.nfo")
	body := `<?xml version="1.0"?>
<episodedetails>
  <title>Timed</title>
  <runtime>3</runtime>
  <fileinfo>
    <streamdetails>
      <video>
        <durationinseconds>125</durationinseconds>
      </video>
    </streamdetails>
  </fileinfo>
</episodedetails>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, durationSec, err := library.ParseEpisodeNFOFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if durationSec != 125 {
		t.Fatalf("durationSec=%d want 125 (prefer durationinseconds over runtime)", durationSec)
	}

	runtimeOnly := filepath.Join(dir, "runtime.nfo")
	if err := os.WriteFile(runtimeOnly, []byte(`<?xml version="1.0"?><episodedetails><title>R</title><runtime>2</runtime></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, durationSec, err = library.ParseEpisodeNFOFile(runtimeOnly)
	if err != nil {
		t.Fatal(err)
	}
	if durationSec != 120 {
		t.Fatalf("durationSec=%d want 120 from runtime minutes", durationSec)
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
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(dir, "Ep.nfo")
	src := `<?xml version="1.0"?><episodedetails>
  <title>From NFO</title>
  <plot>Imported plot</plot>
  <studio>Import Studio</studio>
  <genre>Action</genre>
  <genre>Drama</genre>
  <tag>import-tag</tag>
  <country>US</country>
  <mpaa>TV-14</mpaa>
  <tagline>Tag line</tagline>
  <actor><name>Ann</name><role>Host</role><order>0</order></actor>
  <uniqueid type="yt-dlp" default="true">nfo1</uniqueid>
  <fileinfo><streamdetails><video><durationinseconds>97</durationinseconds></video></streamdetails></fileinfo>
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
	if len(v.Genres) != 2 || v.Genres[0] != "Action" || v.Country != "US" || v.MPAA != "TV-14" || v.Tagline != "Tag line" {
		t.Fatalf("catalog fields=%+v genres=%v", v, v.Genres)
	}
	if len(v.Tags) != 1 || v.Tags[0] != "import-tag" {
		t.Fatalf("tags=%v", v.Tags)
	}
	if len(v.Actors) != 1 || v.Actors[0].Name != "Ann" {
		t.Fatalf("actors=%v", v.Actors)
	}
	if !v.DurationSeconds.Valid || v.DurationSeconds.Int64 != 97 {
		t.Fatalf("duration=%v want 97", v.DurationSeconds)
	}
	got, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "Imported plot") == false {
		t.Fatalf("regenerated nfo missing plot: %s", got)
	}
	// Regenerated NFO should be Creatorr format, not raw source-only tags.
	if !strings.Contains(string(got), "<episodedetails>") || strings.Contains(string(got), "<showtitle>") {
		t.Fatalf("want Creatorr episode nfo without showtitle, got %s", got)
	}
}

func TestApplyImportNFOOverridesUploadDate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Date Show", SourceURL: "https://example.com/date", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, upload_date, season, episode)
		VALUES (?, 'date1', 'Old Date', 'downloaded', '2020-01-01T00:00:00Z', 2020, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'date1'`).Scan(&videoID)

	dir := filepath.Join(root.Path, "Date Show")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "Ep.mkv")
	_ = os.WriteFile(media, []byte("media"), 0o644)
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfo := filepath.Join(dir, "Ep.nfo")
	src := `<?xml version="1.0"?><episodedetails>
  <title>From NFO</title>
  <aired>2024-06-15</aired>
  <uniqueid type="yt-dlp" default="true">date1</uniqueid>
</episodedetails>`
	if err := os.WriteFile(nfo, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyImportNFO(videoID, nfo, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.UploadDate.Valid || !strings.HasPrefix(v.UploadDate.String, "2024-06-15") {
		t.Fatalf("upload_date=%v want 2024-06-15… (NFO aired overrides prior)", v.UploadDate)
	}
	if !v.Season.Valid || v.Season.Int64 != 2024 {
		t.Fatalf("season=%v want 2024 after reindex", v.Season)
	}
}

func TestSoftFillDurationFromMedia(t *testing.T) {
	fakemedia.PrependPATH(t)
	t.Setenv("FAKE_FFPROBE_DURATION", "2")
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Probe Show", SourceURL: "https://example.com/probe", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'probe1', 'Probe Ep', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'probe1'`).Scan(&videoID)

	dir := filepath.Join(root.Path, "Probe Show")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "Ep.mkv")
	fakemedia.WriteDummyMedia(t, media)
	if err := s.CompleteImport(videoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftFillDurationFromMedia(context.Background(), videoID, media); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.DurationSeconds.Valid || v.DurationSeconds.Int64 != 2 {
		t.Fatalf("duration=%v want 2s (fake ffprobe)", v.DurationSeconds)
	}
	// Second call must not overwrite.
	if err := s.SetDurationSeconds(videoID, 99); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftFillDurationFromMedia(context.Background(), videoID, media); err != nil {
		t.Fatal(err)
	}
	v, err = s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.DurationSeconds.Valid || v.DurationSeconds.Int64 != 99 {
		t.Fatalf("soft-fill overwrote: %v", v.DurationSeconds)
	}
}
