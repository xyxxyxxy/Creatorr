package worker_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	apprise "github.com/unraid/apprise-go"
)

func TestRunnerCompletesStub(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	store := queue.NewStore(d)
	id, err := store.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&worker.Runner{
		Queue:    store,
		Handlers: worker.StubHandlers(),
		Interval: 20 * time.Millisecond,
	}).Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := d.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status)
		if err != nil {
			t.Fatal(err)
		}
		if status == queue.StatusDone {
			return
		}
		if status == queue.StatusFailed {
			t.Fatalf("task failed")
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timeout waiting for task done")
}

func TestRunnerCancelDoesNotMarkDownloadFailed(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)

	lib := &library.Store{DB: d, Queue: queue.NewStore(d)}
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "C", SourceURL: "https://example.com/c", RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lib.Queue.CancelAll()
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, source_url, status)
		VALUES (?, 'vid1', 'Ep', 'https://example.com/v/1', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	videoID, _ := res.LastInsertId()

	store := lib.Queue
	id, err := store.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", SeriesID: ser.ID, VideoID: videoID,
	})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	handlers := worker.StubHandlers()
	handlers[queue.KindDownload] = func(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&worker.Runner{
		Queue:    store,
		Library:  lib,
		Handlers: handlers,
		Interval: 20 * time.Millisecond,
	}).Run(ctx)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := store.Cancel(id); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := d.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status)
		if err == nil && status == queue.StatusCancelled {
			var vstatus string
			if err := d.SQL.QueryRow(`SELECT status FROM videos WHERE id = ?`, videoID).Scan(&vstatus); err != nil {
				t.Fatal(err)
			}
			if vstatus != "wanted" {
				t.Fatalf("video status=%s want wanted", vstatus)
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timeout waiting for cancelled task")
}


func TestRunnerRateLimitedNotifiesWithoutUnmonitor(t *testing.T) {
	testRunnerDomainIssueNotify(t, apperrors.CodeDownloadFailed, "HTTP Error 429: Too Many Requests", true)
}

func testRunnerDomainIssueNotify(t *testing.T, returnCode, returnMsg string, wantPaused bool) {
	t.Helper()
	var notified atomic.Bool

	d, err := db.Open(filepath.Join(t.TempDir(), "pause.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)

	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		if title != "" {
			notified.Store(true)
		}
		return nil
	})
	defer notify.SetSendFnForTest(old)

	if _, err := notify.Upsert(d, 0, "t", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{
		notify.EventCookieInvalid, notify.EventRateLimited, notify.EventYtDlpFailed,
	}); err != nil {
		t.Fatal(err)
	}

	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}

	store := queue.NewStore(d)
	id, err := store.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	handlers := worker.StubHandlers()
	handlers[queue.KindDownload] = func(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) error {
		if returnCode == apperrors.CodeCookieInvalid {
			return apperrors.New(apperrors.CodeCookieInvalid, returnMsg)
		}
		return apperrors.WithDetail(apperrors.New(returnCode, "download failed"), returnMsg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&worker.Runner{
		Queue:    store,
		Handlers: handlers,
		Interval: 20 * time.Millisecond,
	}).Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	var sawFailed bool
	for time.Now().Before(deadline) {
		var status string
		err := d.SQL.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status)
		if err != nil {
			t.Fatal(err)
		}
		if status == queue.StatusFailed {
			sawFailed = true
			row, ok, err := domains.Get(d, "example.com")
			if err != nil {
				t.Fatal(err)
			}
			if !ok || !row.Active {
				t.Fatalf("domain should stay active, got %+v", row)
			}
			paused, err := domains.IsPaused(d, "example.com")
			if err != nil {
				t.Fatal(err)
			}
			// Finish lands before SoftPauseAndAlert; wait until pause + notify settle.
			if paused == wantPaused && notified.Load() {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !sawFailed {
		t.Fatal("timeout waiting for failed task")
	}
	paused, _ := domains.IsPaused(d, "example.com")
	t.Fatalf("timeout after failed: paused=%v want %v notified=%v", paused, wantPaused, notified.Load())
}

func TestRunnerDownloadFailedNotifies(t *testing.T) {
	testRunnerDomainIssueNotify(t, apperrors.CodeDownloadFailed, "extractor exploded", true)
}

func TestRunnerRemuxFailedDoesNotPause(t *testing.T) {
	testRunnerDomainIssueNotify(t, apperrors.CodeRemuxFailed, "muxer exploded", false)
}

func TestRunnerDownloadsDoneDigest(t *testing.T) {
	prev := worker.SetDigestDebounceForTest(50 * time.Millisecond)
	defer worker.SetDigestDebounceForTest(prev)

	var digests atomic.Int32
	var digestBody atomic.Value
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		if nt == apprise.NotifySuccess {
			digests.Add(1)
			digestBody.Store(body)
		}
		return nil
	})
	defer notify.SetSendFnForTest(old)

	d, err := db.Open(filepath.Join(t.TempDir(), "digest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 2, "10M", "0", false)
	if _, err := notify.Upsert(d, 0, "d", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{notify.EventDownloadDigest}); err != nil {
		t.Fatal(err)
	}
	_ = domains.EnsureHost(d, "example.com")

	lib := &library.Store{DB: d, Queue: queue.NewStore(d)}
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", SourceURL: "https://example.com/s", RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lib.Queue.CancelAll()

	insertVid := func(remote, title string) int64 {
		t.Helper()
		res, err := d.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, source_url, status)
			VALUES (?, ?, ?, ?, 'downloaded')
		`, ser.ID, remote, title, "https://example.com/"+remote)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	v1 := insertVid("a", "One")
	v2 := insertVid("b", "Two")

	store := lib.Queue
	_, err = store.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", SeriesID: ser.ID, VideoID: v1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Enqueue(queue.EnqueueParams{Kind: queue.KindDownload, Domain: "example.com", SeriesID: ser.ID, VideoID: v2})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&worker.Runner{
		Queue:    store,
		Library:  lib,
		Handlers: worker.StubHandlers(),
		Interval: 20 * time.Millisecond,
	}).Run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if digests.Load() >= 1 {
			body, _ := digestBody.Load().(string)
			if body == "" || !containsAll(body, "One", "Two") {
				t.Fatalf("digest body=%q", body)
			}
			time.Sleep(100 * time.Millisecond)
			if digests.Load() != 1 {
				t.Fatalf("expected one digest, got %d", digests.Load())
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timeout waiting for downloads_done digest")
}

func TestRunnerMediaTypeExcludedMarksIgnored(t *testing.T) {
	var notified atomic.Bool
	old := notify.SetSendFnForTest(func(urls []string, title, body string, nt apprise.NotifyType) error {
		notified.Store(true)
		return nil
	})
	defer notify.SetSendFnForTest(old)

	d, err := db.Open(filepath.Join(t.TempDir(), "mtex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_ = settings.SeedDefaults(d)
	_ = settings.SetDomainDefault(d, 0, 8, 1, "10M", "0", false)
	if _, err := notify.Upsert(d, 0, "t", "discord://111111111111111111/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN012345", []string{
		notify.EventYtDlpFailed,
	}); err != nil {
		t.Fatal(err)
	}

	lib := &library.Store{DB: d, Queue: queue.NewStore(d)}
	root, err := lib.CreateRoot("archive", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("default", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "MT", SourceURL: "https://example.com/mt", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = lib.Queue.CancelAll()
	res, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, source_url, status)
		VALUES (?, 'vid-mt', 'Short', 'https://example.com/v/s', 'wanted')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	videoID, _ := res.LastInsertId()

	store := lib.Queue
	id, err := store.Enqueue(queue.EnqueueParams{
		Kind: queue.KindDownload, Domain: "example.com", SeriesID: ser.ID, VideoID: videoID,
	})
	if err != nil {
		t.Fatal(err)
	}

	handlers := worker.StubHandlers()
	handlers[queue.KindDownload] = func(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) error {
		return apperrors.New(apperrors.CodeMediaTypeExcluded, "media type excluded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&worker.Runner{
		Queue:    store,
		Library:  lib,
		Handlers: handlers,
		Interval: 20 * time.Millisecond,
	}).Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var status, code string
		err := d.SQL.QueryRow(`SELECT status, COALESCE(error_code,'') FROM tasks WHERE id = ?`, id).Scan(&status, &code)
		if err != nil {
			t.Fatal(err)
		}
		if status == queue.StatusDone {
			v, err := lib.GetVideo(videoID)
			if err != nil {
				t.Fatal(err)
			}
			// Finish can land before MarkIgnoredMediaType; wait for ignored.
			if v.Status == "ignored" {
				if code != apperrors.CodeMediaTypeExcluded {
					t.Fatalf("task code=%q", code)
				}
				if notified.Load() {
					t.Fatal("must not ytdlp_failed-notify on media type exclude")
				}
				return
			}
		}
		if status == queue.StatusFailed {
			t.Fatal("media type exclude must finish done, not failed")
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timeout")
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

