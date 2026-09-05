package library_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func TestCommonVideoMetadata(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "common-video.db"))
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
		Title: "S", RootID: root.ID, QualityProfileID: profile.ID, Monitored: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(remote, title string) int64 {
		t.Helper()
		res, err := d.SQL.Exec(`
			INSERT INTO videos (series_id, remote_id, title, status, studio, country, mpaa, genres, tags, actors)
			VALUES (?, ?, ?, 'wanted', '', '', '', '[]', '[]', '[]')
		`, ser.ID, remote, title)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := insert("a", "A")
	b := insert("b", "B")
	actorsSame := []library.SeriesActor{
		{Name: "Host", Role: "Host", Order: 0},
		{Name: "Guest", Role: "Guest", Order: 1},
	}
	actorsReordered := []library.SeriesActor{
		{Name: "Guest", Role: "Guest", Order: 0},
		{Name: "Host", Role: "Host", Order: 1},
	}
	setMeta := func(id int64, studio, country, mpaa string, genres, tags []string, actors []library.SeriesActor) {
		t.Helper()
		gb, err := json.Marshal(genres)
		if err != nil {
			t.Fatal(err)
		}
		tb, err := json.Marshal(tags)
		if err != nil {
			t.Fatal(err)
		}
		ab, err := json.Marshal(actors)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.SQL.Exec(`
			UPDATE videos SET studio = ?, country = ?, mpaa = ?, genres = ?, tags = ?, actors = ?
			WHERE id = ?
		`, studio, country, mpaa, string(gb), string(tb), string(ab), id); err != nil {
			t.Fatal(err)
		}
	}
	setMeta(a, "Studio X", "US", "TV-MA", []string{"Comedy", "Talk"}, []string{"live"}, actorsSame)
	setMeta(b, "Studio X", "UK", "TV-MA", []string{"Comedy", "Talk"}, []string{"live"}, actorsSame)

	got, err := lib.CommonVideoMetadata([]int64{a, b})
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

	setMeta(b, "Studio X", "UK", "TV-MA", []string{"Comedy", "Talk"}, []string{"live"}, actorsReordered)
	got, err = lib.CommonVideoMetadata([]int64{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Actors.Same {
		t.Fatalf("reordered actors must not be same: %+v", got.Actors)
	}
}
