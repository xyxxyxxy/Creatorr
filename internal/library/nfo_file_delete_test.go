package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestNFORegeneratePassSkipsUnchanged(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NFOSkip", SourceURL: "https://www.example.com/@nfosk",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "n1", Title: "Ep One", Description: "plot",
		WebpageURL: "https://www.example.com/watch?v=n1", UploadDate: "2024-06-01T12:00:00Z",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "NFOSkip")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(seriesDir, "ep.mkv")
	if err := os.WriteFile(media, []byte("V"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', datetime('now'))
	`, res.VideoID, media); err != nil {
		t.Fatal(err)
	}
	changed, err := s.RewriteVideoNFO(res.VideoID, seedTaskID(t, s))
	if err != nil || !changed {
		t.Fatalf("first write changed=%v err=%v", changed, err)
	}
	_, _ = s.DB.SQL.Exec(`DELETE FROM video_history WHERE video_id = ?`, res.VideoID)

	tid, err := s.EnqueueRegenerateNFO()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	rewrote, skipped, failed, err := s.NFORegeneratePass(context.Background(), task, nil)
	if err != nil || failed != 0 || rewrote < 1 {
		// series tvshow may rewrite; episode should skip
		t.Fatalf("rewrote=%d skipped=%d failed=%d err=%v", rewrote, skipped, failed, err)
	}
	if skipped < 1 {
		t.Fatalf("expected episode skip, skipped=%d", skipped)
	}
	var histN int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM video_history WHERE video_id = ? AND event = 'nfo_regenerated'`, res.VideoID).Scan(&histN)
	if histN != 0 {
		t.Fatalf("skip should not write history, got %d", histN)
	}

	// Force rewrite by stale file
	nfo := filepath.Join(seriesDir, "ep.nfo")
	if err := os.WriteFile(nfo, []byte("STALE"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = s.RewriteVideoNFO(res.VideoID, tid)
	if err != nil || !changed {
		t.Fatalf("stale rewrite changed=%v err=%v", changed, err)
	}
	var histTask int64
	_ = s.DB.SQL.QueryRow(`SELECT task_id FROM video_history WHERE video_id = ? AND event = 'nfo_regenerated'`, res.VideoID).Scan(&histTask)
	if histTask != tid {
		t.Fatalf("history task_id=%d want %d", histTask, tid)
	}
}

func TestFileDeletePassMarksDeletedWithTaskID(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "FileDel", SourceURL: "https://www.example.com/@fd",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "fd1", Title: "Gone", WebpageURL: "https://www.example.com/v/fd1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "FileDel", "Gone [fd1].mkv")
	if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(res.VideoID, media, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteVideo(res.VideoID); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Queue.ListActiveFileDelete()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	if tasks[0].Kind != queue.KindDeleteFiles {
		t.Fatalf("kind=%s", tasks[0].Kind)
	}
	if err := s.FileDeletePass(context.Background(), &tasks[0], nil); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil || v.Status != "deleted" {
		t.Fatalf("status=%v err=%v", v, err)
	}
	var histTask int64
	_ = s.DB.SQL.QueryRow(`SELECT task_id FROM video_history WHERE video_id = ? AND event = 'file_deleted'`, res.VideoID).Scan(&histTask)
	if histTask != tasks[0].ID {
		t.Fatalf("task_id=%d want %d", histTask, tasks[0].ID)
	}
}
