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
	if got.SeriesCount != 0 || got.VideoCount != 0 || got.DownloadedCount != 0 || got.SizeBytes != 0 {
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
		VALUES (?, 'v1', 'One', 'wanted'), (?, 'v2', 'Two', 'downloaded')
	`, ser.ID, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE series_id = ? AND remote_id = 'v2'`, ser.ID).Scan(&videoID); err != nil {
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
	if got.SeriesCount != 1 || got.VideoCount != 2 || got.DownloadedCount != 1 || got.SizeBytes != 1500 {
		t.Fatalf("totals=%+v", got)
	}
}

func TestListMostWantedSeries(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	few, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Few Wanted", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	many, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Many Wanted", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	none, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "None Wanted", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(sid int64, rid, status string) {
		t.Helper()
		if _, err := s.DB.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status)
			VALUES (?, ?, ?, ?)
		`, sid, rid, rid, status); err != nil {
			t.Fatal(err)
		}
	}
	insert(few.ID, "f1", "wanted")
	insert(many.ID, "m1", "wanted")
	insert(many.ID, "m2", "wanted_archive")
	insert(many.ID, "m3", "downloaded")
	insert(none.ID, "n1", "downloaded")

	got, err := s.ListMostWantedSeries(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (same order as Browser sort=wanted)", len(got))
	}
	if got[0].ID != many.ID || got[0].WantedCount != 2 {
		t.Fatalf("first=%+v want Many Wanted count=2", got[0])
	}
	if got[1].ID != few.ID || got[1].WantedCount != 1 {
		t.Fatalf("second=%+v want Few Wanted count=1", got[1])
	}
	if got[2].ID != none.ID || got[2].WantedCount != 0 {
		t.Fatalf("third=%+v want None Wanted count=0", got[2])
	}
	got, err = s.ListMostWantedSeries(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != many.ID || got[1].ID != few.ID {
		t.Fatalf("limit=2 got=%+v", got)
	}
}

func TestListRecentVideos(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	a, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Alpha", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Beta", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		sid      int64
		rid      string
		title    string
		status   string
		acquired any
	}{
		{a.ID, "old", "Old", "downloaded", "2024-01-01T00:00:00Z"},
		{b.ID, "mid", "Mid", "downloaded", "2024-06-01T00:00:00Z"},
		{a.ID, "wanted", "WantedOnly", "wanted", nil},
		{a.ID, "new", "New", "downloaded", "2025-01-01T00:00:00Z"},
	} {
		if _, err := s.DB.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status, acquired_at)
			VALUES (?, ?, ?, ?, ?)
		`, row.sid, row.rid, row.title, row.status, row.acquired); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListRecentVideos(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (wanted excluded)", len(got))
	}
	if got[0].Title != "New" || got[1].Title != "Mid" || got[2].Title != "Old" {
		t.Fatalf("order=%q,%q,%q want New,Mid,Old", got[0].Title, got[1].Title, got[2].Title)
	}
	titles, err := s.SeriesTitles([]int64{got[0].SeriesID, got[1].SeriesID})
	if err != nil {
		t.Fatal(err)
	}
	if titles[a.ID] != "Alpha" || titles[b.ID] != "Beta" {
		t.Fatalf("titles=%v", titles)
	}
}
