package library_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func countArchiveDownloads(t *testing.T, s *library.Store, videoID int64, statuses ...string) int {
	t.Helper()
	if len(statuses) == 0 {
		statuses = []string{queue.StatusPending, queue.StatusRunning}
	}
	n := 0
	for _, st := range statuses {
		var c int
		err := s.DB.SQL.QueryRow(`
			SELECT COUNT(*) FROM tasks
			WHERE kind = ? AND video_id = ? AND domain = ? AND status = ?
		`, queue.KindDownload, videoID, library.ArchiveOrgDomain, st).Scan(&c)
		if err != nil {
			t.Fatal(err)
		}
		n += c
	}
	return n
}

func TestQueueArchiveFallbackAfterUnavailable(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ArchFB", SourceURL: "https://www.youtube.com/@archfb",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "abc123XYZ01", Title: "Gone", WebpageURL: "https://www.youtube.com/watch?v=abc123XYZ01",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	liveTID := seedTaskID(t, s)
	taken, err := s.QueueArchiveFallbackAfterUnavailable(res.VideoID, liveTID, "Video unavailable")
	if err != nil || !taken {
		t.Fatalf("taken=%v err=%v", taken, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != library.StatusWantedArchive {
		t.Fatalf("status=%q want wanted_archive", v.Status)
	}
	if countArchiveDownloads(t, s, res.VideoID, queue.StatusPending) < 1 {
		t.Fatal("expected pending archive.org download task")
	}
	var payload string
	if err := s.DB.SQL.QueryRow(`
		SELECT payload FROM tasks
		WHERE kind = ? AND video_id = ? AND domain = ? AND status = ?
		LIMIT 1
	`, queue.KindDownload, res.VideoID, library.ArchiveOrgDomain, queue.StatusPending).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !library.TaskPayloadArchive(payload) {
		t.Fatalf("payload missing archive_fallback: %s", payload)
	}
	src := ""
	if v.SourceURL.Valid {
		src = v.SourceURL.String
	}
	if src != "https://www.youtube.com/watch?v=abc123XYZ01" {
		t.Fatalf("source_url rewritten: %q", src)
	}
}

func TestQueueArchiveFallbackSettingOff(t *testing.T) {
	s := openLib(t)
	if err := settings.Set(s.DB, settings.KeyArchiveFallback, "0"); err != nil {
		t.Fatal(err)
	}
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Off", SourceURL: "https://www.youtube.com/@off",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "offvid00001", Title: "T", WebpageURL: "https://www.youtube.com/watch?v=offvid00001",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := s.QueueArchiveFallbackAfterUnavailable(res.VideoID, seedTaskID(t, s), "gone")
	if err != nil || taken {
		t.Fatalf("taken=%v err=%v want false", taken, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != "wanted" {
		t.Fatalf("status=%q want wanted", v.Status)
	}
}

func TestQueueArchiveFallbackNonYouTube(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Other", SourceURL: "https://www.example.com/@o",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ex1", Title: "T", WebpageURL: "https://www.example.com/watch?v=ex1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := s.QueueArchiveFallbackAfterUnavailable(res.VideoID, seedTaskID(t, s), "gone")
	if err != nil || taken {
		t.Fatalf("taken=%v err=%v want false", taken, err)
	}
}

func TestEnqueueDownloadWantedIgnoresWantedArchive(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "WA", SourceURL: "https://www.youtube.com/@wa",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "waid0000001", Title: "T", WebpageURL: "https://www.youtube.com/watch?v=waid0000001",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status=? WHERE id=?`, library.StatusWantedArchive, res.VideoID); err != nil {
		t.Fatal(err)
	}
	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("enqueued %d want 0 (wanted_archive owned by archive lane)", n)
	}
}

func TestRetrySourceErrorsCancelsArchiveDownload(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "RetryArch", SourceURL: "https://www.youtube.com/@ra",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "raid0000001", Title: "T", WebpageURL: "https://www.youtube.com/watch?v=raid0000001",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueArchiveFallbackAfterUnavailable(res.VideoID, seedTaskID(t, s), "gone"); err != nil {
		t.Fatal(err)
	}
	n, err := s.RetrySourceErrors(ser.Sources[0].ID)
	if err != nil || n != 1 {
		t.Fatalf("retry n=%d err=%v", n, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != "wanted" {
		t.Fatalf("status=%q want wanted", v.Status)
	}
	if countArchiveDownloads(t, s, res.VideoID, queue.StatusPending, queue.StatusRunning) != 0 {
		t.Fatal("archive task still pending/running after retry")
	}
}

func TestEnqueueArchiveDownloadHonorsSoftPause(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "PauseArch", SourceURL: "https://www.youtube.com/@pa",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "paid0000001", Title: "T", WebpageURL: "https://www.youtube.com/watch?v=paid0000001",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWantedArchive(res.VideoID, seedTaskID(t, s), "gone"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetPaused(s.DB, library.ArchiveOrgDomain, true); err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.EnqueueArchiveDownload(res.VideoID)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v want skip while paused", ok, err)
	}
	v, _ := s.GetVideo(res.VideoID)
	if v.Status != library.StatusWantedArchive {
		t.Fatalf("status=%q want wanted_archive for cron backfill", v.Status)
	}
}

func TestYtArchiveURL(t *testing.T) {
	if got := library.YtArchiveURL("abc"); got != "ytarchive:abc" {
		t.Fatalf("got %q", got)
	}
	if library.YtArchiveURL("  ") != "" {
		t.Fatal("empty remote should yield empty URL")
	}
}

func TestIsYouTubeSourceURL(t *testing.T) {
	if !library.IsYouTubeSourceURL("https://www.youtube.com/watch?v=x") {
		t.Fatal("expected youtube true")
	}
	if library.IsYouTubeSourceURL("https://www.example.com/watch?v=x") {
		t.Fatal("expected example false")
	}
}
