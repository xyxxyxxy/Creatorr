package library

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/config"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func TestListSourcesFiltered(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = settings.SeedDefaults(d)
	_ = SeedDefaults(d, config.Config{InitialRootFolder: t.TempDir()})
	q := queue.NewStore(d)
	s := NewStore(d, q)

	a, err := s.CreateSeries(CreateSeriesParams{
		Title: "Alpha", RootID: 1, QualityProfileID: 1, Monitored: true,
		SourceURL: "https://www.example.com/@alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSeries(CreateSeriesParams{
		Title: "Beta", RootID: 1, QualityProfileID: 1, Monitored: false,
		SourceURL: "https://cdn.example.org/@beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	single, err := s.AddSource(a.ID, AddSourceParams{
		URL: "https://www.example.com/watch?v=single1", Kind: SourceKindSingle, Label: "Main single",
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.CountSourcesFiltered(SourceListFilter{})
	if err != nil || n < 3 {
		t.Fatalf("library-wide count=%d err=%v", n, err)
	}
	ids, err := s.ListSourceIDsFiltered(SourceListFilter{SeriesID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("ListSourceIDsFiltered series a want >=2 got %v", ids)
	}
	scoped, err := s.CountSourcesFiltered(SourceListFilter{SeriesID: a.ID})
	if err != nil || scoped != 2 {
		t.Fatalf("series A count=%d want 2 err=%v", scoped, err)
	}
	feedOnly, err := s.ListSourcesFiltered(SourceListFilter{Kinds: []string{SourceKindFeed}}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range feedOnly {
		if row.Kind != SourceKindFeed {
			t.Fatalf("kind filter leaked %q", row.Kind)
		}
		if row.SeriesTitle == "" {
			t.Fatalf("missing series title")
		}
	}
	singles, err := s.ListSourcesFiltered(SourceListFilter{Kinds: []string{SourceKindSingle}, SeriesID: a.ID}, 50, 0)
	if err != nil || len(singles) != 1 {
		t.Fatalf("single scoped len=%d err=%v", len(singles), err)
	}

	byURL, err := s.ListSourcesFiltered(SourceListFilter{Q: "cdn.example", QField: QFieldSourceURL}, 50, 0)
	if err != nil || len(byURL) != 1 || !strings.Contains(byURL[0].URL, "cdn.example") {
		t.Fatalf("q_field=url got %#v err=%v", byURL, err)
	}
	byLabel, err := s.ListSourcesFiltered(SourceListFilter{Q: "Main", QField: QFieldSourceLabel}, 50, 0)
	if err != nil || len(byLabel) != 1 {
		t.Fatalf("q_field=label len=%d err=%v", len(byLabel), err)
	}
	if !byLabel[0].Label.Valid || !strings.Contains(byLabel[0].Label.String, "Main") {
		t.Fatalf("q_field=label got %#v", byLabel[0])
	}
	bySeries, err := s.ListSourcesFiltered(SourceListFilter{Q: "Alpha", QField: QFieldSourceSeries}, 50, 0)
	if err != nil || len(bySeries) < 1 {
		t.Fatalf("q_field=series len=%d err=%v", len(bySeries), err)
	}
	for _, row := range bySeries {
		if !strings.Contains(row.SeriesTitle, "Alpha") {
			t.Fatalf("q_field=series leaked title=%q", row.SeriesTitle)
		}
	}

	facets, err := s.SourceFilterFacetsForSeries(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Kinds) != 2 {
		t.Fatalf("series A kinds=%v want feed+single", facets.Kinds)
	}
	feedOnlyFacets, err := s.SourceFilterFacetsForSeries(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(feedOnlyFacets.Kinds) != 1 || feedOnlyFacets.Kinds[0] != SourceKindFeed {
		t.Fatalf("series B kinds=%v want [feed]", feedOnlyFacets.Kinds)
	}

	domEx, err := s.ListSourcesFiltered(SourceListFilter{Domains: []string{"example.com"}}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range domEx {
		if !strings.Contains(row.URL, "example.com") {
			t.Fatalf("domain filter leaked %q", row.URL)
		}
	}
	domCDN, err := s.CountSourcesFiltered(SourceListFilter{Domains: []string{"cdn.example.org"}})
	if err != nil || domCDN != 1 {
		t.Fatalf("cdn domain count=%d err=%v", domCDN, err)
	}

	on := true
	off := false
	schedOn, err := s.ListSourcesFiltered(SourceListFilter{ScheduleOn: &on}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range schedOn {
		if row.IsSingle() || row.ScanCronNever() {
			t.Fatalf("schedule on leaked id=%d kind=%s cron=%q", row.ID, row.Kind, row.ScanCron)
		}
	}
	schedOff, err := s.ListSourcesFiltered(SourceListFilter{ScheduleOn: &off}, 50, 0)
	if err != nil || len(schedOff) < 1 {
		t.Fatalf("schedule off len=%d err=%v", len(schedOff), err)
	}
	foundSingle := false
	for _, row := range schedOff {
		if row.ID == single.ID {
			foundSingle = true
		}
	}
	if !foundSingle {
		t.Fatal("schedule off should include single source")
	}

	ignOn := true
	ignOff := false
	// Default feeds are wanted; flip one feed to ignored.
	feedID := a.Sources[0].ID
	if _, err := s.UpdateSource(a.ID, feedID, UpdateSourceParams{IndexAsIgnored: &ignOn}); err != nil {
		t.Fatal(err)
	}
	ignoredOnly, err := s.ListSourcesFiltered(SourceListFilter{IndexAsIgnored: &ignOn}, 50, 0)
	if err != nil || len(ignoredOnly) != 1 || ignoredOnly[0].ID != feedID {
		t.Fatalf("discovered ignored got %#v err=%v", ignoredOnly, err)
	}
	wantedOnly, err := s.ListSourcesFiltered(SourceListFilter{IndexAsIgnored: &ignOff}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range wantedOnly {
		if row.IndexAsIgnored {
			t.Fatalf("discovered wanted leaked ignored id=%d", row.ID)
		}
	}
	facetsA, err := s.SourceFilterFacetsForSeries(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !facetsA.HasDiscoveredWanted || !facetsA.HasDiscoveredIgnored {
		t.Fatalf("series A discovered facets wanted=%v ignored=%v", facetsA.HasDiscoveredWanted, facetsA.HasDiscoveredIgnored)
	}

	done := true
	incomplete := false
	if n, err := s.CountSourcesFiltered(SourceListFilter{FullScanDone: &incomplete}); err != nil || n < 1 {
		t.Fatalf("incomplete full scan count=%d err=%v", n, err)
	}
	_ = s.MarkFullScanDone(a.Sources[0].ID)
	if n, err := s.CountSourcesFiltered(SourceListFilter{FullScanDone: &done, SeriesID: a.ID}); err != nil || n < 1 {
		t.Fatalf("done full scan count=%d err=%v", n, err)
	}

	monYes := true
	monNo := false
	if n, err := s.CountSourcesFiltered(SourceListFilter{SeriesMonitored: &monYes}); err != nil || n < 2 {
		t.Fatalf("monitored yes count=%d err=%v", n, err)
	}
	if n, err := s.CountSourcesFiltered(SourceListFilter{SeriesMonitored: &monNo}); err != nil || n < 1 {
		t.Fatalf("monitored no count=%d err=%v", n, err)
	}

	byDom, err := s.ListSourcesFiltered(SourceListFilter{Sort: SortSourceDomain, SortDir: SortDirAsc}, 50, 0)
	if err != nil || len(byDom) < 2 {
		t.Fatalf("domain sort len=%d err=%v", len(byDom), err)
	}
	byScan, err := s.ListSourcesFiltered(SourceListFilter{Sort: SortSourceLastScanned, SortDir: SortDirDesc}, 50, 0)
	if err != nil || len(byScan) < 1 {
		t.Fatalf("last_scanned sort len=%d err=%v", len(byScan), err)
	}
	// Empty dir must use DefaultSortDir (desc), same as series/videos orderBy.
	byScanDefault, err := s.ListSourcesFiltered(SourceListFilter{Sort: SortSourceLastScanned}, 50, 0)
	if err != nil || len(byScanDefault) < 1 {
		t.Fatalf("last_scanned default dir len=%d err=%v", len(byScanDefault), err)
	}
	_ = b
}

func TestListSourcesFilteredActive(t *testing.T) {
	if (SourceListFilter{}).Active() {
		t.Fatal("empty filter should be inactive")
	}
	v := true
	if !(SourceListFilter{Domains: []string{"x"}}).Active() || !(SourceListFilter{FullScanDone: &v}).Active() ||
		!(SourceListFilter{ScheduleOn: &v}).Active() || !(SourceListFilter{SeriesMonitored: &v}).Active() ||
		!(SourceListFilter{SeriesID: 1}).Active() || !(SourceListFilter{IndexAsIgnored: &v}).Active() {
		t.Fatal("operator filters should be active")
	}
	if !(SourceListFilter{Q: "hi"}).Active() {
		t.Fatal("search should be active")
	}
	if (SourceListFilter{Q: "hi"}).MenuActive() {
		t.Fatal("search alone should not be menu-active")
	}
	if !(SourceListFilter{Domains: []string{"x"}}).MenuActive() {
		t.Fatal("domain should be menu-active")
	}
}

func TestNormalizeSourceQField(t *testing.T) {
	if got := NormalizeSourceQField(""); got != QFieldSourceLabel {
		t.Fatalf("default: got %q", got)
	}
	if got := NormalizeSourceQField("url"); got != QFieldSourceURL {
		t.Fatalf("url: got %q", got)
	}
	if got := NormalizeSourceQField("series"); got != QFieldSourceSeries {
		t.Fatalf("series: got %q", got)
	}
	if got := NormalizeSourceQField("name"); got != QFieldSourceLabel {
		t.Fatalf("name alias: got %q", got)
	}
	if got := NormalizeSourceQField("label"); got != QFieldSourceLabel {
		t.Fatalf("legacy label: got %q", got)
	}
}
