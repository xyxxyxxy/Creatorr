package library_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestVideoMediaHasContentHash(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "HashInd", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Path, "ep.mkv")
	if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'hi1', 'Ep', 'downloaded')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'hi1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	ok, err := s.VideoMediaHasContentHash(videoID)
	if err != nil || ok {
		t.Fatalf("before register: ok=%v err=%v", ok, err)
	}
	if err := s.RegisterFileKind(videoID, path, "video"); err != nil {
		t.Fatal(err)
	}
	ok, err = s.VideoMediaHasContentHash(videoID)
	if err != nil || ok {
		t.Fatalf("after register no hash: ok=%v err=%v", ok, err)
	}
	var fileID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM files WHERE video_id = ? AND kind = 'video'`, videoID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFileContentHash(fileID, "abc123"); err != nil {
		t.Fatal(err)
	}
	ok, err = s.VideoMediaHasContentHash(videoID)
	if err != nil || !ok {
		t.Fatalf("after hash: ok=%v err=%v", ok, err)
	}
}

func TestLastIntegrityCheckAt(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "LastCheck", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'lc1', 'Ep', 'downloaded')
	`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'lc1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LastIntegrityCheckAt(videoID); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	tid, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind: queue.KindIntegrityCheck, Domain: queue.SystemDomain, Origin: queue.OriginManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	newer := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'integrity_checked', 'old', '{}', ?)
	`, videoID, older, tid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, 'integrity_check_failed', 'new', '{}', ?)
	`, videoID, newer, tid); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LastIntegrityCheckAt(videoID)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !got.Equal(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
}
