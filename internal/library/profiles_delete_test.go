package library_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestDeleteProfileUnused(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	p, err := lib.CreateProfile("temp", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.DeleteProfile(p.ID); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
	if _, err := lib.GetProfile(p.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteProfileInUse(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("r", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := lib.CreateProfile("used", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: p.ID, Monitored: false,
	}); err != nil {
		t.Fatal(err)
	}
	err = lib.DeleteProfile(p.ID)
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	n, err := lib.CountSeriesUsingProfile(p.ID)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	counts, err := lib.SeriesCountsByProfile()
	if err != nil {
		t.Fatal(err)
	}
	if counts[p.ID] != 1 {
		t.Fatalf("SeriesCountsByProfile[%d]=%d", p.ID, counts[p.ID])
	}
}

func TestDeleteProfileMissing(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	if err := lib.DeleteProfile(99999); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
