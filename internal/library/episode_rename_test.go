package library_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestAlignSeriesYearEpisodesRenamesPeers(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Align", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@align"})
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "late", Title: "Late", SourceID: src.ID,
		UploadDate: "2024-03-15T18:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seasonDir := filepath.Join(root.Path, "Align", "S2024")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldMedia := filepath.Join(seasonDir, "S2024E0001 [late].mkv")
	if err := os.WriteFile(oldMedia, []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, late.VideoID)
	_, err = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		late.VideoID, oldMedia, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	early, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "early", Title: "Early", SourceID: src.ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel Apply auto-queued by UpsertListed so Align can move peers itself.
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'rename_episodes' AND status IN ('pending', 'running')`)

	earlyMedia := filepath.Join(seasonDir, "S2024E0001 [early].mkv")
	if err := os.WriteFile(earlyMedia, []byte("early"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, early.VideoID)
	_, err = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		early.VideoID, earlyMedia, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	tid := seedTaskID(t, s)
	leftovers, err := s.AlignSeriesYearEpisodes(ser.ID, 2024, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("leftovers=%v", leftovers)
	}
	var latePath, earlyPath string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, late.VideoID).Scan(&latePath)
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, early.VideoID).Scan(&earlyPath)
	if !strings.Contains(latePath, "E0002") {
		t.Fatalf("late should be episode 2 stem: %q", latePath)
	}
	if !strings.Contains(earlyPath, "E0001") {
		t.Fatalf("early should stay episode 1: %q", earlyPath)
	}
	if !fileExistsTest(latePath) || !fileExistsTest(earlyPath) {
		t.Fatalf("media missing after align late=%q early=%q", latePath, earlyPath)
	}
}

func TestAlignSeriesYearEpisodesNoOpWhenApplyCovers(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Covered", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@cov"})
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "late", Title: "Late", SourceID: src.ID,
		UploadDate: "2024-03-15T18:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seasonDir := filepath.Join(root.Path, "Covered", "S2024")
	_ = os.MkdirAll(seasonDir, 0o755)
	oldMedia := filepath.Join(seasonDir, "old.mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, late.VideoID)
	_, _ = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		late.VideoID, oldMedia, "2024-01-01T00:00:00Z")

	_, err = s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "early", Title: "Early", SourceID: src.ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// UpsertListed already queued Apply covering the packed late video.
	tid := seedTaskID(t, s)
	leftovers, err := s.AlignSeriesYearEpisodes(ser.ID, 2024, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("leftovers=%v", leftovers)
	}
	var path string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, late.VideoID).Scan(&path)
	if path != oldMedia {
		t.Fatalf("peer-move must no-op when Apply covers series, got %q", path)
	}
}

func TestHealMissingEpisodePathFromTempStem(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Heal", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@heal"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "One", SourceID: src.ID,
		UploadDate: "2024-01-10T12:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "Heal")
	_ = os.MkdirAll(seriesDir, 0o755)
	tempMedia := filepath.Join(seriesDir, ".creatorr-renaming-"+strconv.FormatInt(res.VideoID, 10)+".mkv")
	if err := os.WriteFile(tempMedia, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(seriesDir, "S2024", "gone.mkv")
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, res.VideoID)
	_, _ = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		res.VideoID, stale, "2024-01-01T00:00:00Z")

	tid := seedTaskID(t, s)
	leftovers, err := s.AlignSeriesYearEpisodes(ser.ID, 2024, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("leftovers=%v", leftovers)
	}
	var path string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, res.VideoID).Scan(&path)
	if path == "" || path == stale || !fileExistsTest(path) {
		t.Fatalf("expected heal+rename to ideal, path=%q", path)
	}
	if strings.Contains(path, ".creatorr-renaming-") {
		t.Fatalf("should leave temp stem: %q", path)
	}
}
