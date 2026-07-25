package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSubtitleLangAndExt(t *testing.T) {
	if got := library.SubtitleLangAndExt("/tmp/meta.en.vtt", "meta"); got != ".en.vtt" {
		t.Fatalf("got %q", got)
	}
	if got := library.SubtitleLangAndExt("/tmp/Show [id].en-US.srt", "Show [id]"); got != ".en-US.srt" {
		t.Fatalf("got %q", got)
	}
	if got := library.SubtitleLangAndExt("/tmp/meta.en.auto.srt", "meta"); got != ".en.auto.srt" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkAutoSubtitleFiles(t *testing.T) {
	dir := t.TempDir()
	info := filepath.Join(dir, "clip.info.json")
	manual := filepath.Join(dir, "clip.en.srt")
	autoOnly := filepath.Join(dir, "clip.de.srt")
	if err := os.WriteFile(info, []byte(`{
		"subtitles": {"en": [{"ext":"srt"}]},
		"automatic_captions": {"en": [{"ext":"srt"}], "de": [{"ext":"srt"}]}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manual, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(autoOnly, []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := library.MarkAutoSubtitleFiles([]string{manual, autoOnly}, info)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != manual {
		t.Fatalf("manual renamed: %q", got[0])
	}
	wantAuto := filepath.Join(dir, "clip.de.auto.srt")
	if got[1] != wantAuto {
		t.Fatalf("auto path=%q want %q", got[1], wantAuto)
	}
	if _, err := os.Stat(wantAuto); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(autoOnly); !os.IsNotExist(err) {
		t.Fatal("old auto path should be gone")
	}
	// idempotent
	got2 := library.MarkAutoSubtitleFiles(got, info)
	if got2[1] != wantAuto {
		t.Fatalf("second pass %q", got2[1])
	}
}


func TestRefreshDiskSidecarsRemovesOrphansAndKeepsLang(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "SideOrphan",
		SourceURL:        "https://www.example.com/@sideorphan",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "Ep Title", Description: "plot",
		WebpageURL: "https://www.example.com/watch?v=vid1", UploadDate: "2024-01-15",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	seriesDir := filepath.Join(root.Path, "SideOrphan")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].mkv")
	oldThumb := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1]-thumb.jpg")
	oldSub := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].de.vtt")
	for path, body := range map[string]string{media: "VIDEO", oldThumb: "OLD", oldSub: "OLD"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, kindPath := range [][2]string{
		{"video", media},
		{"thumb", oldThumb},
		{"sub", oldSub},
	} {
		if _, err := s.DB.SQL.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, ?, datetime('now'))
		`, res.VideoID, kindPath[1], kindPath[0]); err != nil {
			t.Fatal(err)
		}
	}

	tmp := t.TempDir()
	newSub := filepath.Join(tmp, "meta.en.vtt")
	if err := os.WriteFile(newSub, []byte("WEBVTT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshDiskSidecars(res.VideoID, library.SidecarBundle{
		SubSrcs: []string{newSub},
	}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldThumb); !os.IsNotExist(err) {
		t.Fatal("old thumb should be removed")
	}
	if _, err := os.Stat(oldSub); !os.IsNotExist(err) {
		t.Fatal("old sub should be removed")
	}
	wantSub := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].en.vtt")
	if _, err := os.Stat(wantSub); err != nil {
		t.Fatalf("new sub missing: %v", err)
	}
}

func TestRefreshDiskSidecarsPreservesInfoJSON(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "SideJSON",
		SourceURL:        "https://www.example.com/@sidejson",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "Ep Title", Description: "plot",
		WebpageURL: "https://www.example.com/watch?v=vid1", UploadDate: "2024-01-15",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "SideJSON")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].mkv")
	info := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].info.json")
	orig := `{"id":"vid1","provenance":"download"}`
	for path, body := range map[string]string{media: "VIDEO", info: orig} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, kindPath := range [][2]string{{"video", media}, {"json", info}} {
		if _, err := s.DB.SQL.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, ?, datetime('now'))
		`, res.VideoID, kindPath[1], kindPath[0]); err != nil {
			t.Fatal(err)
		}
	}
	tmp := t.TempDir()
	newSub := filepath.Join(tmp, "meta.en.vtt")
	if err := os.WriteFile(newSub, []byte("WEBVTT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshDiskSidecars(res.VideoID, library.SidecarBundle{
		InfoJSON: []byte(`{"id":"vid1","provenance":"should-not-write"}`),
		SubSrcs:  []string{newSub},
	}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(info)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Fatalf("info.json changed: %q", got)
	}
	var n int
	if err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ? AND kind = 'json'`, res.VideoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("json file row count = %d", n)
	}
}
