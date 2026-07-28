package library_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestFileSyncSizeMismatchMarksVerifyFailed(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SizedSync", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "ep.mkv")
	if err := os.WriteFile(media, []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'sz1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'sz1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, ?, 'video', ?, 100)
	`, videoID, media, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	tid := seedTaskID(t, s)
	res, err := s.FileSyncPass(tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ExternallyChangedIDs) != 1 || res.ExternallyChangedIDs[0] != videoID {
		t.Fatalf("externally_changed=%v", res.ExternallyChangedIDs)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "verify_failed" {
		t.Fatalf("status=%s want verify_failed", v.Status)
	}
	n, ok, err := s.VideoSizeBytes(videoID)
	if err != nil || !ok || n != 4 {
		t.Fatalf("size=%d ok=%v err=%v want 4", n, ok, err)
	}
	var histEvent string
	if err := s.DB.SQL.QueryRow(`
		SELECT event FROM video_history WHERE video_id = ? AND event = 'file_externally_changed'
	`, videoID).Scan(&histEvent); err != nil {
		t.Fatal(err)
	}

	// Second pass: same bytes → no new change.
	res2, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.ExternallyChangedIDs) != 0 {
		t.Fatalf("second pass changed=%v want empty", res2.ExternallyChangedIDs)
	}
}

func TestFileSyncNullSizeBackfillsQuietly(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NullSize", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "ep.mkv")
	payload := []byte("hello-world")
	if err := os.WriteFile(media, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'ns1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'ns1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, ?, 'video', ?, NULL)
	`, videoID, media, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ExternallyChangedIDs) != 0 || res.Total() != 0 {
		t.Fatalf("backfill should not count as change: %+v", res)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "downloaded" {
		t.Fatalf("status=%s want downloaded", v.Status)
	}
	n, ok, err := s.VideoSizeBytes(videoID)
	if err != nil || !ok || n != int64(len(payload)) {
		t.Fatalf("size=%d ok=%v err=%v want %d", n, ok, err, len(payload))
	}
	var histN int
	_ = s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM video_history WHERE video_id = ? AND event = 'file_externally_changed'
	`, videoID).Scan(&histN)
	if histN != 0 {
		t.Fatalf("unexpected file_externally_changed history count=%d", histN)
	}
}

func TestFileSyncMissingAndSizeInSamePass(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Mix", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}

	insertVideo := func(remote, status, path string, size any) int64 {
		t.Helper()
		_, err := s.DB.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status, season, episode)
			VALUES (?, ?, ?, ?, 2026, 1)
		`, ser.ID, remote, remote, status)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = ?`, remote).Scan(&id); err != nil {
			t.Fatal(err)
		}
		_, err = s.DB.SQL.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes)
			VALUES (?, ?, 'video', ?, ?)
		`, id, path, time.Now().UTC().Format(time.RFC3339Nano), size)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	gone := filepath.Join(root.Path, "gone.mkv")
	changedPath := filepath.Join(root.Path, "changed.mkv")
	if err := os.WriteFile(changedPath, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingID := insertVideo("gone", "downloaded", gone, int64(10))
	changedID := insertVideo("chg", "downloaded", changedPath, int64(99))

	res, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.MissingIDs) != 1 || res.MissingIDs[0] != missingID {
		t.Fatalf("missing=%v", res.MissingIDs)
	}
	if len(res.ExternallyChangedIDs) != 1 || res.ExternallyChangedIDs[0] != changedID {
		t.Fatalf("changed=%v", res.ExternallyChangedIDs)
	}
}
