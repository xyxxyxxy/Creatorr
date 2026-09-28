package library_test

import (
	"path/filepath"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestListSourcesOrderByLabelThenURL(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	s := library.NewStore(d, queue.NewStore(d))
	root, err := s.CreateRoot("lib", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := s.CreateProfile("best", "bv*+ba/b")
	if err != nil {
		t.Fatal(err)
	}
	ser, err := s.CreateSeries(library.CreateSeriesParams{
		Title: "Show", RootID: root.ID, QualityProfileID: prof.ID, Monitored: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Insert out of display order: unlabeled Z, labeled Beta, labeled Alpha, unlabeled A.
	adds := []library.AddSourceParams{
		{URL: "https://example.com/z"},
		{URL: "https://example.com/beta", Label: "Beta"},
		{URL: "https://example.com/alpha", Label: "Alpha"},
		{URL: "https://example.com/a"},
	}
	for _, p := range adds {
		if _, err := s.AddSource(ser.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListSources(ser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	want := []string{
		"https://example.com/alpha", // Alpha
		"https://example.com/beta",  // Beta
		"https://example.com/a",     // no label → URL
		"https://example.com/z",
	}
	for i, u := range want {
		if got[i].URL != u {
			t.Fatalf("[%d]=%s want %s (label=%q)", i, got[i].URL, u, got[i].Label.String)
		}
	}
}
