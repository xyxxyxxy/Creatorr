package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestRefreshDiskSidecarsRewritesBesideVideo(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Side",
		SourceURL:        "https://www.example.com/@side",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vid1", Title: "Ep Title", Description: "plot text",
		WebpageURL: "https://www.example.com/watch?v=vid1", UploadDate: "2024-01-15",
SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	seriesDir := filepath.Join(root.Path, "Side")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1].mkv")
	if err := os.WriteFile(media, []byte("VIDEO"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))
	`, res.VideoID, media)
	if err != nil {
		t.Fatal(err)
	}
	oldNFO := strings.TrimSuffix(media, ".mkv") + ".nfo"
	_ = os.WriteFile(oldNFO, []byte("OLD"), 0o644)

	tmp := t.TempDir()
	info := filepath.Join(tmp, "meta.info.json")
	thumb := filepath.Join(tmp, "meta.jpg")
	_ = os.WriteFile(info, []byte(`{"id":"vid1","title":"Ep Title"}`), 0o644)
	_ = os.WriteFile(thumb, []byte("JPG"), 0o644)

	if err := s.RefreshDiskSidecars(res.VideoID, library.SidecarBundle{
		InfoJSON: []byte(`{"id":"vid1","title":"Ep Title"}`),
		ThumbSrc: thumb,
	}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	nfoBody, err := os.ReadFile(oldNFO)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nfoBody), "Ep Title") || !strings.Contains(string(nfoBody), "plot text") {
		t.Fatalf("nfo missing fields: %s", nfoBody)
	}
	if _, err := os.Stat(strings.TrimSuffix(media, ".mkv") + ".info.json"); !os.IsNotExist(err) {
		t.Fatal("info.json must not be written on sidecar refresh")
	}
	if _, err := os.Stat(filepath.Join(seriesDir, "S2024E015 - Ep Title [vid1]-thumb.jpg")); err != nil {
		t.Fatal("thumb missing")
	}
	// video bytes untouched
	got, _ := os.ReadFile(media)
	if string(got) != "VIDEO" {
		t.Fatalf("video mutated: %q", got)
	}
}

func TestRegenerateAllNFOs(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Regen", SourceURL: "https://www.example.com/@regen",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "r1", Title: "New Title", Description: "new plot",
		WebpageURL: "https://www.example.com/watch?v=r1", UploadDate: "2024-06-01T12:00:00Z",
SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "Regen")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(seriesDir, "ep.mkv")
	nfo := filepath.Join(seriesDir, "ep.nfo")
	if err := os.WriteFile(media, []byte("V"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfo, []byte("STALE"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))
	`, res.VideoID, media)
	if err != nil {
		t.Fatal(err)
	}

	tid, err := s.EnqueueRegenerateNFO()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	rewrote, skipped, failed, err := s.NFORegeneratePass(context.Background(), task, nil)
	if err != nil || failed != 0 || rewrote != 2 {
		t.Fatalf("rewrote=%d skipped=%d failed=%d err=%v (want rewrote=2: episode + tvshow)", rewrote, skipped, failed, err)
	}
	body, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "New Title") || !strings.Contains(string(body), "new plot") {
		t.Fatalf("nfo not regenerated: %s", body)
	}
	tvshow, err := os.ReadFile(filepath.Join(seriesDir, "tvshow.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tvshow), "<title>Regen</title>") {
		t.Fatalf("tvshow.nfo: %s", tvshow)
	}
	if err := s.Queue.UpdatePayload(tid, map[string]any{
		"video_cursor":  0,
		"series_cursor": 0,
		"phase":         "videos",
		"rewrote":       0,
		"skipped":       0,
		"failed":        0,
	}); err != nil {
		t.Fatal(err)
	}
	task2, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	rewrote2, skipped2, failed2, err := s.NFORegeneratePass(context.Background(), task2, nil)
	if err != nil || failed2 != 0 || rewrote2 != 0 || skipped2 < 2 {
		t.Fatalf("second pass want all skipped: rewrote=%d skipped=%d failed=%d err=%v", rewrote2, skipped2, failed2, err)
	}
}
