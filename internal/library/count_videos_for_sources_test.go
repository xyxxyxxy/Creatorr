package library

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCountVideosForSources(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "cnt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	s := NewStore(d, q)
	ser, err := s.CreateSeries(CreateSeriesParams{
		Title: "Count Src", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@countsrc",
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	wanted, err := s.UpsertListed(ser.ID, ListedVideo{
		RemoteID: "w1", Title: "Wanted", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	dl, err := s.UpsertListed(ser.ID, ListedVideo{
		RemoteID: "d1", Title: "Downloaded", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusDownloaded, dl.VideoID); err != nil {
		t.Fatal(err)
	}
	_ = wanted
	indexed, downloaded, err := s.CountVideosForSources([]int64{srcID})
	if err != nil {
		t.Fatal(err)
	}
	if indexed != 2 || downloaded != 1 {
		t.Fatalf("indexed=%d downloaded=%d want 2/1", indexed, downloaded)
	}
	indexed, downloaded, err = s.CountVideosForSources(nil)
	if err != nil || indexed != 0 || downloaded != 0 {
		t.Fatalf("empty ids: %d/%d err=%v", indexed, downloaded, err)
	}
}
