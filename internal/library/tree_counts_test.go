package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestCountSeriesByRootAndFilesByVideo(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "tc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	root := t.TempDir()
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: root})
	lib := library.NewStore(d, queue.NewStore(d))
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Count Show", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@count",
	})
	if err != nil {
		t.Fatal(err)
	}
	byRoot, err := lib.CountSeriesByRootIDs([]int64{1})
	if err != nil || byRoot[1] < 1 {
		t.Fatalf("byRoot=%v err=%v", byRoot, err)
	}
	up, err := lib.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "c1", Title: "C1", SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO files (series_id, video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, ?, ?, 'nfo', datetime('now'), 1)
	`, ser.ID, up.VideoID, filepath.Join(root, "c1.nfo")); err != nil {
		t.Fatal(err)
	}
	byVid, err := lib.CountFilesByVideoIDs([]int64{up.VideoID})
	if err != nil || byVid[up.VideoID] != 1 {
		t.Fatalf("byVid=%v err=%v", byVid, err)
	}
}
