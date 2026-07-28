package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestStageAndEnqueueSponsorblockCut(t *testing.T) {
	s := openLib(t)
	s.CacheDir = t.TempDir()
	rootID, profID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CutSeries", RootID: rootID, QualityProfileID: profID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status, season, episode)
		VALUES (?, 'cut-remote', 'Cut Vid', 'wanted', 2026, 1)
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var vid int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE remote_id = 'cut-remote'`).Scan(&vid); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	media := filepath.Join(work, "vid.webm")
	if err := os.WriteFile(media, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(work, "vid.info.json")
	if err := os.WriteFile(info, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := s.StageSponsorblockCut(vid, media, info, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if staged.MediaPath == "" || staged.StageDir == "" {
		t.Fatalf("%#v", staged)
	}
	if _, err := os.Stat(staged.MediaPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(media); !os.IsNotExist(err) {
		t.Fatal("source media should be moved")
	}

	staged.SeriesID = ser.ID
	id, err := s.EnqueueSponsorblockCut(staged)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Queue.GetTask(id)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	if task.Kind != queue.KindSponsorblockCut || task.Domain != queue.SystemDomain {
		t.Fatalf("%#v", task)
	}
	if task.Priority != queue.PrioritySponsorblockCut {
		t.Fatalf("priority=%d", task.Priority)
	}

	s.RemoveSponsorblockCutStaging(vid)
	if _, err := os.Stat(s.SponsorblockCutStageDir(vid)); !os.IsNotExist(err) {
		t.Fatal("staging should be gone")
	}
}
