package library_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestParseSeriesNFOFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	body := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>Show Title</title>
  <sorttitle>Show Sort</sorttitle>
  <plot>Show plot</plot>
  <tagline>Tag</tagline>
  <studio>Studio X</studio>
  <genre>Comedy</genre>
  <tag>indie</tag>
  <premiered>2020-05-01</premiered>
  <status>Ended</status>
  <country>US</country>
  <mpaa>TV-14</mpaa>
  <uniqueid type="creatorr" default="true">abc123</uniqueid>
  <actor>
    <name>Alice</name>
    <role>Host</role>
    <order>0</order>
  </actor>
</tvshow>
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := library.ParseSeriesNFOFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Show Title" || p.Plot != "Show plot" || p.Studio != "Studio X" {
		t.Fatalf("meta: %+v", p)
	}
	if p.Monitored {
		t.Fatal("Ended status should be unmonitored")
	}
	if p.UniqueIDType != "creatorr" || p.UniqueIDValue != "abc123" {
		t.Fatalf("uniqueid: %s/%s", p.UniqueIDType, p.UniqueIDValue)
	}
	if len(p.Genres) != 1 || p.Genres[0] != "Comedy" {
		t.Fatalf("genres: %v", p.Genres)
	}
	if len(p.Actors) != 1 || p.Actors[0].Name != "Alice" || p.Actors[0].Role != "Host" {
		t.Fatalf("actors: %+v", p.Actors)
	}
	if p.Premiered != "2020-05-01" {
		t.Fatalf("premiered: %q", p.Premiered)
	}
}

func TestParseSeriesNFOFileMonitoredDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tvshow.nfo")
	if err := os.WriteFile(path, []byte(`<tvshow><title>X</title><status>Continuing</status></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := library.ParseSeriesNFOFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Monitored {
		t.Fatal("Continuing should be monitored")
	}
}

func TestDiscoverSeriesFolderArt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clearlogo.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	art := library.DiscoverSeriesFolderArt(dir)
	if art["poster"] == "" || art["clearlogo"] == "" {
		t.Fatalf("art: %v", art)
	}
	if art["banner"] != "" {
		t.Fatal("unexpected banner")
	}
}
