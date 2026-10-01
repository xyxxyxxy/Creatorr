package library_test

import (
	"errors"
	"testing"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestMarkDownloadFailedDoesNotHoldSiblings(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ErrOnly", SourceURL: "https://www.example.com/@eo", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID

	var ids []int64
	for _, rid := range []string{"e1", "e2", "e3"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	for _, id := range ids[:2] {
		if err := s.MarkDownloadFailed(id, seedTaskID(t, s), apperrors.CodeDownloadFailed, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	v3, _ := s.GetVideo(ids[2])
	if v3.Status != "wanted" {
		t.Fatalf("sibling stays wanted, got %s", v3.Status)
	}
}

func TestRetrySourceErrorsClearsDownloadErrors(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Retry", SourceURL: "https://www.example.com/@rt", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "r1", Title: "R1", WebpageURL: "https://www.example.com/watch?v=r1",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeRemuxFailed, "ffmpeg"); err != nil {
		t.Fatal(err)
	}
	n, err := s.RetrySourceErrors(srcID)
	if err != nil || n != 1 {
		t.Fatalf("retry n=%d err=%v", n, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != "wanted" {
		t.Fatalf("want wanted after retry, got %s", v.Status)
	}
}

func TestClearVideoDownloadError(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearOne", SourceURL: "https://www.example.com/@c1", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "c1", Title: "C1", WebpageURL: "https://www.example.com/watch?v=c1",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClearVideoDownloadError(res.VideoID); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("wanted refuse: %v", err)
	}
	if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeDownloadFailed, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearVideoDownloadError(res.VideoID); err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != library.StatusWanted {
		t.Fatalf("status=%q want wanted", v.Status)
	}
}

func TestClearVideoDownloadErrorRefusesArchive(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearArch", SourceURL: "https://www.example.com/@ca", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "a1", Title: "A1", WebpageURL: "https://www.example.com/watch?v=a1",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, library.StatusWantedArchive, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearVideoDownloadError(res.VideoID); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("archive refuse: %v", err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != library.StatusWantedArchive {
		t.Fatalf("status=%q want wanted_archive", v.Status)
	}
}

func TestClearSeriesDownloadErrors(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearSer", SourceURL: "https://www.example.com/@cs", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcA := ser.Sources[0].ID
	srcB, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@cs2",
	})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(rid string, srcID int64, status string) int64 {
		t.Helper()
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if status == library.StatusWantedDownloadError {
			if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeDownloadFailed, "x"); err != nil {
				t.Fatal(err)
			}
		} else if status != library.StatusWanted {
			if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, status, res.VideoID); err != nil {
				t.Fatal(err)
			}
		}
		return res.VideoID
	}
	errIDA := mk("e1", srcA, library.StatusWantedDownloadError)
	errIDB := mk("e2", srcB.ID, library.StatusWantedDownloadError)
	archID := mk("a1", srcA, library.StatusWantedArchive)
	wantID := mk("w1", srcA, library.StatusWanted)

	ok, err := s.SeriesHasDownloadErrors(ser.ID)
	if err != nil || !ok {
		t.Fatalf("SeriesHasDownloadErrors=%v err=%v", ok, err)
	}
	n, err := s.ClearSeriesDownloadErrors(ser.ID)
	if err != nil || n != 2 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
	for _, id := range []int64{errIDA, errIDB} {
		v, _ := s.GetVideo(id)
		if v.Status != library.StatusWanted {
			t.Fatalf("video %d status=%q want wanted", id, v.Status)
		}
	}
	arch, _ := s.GetVideo(archID)
	if arch.Status != library.StatusWantedArchive {
		t.Fatalf("archive touched: %q", arch.Status)
	}
	w, _ := s.GetVideo(wantID)
	if w.Status != library.StatusWanted {
		t.Fatalf("wanted changed: %q", w.Status)
	}
	ok, err = s.SeriesHasDownloadErrors(ser.ID)
	if err != nil || ok {
		t.Fatalf("after clear HasDownloadErrors=%v err=%v", ok, err)
	}
}

func TestClearVideoDownloadErrorsBulk(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ClearBulk", SourceURL: "https://www.example.com/@cb", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	mk := func(rid string, fail bool) int64 {
		t.Helper()
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if fail {
			if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeDownloadFailed, "x"); err != nil {
				t.Fatal(err)
			}
		}
		return res.VideoID
	}
	errID := mk("e1", true)
	wantID := mk("w1", false)
	updated, skipped, err := s.ClearVideoDownloadErrorsBulk([]int64{errID, wantID})
	if err != nil || updated != 1 || skipped != 1 {
		t.Fatalf("updated=%d skipped=%d err=%v", updated, skipped, err)
	}
	v, _ := s.GetVideo(errID)
	if v.Status != library.StatusWanted {
		t.Fatalf("error video status=%q", v.Status)
	}
}
