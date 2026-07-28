package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSeasonYearFromUpload(t *testing.T) {
	if got := library.SeasonYearFromUpload("2024-01-15T14:30:00Z"); got != 2024 {
		t.Fatalf("got %d", got)
	}
	if got := library.SeasonYearFromUpload(""); got != 0 {
		t.Fatalf("undated year %d", got)
	}
}

func TestEpisodeMMDDIndex(t *testing.T) {
	if got := library.EpisodeMMDDIndex(3, 15, 0); got != 31500 {
		t.Fatalf("got %d", got)
	}
	if got := library.EpisodeMMDDIndex(1, 5, 1); got != 10501 {
		t.Fatalf("got %d", got)
	}
}

func TestAssignSeasonEpisodeMMDD(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "MMDD", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@mmdd",
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := src.ID

	r1, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: srcID,
		UploadDate: "2024-03-15T12:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b", Title: "B", SourceID: srcID,
		UploadDate: "2024-03-15T18:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := s.GetVideo(r1.VideoID)
	v2, _ := s.GetVideo(r2.VideoID)
	if !v1.Season.Valid || int(v1.Season.Int64) != 2024 {
		t.Fatalf("v1 season %v", v1.Season)
	}
	if !v1.Episode.Valid || int(v1.Episode.Int64) != 31500 {
		t.Fatalf("v1 episode %v want 31500", v1.Episode)
	}
	if !v2.Episode.Valid || int(v2.Episode.Int64) != 31501 {
		t.Fatalf("v2 episode %v want 31501", v2.Episode)
	}
}

func TestSameDayDateOnlyArrivalOrder(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "DateOnly", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@do"})
	if err != nil {
		t.Fatal(err)
	}
	midnight := "2024-06-01T00:00:00Z"
	r1, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID, UploadDate: midnight,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b", Title: "B", SourceID: src.ID, UploadDate: midnight,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r3, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "c", Title: "C", SourceID: src.ID, UploadDate: midnight,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := s.GetVideo(r1.VideoID)
	v2, _ := s.GetVideo(r2.VideoID)
	v3, _ := s.GetVideo(r3.VideoID)
	if int(v1.Episode.Int64) != 60100 || int(v2.Episode.Int64) != 60101 || int(v3.Episode.Int64) != 60102 {
		t.Fatalf("episodes %v %v %v", v1.Episode, v2.Episode, v3.Episode)
	}
}

func TestEarlierSameDayBumpsAndRepacks(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Bump", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@bump"})
	if err != nil {
		t.Fatal(err)
	}
	later, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "late", Title: "Late", SourceID: src.ID,
		UploadDate: "2024-03-15T18:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root.Path, "Bump", "S2024")
	_ = os.MkdirAll(seriesDir, 0o755)
	oldMedia := filepath.Join(seriesDir, "old.mkv")
	if err := os.WriteFile(oldMedia, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, later.VideoID)
	_, err = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		later.VideoID, oldMedia, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "early", Title: "Early", SourceID: src.ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vLate, _ := s.GetVideo(later.VideoID)
	if !vLate.Episode.Valid || int(vLate.Episode.Int64) != 31501 {
		t.Fatalf("late episode after bump %v want 31501", vLate.Episode)
	}
	var newPath string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, later.VideoID).Scan(&newPath)
	if newPath == oldMedia {
		t.Fatalf("expected repack rename, still %q", newPath)
	}
	if !fileExistsTest(newPath) {
		t.Fatalf("missing renamed media %q", newPath)
	}
}

func fileExistsTest(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func TestUndatedLeavesSeasonEpisodeUnset(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Undated", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@u"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "x", Title: "X", SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Season.Valid || v.Episode.Valid {
		t.Fatalf("want unset S/E, got season=%v episode=%v", v.Season, v.Episode)
	}
}

func TestDifferentDaysNoCrossBump(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Days", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@days"})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID, UploadDate: "2024-03-15T12:00:00Z",
	}, 0)
	r2, _ := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b", Title: "B", SourceID: src.ID, UploadDate: "2024-03-16T08:00:00Z",
	}, 0)
	v1, _ := s.GetVideo(r1.VideoID)
	v2, _ := s.GetVideo(r2.VideoID)
	if int(v1.Episode.Int64) != 31500 {
		t.Fatalf("day15 %v", v1.Episode)
	}
	if int(v2.Episode.Int64) != 31600 {
		t.Fatalf("day16 %v", v2.Episode)
	}
}
