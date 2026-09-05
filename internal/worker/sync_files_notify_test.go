package worker_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func TestSyncFilesHandlerDigestOnce(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "sync-digest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	rootDir := t.TempDir()
	root, err := lib.CreateRoot("r", rootDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: prof.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	changedPath := filepath.Join(rootDir, "a.mkv")
	if err := os.WriteFile(changedPath, []byte("ab"), 0o644); err != nil {
		t.Fatal(err)
	}
	gonePath := filepath.Join(rootDir, "gone.mkv")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range []struct {
		remote, status, path string
		size                 any
	}{
		{"gone", "downloaded", gonePath, int64(5)},
		{"chg", "downloaded", changedPath, int64(99)},
	} {
		res, err := d.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status) VALUES (?, ?, ?, ?)
		`, ser.ID, row.remote, row.remote, row.status)
		if err != nil {
			t.Fatal(err)
		}
		vid, _ := res.LastInsertId()
		if _, err := d.SQL.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'video', ?, ?)
		`, vid, row.path, now, row.size); err != nil {
			t.Fatal(err)
		}
	}
	// Sidecar-only video: media ok, nfo gone → still digests.
	okMedia := filepath.Join(rootDir, "ok.mkv")
	if err := os.WriteFile(okMedia, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	resOK, err := d.SQL.Exec(`
		INSERT INTO videos (series_id, remote_id, title, status) VALUES (?, 'ok', 'ok', 'downloaded')
	`, ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	okVid, _ := resOK.LastInsertId()
	if _, err := d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'video', ?, 2)
	`, okVid, okMedia, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
		INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'nfo', ?, 10)
	`, okVid, filepath.Join(rootDir, "missing.nfo"), now); err != nil {
		t.Fatal(err)
	}

	tid, err := q.Enqueue(queue.EnqueueParams{Kind: queue.KindSyncFiles, Domain: queue.SystemDomain, Message: "sync"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := q.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}

	h := worker.SyncFilesHandler(worker.Deps{
		Library: lib,
		YtDlp:   &ytdlp.Client{Bin: filepath.Join(t.TempDir(), "missing")},
	})
	if err := h(context.Background(), task, func(string, *float64) {}); err != nil {
		t.Fatal(err)
	}

	items, err := notify.ListNotifications(d, notify.ListFilter{Event: notify.EventFileSyncIssues}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 digest notification, got %d", len(items))
	}
	if !items[0].TaskID.Valid || items[0].TaskID.Int64 != tid {
		t.Fatalf("task_id=%v want %d", items[0].TaskID, tid)
	}
}
