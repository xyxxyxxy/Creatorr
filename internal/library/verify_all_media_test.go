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
	id, err := s.EnqueueVerifyAllMedia(queue.OriginManual)
	if err != nil || id <= 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	_, err = s.EnqueueVerifyAllMedia(queue.OriginManual)
	if err == nil {
		t.Fatal("want duplicate")
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != queue.KindIntegrityCheck || task.Domain != queue.SystemDomain {
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
	if err := s.CompleteImport(res.VideoID, media, "", "", "", nil, library.MediaCompleteMeta{}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	tid := seedTaskID(t, s)
	if err := s.MarkVerifyFailed(res.VideoID, tid, "Media verify failed", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkVerified(res.VideoID, tid, nil); err != nil {
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
	id, err := s.EnqueueVerifyAllMedia(queue.OriginManual)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.VerifyAllMediaPass(context.Background(), task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IntegrityChecked != 0 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("verified=%d skipped=%d failed=%d", res.IntegrityChecked, res.Skipped, res.Failed)
	}
}

func TestVerifyAllMediaPassReportsCumulativeProgressOnResume(t *testing.T) {
	s := openLib(t)
	id, err := s.EnqueueVerifyAllMedia(queue.OriginManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Queue.UpdatePayload(id, map[string]any{
		"cursor":              42,
		"integrity_checked":   800,
		"partial":             2,
		"skipped":             5,
		"failed":              1,
		"skipped_busy":        0,
		"skipped_profile_off": 0,
		"skipped_no_media":    0,
	}); err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil || task == nil {
		t.Fatalf("get: %v", err)
	}
	var msgs []string
	res, err := s.VerifyAllMediaPass(context.Background(), task, func(msg string, _ *float64) {
		msgs = append(msgs, msg)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IntegrityChecked != 800 || res.Partial != 2 || res.Skipped != 5 || res.Failed != 1 {
		t.Fatalf("counters=%+v", res)
	}
	if len(msgs) < 1 || msgs[0] != "Integrity check 808…" {
		t.Fatalf("want first live msg with cumulative 808, got %v", msgs)
	}
}
