package library_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestEnqueueBulkEditSeriesBusy(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bulk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	mode := library.DeliveryAudio
	id, err := lib.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs:    []int64{ser.ID},
		DeliveryMode: &mode,
	})
	if err != nil || id <= 0 {
		t.Fatalf("enqueue: id=%d err=%v", id, err)
	}
	busy, err := lib.BulkEditSeriesBusy()
	if err != nil || !busy {
		t.Fatalf("busy=%v err=%v", busy, err)
	}
	_, err = lib.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs:    []int64{ser.ID},
		DeliveryMode: &mode,
	})
	if !errors.Is(err, library.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestBulkEditSeriesPassMetadata(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bulk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	a, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "A", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "B", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	studio := "Studio X"
	tid, err := lib.EnqueueBulkEditSeries(library.BulkEditSeriesParams{
		SeriesIDs: []int64{a.ID, b.ID},
		Studio:    &studio,
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := q.GetTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	updated, skipped, failed, err := lib.BulkEditSeriesPass(context.Background(), task, nil)
	if err != nil || updated != 2 || skipped != 0 || failed != 0 {
		t.Fatalf("pass updated=%d skipped=%d failed=%d err=%v", updated, skipped, failed, err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		got, err := lib.GetSeries(id, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Meta.Studio != studio {
			t.Fatalf("series %d studio=%q", id, got.Meta.Studio)
		}
	}
}

func TestSetSeriesMonitoredBulk(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bulk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	lib := library.NewStore(d, queue.NewStore(d))
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, skipped, err := lib.SetSeriesMonitoredBulk([]int64{ser.ID, 99999}, false)
	if err != nil || updated != 1 || skipped != 1 {
		t.Fatalf("updated=%d skipped=%d err=%v", updated, skipped, err)
	}
	got, err := lib.GetSeries(ser.ID, false)
	if err != nil || got.Monitored {
		t.Fatalf("monitored=%v err=%v", got.Monitored, err)
	}
}

func TestCommonSeriesMetadata(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "common.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := queue.NewStore(d)
	lib := library.NewStore(d, q)
	root, err := lib.CreateRoot("r", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := lib.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	a, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "A", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := lib.CreateSeries(library.CreateSeriesParams{
		Title: "B", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	actorsSame := []library.SeriesActor{
		{Name: "Host", Role: "Host", Order: 0},
		{Name: "Guest", Role: "Guest", Order: 1},
	}
	actorsReordered := []library.SeriesActor{
		{Name: "Guest", Role: "Guest", Order: 0},
		{Name: "Host", Role: "Host", Order: 1},
	}
	if err := lib.SaveSeriesMetadata(a.ID, library.SaveSeriesMetadataParams{
		Studio: "Studio X",
		Country: "US",
		MPAA:   "TV-MA",
		Genres: []string{"Comedy", "Talk"},
		Tags:   []string{"live"},
		Actors: actorsSame,
	}); err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveSeriesMetadata(b.ID, library.SaveSeriesMetadataParams{
		Studio: "Studio X",
		Country: "UK",
		MPAA:   "TV-MA",
		Genres: []string{"Comedy", "Talk"},
		Tags:   []string{"live"},
		Actors: actorsSame,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := lib.CommonSeriesMetadata([]int64{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Studio.Same || got.Studio.Value != "Studio X" {
		t.Fatalf("studio=%+v", got.Studio)
	}
	if got.Country.Same {
		t.Fatalf("country should differ: %+v", got.Country)
	}
	if !got.MPAA.Same || got.MPAA.Value != "TV-MA" {
		t.Fatalf("mpaa=%+v", got.MPAA)
	}
	if !got.Genres.Same || len(got.Genres.Value) != 2 || got.Genres.Value[0] != "Comedy" {
		t.Fatalf("genres=%+v", got.Genres)
	}
	if !got.Tags.Same || len(got.Tags.Value) != 1 || got.Tags.Value[0] != "live" {
		t.Fatalf("tags=%+v", got.Tags)
	}
	if !got.Actors.Same || len(got.Actors.Value) != 2 || got.Actors.Value[0].Name != "Host" {
		t.Fatalf("actors=%+v", got.Actors)
	}

	if err := lib.SaveSeriesMetadata(b.ID, library.SaveSeriesMetadataParams{
		Studio: "Studio X",
		Country: "UK",
		MPAA:   "TV-MA",
		Genres: []string{"Comedy", "Talk"},
		Tags:   []string{"live"},
		Actors: actorsReordered,
	}); err != nil {
		t.Fatal(err)
	}
	got, err = lib.CommonSeriesMetadata([]int64{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Actors.Same {
		t.Fatalf("reordered actors must not be same: %+v", got.Actors)
	}
}
