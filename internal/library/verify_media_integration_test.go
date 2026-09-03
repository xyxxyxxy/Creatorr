package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestMarkVerifyFailedKeepsFilesNoThreshold(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "VF", SourceURL: "https://www.example.com/@vf", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vf1", Title: "One", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "lib")
	_ = os.MkdirAll(dir, 0o755)
	media := filepath.Join(dir, "ep.mkv")
	if err := os.WriteFile(media, []byte("MEDIA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(res.VideoID, media, "", "", "", nil, library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	tid := seedTaskID(t, s)
	if err := s.MarkVerifyFailed(res.VideoID, tid, "Media verify failed"); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "verify_failed" {
		t.Fatalf("status=%s", v.Status)
	}
	if _, err := os.Stat(media); err != nil {
		t.Fatal("file must be kept on verify fail")
	}
	hist, err := s.ListVideoHistory(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range hist {
		if e.Event == library.VideoHistVerifyFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing verify_failed history")
	}
	res2, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "vf2", Title: "Two", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := s.GetVideo(res2.VideoID)
	if v2.Status != "wanted" {
		t.Fatalf("sibling status=%s", v2.Status)
	}
}

func TestEnqueueMediaVerifyDuplicate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "EV", SourceURL: "https://www.example.com/@ev", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ev1", Title: "One", SourceID: ser.Sources[0].ID,
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
	id, err := s.EnqueueMediaVerify(res.VideoID)
	if err != nil || id <= 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	_, err = s.EnqueueMediaVerify(res.VideoID)
	if err == nil {
		t.Fatal("want duplicate")
	}
	task, err := s.Queue.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != queue.KindMediaVerify || task.Priority != queue.PriorityMediaVerify {
		t.Fatalf("%#v", task)
	}
}
