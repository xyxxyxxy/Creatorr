package library_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestFileSyncSidecarMissingRestoreAndSize(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SidecarSync", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "ep.mkv")
	nfo := filepath.Join(root.Path, "ep.nfo")
	thumb := filepath.Join(root.Path, "ep-thumb.jpg")
	if err := os.WriteFile(media, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfo, []byte("nfo-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumb, []byte("thumb"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'sc1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'sc1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range []struct {
		path, kind string
		size       any
	}{
		{media, "video", int64(5)},
		{nfo, "nfo", int64(8)},
		{thumb, "thumb", int64(99)}, // wrong size on purpose
	} {
		if _, err := s.DB.SQL.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes)
			VALUES (?, ?, ?, ?, ?)
		`, videoID, row.path, row.kind, now, row.size); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Remove(nfo); err != nil {
		t.Fatal(err)
	}

	tid := seedTaskID(t, s)
	res, err := s.FileSyncPass(tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SidecarMissing) != 1 || res.SidecarMissing[0].Kind != "nfo" {
		t.Fatalf("missing=%v", res.SidecarMissing)
	}
	if len(res.SidecarChanged) != 1 || res.SidecarChanged[0].Kind != "thumb" {
		t.Fatalf("changed=%v", res.SidecarChanged)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "downloaded" {
		t.Fatalf("status=%s want downloaded (sidecar-only)", v.Status)
	}
	var nfoSize int64
	if err := s.DB.SQL.QueryRow(`SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoSize); err != nil {
		t.Fatal(err)
	}
	if nfoSize != -1 {
		t.Fatalf("nfo size_bytes=%d want -1 sentinel", nfoSize)
	}
	var thumbSize int64
	if err := s.DB.SQL.QueryRow(`SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'thumb'`, videoID).Scan(&thumbSize); err != nil {
		t.Fatal(err)
	}
	if thumbSize != 5 {
		t.Fatalf("thumb size_bytes=%d want 5", thumbSize)
	}

	// Second pass: no re-alert for known-missing / already-updated sizes.
	res2, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.SidecarMissing) != 0 || len(res2.SidecarChanged) != 0 {
		t.Fatalf("second pass missing=%v changed=%v", res2.SidecarMissing, res2.SidecarChanged)
	}

	if err := os.WriteFile(nfo, []byte("nfo-restored"), 0o644); err != nil {
		t.Fatal(err)
	}
	res3, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.SidecarRestored) != 1 || res3.SidecarRestored[0].Kind != "nfo" {
		t.Fatalf("restored=%v", res3.SidecarRestored)
	}
	if err := s.DB.SQL.QueryRow(`SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoSize); err != nil {
		t.Fatal(err)
	}
	if nfoSize != int64(len("nfo-restored")) {
		t.Fatalf("nfo size after restore=%d", nfoSize)
	}
	var hist string
	if err := s.DB.SQL.QueryRow(`
		SELECT event FROM video_history WHERE video_id = ? AND event = 'sidecar_restored'
	`, videoID).Scan(&hist); err != nil {
		t.Fatal(err)
	}
}

func TestFileSyncSidecarNullSizeBackfillsQuietly(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SidecarNull", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(root.Path, "ep.mkv")
	nfo := filepath.Join(root.Path, "ep.nfo")
	payload := []byte("sidecar-bytes")
	if err := os.WriteFile(media, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfo, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'scn', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'scn'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'video', ?, 1)
	`, videoID, media, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'nfo', ?, NULL)
	`, videoID, nfo, now); err != nil {
		t.Fatal(err)
	}

	res, err := s.FileSyncPass(seedTaskID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SidecarMissing) != 0 || len(res.SidecarChanged) != 0 || res.Total() != 0 {
		t.Fatalf("want quiet backfill, got %#v", res)
	}
	var nfoSize int64
	if err := s.DB.SQL.QueryRow(`SELECT size_bytes FROM files WHERE video_id = ? AND kind = 'nfo'`, videoID).Scan(&nfoSize); err != nil {
		t.Fatal(err)
	}
	if nfoSize != int64(len(payload)) {
		t.Fatalf("nfo size=%d want %d", nfoSize, len(payload))
	}
}
