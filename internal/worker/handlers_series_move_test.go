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

func TestSeriesMoveHandlerUpdatesTitleAndRoot(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	lib := library.NewStore(d, queue.NewStore(d))
	rootA, err := lib.CreateRoot("a", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := lib.CreateRoot("b", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Before", RootID: rootA.ID, QualityProfileID: prof.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := lib.EnqueueSeriesMove(ser.ID, "After", rootB.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := lib.Queue.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	h := worker.SeriesMoveHandler(worker.Deps{Library: lib})
	if err := h(context.Background(), task, func(string, *float64) {}); err != nil {
		t.Fatal(err)
	}
	got, err := lib.GetSeries(ser.ID, false)
	if err != nil || got.Title != "After" || got.RootID != rootB.ID {
		t.Fatalf("series=%+v err=%v", got, err)
	}
	if h2 := worker.DefaultHandlers(worker.Deps{Library: lib}); h2[queue.KindSeriesMove] == nil {
		t.Fatal("series_move handler missing")
	}
}
