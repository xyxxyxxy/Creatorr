package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestOverviewTotals(t *testing.T) {
	s := openLib(t)

	got, err := s.OverviewTotals()
	if err != nil {
		t.Fatal(err)
	}
	if got.SeriesCount != 0 || got.VideoCount != 0 || got.SizeBytes != 0 {
		t.Fatalf("empty library totals=%+v", got)
	}

	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'v1', 'One', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE series_id = ?`, ser.ID).Scan(&videoID); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes)
		VALUES (?, '/tmp/one.mkv', 'video', datetime('now'), 1500)
	`, videoID)
	if err != nil {
		t.Fatal(err)
	}

	got, err = s.OverviewTotals()
	if err != nil {
		t.Fatal(err)
	}
	if got.SeriesCount != 1 || got.VideoCount != 1 || got.SizeBytes != 1500 {
		t.Fatalf("totals=%+v want series=1 video=1 size=1500", got)
	}
}
