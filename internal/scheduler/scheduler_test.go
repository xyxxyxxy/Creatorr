package scheduler_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/scheduler"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestTickEnqueuesCatchupScanAndDownloadWanted(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.Set(d, settings.KeyDownloadWantedCron, "*/1 * * * *")
	_ = settings.Set(d, settings.KeySyncFilesCron, "")
	_ = settings.Set(d, settings.KeyRetentionDeleteCron, "")
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)

	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title:            "S",
		SourceURL:        "https://example.com/feed",
		RootID:           root.ID,
		QualityProfileID: prof.ID,
		Monitored:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	// Tip Scan schedule only enqueues when full scan is done.
	if _, err := d.SQL.Exec(`UPDATE sources SET full_scan_done = 1, scan_cron = '*/1 * * * *' WHERE series_id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var sourceID int64
	if err := d.SQL.QueryRow(`SELECT id FROM sources WHERE series_id = ?`, ser.ID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}

	_, err = d.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, ?, 'x1', 'Wanted', 'https://example.com/v/1', 'wanted')
	`, ser.ID, sourceID)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	sch := &scheduler.Scheduler{
		Library: lib,
		Now:     func() time.Time { return now },
	}
	// TickOnce without Run: zero last / startedAt = mid-process catch-up still works.
	sch.TickOnce(context.Background(), nil)

	var scans, downloads int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&scans)
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'download' AND status = 'pending'`).Scan(&downloads)
	if scans < 1 {
		t.Fatalf("expected catch-up scan task, got %d", scans)
	}
	if downloads < 1 {
		t.Fatalf("expected download wanted task, got %d", downloads)
	}

	sch.TickOnce(context.Background(), nil)
	var scans2 int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&scans2)
	if scans2 != scans {
		t.Fatalf("duplicate scans: %d -> %d", scans, scans2)
	}
}

func TestRunSkipsMissedSchedulesAtBoot(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "boot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.Set(d, settings.KeyDownloadWantedCron, "0 * * * *")
	_ = settings.Set(d, settings.KeySyncFilesCron, "0 * * * *")
	_ = settings.Set(d, settings.KeyRetentionDeleteCron, "")
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)

	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", SourceURL: "https://example.com/feed",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	if _, err := d.SQL.Exec(`UPDATE sources SET full_scan_done = 1, scan_cron = '0 * * * *' WHERE series_id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}
	var sourceID int64
	if err := d.SQL.QueryRow(`SELECT id FROM sources WHERE series_id = ?`, ser.ID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	lastTip := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	histTask, err := q.InsertRunning(queue.EnqueueParams{
		Kind: "test", Domain: queue.SystemDomain, Message: "hist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.AddSourceHistory(sourceID, library.SourceHistScanned, "tip", map[string]any{
		"mode": library.SourceHistModeScan, "created": 0, "updated": 0,
		"created_ids": []int64{}, "updated_ids": []int64{},
	}, histTask); err != nil {
		t.Fatal(err)
	}
	_, _ = d.SQL.Exec(`UPDATE source_history SET created_at = ? WHERE source_id = ?`, lastTip.Format(time.RFC3339Nano), sourceID)
	_, err = d.SQL.Exec(`
		INSERT INTO videos (series_id, source_id, remote_id, title, source_url, status)
		VALUES (?, ?, 'x1', 'Wanted', 'https://example.com/v/1', 'wanted')
	`, ser.ID, sourceID)
	if err != nil {
		t.Fatal(err)
	}

	boot := time.Date(2026, 7, 17, 12, 30, 0, 0, time.UTC)
	clock := boot
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sch := &scheduler.Scheduler{
		Library: lib,
		Tick:    time.Hour, // do not advance past boot in this test
		Now:     func() time.Time { return clock },
	}
	done := make(chan struct{})
	go func() {
		sch.Run(ctx)
		close(done)
	}()
	// First TickOnce runs synchronously at start of Run; give it a beat.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	var scans, downloads, syncs int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status = 'pending'`).Scan(&scans)
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'download' AND status = 'pending'`).Scan(&downloads)
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'sync_files' AND status = 'pending'`).Scan(&syncs)
	if scans != 0 || downloads != 0 || syncs != 0 {
		t.Fatalf("boot must not catch up missed schedules: scans=%d downloads=%d syncs=%d", scans, downloads, syncs)
	}
}

func TestTickEnqueuesFullScanWhenBackfillIncomplete(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.Set(d, settings.KeyDownloadWantedCron, "")
	_ = settings.Set(d, settings.KeySyncFilesCron, "")
	_ = settings.Set(d, settings.KeyRetentionDeleteCron, "")

	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", SourceURL: "https://example.com/feed",
		RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	// CreateSeries defaults weekly scan_cron; leave full_scan_done false.
	if _, err := d.SQL.Exec(`UPDATE sources SET full_scan_done = 0 WHERE series_id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}

	sch := &scheduler.Scheduler{
		Library: lib,
		Now:     func() time.Time { return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC) },
	}
	sch.TickOnce(context.Background(), nil)

	tasks, err := q.ListActiveForSeries(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	var full int
	for _, tsk := range tasks {
		if tsk.Kind != queue.KindScan {
			continue
		}
		if strings.Contains(tsk.Payload, `"mode":"full"`) || strings.Contains(tsk.Payload, `"mode": "full"`) {
			full++
		}
	}
	if full != 1 {
		t.Fatalf("expected one full Scan while backfill incomplete, got full=%d tasks=%d", full, len(tasks))
	}

	// Empty scan_cron: scheduled tick must not enqueue tip or full.
	if _, err := d.SQL.Exec(`UPDATE sources SET scan_cron = '' WHERE series_id = ?`, ser.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = q.CancelAll()
	sch.TickOnce(context.Background(), nil)
	var scans int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM tasks WHERE kind = 'scan' AND status IN ('pending','running')`).Scan(&scans)
	if scans != 0 {
		t.Fatalf("never scan_cron: expected 0 scans, got %d", scans)
	}
}

func TestFileSyncMarksMissing(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'g', 'Gone', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	_, err = d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', ?)
	`, vid, filepath.Join(t.TempDir(), "missing.mkv"), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "test sync"})
	if err != nil {
		t.Fatal(err)
	}
	syncRes, err := lib.FileSyncPass(tid)
	if err != nil || syncRes.Total() != 1 {
		t.Fatalf("file sync: n=%d err=%v", syncRes.Total(), err)
	}
	var status string
	_ = d.SQL.QueryRow(`SELECT status FROM videos WHERE id = ?`, vid).Scan(&status)
	if status != "missing" {
		t.Fatalf("status=%s", status)
	}
	var files int
	_ = d.SQL.QueryRow(`SELECT COUNT(*) FROM files WHERE video_id = ?`, vid).Scan(&files)
	if files != 1 {
		t.Fatalf("want file row kept, got %d", files)
	}
}

func TestFileSyncRestoresMissing(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fs2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	rootDir := t.TempDir()
	root, err := lib.CreateRoot("r", rootDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(rootDir, "back.mkv")
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'b', 'Back', 'missing')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	_, err = d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', ?)
	`, vid, media, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "test sync"})
	if err != nil {
		t.Fatal(err)
	}
	syncRes, err := lib.FileSyncPass(tid)
	if err != nil || syncRes.Total() != 1 {
		t.Fatalf("file sync: n=%d err=%v", syncRes.Total(), err)
	}
	var status string
	_ = d.SQL.QueryRow(`SELECT status FROM videos WHERE id = ?`, vid).Scan(&status)
	if status != "downloaded" {
		t.Fatalf("status=%s", status)
	}
}

func TestFileSyncSkipsOfflineRoot(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "fs3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	offline := filepath.Join(t.TempDir(), "gone-root")
	if err := os.MkdirAll(offline, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := lib.CreateRoot("r", offline, nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'g', 'Gone', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	_, err = d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', ?)
	`, vid, filepath.Join(offline, "clip.mkv"), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(offline); err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "test sync"})
	if err != nil {
		t.Fatal(err)
	}
	syncRes, err := lib.FileSyncPass(tid)
	if err != nil || syncRes.Total() != 0 {
		t.Fatalf("file sync: n=%d err=%v want 0 (offline root)", syncRes.Total(), err)
	}
	var status string
	_ = d.SQL.QueryRow(`SELECT status FROM videos WHERE id = ?`, vid).Scan(&status)
	if status != "downloaded" {
		t.Fatalf("status=%s want still downloaded", status)
	}
}

func TestRetentionPurge(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "rm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	ttl := int64(60)
	rootDir := t.TempDir()
	root, err := lib.CreateRoot("r", rootDir, &ttl)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(rootDir, "old.mkv")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status)
		VALUES (?, 'old', 'Old', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	vid, _ := res.LastInsertId()
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	_, err = d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'video', ?)
	`, vid, media, old)
	if err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{Kind: queue.KindRetentionDelete, Domain: queue.SystemDomain, Message: "test purge"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := lib.RetentionPurgePassAt(time.Now().UTC(), tid)
	if err != nil || n != 1 {
		t.Fatalf("retention purge: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(media); !os.IsNotExist(err) {
		t.Fatal("media still present")
	}
	var status string
	_ = d.SQL.QueryRow(`SELECT status FROM videos WHERE id = ?`, vid).Scan(&status)
	if status != "deleted" {
		t.Fatalf("status=%s", status)
	}
	var reason string
	_ = d.SQL.QueryRow(`SELECT detail FROM video_history WHERE video_id = ? ORDER BY id DESC LIMIT 1`, vid).Scan(&reason)
	if reason != `{"reason":"retention"}` {
		t.Fatalf("history detail=%q", reason)
	}
}
