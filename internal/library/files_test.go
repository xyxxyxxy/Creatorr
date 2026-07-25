package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.0 GiB"},
	}
	for _, tc := range cases {
		if got := library.FormatBytes(tc.n); got != tc.want {
			t.Errorf("FormatBytes(%d)=%q want %q", tc.n, got, tc.want)
		}
	}
}

func TestCompleteDownloadStoresVideoSizeBytes(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Sized", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	media := filepath.Join(dir, "video.mkv")
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(media, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(dir, "video.nfo")
	if err := os.WriteFile(nfo, []byte("<movie/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'r1', 'T', 'wanted', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'r1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	if err := s.CompleteDownload(videoID, media, nfo, "", "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	var histEvent, histMsg string
	if err := s.DB.SQL.QueryRow(`
		SELECT event, message FROM video_history WHERE video_id = ? ORDER BY id DESC LIMIT 1
	`, videoID).Scan(&histEvent, &histMsg); err != nil {
		t.Fatal(err)
	}
	if histEvent != "packed" || histMsg != "Packed to library" {
		t.Fatalf("history event=%q msg=%q want packed / Packed to library", histEvent, histMsg)
	}

	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.AcquiredAt.Valid || strings.TrimSpace(v.AcquiredAt.String) == "" {
		t.Fatal("expected acquired_at set on pack")
	}

	n, ok, err := s.VideoSizeBytes(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || n != 4096 {
		t.Fatalf("VideoSizeBytes=%d ok=%v want 4096", n, ok)
	}

	var nfoSize any
	err = s.DB.SQL.QueryRow(`SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoSize)
	if err != nil {
		t.Fatal(err)
	}
	if nfoSize != nil {
		t.Fatalf("nfo size_bytes=%v want NULL", nfoSize)
	}

	m, err := s.VideoSizeBytesMap([]int64{videoID})
	if err != nil {
		t.Fatal(err)
	}
	if m[videoID] != 4096 {
		t.Fatalf("map=%v", m)
	}
}

func TestCompleteDownloadSoftFillsMediaType(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Typed", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	media := filepath.Join(dir, "video.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := filepath.Join(dir, "video.nfo")
	if err := os.WriteFile(nfo, []byte("<movie/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(dir, "video.info.json")
	if err := os.WriteFile(info, []byte(`{"id":"r2","title":"T","media_type":"short","duration":12,"width":1080,"height":1920}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode, media_type)
		VALUES (?, 'r2', 'T', 'wanted', 2026, 1, '')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'r2'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	if err := s.CompleteDownload(videoID, media, nfo, info, "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.MediaType != "short" {
		t.Fatalf("media_type=%q want short", v.MediaType)
	}
}

func TestListVideoSidecarsStemPrefix(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Stem", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stem := "Show S01E01 [abc]"
	media := filepath.Join(dir, stem+".mkv")
	nfo := filepath.Join(dir, stem+".nfo")
	info := filepath.Join(dir, stem+".info.json")
	thumb := filepath.Join(dir, stem+"-thumb.webp")
	sb := filepath.Join(dir, stem+".sponsorblock.json")
	orphan := filepath.Join(dir, stem+".extra.txt")
	other := filepath.Join(dir, "unrelated.nfo")
	for path, body := range map[string]string{
		media: "media", nfo: "<nfo/>", info: `{}`, thumb: "img", sb: `{"version":1}`, orphan: "x", other: "no",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'stem1', 'T', 'wanted', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'stem1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteDownload(videoID, media, nfo, info, thumb, nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, sb, "sponsorblock"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListVideoSidecars(videoID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	paths := map[string]bool{}
	var sbID int64
	for _, f := range got {
		kinds[f.Kind] = true
		paths[f.Path] = true
		if f.Kind == "sponsorblock" {
			sbID = f.ID
		}
		if f.Path == media {
			t.Fatalf("media listed as sidecar: %s", f.Path)
		}
		if f.Path == other {
			t.Fatalf("unrelated file listed: %s", f.Path)
		}
	}
	for _, want := range []string{"nfo", "json", "thumb", "sponsorblock", "other"} {
		if !kinds[want] {
			t.Fatalf("missing kind %q in %#v", want, got)
		}
	}
	if !paths[orphan] {
		t.Fatal("expected disk-only orphan sidecar")
	}
	if sbID <= 0 {
		t.Fatal("sponsorblock should keep DB id")
	}
	if library.InferEpisodeSidecarKind(stem+".sponsorblock.json") != "sponsorblock" {
		t.Fatal("InferEpisodeSidecarKind sponsorblock")
	}
}
