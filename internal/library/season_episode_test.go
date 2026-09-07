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

func TestAssignSeasonEpisodeYearSequential(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "YearSeq", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@year",
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
	if !v1.Episode.Valid || int(v1.Episode.Int64) != 1 {
		t.Fatalf("v1 episode %v want 1", v1.Episode)
	}
	if !v2.Episode.Valid || int(v2.Episode.Int64) != 2 {
		t.Fatalf("v2 episode %v want 2", v2.Episode)
	}
}

func TestSameDayArrivalOrderYearSequential(t *testing.T) {
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
	if int(v1.Episode.Int64) != 1 || int(v2.Episode.Int64) != 2 || int(v3.Episode.Int64) != 3 {
		t.Fatalf("episodes %v %v %v", v1.Episode, v2.Episode, v3.Episode)
	}
}

func TestEarlierUploadBumpsAndRepacks(t *testing.T) {
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
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded', season = 2024, episode = 1 WHERE id = ?`, later.VideoID)
	_, err = s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'video', ?, ?)`,
		later.VideoID, oldMedia, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	// Index earlier video: DB reindexes; disk rename is via Apply enqueue (scan does not move files).
	_, err = s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "early", Title: "Early", SourceID: src.ID,
		UploadDate: "2024-03-15T08:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	vLate, _ := s.GetVideo(later.VideoID)
	if !vLate.Episode.Valid || int(vLate.Episode.Int64) != 2 {
		t.Fatalf("late episode after bump %v want 2", vLate.Episode)
	}
	// Path unchanged until Apply / peer-move (scan is DB-only).
	var path string
	_ = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, later.VideoID).Scan(&path)
	if path != oldMedia {
		t.Fatalf("scan must not rename disk, got %q", path)
	}
	var nApply int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'rename_episodes' AND status IN ('pending', 'running')`).Scan(&nApply)
	if nApply != 1 {
		t.Fatalf("want scoped Apply queued after packed peer shift, got %d", nApply)
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

func TestDifferentDaysContinueYearCount(t *testing.T) {
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
	if int(v1.Episode.Int64) != 1 {
		t.Fatalf("day15 %v", v1.Episode)
	}
	if int(v2.Episode.Int64) != 2 {
		t.Fatalf("day16 %v", v2.Episode)
	}
}

func TestDifferentYearsIndependent(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Years", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{URL: "https://www.example.com/@yrs"})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a", Title: "A", SourceID: src.ID, UploadDate: "2024-12-31T12:00:00Z",
	}, 0)
	r2, _ := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b", Title: "B", SourceID: src.ID, UploadDate: "2025-01-01T08:00:00Z",
	}, 0)
	v1, _ := s.GetVideo(r1.VideoID)
	v2, _ := s.GetVideo(r2.VideoID)
	if int(v1.Season.Int64) != 2024 || int(v1.Episode.Int64) != 1 {
		t.Fatalf("2024 %v/%v", v1.Season, v1.Episode)
	}
	if int(v2.Season.Int64) != 2025 || int(v2.Episode.Int64) != 1 {
		t.Fatalf("2025 %v/%v", v2.Season, v2.Episode)
	}
}
