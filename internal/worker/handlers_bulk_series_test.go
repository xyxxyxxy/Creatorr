package worker_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/worker"
)

func TestBulkEditSeriesHandlerFailsWhenAllFailed(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("a", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "S", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	monitored := false
	tid, err := lib.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs: []int64{ser.ID}, Monitored: &monitored,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.Queue.UpdatePayload(tid, map[string]any{
		"series_ids":         []int64{ser.ID},
		"index":              0,
		"quality_profile_id": int64(99999),
	}); err != nil {
		t.Fatal(err)
	}
	task, err := lib.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	h := worker.BulkEditSeriesHandler(worker.Deps{Library: lib})
	if err := h(context.Background(), task, func(string, *float64) {}); err == nil {
		t.Fatal("want handler error when all series failed")
	}
}

func TestBulkEditSeriesHandlerDoneWhenSomeUpdated(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("a", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	a, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "A", RootID: root.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	monitored := false
	tid, err := lib.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs: []int64{a.ID, 99999}, Monitored: &monitored,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := lib.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	h := worker.BulkEditSeriesHandler(worker.Deps{Library: lib})
	if err := h(context.Background(), task, func(string, *float64) {}); err != nil {
		t.Fatalf("updated+skipped must stay done: %v", err)
	}
}
