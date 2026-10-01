package library_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// seedDownloadedSeries creates series "Old" with one packed video on disk under its folder.
func seedDownloadedSeries(t *testing.T, s *library.Store, rootID, profileID int64) (*library.Series, int64, string) {
	t.Helper()
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Old", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@old"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "Ep", SourceID: src.ID, UploadDate: "2024-03-15T08:00:00Z",
		WebpageURL: "https://www.example.com/watch?v=v1",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root.Path, "Old", "S2024")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "S2024E0001 [v1].mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		res.VideoID, media, "2024-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Scan / Apply tasks queued by seeding must not block the move.
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE status IN ('pending', 'running')`); err != nil {
		t.Fatal(err)
	}
	return ser, res.VideoID, media
}

func TestEnqueueSeriesMoveLeavesTitleAndLocks(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, _, _ := seedDownloadedSeries(t, s, rootID, profileID)
	other, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Other", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProfile("alt", "bv*")
	if err != nil {
		t.Fatal(err)
	}

	title := "New"
	out, err := s.UpdateSeriesDetailed(ser.ID, library.UpdateSeriesParams{Title: &title, QualityProfileID: &p2.ID})
	if err != nil || !out.MoveQueued || out.MoveTaskID <= 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if out.Series.Title != "Old" || out.Series.QualityProfileID != p2.ID {
		t.Fatalf("HTTP path must keep title and update quality: %+v", out.Series)
	}
	if busy, _ := s.SeriesHasBlockingTasks(ser.ID); !busy {
		t.Fatal("series should be blocked while move pending")
	}
	if busy, _ := s.SeriesHasBlockingTasks(other.ID); busy {
		t.Fatal("other series must not be blocked")
	}

	// Second move on the same series is refused.
	t2 := "Newer"
	if _, err := s.EnqueueSeriesMove(ser.ID, t2, 0); !errors.Is(err, library.ErrSeriesBusy) {
		t.Fatalf("second move: %v", err)
	}
	if _, err := s.UpdateSeriesDetailed(ser.ID, library.UpdateSeriesParams{Title: &t2}); !errors.Is(err, library.ErrSeriesBusy) {
		t.Fatalf("update during move: %v", err)
	}
	// Other path-touching kinds are refused while the move is open.
	if _, err := s.EnqueueRenameEpisodesSeries(other.ID); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("rename during move: %v", err)
	}
	if _, err := s.Queue.Enqueue(queue.EnqueueParams{
		Origin: queue.OriginManual, Kind: queue.KindSyncFiles, Domain: queue.SystemDomain,
	}); !errors.Is(err, queue.ErrDuplicate) {
		t.Fatalf("sync_files during move: %v", err)
	}
	// Moves are refused while another path-touching kind is open.
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = ?`, queue.KindSeriesMove); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueRenameEpisodesSeries(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueSeriesMove(ser.ID, "Third", 0); !errors.Is(err, library.ErrSeriesBusy) && !errors.Is(err, library.ErrConflict) {
		t.Fatalf("move during rename: %v", err)
	}
}

func TestSeriesMovePassHappyPath(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, vid, media := seedDownloadedSeries(t, s, rootID, profileID)
	oldRoot, _ := s.GetRoot(rootID)
	newRoot, err := s.CreateRoot("other", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	tid, err := s.EnqueueSeriesMove(ser.ID, "New Name", newRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SeriesMovePass(context.Background(), task, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSeries(ser.ID, false)
	if err != nil || got.Title != "New Name" || got.RootID != newRoot.ID {
		t.Fatalf("series=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(oldRoot.Path, "Old")); !os.IsNotExist(err) {
		t.Fatalf("old folder should be gone: %v", err)
	}
	var path string
	if err := s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, vid).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path == media || filepath.Dir(filepath.Dir(path)) != filepath.Join(newRoot.Path, "New Name") {
		t.Fatalf("file path not moved: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("moved media missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newRoot.Path, "New Name", "tvshow.nfo")); err != nil {
		t.Fatalf("tvshow.nfo: %v", err)
	}
}

func TestSeriesMovePassFailureRevertsDB(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, _, media := seedDownloadedSeries(t, s, rootID, profileID)
	root, _ := s.GetRoot(rootID)

	tid, err := s.EnqueueSeriesMove(ser.ID, "Taken", rootID)
	if err != nil {
		t.Fatal(err)
	}
	// Target folder appears on disk after enqueue, so MoveSeriesFolder refuses.
	if err := os.MkdirAll(filepath.Join(root.Path, "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	task, _ := s.Queue.GetTask(tid)
	if _, _, _, err := s.SeriesMovePass(context.Background(), task, nil); err == nil {
		t.Fatal("expected move failure")
	}
	got, err := s.GetSeries(ser.ID, false)
	if err != nil || got.Title != "Old" || got.RootID != rootID {
		t.Fatalf("title/root not reverted: %+v err=%v", got, err)
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatalf("media must stay: %v", err)
	}
}

func TestDownloadAndVideoMetadataRefusedWhileMovePending(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, vid, _ := seedDownloadedSeries(t, s, rootID, profileID)
	// A wanted video in the same series for Download now.
	srcRow, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@old2"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v2", Title: "Two", SourceID: srcRow.ID, WebpageURL: "https://www.example.com/watch?v=v2",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE status IN ('pending', 'running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IgnoreVideo(res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueSeriesMove(ser.ID, "Moved", 0); err != nil {
		t.Fatal(err)
	}

	if _, err := s.EnqueueDownloadNow(res.VideoID); !errors.Is(err, library.ErrSeriesMoveBusy) {
		t.Fatalf("download now: %v", err)
	}
	if _, err := s.EnqueueDownload(res.VideoID); !errors.Is(err, library.ErrSeriesMoveBusy) {
		t.Fatalf("download: %v", err)
	}
	// Want works but never enqueues a download.
	v, err := s.WantVideo(res.VideoID)
	if err != nil || v.Status != library.StatusWanted {
		t.Fatalf("want: %+v err=%v", v, err)
	}
	if n, err := s.EnqueueDownloadWanted(); err != nil || n != 0 {
		t.Fatalf("download wanted during move: n=%d err=%v", n, err)
	}
	// Path-affecting metadata is refused.
	if _, err := s.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{Title: "Renamed Ep"}); !errors.Is(err, library.ErrSeriesMoveBusy) {
		t.Fatalf("metadata: %v", err)
	}
}

func TestBulkEditRootChangeEnqueuesSeriesMove(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	a, err := s.CreateSeries(library.CreateSeriesParams{Title: "A", RootID: rootID, QualityProfileID: profileID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSeries(library.CreateSeriesParams{Title: "B", RootID: rootID, QualityProfileID: profileID, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	newRoot, err := s.CreateRoot("other", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bulkID, err := s.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs: []int64{a.ID, b.ID}, RootID: &newRoot.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := s.Queue.GetTask(bulkID)
	updated, skipped, failed, err := s.BulkEditSeriesPass(context.Background(), task, nil)
	if err != nil || updated != 2 || skipped != 0 || failed != 0 {
		t.Fatalf("pass updated=%d skipped=%d failed=%d err=%v", updated, skipped, failed, err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		got, _ := s.GetSeries(id, false)
		if got.RootID != rootID {
			t.Fatalf("series %d root changed before worker move", id)
		}
		var n int
		if err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = ? AND series_id = ? AND status = 'pending'`,
			queue.KindSeriesMove, id).Scan(&n); err != nil || n != 1 {
			t.Fatalf("series %d series_move count=%d err=%v", id, n, err)
		}
	}
}
