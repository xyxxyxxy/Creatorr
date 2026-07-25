package library_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func openLib(t *testing.T) *library.Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	q := queue.NewStore(d)
	return library.NewStore(d, q)
}

func seedRootProfile(t *testing.T, s *library.Store) (rootID, profileID int64) {
	t.Helper()
	r, err := s.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	return r.ID, p.ID
}

// seedTaskID inserts a running bookkeeping task so video_history can link task_id.
func seedTaskID(t *testing.T, s *library.Store) int64 {
	t.Helper()
	tid, err := s.Queue.InsertRunning(queue.EnqueueParams{
		Kind:    "test",
		Domain:  queue.SystemDomain,
		Message: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return tid
}

func TestSeriesCRUDAndScan(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)

	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Demo",
		SourceURL:        "https://www.example.com/@demo",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ser.SourceCount != 1 {
		t.Fatalf("source_count=%d", ser.SourceCount)
	}

	newTitle := "Demo Renamed"
	r2, err := s.CreateRoot("other", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProfile("alt", "bv*")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateSeries(ser.ID, library.UpdateSeriesParams{
		Title:            &newTitle,
		RootID:           &r2.ID,
		QualityProfileID: &p2.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != newTitle || updated.RootID != r2.ID || updated.QualityProfileID != p2.ID {
		t.Fatalf("update: title=%q root=%d profile=%d", updated.Title, updated.RootID, updated.QualityProfileID)
	}

	// create already enqueued a scan; second should conflict
	_, err = s.EnqueueScan(ser.ID)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}

	got, err := s.GetSeries(ser.ID, true)
	if err != nil || got.Title != "Demo Renamed" {
		t.Fatalf("get: %v %+v", err, got)
	}

	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL:   "https://example.com/watch/x",
		Label: "oneshot",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSource(ser.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSeries(ser.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestCreateSeriesPassesSourceOptions(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Opts",
		SourceURL:        "https://www.example.com/@opts",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
		ScanCron:         "0 0 * * 0",
		IndexAsIgnored:   true,
		TitleRegexpInclude: `(?i)show`,
		AutoIgnoreMediaTypes: []string{"short"},
		ScanCutoff:       "2020-01-01",
		SourceLabel:      "My feed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ser.Sources) != 1 {
		t.Fatalf("sources=%d", len(ser.Sources))
	}
	if len(ser.AutoIgnoreMediaTypes) != 1 || ser.AutoIgnoreMediaTypes[0] != "short" {
		t.Fatalf("series auto_ignore_media_types=%v", ser.AutoIgnoreMediaTypes)
	}
	src := ser.Sources[0]
	if src.ScanCron != "0 0 * * 0" {
		t.Fatalf("scan_cron=%q", src.ScanCron)
	}
	if !src.IndexAsIgnored {
		t.Fatal("want index_as_ignored")
	}
	if src.TitleRegexpInclude != `(?i)show` {
		t.Fatalf("title_regexp_include=%q", src.TitleRegexpInclude)
	}
	if !src.Label.Valid || src.Label.String != "My feed" {
		t.Fatalf("label=%v", src.Label)
	}
	if !src.ScanCutoff.Valid || src.ScanCutoff.String != "2020-01-01" {
		t.Fatalf("cutoff=%v", src.ScanCutoff)
	}
}

func TestCreateSeriesRejectsDuplicateSourceURL(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "One", SourceURL: "https://www.example.com/@dup",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateSeries(library.CreateSeriesParams{
		Title: "Two", SourceURL: "https://example.com/@dup",
		RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestCreateSeriesRejectsSameFolderTitle(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Demo Show", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateSeries(library.CreateSeriesParams{
		Title: "Demo Show", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestUnmonitorSeriesCancelsCatchupKeepsHistory(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ScanCancel", SourceURL: "https://www.example.com/@sc", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending < 1 {
		t.Fatal("want pending full scan from AddSource")
	}
	if err := s.SetSeriesMonitored(ser.ID, false); err != nil {
		t.Fatal(err)
	}
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending < 1 {
		t.Fatalf("want full scan still pending after unmonitor, got %d", pending)
	}

	// Mark history done and enqueue catch-up, then unmonitor should cancel catch-up only.
	srcID := ser.Sources[0].ID
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'scan'`)
	if err := s.MarkFullScanDone(srcID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeriesMonitored(ser.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueScanSource(srcID); err != nil {
		t.Fatal(err)
	}
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending != 1 {
		t.Fatalf("want 1 catch-up pending, got %d", pending)
	}
	if err := s.SetSeriesMonitored(ser.ID, false); err != nil {
		t.Fatal(err)
	}
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("want 0 catch-up after unmonitor, got %d", pending)
	}
}

func TestAddSourceIndexesUnmonitoredSeries(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "UnmonIndex", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@unmon",
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'
		AND CAST(json_extract(payload, '$.source_id') AS INTEGER) = ?`, src.ID).Scan(&pending)
	if pending != 1 {
		t.Fatalf("want initial full scan on unmonitored series, got %d", pending)
	}
}

func TestEnqueueScanRequiresActiveDomain(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "Empty",
		RootID:           rootID,
		QualityProfileID: profileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueScan(ser.ID)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want invalid no source, got %v", err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://example.com/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	if err := domains.SetActive(s.DB, "example.com", false); err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueScan(ser.ID)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want invalid inactive domain, got %v", err)
	}
}

func TestEnqueueDownloadRequiresSeriesMonitored(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "DL", SourceURL: "https://www.example.com/@dl", RootID: rootID,
		QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@dl2",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueDownload(res.VideoID)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want series unmonitored, got %v", err)
	}
	if err := s.SetSeriesMonitored(ser.ID, true); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueDownload(res.VideoID)
	if err != nil || id == 0 {
		t.Fatalf("enqueue after remonitor: %v id=%d", err, id)
	}
}

func TestListSeriesStreamProgressCounts(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Prog", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@prog",
	})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(remote, status string, beginningCached bool, streamKind string) {
		t.Helper()
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: remote, Title: remote, WebpageURL: "https://www.example.com/watch?v=" + remote,
			SourceID: src.ID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		beginning := 0
		if beginningCached {
			beginning = 1
		}
		var kind any
		if streamKind != "" {
			kind = streamKind
		}
		if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = ?, stream_beginning_cached = ?, stream_urls_kind = ? WHERE id = ?`, status, beginning, kind, res.VideoID); err != nil {
			t.Fatal(err)
		}
	}
	mk("opt-beginning", "streamable", true, "pipe")
	mk("opt-cdn", "streamable", false, "hls")
	mk("cold", "streamable", false, "pipe")
	mk("want", "wanted", false, "")

	list, err := s.ListSeries()
	if err != nil {
		t.Fatal(err)
	}
	var found *library.Series
	for i := range list {
		if list[i].ID == ser.ID {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatal("series missing")
	}
	if found.StreamOptimizedCount != 2 || found.StreamColdCount != 1 {
		t.Fatalf("opt=%d cold=%d want 2/1", found.StreamOptimizedCount, found.StreamColdCount)
	}
	if found.DownloadedCount != 3 || found.WantedCount != 1 {
		t.Fatalf("ready=%d wanted=%d", found.DownloadedCount, found.WantedCount)
	}
}

func TestListSeriesFiltered(t *testing.T) {
	s := openLib(t)
	rootA, profileA := seedRootProfile(t, s)
	rootB, err := s.CreateRoot("inbox", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	profileB, err := s.CreateProfile("best", "b")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Alpha Show", RootID: rootA, QualityProfileID: profileA, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateSeries(library.CreateSeriesParams{
		Title: "Beta Show", RootID: rootB.ID, QualityProfileID: profileB.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	got, err := s.ListSeriesFiltered(library.SeriesListFilter{Title: "alpha", Monitored: &on}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != alpha.ID {
		t.Fatalf("title+monitored got %#v", got)
	}
	n, err := s.CountSeriesFiltered(library.SeriesListFilter{RootID: rootB.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("root count=%d", n)
	}
	n, err = s.CountSeriesFiltered(library.SeriesListFilter{QualityProfileID: profileA})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("quality count=%d", n)
	}
}

func TestIgnoreVideoCancelsDownloads(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Skip", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@skip",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sk1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=sk1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tid, err := s.EnqueueDownload(res.VideoID)
	if err != nil || tid == 0 {
		t.Fatalf("enqueue: %v id=%d", err, tid)
	}
	cancelled, err := s.IgnoreVideo(res.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cancelled) != 1 || cancelled[0].ID != tid {
		t.Fatalf("cancelled=%+v", cancelled)
	}
	st, err := s.Queue.TaskStatus(tid)
	if err != nil || st != queue.StatusCancelled {
		t.Fatalf("status=%q err=%v", st, err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil || v.Status != "ignored" {
		t.Fatalf("video %+v", v)
	}
}

func TestIgnoreVideoRejectsStreamable(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Stream", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@st",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "st1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=st1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IgnoreVideo(res.VideoID); err == nil {
		t.Fatal("expected ignore to fail for streamable")
	}
}

func TestDeleteVideoStreamable(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "StreamDel", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@sd",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sd1", Title: "Ep", WebpageURL: "https://www.example.com/watch?v=sd1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.GetRoot(rootID)
	if err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(root.Path, "ep.strm")
	if err := os.WriteFile(strm, []byte("https://example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'streamable' WHERE id = ?`, res.VideoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQL.Exec(`INSERT INTO files (video_id, kind, path, acquired_at) VALUES (?, 'strm', ?, datetime('now'))`, res.VideoID, strm); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteVideo(res.VideoID); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Queue.ListActiveFileDelete()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("want 1 delete_files, got %d err=%v", len(tasks), err)
	}
	if err := s.FileDeletePass(context.Background(), &tasks[0], nil); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil || v.Status != "deleted" {
		t.Fatalf("video %+v err=%v", v, err)
	}
	if _, err := os.Stat(strm); !os.IsNotExist(err) {
		t.Fatalf("strm still on disk: %v", err)
	}
	var n int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ?`, res.VideoID).Scan(&n)
	if n != 0 {
		t.Fatalf("files left: %d", n)
	}
	var histTask int64
	_ = s.DB.SQL.QueryRow(`SELECT task_id FROM video_history WHERE video_id = ? AND event = 'file_deleted'`, res.VideoID).Scan(&histTask)
	if histTask != tasks[0].ID {
		t.Fatalf("file_deleted task_id=%d want %d", histTask, tasks[0].ID)
	}
}

func TestEnqueueDownloadWantedSkipsUnmonitoredSeries(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Wanted", RootID: rootID, QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@w",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "w1", Title: "W", WebpageURL: "https://www.example.com/watch?v=w1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, res.VideoID)
	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 downloads for unmonitored series, got %d", n)
	}
}

func TestUpsertListedNoLongerIgnoresByCutoff(t *testing.T) {
	// Cutoff stop/discard is handled in the scan worker, not UpsertListed.
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "CutIgn", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@cutign", ScanCutoff: "2024-01-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "old1", Title: "Old", WebpageURL: "https://www.example.com/watch?v=old1",
		SourceID: src.ID, UploadDate: "2020-06-01T12:00:00Z",
	}, 0)
	if err != nil || !res.Created || res.Status != "wanted" {
		t.Fatalf("upsert past-cutoff date must not force ignored: %+v err=%v", res, err)
	}
}

func TestUpsertListedIndexAsIgnored(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "IdxIgn", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@idxign", IndexAsIgnored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ig1", Title: "Ig", WebpageURL: "https://www.example.com/watch?v=ig1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !res.Created || res.Status != "ignored" {
		t.Fatalf("upsert: %+v err=%v", res, err)
	}
	if res.IgnoreReason != library.IgnoreReasonIndexAsIgnored {
		t.Fatalf("IgnoreReason=%q", res.IgnoreReason)
	}
	v, err := s.GetVideo(res.VideoID)
	if err != nil || v.Status != "ignored" {
		t.Fatalf("video %+v err=%v", v, err)
	}
}

func TestUpsertListedTitleRegexp(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "TitleRe", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@titlere", TitleRegexpInclude: `(?i)podcast`,
	})
	if err != nil {
		t.Fatal(err)
	}
	miss, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "tr1", Title: "Vlog day 1", WebpageURL: "https://www.example.com/watch?v=tr1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !miss.Skipped || miss.Created || miss.SkipReason != library.SkipReasonTitleRegexpInclude || miss.VideoID != 0 {
		t.Fatalf("miss skip: %+v err=%v", miss, err)
	}
	var n int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM videos WHERE series_id = ? AND remote_id = 'tr1'`, ser.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("title non-match must not create row, got %d", n)
	}
	hit, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "tr2", Title: "Weekly Podcast", WebpageURL: "https://www.example.com/watch?v=tr2",
		SourceID: src.ID,
	}, 0)
	if err != nil || !hit.Created || hit.Status != "wanted" || hit.IgnoreReason != "" || hit.Skipped {
		t.Fatalf("hit: %+v err=%v", hit, err)
	}
}

func TestUpsertListedTitleRegexpExclude(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "TitleEx", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@titleex", TitleRegexpExclude: `(?i)short`,
	})
	if err != nil {
		t.Fatal(err)
	}
	miss, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ex1", Title: "My Short Clip", WebpageURL: "https://www.example.com/watch?v=ex1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !miss.Skipped || miss.SkipReason != library.SkipReasonTitleRegexpExclude {
		t.Fatalf("exclude skip: %+v err=%v", miss, err)
	}
	hit, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "ex2", Title: "Full episode", WebpageURL: "https://www.example.com/watch?v=ex2",
		SourceID: src.ID,
	}, 0)
	if err != nil || !hit.Created || hit.Skipped {
		t.Fatalf("exclude pass: %+v err=%v", hit, err)
	}
}

func TestUpsertListedTitleExcludeWinsOverInclude(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BothRe", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL:                "https://www.example.com/@bothre",
		TitleRegexpInclude: `(?i)show`,
		TitleRegexpExclude: `(?i)trailer`,
	})
	if err != nil {
		t.Fatal(err)
	}
	both, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "br1", Title: "Show Trailer", WebpageURL: "https://www.example.com/watch?v=br1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !both.Skipped || both.SkipReason != library.SkipReasonTitleRegexpExclude {
		t.Fatalf("both match → exclude: %+v err=%v", both, err)
	}
}

func TestUpsertListedTitleRegexpBeatsIndexAsIgnored(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BothIgn", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@bothign", TitleRegexpInclude: `keep`, IndexAsIgnored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	miss, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b1", Title: "drop me", WebpageURL: "https://www.example.com/watch?v=b1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !miss.Skipped || miss.SkipReason != library.SkipReasonTitleRegexpInclude {
		t.Fatalf("miss skip: %+v err=%v", miss, err)
	}
	hit, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "b2", Title: "keep this", WebpageURL: "https://www.example.com/watch?v=b2",
		SourceID: src.ID,
	}, 0)
	if err != nil || hit.Status != "ignored" || hit.IgnoreReason != library.IgnoreReasonIndexAsIgnored {
		t.Fatalf("hit with index_as_ignored: %+v err=%v", hit, err)
	}
}

func TestAddSourceRejectsInvalidTitleRegexp(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BadRe", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@badre", TitleRegexpInclude: `(unclosed`,
	})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want ErrInvalid include, got %v", err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@badrex", TitleRegexpExclude: `(unclosed`,
	})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want ErrInvalid exclude, got %v", err)
	}
}

func TestAddSourceRejectsInvalidURL(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BadURL", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{URL: "sdasd"})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestUpdateSourceRejectsInvalidTitleRegexp(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BadReUp", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		SourceURL: "https://www.example.com/@badreup",
	})
	if err != nil {
		t.Fatal(err)
	}
	src := ser.Sources[0]
	bad := `(unclosed`
	_, err = s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{TitleRegexpInclude: &bad})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want ErrInvalid include, got %v", err)
	}
	_, err = s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{TitleRegexpExclude: &bad})
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want ErrInvalid exclude, got %v", err)
	}
}

func TestUpsertListedEmptyTitleRegexpWanted(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "EmptyRe", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@emptyre", TitleRegexpInclude: "", TitleRegexpExclude: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "e1", Title: "Anything", WebpageURL: "https://www.example.com/watch?v=e1",
		SourceID: src.ID,
	}, 0)
	if err != nil || !res.Created || res.Status != "wanted" || res.IgnoreReason != "" {
		t.Fatalf("upsert: %+v err=%v", res, err)
	}
}

func TestEnqueueDownloadWantedHonorsWantedOnIndexAsIgnoredSource(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "IdxDL", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@idxdl", IndexAsIgnored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "igd1", Title: "W", WebpageURL: "https://www.example.com/watch?v=igd1",
		SourceID: src.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, res.VideoID)
	_, _ = s.Queue.CancelAll()
	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want download for forced wanted on index_as_ignored source, got %d", n)
	}
}

func TestEnqueueDownloadWantedSkipsInactiveDomain(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Mixed", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		SourceURL: "https://www.on.example/@on",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	off, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.off.example/@off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := domains.SetActive(s.DB, "www.off.example", false); err != nil {
		t.Fatal(err)
	}
	resOff, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "off1", Title: "Off", WebpageURL: "https://www.off.example/watch?v=off1",
		SourceID: off.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, resOff.VideoID)

	var onID int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM sources WHERE series_id = ? AND url LIKE '%on.example%'`, ser.ID).Scan(&onID); err != nil {
		t.Fatal(err)
	}
	resOn, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "on1", Title: "On", WebpageURL: "https://www.on.example/watch?v=on1",
		SourceID: onID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, resOn.VideoID)

	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 download (active domain only), got %d", n)
	}
	var vid int64
	if err := s.DB.SQL.QueryRow(`
		SELECT video_id FROM tasks WHERE kind = 'download' AND status = 'pending'
	`).Scan(&vid); err != nil {
		t.Fatal(err)
	}
	if vid != resOn.VideoID {
		t.Fatalf("queued video=%d want %d (on)", vid, resOn.VideoID)
	}
}

func TestEnqueueDownloadWantedSkipsOrphanAndUnmonitoredDomain(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Orphans", RootID: rootID, QualityProfileID: profileID, Monitored: true,
		SourceURL: "https://www.keep.example/@on",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()

	// Orphan wanted video (no source) on an unmonitored domain - must not enqueue.
	res, err := s.DB.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, NULL, 'gone1', 'Gone', 'https://www.gone.example/watch?v=1', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	if err := domains.EnsureHost(s.DB, "www.gone.example"); err != nil {
		t.Fatal(err)
	}
	if err := domains.SetActive(s.DB, "www.gone.example", false); err != nil {
		t.Fatal(err)
	}

	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 for orphan / unmonitored domain, got %d", n)
	}
	_ = vid
}

func TestDeleteSeriesKeepsOrRemovesFiles(t *testing.T) {
	s := openLib(t)
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}

	keepSer, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "KeepFiles", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
		SourceURL: "https://www.example.com/@keepfiles",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	resKeep, err := s.UpsertListed(keepSer.ID, library.ListedVideo{
		RemoteID: "kf1", Title: "Keep Media", WebpageURL: "https://www.example.com/v/kf1",
		SourceID: keepSer.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	keepMedia := filepath.Join(libRoot, "KeepFiles", "Keep Media [kf1].mkv")
	if err := os.MkdirAll(filepath.Dir(keepMedia), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepMedia, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(resKeep.VideoID, keepMedia, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSeries(keepSer.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keepMedia); err != nil {
		t.Fatalf("library file should remain when deleteFiles=false: %v", err)
	}

	purgeSer, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "PurgeFiles", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
		SourceURL: "https://www.example.com/@purgefiles",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	resPurge, err := s.UpsertListed(purgeSer.ID, library.ListedVideo{
		RemoteID: "pf1", Title: "Purge Media", WebpageURL: "https://www.example.com/v/pf1",
		SourceID: purgeSer.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	purgeMedia := filepath.Join(libRoot, "PurgeFiles", "Purge Media [pf1].mkv")
	if err := os.MkdirAll(filepath.Dir(purgeMedia), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(purgeMedia, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(resPurge.VideoID, purgeMedia, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSeries(purgeSer.ID, true); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Queue.ListActiveFileDelete()
	if err != nil || len(tasks) != 1 {
		t.Fatalf("want 1 delete_files task, got %d err=%v", len(tasks), err)
	}
	if err := s.FileDeletePass(context.Background(), &tasks[0], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(purgeMedia); !os.IsNotExist(err) {
		t.Fatalf("library file should be gone when deleteFiles=true, err=%v", err)
	}
}

func TestDeleteSourcePurgesVideosAndFiles(t *testing.T) {
	s := openLib(t)
	libRoot := t.TempDir()
	root, err := s.CreateRoot("archive", libRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Purge", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
		SourceURL: "https://www.keep.example/@on",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()

	gone, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.purge.example/@x",
	})
	if err != nil {
		t.Fatal(err)
	}
	resGone, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "p1", Title: "Purge Me", WebpageURL: "https://www.purge.example/v/1",
		SourceID: gone.ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(libRoot, "Purge", "Purge Me [p1].mkv")
	if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(resGone.VideoID, media, "", "", library.MediaCompleteMeta{Tool: "test"}, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}

	var keepSrc int64
	if err := s.DB.SQL.QueryRow(`SELECT id FROM sources WHERE series_id = ? AND id != ?`, ser.ID, gone.ID).Scan(&keepSrc); err != nil {
		t.Fatal(err)
	}
	resKeep, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "k1", Title: "Keep Me", WebpageURL: "https://www.keep.example/v/1",
		SourceID: keepSrc,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSource(ser.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetVideo(resGone.VideoID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("purged video still present: %v", err)
	}
	if _, err := os.Stat(media); !os.IsNotExist(err) {
		t.Fatalf("media should be gone, err=%v", err)
	}
	if _, err := s.GetVideo(resKeep.VideoID); err != nil {
		t.Fatalf("other source video should remain: %v", err)
	}
	if _, err := s.GetSource(ser.ID, gone.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("source should be gone")
	}
}

func TestListSeriesLoadsSourcesWithoutDeadlock(t *testing.T) {
	// Regression: nested listSources while series rows open deadlocks under MaxOpenConns(1).
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	if _, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "A",
		SourceURL:        "https://www.example.com/@a",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSeries(library.CreateSeriesParams{
		Title:            "B",
		SourceURL:        "https://www.example.com/@b",
		RootID:           rootID,
		QualityProfileID: profileID,
		Monitored:        true,
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		list, err := s.ListSeries()
		if err != nil {
			done <- err
			return
		}
		if len(list) != 2 {
			done <- errors.New("want 2 series")
			return
		}
		for _, ser := range list {
			if len(ser.Sources) != 1 {
				done <- errors.New("want 1 source each")
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListSeries deadlocked (nested query with MaxOpenConns(1))")
	}
}

func TestSetSeriesMonitoredDoesNotTouchSources(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Flags", SourceURL: "https://www.example.com/@flags", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	wantCron := cronexpr.ScanCronWeekly
	if err := s.SetSeriesMonitored(ser.ID, false); err != nil {
		t.Fatal(err)
	}
	src, err := s.GetSource(ser.ID, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if src.ScanCron != wantCron {
		t.Fatalf("source scan_cron=%q want unchanged %q", src.ScanCron, wantCron)
	}
	got, err := s.GetSeries(ser.ID, false)
	if err != nil || got.Monitored {
		t.Fatalf("series monitored=%v err=%v", got.Monitored, err)
	}
}

func TestDownloadErrorHoldAndRetry(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ErrHold", SourceURL: "https://www.example.com/@eh", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_ = settings.Set(s.DB, settings.KeySourceDownloadErrorThreshold, "2")

	var ids []int64
	for i, rid := range []string{"e1", "e2", "e3"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
		_ = i
	}
	if err := s.MarkDownloadFailed(ids[0], seedTaskID(t, s), "DownloadFailed", "boom1"); err != nil {
		t.Fatal(err)
	}
	v2, _ := s.GetVideo(ids[1])
	if v2.Status != "wanted" {
		t.Fatalf("before threshold sibling want wanted, got %s", v2.Status)
	}
	if err := s.MarkDownloadFailed(ids[1], seedTaskID(t, s), "DownloadFailed", "boom2"); err != nil {
		t.Fatal(err)
	}
	v3, _ := s.GetVideo(ids[2])
	if v3.Status != "wanted_source_error" {
		t.Fatalf("want wanted_source_error, got %s", v3.Status)
	}
	n, err := s.RetrySourceErrors(srcID)
	if err != nil || n < 3 {
		t.Fatalf("retry n=%d err=%v", n, err)
	}
	for _, id := range ids {
		v, _ := s.GetVideo(id)
		if v.Status != "wanted" {
			t.Fatalf("video %d status=%s", id, v.Status)
		}
	}
}

func TestHoldSourceOnYtDlpErrorImmediate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "ScanHold", SourceURL: "https://www.example.com/@sh", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_ = settings.Set(s.DB, settings.KeySourceDownloadErrorThreshold, "99")

	var ids []int64
	for _, rid := range []string{"s1", "s2"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.VideoID)
	}
	if err := s.HoldSourceOnYtDlpError(srcID, seedTaskID(t, s)); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		v, _ := s.GetVideo(id)
		if v.Status != "wanted_source_error" {
			t.Fatalf("video %d want wanted_source_error, got %s", id, v.Status)
		}
	}
	n, err := s.RetrySourceErrors(srcID)
	if err != nil || n != 2 {
		t.Fatalf("retry n=%d err=%v", n, err)
	}
}

func TestEnqueueDownloadNowAllowsUnmonitoredSeries(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NowOff", SourceURL: "https://www.example.com/@nowoff", RootID: rootID,
		QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "n1", Title: "N", WebpageURL: "https://www.example.com/watch?v=n1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueDownload(res.VideoID)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("plain enqueue want unmonitored, got %v", err)
	}
	id, err := s.EnqueueDownloadNow(res.VideoID)
	if err != nil || id == 0 {
		t.Fatalf("download now: %v id=%d", err, id)
	}
}

func TestEnqueueDownloadNowRejectsInactiveDomain(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "NowDom", SourceURL: "https://www.example.com/@nowdom", RootID: rootID,
		QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := domains.SetActive(s.DB, "example.com", false); err != nil {
		t.Fatal(err)
	}
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "d1", Title: "D", WebpageURL: "https://www.example.com/watch?v=d1",
		SourceID: ser.Sources[0].ID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueDownloadNow(res.VideoID)
	if !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("want inactive domain, got %v", err)
	}
}

func TestEnqueueDownloadNowBypassesCap(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_ = settings.SetDomainDefault(s.DB, 0, 1, 1, "10M", "0", false)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Cap", SourceURL: "https://www.example.com/@cap", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	var vids []int64
	for _, rid := range []string{"c1", "c2"} {
		res, err := s.UpsertListed(ser.ID, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://www.example.com/watch?v=" + rid,
			SourceID: srcID,
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		vids = append(vids, res.VideoID)
	}
	if _, err := s.EnqueueDownload(vids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueDownload(vids[1]); !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want queue full conflict, got %v", err)
	}
	id, err := s.EnqueueDownloadNow(vids[1])
	if err != nil || id == 0 {
		t.Fatalf("download now: %v id=%d", err, id)
	}
}

func TestEnqueueDownloadWantedOrderByUploadDate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_ = settings.SetDomainDefault(s.DB, 0, 10, 1, "10M", "0", false)
	_ = settings.Set(s.DB, settings.KeyDownloadWantedOrder, settings.DownloadWantedOrderOldest)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Ord", SourceURL: "https://www.example.com/@ord", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	newer, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "n", Title: "n", WebpageURL: "https://www.example.com/n",
		SourceID: srcID, UploadDate: "2024-06-02T12:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	older, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "o", Title: "o", WebpageURL: "https://www.example.com/o",
		SourceID: srcID, UploadDate: "2024-06-01T12:00:00Z",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.EnqueueDownloadWanted()
	if err != nil || n != 2 {
		t.Fatalf("enqueue n=%d err=%v", n, err)
	}
	var firstVid int64
	_ = s.DB.SQL.QueryRow(`
		SELECT video_id FROM tasks WHERE kind = 'download' AND status = 'pending'
		ORDER BY id ASC LIMIT 1
	`).Scan(&firstVid)
	if firstVid != older.VideoID {
		t.Fatalf("oldest-first want video %d, got %d", older.VideoID, firstVid)
	}

	_, _ = s.DB.SQL.Exec(`DELETE FROM tasks`)
	_ = settings.Set(s.DB, settings.KeyDownloadWantedOrder, settings.DownloadWantedOrderNewest)
	n, err = s.EnqueueDownloadWanted()
	if err != nil || n != 2 {
		t.Fatalf("newest enqueue n=%d err=%v", n, err)
	}
	_ = s.DB.SQL.QueryRow(`
		SELECT video_id FROM tasks WHERE kind = 'download' AND status = 'pending'
		ORDER BY id ASC LIMIT 1
	`).Scan(&firstVid)
	if firstVid != newer.VideoID {
		t.Fatalf("newest-first want video %d, got %d", newer.VideoID, firstVid)
	}
}

func TestEnqueueDownloadWantedRoundRobinFair(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	_ = settings.SetDomainDefault(s.DB, 0, 4, 1, "10M", "0", false)
	_ = settings.Set(s.DB, settings.KeyDownloadWantedOrder, settings.DownloadWantedOrderOldest)

	makeSer := func(title, host string) (seriesID, srcID int64) {
		t.Helper()
		ser, err := s.CreateSeries(library.CreateSeriesParams{
			Title: title, SourceURL: "https://" + host + "/@x", RootID: rootID,
			QualityProfileID: profileID, Monitored: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return ser.ID, ser.Sources[0].ID
	}
	s1, src1 := makeSer("S1", "a.example.com")
	s2, src2 := makeSer("S2", "b.example.com")
	for i, rid := range []string{"a1", "a2"} {
		if _, err := s.UpsertListed(s1, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://a.example.com/" + rid,
			SourceID: src1, UploadDate: time.Date(2024, 1, i+1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	for i, rid := range []string{"b1", "b2"} {
		if _, err := s.UpsertListed(s2, library.ListedVideo{
			RemoteID: rid, Title: rid, WebpageURL: "https://b.example.com/" + rid,
			SourceID: src2, UploadDate: time.Date(2024, 2, i+1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Pre-load S1 with one active download so fair order prefers S2 first.
	var pre int64
	_ = s.DB.SQL.QueryRow(`SELECT id FROM videos WHERE series_id = ? ORDER BY id ASC LIMIT 1`, s1).Scan(&pre)
	if _, err := s.EnqueueDownload(pre); err != nil {
		t.Fatal(err)
	}
	n, err := s.EnqueueDownloadWanted()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("want at least one enqueue")
	}
	var firstSeries int64
	_ = s.DB.SQL.QueryRow(`
		SELECT series_id FROM tasks WHERE kind = 'download' AND status = 'pending' AND video_id != ?
		ORDER BY id ASC LIMIT 1
	`, pre).Scan(&firstSeries)
	if firstSeries != s2 {
		t.Fatalf("fair RR: want series %d (zero active) before series %d, got %d", s2, s1, firstSeries)
	}
}

func TestMarkDownloadFailedStage(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "StageFail", SourceURL: "https://www.example.com/@sf", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "sf1", Title: "sf1", WebpageURL: "https://www.example.com/watch?v=sf1",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDownloadFailed(res.VideoID, seedTaskID(t, s), apperrors.CodeRemuxFailed, "ffmpeg boom"); err != nil {
		t.Fatal(err)
	}
	hist, err := s.ListVideoHistory(res.VideoID)
	if err != nil || len(hist) == 0 {
		t.Fatalf("history: %v len=%d", err, len(hist))
	}
	var found bool
	for _, e := range hist {
		if e.Event != "download_failed" {
			continue
		}
		found = true
		if e.Message != "Remux failed" {
			t.Fatalf("message=%q", e.Message)
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &detail); err != nil {
			t.Fatal(err)
		}
		if detail["stage"] != "remux" || detail["code"] != apperrors.CodeRemuxFailed {
			t.Fatalf("detail=%v", detail)
		}
	}
	if !found {
		t.Fatal("expected download_failed history")
	}
}
