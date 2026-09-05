package library_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestEnqueueVerifyAllMediaDuplicate(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueVerifyAllMedia()
	if err != nil || id <= 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	_, err = s.EnqueueVerifyAllMedia()
	if err == nil {
		t.Fatal("want duplicate")
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != queue.KindVerifyAllMedia || task.Domain != queue.SystemDomain {
		t.Fatalf("%#v", task)
	}
}

func TestMarkVerifiedRestoresFromVerifyFailed(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MV", SourceURL: "https://www.example.com/@mv", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "mv1", Title: "One", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "lib")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "ep.mkv")
	_ = os.WriteFile(media, []byte("MEDIA"), 0o644)
	if err := s.CompleteImport(res.VideoID, media, "", "", "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	tid := seedTaskID(t, s)
	if err := s.MarkVerifyFailed(res.VideoID, tid, "Media verify failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkVerified(res.VideoID, tid); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "downloaded" {
		t.Fatalf("status=%s want downloaded", v.Status)
	}
}

func TestVerifyAllMediaPassSkipsEmpty(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueVerifyAllMedia()
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	verified, skipped, failed, err := s.VerifyAllMediaPass(context.Background(), task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verified != 0 || skipped != 0 || failed != 0 {
		t.Fatalf("verified=%d skipped=%d failed=%d", verified, skipped, failed)
	}
}
