package web

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestBuildScheduledTasks(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := settings.SeedDefaults(d); err != nil {
		t.Fatal(err)
	}
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	_ = library.SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})

	now := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)
	got, err := buildScheduledTasks(lib, now)
	if err != nil {
		t.Fatal(err)
	}
	// No root retention TTL → retention_delete hidden (3 of 4 schedules).
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 without retention roots", len(got))
	}
	for _, row := range got {
		if row.Kind == queue.KindRetentionDelete {
			t.Fatalf("retention_delete shown without root TTL: %+v", row)
		}
	}

	ttl := int64(86400)
	if _, err := lib.CreateRoot("keep", t.TempDir(), "", &ttl); err != nil {
		t.Fatal(err)
	}
	got, err = buildScheduledTasks(lib, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len=%d want 4 with retention root", len(got))
	}
	for i := 1; i < len(got); i++ {
		prev, err := time.Parse(time.RFC3339Nano, got[i-1].EndsAt)
		if err != nil {
			t.Fatal(err)
		}
		cur, err := time.Parse(time.RFC3339Nano, got[i].EndsAt)
		if err != nil {
			t.Fatal(err)
		}
		if cur.After(prev) {
			t.Fatalf("not sorted furthest-first: %v before %v", got[i-1], got[i])
		}
	}
	kinds := map[string]bool{}
	for _, row := range got {
		kinds[row.Kind] = true
		if row.Key == "" || row.Schedule == "" || row.WaitTip == "" {
			t.Fatalf("row=%+v", row)
		}
	}
	for _, want := range []string{"download_wanted", queue.KindSyncFiles, queue.KindRetentionDelete, queue.KindYtDlpUpdate} {
		if !kinds[want] {
			t.Fatalf("missing kind %q in %+v", want, got)
		}
	}

	if err := settings.Set(d, settings.KeyYtDlpUpdateCron, ""); err != nil {
		t.Fatal(err)
	}
	got, err = buildScheduledTasks(lib, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 after disabling yt-dlp schedule", len(got))
	}
}

func TestScheduledTaskWaitTip(t *testing.T) {
	cases := []struct {
		rem  int
		want string
	}{
		{90, "in 1min"},
		{45, "in 45sec"},
		{3600 + 120, "in 1h"},
		{86400 + 3600, "in 1d"},
	}
	for _, tc := range cases {
		if got := scheduledTaskWaitTip(tc.rem); got != tc.want {
			t.Fatalf("scheduledTaskWaitTip(%d)=%q want %q", tc.rem, got, tc.want)
		}
	}
}
