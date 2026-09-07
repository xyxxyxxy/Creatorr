package library_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestFileContentHashNotSetOnRegister(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Hash", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.Path, "blob.info.json")
	payload := []byte("abc")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'h1', 'Ep', 'downloaded', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'h1'`).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterFileKind(videoID, path, "json"); err != nil {
		t.Fatal(err)
	}
	var fileID int64
	var hash sql.NullString
	if err := s.DB.SQL.QueryRow(`SELECT id, content_hash FROM files WHERE video_id = ? AND kind = 'json'`, videoID).Scan(&fileID, &hash); err != nil {
		t.Fatal(err)
	}
	if hash.Valid {
		t.Fatalf("register must leave content_hash NULL, got %q", hash.String)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	if err := s.SetFileContentHash(fileID, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.FileContentHash(fileID)
	if err != nil || !ok || got != want {
		t.Fatalf("stored hash=%q ok=%v err=%v", got, ok, err)
	}
}

func TestVerifyAllMediaPassSkipsProfileOff(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s) // CreateProfile defaults verify_media off
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "SkipOff", SourceURL: "https://www.example.com/@skip", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "skip1", Title: "One", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "lib")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "ep.mkv")
	_ = os.WriteFile(media, []byte("MEDIA"), 0o644)
	if err := s.CompleteImport(res.VideoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueVerifyAllMedia(queue.OriginManual)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	resPass, err := s.VerifyAllMediaPass(context.Background(), task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resPass.IntegrityChecked != 0 || resPass.Failed != 0 || resPass.Skipped < 1 {
		t.Fatalf("verified=%d skipped=%d failed=%d want skip", resPass.IntegrityChecked, resPass.Skipped, resPass.Failed)
	}
	if task.Kind != queue.KindIntegrityCheck {
		t.Fatalf("kind=%s", task.Kind)
	}
}
