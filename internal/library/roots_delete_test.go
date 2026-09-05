package library_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestDeleteRootUnused(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("temp", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.DeleteRoot(root.ID); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
	if _, err := lib.GetRoot(root.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteRootInUse(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("used", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := lib.CreateProfile("p", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: p.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	err = lib.DeleteRoot(root.ID)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	n, err := lib.CountSeriesUsingRoot(root.ID)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	counts, err := lib.SeriesCountsByRoot()
	if err != nil {
		t.Fatal(err)
	}
	if counts[root.ID] != 1 {
		t.Fatalf("SeriesCountsByRoot[%d]=%d", root.ID, counts[root.ID])
	}
}

func TestDeleteRootMissing(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	if err := lib.DeleteRoot(99999); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
