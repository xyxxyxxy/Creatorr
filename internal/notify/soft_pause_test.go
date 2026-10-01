package notify_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestSoftPauseAndAlertCookieInvalid(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "soft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	tid, err := q.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	notify.SoftPauseAndAlert(context.Background(), d, nil, tid, "example.com", apperrors.CodeCookieInvalid, "need login")

	paused, err := domains.IsPaused(d, "example.com")
	if err != nil || !paused {
		t.Fatalf("paused=%v err=%v", paused, err)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event = ? AND task_id = ?`,
		notify.EventCookieInvalid, tid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("notifications=%d", n)
	}
	row, ok, err := domains.Get(d, "example.com")
	if err != nil || !ok || !row.Active {
		t.Fatalf("domain should stay active: ok=%v row=%+v err=%v", ok, row, err)
	}
}

func TestSoftPauseAndAlertDownloadFailedNoPause(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "soft2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = settings.SeedDefaults(d)
	if err := domains.EnsureHost(d, "example.com"); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	tid, err := q.Enqueue(queue.EnqueueParams{Origin: queue.OriginManual, Kind: queue.KindDownload, Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	notify.SoftPauseAndAlert(context.Background(), d, nil, tid, "example.com", apperrors.CodeDownloadFailed, "boom")

	paused, err := domains.IsPaused(d, "example.com")
	if err != nil || paused {
		t.Fatalf("paused=%v err=%v want false", paused, err)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event = ? AND task_id = ?`,
		notify.EventYtDlpFailed, tid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("notifications=%d", n)
	}
}
