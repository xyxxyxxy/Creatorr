package library

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestEnqueueYtDlpUpdateDuplicate(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ytdlp-enq.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := queue.NewStore(d)
	lib := NewStore(d, q)
	id1, err := lib.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateDue, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == 0 {
		t.Fatal("expected task id")
	}
	_, err = lib.EnqueueYtDlpUpdate(queue.PriorityYtDlpUpdateDue, "manual")
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
}

func TestEnqueueYtDlpUpdateWhenCronDisabled(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "ytdlp-off.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = settings.Set(d, settings.KeyYtDlpUpdateCron, "")
	enabled, err := settings.YtDlpUpdatesEnabled(d)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected updates off")
	}
}
