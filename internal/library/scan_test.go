package library_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestFullRescanResetsBackfill(t *testing.T) {
	s := openLib(t)
	_ = settings.SeedDefaults(s.DB)
	_ = settings.SetDomainDefault(s.DB, 0, 8, 1, "10M", "0", false)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "BF", SourceURL: "https://www.example.com/@bf", RootID: rootID,
		QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@bf2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFullScanDone(src.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSourceByID(src.ID)
	if err != nil || !got.FullScanDone {
		t.Fatalf("want full scan done: %+v %v", got, err)
	}
	if err := s.ResetFullScan(src.ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSourceByID(src.ID)
	if err != nil || got.FullScanDone {
		t.Fatalf("want reset: %+v %v", got, err)
	}
}

func TestUpdateSourceCutoffExpandedRescans(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Cut", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cut := "2024-06-01"
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@cut", ScanCutoff: cut,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.MarkFullScanDone(src.ID)
	_, _ = s.DB.SQL.Exec(`DELETE FROM tasks WHERE kind = 'scan'`)

	older := "2020-01-01"
	got, err := s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{ScanCutoff: &older})
	if err != nil {
		t.Fatal(err)
	}
	if got.FullScanDone {
		t.Fatal("want history reset after cutoff expand")
	}
	var n int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending' AND json_extract(payload, '$.source_id') = ?`, src.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 pending full scan, got %d", n)
	}

	_, _ = s.DB.SQL.Exec(`DELETE FROM tasks WHERE kind = 'scan'`)
	_ = s.MarkFullScanDone(src.ID)
	newer := "2023-01-01" // later than 2020 → shrink toward present
	got, err = s.UpdateSource(ser.ID, src.ID, library.UpdateSourceParams{ScanCutoff: &newer})
	if err != nil {
		t.Fatal(err)
	}
	if !got.FullScanDone {
		t.Fatal("shrinking cutoff toward present must not reset history")
	}
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&n)
	if n != 0 {
		t.Fatalf("want 0 scans after shrink, got %d", n)
	}
}

func TestEnqueueScanPerSource(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Multi", SourceURL: "https://www.example.com/@a", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@b",
	})
	if err != nil {
		t.Fatal(err)
	}
	// CreateSeries + AddSource each enqueued; clear by checking count
	var pending int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending < 2 {
		t.Fatalf("want >=2 pending scans, got %d", pending)
	}
}

func TestAddSourceScanIsIndexOnly(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Seed", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	err = s.DB.SQL.QueryRow(`
		SELECT payload FROM tasks WHERE kind = 'scan' AND status = 'pending' ORDER BY id DESC LIMIT 1
	`).Scan(&payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, `seed_downloads`) {
		t.Fatalf("AddSource scan must not seed downloads, got %s", payload)
	}
	_ = src
}

func TestUpsertListedNeverEnqueuesDownload(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Upsert", SourceURL: "https://www.example.com/@u", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'scan'`)
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "v1", Title: "T", WebpageURL: "https://www.example.com/watch?v=v1",
SourceID: srcID,
	}, 0)
	if err != nil || !res.Created {
		t.Fatalf("upsert: %+v %v", res, err)
	}
	var n int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'download'`).Scan(&n)
	if n != 0 {
		t.Fatalf("want 0 downloads from upsert, got %d", n)
	}
}

func TestSingleSourceSkipsCatchup(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Single", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/watch?v=abc", Kind: library.SourceKindSingle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != library.SourceKindSingle {
		t.Fatalf("want single: %+v", src)
	}
	if src.ScanCutoff.Valid {
		t.Fatalf("single must clear cutoff: %+v", src)
	}
	// Initial scan enqueued on add.
	var pending int
	_ = s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&pending)
	if pending != 1 {
		t.Fatalf("want 1 initial scan, got %d", pending)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'scan'`)
	if err := s.MarkFullScanDone(src.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.EnqueueScanSource(src.ID)
	if err == nil {
		t.Fatal("want catch-up rejected for single")
	}
	n, err := s.EnqueueScansDue(time.Now().UTC(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("catch-up cron must skip single, got %d", n)
	}
	_, _, err = s.EnqueueScansForSeries(ser.ID)
	if err == nil {
		t.Fatal("series tip Scan must reject when only indexed singles")
	}
	// Full re-scan still works.
	tid, err := s.FullRescanSource(src.ID)
	if err != nil || tid == 0 {
		t.Fatalf("full rescan: id=%d err=%v", tid, err)
	}
}

func TestAddSourceDuplicate(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Dup", RootID: rootID, QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://www.example.com/@chan",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddSource(ser.ID, library.AddSourceParams{
		URL: "https://example.com/@chan",
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestEnqueueScansDue(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Catch", SourceURL: "https://www.example.com/@catch", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	if err := s.MarkFullScanDone(srcID); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	n, err := s.EnqueueScansDue(now, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("default weekly cron should be due with no tip scanned history, got %d", n)
	}

	_, _ = s.Queue.CancelAll()
	tid := seedTaskID(t, s)
	if err := s.AddSourceHistory(srcID, library.SourceHistScanned, "tip", map[string]any{
		"mode": library.SourceHistModeScan, "created": 0, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}
	// Backdate tip scan to "now" so Due sees it as last run this minute.
	_, _ = s.DB.SQL.Exec(`UPDATE source_history SET created_at = ? WHERE source_id = ?`, now.Format(time.RFC3339Nano), srcID)
	n2, err := s.EnqueueScansDue(now, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("same-minute tick should not re-enqueue, got %d", n2)
	}

	_, _ = s.DB.SQL.Exec(`UPDATE sources SET scan_cron = '' WHERE id = ?`, srcID)
	n3, err := s.EnqueueScansDue(now.Add(time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 0 {
		t.Fatalf("never scan_cron should skip, got %d", n3)
	}

	if err := domains.SetActive(s.DB, "www.example.com", false); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE sources SET scan_cron = '0 * * * *' WHERE id = ?`, srcID)
	_, _ = s.DB.SQL.Exec(`DELETE FROM source_history WHERE source_id = ?`, srcID)
	n4, err := s.EnqueueScansDue(now.Add(2*time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n4 != 0 {
		t.Fatalf("inactive domain should skip catch-up, got %d", n4)
	}
}

func TestEnqueueScansDueSkipsMissedAtNotBefore(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Boot", SourceURL: "https://www.example.com/@boot", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	if err := s.MarkFullScanDone(srcID); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Queue.CancelAll()
	_, _ = s.DB.SQL.Exec(`UPDATE sources SET scan_cron = '0 * * * *' WHERE id = ?`, srcID)

	lastTip := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	boot := time.Date(2026, 7, 17, 12, 30, 0, 0, time.UTC)
	tid := seedTaskID(t, s)
	if err := s.AddSourceHistory(srcID, library.SourceHistScanned, "tip", map[string]any{
		"mode": library.SourceHistModeScan, "created": 0, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, tid); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE source_history SET created_at = ? WHERE source_id = ?`, lastTip.Format(time.RFC3339Nano), srcID)

	n, err := s.EnqueueScansDue(boot, boot)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("missed hourly fires before boot must wait for next cron, got %d", n)
	}

	nextHour := time.Date(2026, 7, 17, 13, 0, 1, 0, time.UTC)
	n2, err := s.EnqueueScansDue(nextHour, boot)
	if err != nil {
		t.Fatal(err)
	}
	if n2 < 1 {
		t.Fatalf("first cron after boot should enqueue, got %d", n2)
	}
}

func TestEnqueueScansDueIncompleteFullScan(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Incomplete", SourceURL: "https://www.example.com/@incomplete", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_, _ = s.Queue.CancelAll()
	// Leave full_scan_done false (CreateSeries may have queued a scan; cancel it).
	_, _ = s.DB.SQL.Exec(`UPDATE sources SET full_scan_done = 0, scan_cron = '0 * * * *' WHERE id = ?`, srcID)

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	n, err := s.EnqueueScansDue(now, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("incomplete feed with schedule should enqueue full scan when due, got %d", n)
	}
	tasks, err := s.Queue.ListActiveForSeries(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tsk := range tasks {
		if tsk.Kind != queue.KindScan {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(tsk.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["mode"] == "full" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected queued scan with mode=full")
	}
}

func TestEnqueueTipScanDownloadsOrderAndCap(t *testing.T) {
	s := openLib(t)
	_ = settings.SetDomainDefault(s.DB, 0, 1, 1, "10M", "0", false)
	_ = settings.Set(s.DB, settings.KeyDownloadWantedOrder, settings.DownloadWantedOrderNewest)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "TipDL", SourceURL: "https://www.example.com/@tipdl", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled' WHERE kind = 'scan'`)
	srcID := ser.Sources[0].ID
	old, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "old", Title: "Old", WebpageURL: "https://www.example.com/watch?v=old",
		UploadDate: "2020-01-01", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	neu, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "new", Title: "New", WebpageURL: "https://www.example.com/watch?v=new",
		UploadDate: "2024-06-01", SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.EnqueueTipScanDownloads(ser.ID, []int64{old.VideoID, neu.VideoID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 enqueue (cap 1), got %d", n)
	}
	var vid int64
	err = s.DB.SQL.QueryRow(`SELECT video_id FROM tasks WHERE kind = 'download' AND status = 'pending'`).Scan(&vid)
	if err != nil {
		t.Fatal(err)
	}
	if vid != neu.VideoID {
		t.Fatalf("newest-first should enqueue new first, got video %d want %d", vid, neu.VideoID)
	}
}

func TestEnqueueTipScanDownloadsSkipsUnmonitored(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Unmon", SourceURL: "https://www.example.com/@unmon", RootID: rootID,
		QualityProfileID: profileID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.SQL.Exec(`UPDATE tasks SET status = 'cancelled'`)
	srcID := ser.Sources[0].ID
	res, err := s.UpsertListed(ser.ID, library.ListedVideo{
		RemoteID: "x", Title: "X", WebpageURL: "https://www.example.com/watch?v=x",
		SourceID: srcID,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.EnqueueTipScanDownloads(ser.ID, []int64{res.VideoID})
	if err != nil || n != 0 {
		t.Fatalf("unmonitored: n=%d err=%v", n, err)
	}
}

func TestEnqueueScanSourceConflictMessage(t *testing.T) {
	s := openLib(t)
	rootID, profileID := seedRootProfile(t, s)
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Busy", SourceURL: "https://www.example.com/@busy", RootID: rootID,
		QualityProfileID: profileID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srcID := ser.Sources[0].ID
	_, err = s.EnqueueScanSource(srcID)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict while pending from AddSource, got %v", err)
	}
	if !strings.Contains(err.Error(), "scan already queued or running") {
		t.Fatalf("want actionable message, got %q", err.Error())
	}
	_, err = s.FullRescanSource(srcID)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want conflict on full rescan while active, got %v", err)
	}
	var done int
	_ = s.DB.SQL.QueryRow(`SELECT full_scan_done FROM sources WHERE id = ?`, srcID).Scan(&done)
	if done != 0 {
		t.Fatalf("full_scan_done must stay false when FullRescanSource conflicts before reset, got %d", done)
	}
}

