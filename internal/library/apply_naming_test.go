package library_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestBuildEpisodePathsRelDefault(t *testing.T) {
	root := t.TempDir()
	paths, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Hello World", Season: 2024, Episode: 3, UniqueID: "abc",
	}, library.NamingConfig{
		EpisodeFormat: library.DefaultEpisodeFormat,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStem := "S2024E0003 [abc]"
	if paths.Stem != wantStem {
		t.Fatalf("stem %q want %q", paths.Stem, wantStem)
	}
	wantDir := filepath.Join(root, "Show", "S2024")
	if paths.EpisodeDir != wantDir {
		t.Fatalf("episode dir %q want %q", paths.EpisodeDir, wantDir)
	}
	if paths.SeriesDir != filepath.Join(root, "Show") {
		t.Fatalf("series dir %q", paths.SeriesDir)
	}
	if paths.PrimaryBase != filepath.Join(wantDir, wantStem) {
		t.Fatalf("primary %q", paths.PrimaryBase)
	}
}

func TestBuildEpisodePathsWithDateParts(t *testing.T) {
	root := t.TempDir()
	paths, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Ep", Season: 2024, Episode: 1, UniqueID: "x",
		Aired: "2024-03-15T18:00:00Z", Domain: "www.example.com",
	}, library.NamingConfig{
		EpisodeFormat: "{date} [{domain}] - {title}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if paths.Stem != "2024-03-15 [example.com] - Ep" {
		t.Fatalf("stem %q", paths.Stem)
	}
	if paths.EpisodeDir != filepath.Join(root, "Show") {
		t.Fatalf("flat format should land in series dir: %q", paths.EpisodeDir)
	}
}

func TestBuildEpisodePathsNestedSeason(t *testing.T) {
	root := t.TempDir()
	paths, err := library.BuildEpisodePaths(root, library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Ep", Season: 2, Episode: 5, UniqueID: "id1",
	}, library.NamingConfig{
		EpisodeFormat: "Season {year}/{title} [{id}]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(paths.SeasonDir) != "Season 2" {
		t.Fatalf("season dir %q", paths.SeasonDir)
	}
	if paths.SeriesDir != filepath.Join(root, "Show") {
		t.Fatalf("series dir %q", paths.SeriesDir)
	}
	if paths.Stem != "Ep [id1]" {
		t.Fatalf("stem %q", paths.Stem)
	}
}

func TestApplyEpisodeNamingRenamesAndHistory(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	fmtStr := library.DefaultEpisodeFormat
	if _, err := s.UpdateRoot(root.ID, nil, nil, &fmtStr, nil, false); err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "OldShow", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@old",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "r1", Title: "Hello", WebpageURL: "https://www.example.com/watch?v=r1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vid := res.VideoID
	seriesDir := filepath.Join(root.Path, "OldShow")
	_ = os.MkdirAll(seriesDir, 0o755)
	oldMedia := filepath.Join(seriesDir, "old.mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 1, episode = 1 WHERE id = ?`, vid)
	_, err = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`, vid, oldMedia, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	tid, err := s.EnqueueRenameEpisodes()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'running' WHERE id = ?`, tid)
	task.Status = queue.StatusRunning

	renamed, skipped, failed, err := s.ApplyEpisodeNamingPass(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if renamed != 1 || skipped != 0 || failed != 0 {
		t.Fatalf("renamed=%d skipped=%d failed=%d", renamed, skipped, failed)
	}
	hist, err := s.ListVideoHistory(vid)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hist {
		if h.Event == "renamed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected renamed history")
	}
	var newPath string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, vid).Scan(&newPath)
	if newPath == oldMedia || newPath == "" {
		t.Fatalf("path not updated: %q", newPath)
	}
	if !fileExists(newPath) {
		t.Fatalf("missing new file %q", newPath)
	}
}

func TestSystemApplyDuplicateRejected(t *testing.T) {
	s := openLib(t)
	if _, err := s.EnqueueRenameEpisodes(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueRenameEpisodes(); err == nil {
		t.Fatal("expected duplicate")
	}
}

func TestScopedRenameCoexistsWithFullApply(t *testing.T) {
	s := openLib(t)
	if _, err := s.EnqueueRenameEpisodes(); err != nil {
		t.Fatal(err)
	}
	tid, err := s.EnqueueRenameEpisodesVideos([]int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if tid <= 0 {
		t.Fatal("expected scoped task")
	}
	tid2, err := s.EnqueueRenameEpisodesVideos([]int64{2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if tid2 != tid {
		t.Fatalf("expected merge into pending scoped task, got %d want %d", tid2, tid)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.Payload, "3") {
		t.Fatalf("merged payload missing id 3: %s", task.Payload)
	}
}

func TestDisambiguateEpisodeBase(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "ep")
	if err := os.WriteFile(base+".mkv", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, n, err := library.DisambiguateEpisodeBase(base, ".mkv", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || got != base+"_1" {
		t.Fatalf("got %q n=%d", got, n)
	}
}

func TestPackMediaCollisionSuffix(t *testing.T) {
	root := t.TempDir()
	meta := library.EpisodeNFO{
		SeriesTitle: "Show", Title: "Ep", Season: 2024, Episode: 3, UniqueID: "abc",
	}
	cfg := library.NamingConfig{EpisodeFormat: library.DefaultEpisodeFormat}
	paths, err := library.BuildEpisodePaths(root, meta, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.EpisodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PrimaryBase+".mkv", []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "src.mkv")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaPath, _, _, _, _, suffix, err := library.PackMedia(src, root, meta, cfg, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if suffix != 1 {
		t.Fatalf("suffix=%d", suffix)
	}
	if !strings.HasSuffix(mediaPath, "_1.mkv") {
		t.Fatalf("mediaPath=%q", mediaPath)
	}
}

func TestEnqueueRetentionDeleteSkipsWithoutTTL(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueRetentionDelete(queue.PriorityRetentionDeleteDue)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected skip without TTL, got task %d", id)
	}
	ttl := int64(3600)
	if _, err := s.CreateRoot("with-ttl", t.TempDir(), "", &ttl); err != nil {
		t.Fatal(err)
	}
	id, err = s.EnqueueRetentionDelete(queue.PriorityRetentionDeleteDue)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected enqueue when a root has TTL")
	}
}

func TestEnqueueSyncFilesSkipsWithoutVideos(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueSyncFiles(queue.PrioritySyncFilesDue)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected skip with no videos, got task %d", id)
	}
	root, err := s.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'v1', 'One', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, err = s.EnqueueSyncFiles(queue.PrioritySyncFilesDue)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected enqueue when library has videos")
	}
}
