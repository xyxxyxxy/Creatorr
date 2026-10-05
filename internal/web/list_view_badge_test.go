package web

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestClusterListViewBadges(t *testing.T) {
	in := []listViewBadge{
		{Group: "status", GroupTitle: "Status", Label: "Queued", Href: "/a", ClearHref: "/clear-status"},
		{Group: "status", GroupTitle: "Status", Label: "Running", Href: "/b", ClearHref: "/clear-status"},
		{Label: "Domain: example.com", Href: "/c"},
		{Group: "status", GroupTitle: "Status", Label: "Success", Href: "/d", ClearHref: "/clear-status-2"},
	}
	got := clusterListViewBadges(in)
	if len(got) != 3 {
		t.Fatalf("clusters=%d want 3", len(got))
	}
	if got[0].Title != "Status" || got[0].ClearHref != "/clear-status" || len(got[0].Items) != 2 {
		t.Fatalf("first cluster: %+v", got[0])
	}
	if got[1].Title != "" || len(got[1].Items) != 1 || got[1].Items[0].Label != "Domain: example.com" {
		t.Fatalf("standalone: %+v", got[1])
	}
	if got[2].Title != "Status" || len(got[2].Items) != 1 || got[2].Items[0].Label != "Success" {
		t.Fatalf("second status cluster: %+v", got[2])
	}
}

func TestVideoAndSeriesORJoinBadges(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/videos?status=wanted&status=downloaded&genre=A&genre=B&tag=t1&actor=Ann", nil)
	vb := videoListBadges(req, library.VideoListFilter{
		Statuses: []string{"wanted", "downloaded"},
		Genres:   []string{"A", "B"},
		Tags:     []string{"t1"},
		Actors:   []string{"Ann"},
	}, false, nil)
	clusters := clusterListViewBadges(vb)
	if len(clusters) != 4 {
		t.Fatalf("video clusters=%d want 4: %+v", len(clusters), clusters)
	}
	want := []struct {
		title string
		n     int
		first string
	}{
		{"Status", 2, "wanted"},
		{"Genre", 2, "A"},
		{"Tag", 1, "t1"},
		{"Actor", 1, "Ann"},
	}
	for i, w := range want {
		if clusters[i].Title != w.title || len(clusters[i].Items) != w.n || clusters[i].Items[0].Label != w.first {
			t.Fatalf("cluster[%d]=%+v want title=%q n=%d first=%q", i, clusters[i], w.title, w.n, w.first)
		}
		if clusters[i].Items[0].Group == "" || clusters[i].ClearHref == "" {
			t.Fatalf("cluster[%d] missing Group/ClearHref: %+v", i, clusters[i])
		}
		u, err := url.Parse(clusters[i].ClearHref)
		if err != nil {
			t.Fatal(err)
		}
		// ClearHref clears the query key (empty marker); no value chips remain.
		key := map[string]string{"Status": "status", "Genre": "genre", "Tag": "tag", "Actor": "actor"}[w.title]
		if _, ok := u.Query()[key]; !ok || u.Query().Get(key) != "" || len(u.Query()[key]) != 1 {
			t.Fatalf("cluster[%d] ClearHref %q should clear %s=", i, clusters[i].ClearHref, key)
		}
	}
	for _, b := range vb {
		if b.Label == "Status: wanted" || b.Label == "Genre: A" {
			t.Fatalf("flat Field: value labels must not remain: %q", b.Label)
		}
	}

	sreq := httptest.NewRequest("GET", "/series?genre=G&tag=T&actor=Act", nil)
	sb := seriesListBadges(sreq, library.SeriesListFilter{
		Genres: []string{"G"},
		Tags:   []string{"T"},
		Actors: []string{"Act"},
	})
	sc := clusterListViewBadges(sb)
	if len(sc) != 3 || sc[0].Title != "Genre" || sc[1].Title != "Tag" || sc[2].Title != "Actor" {
		t.Fatalf("series clusters=%+v", sc)
	}

	statusBadges := seriesListBadges(httptest.NewRequest("GET", "/series?status=has_errors", nil), library.SeriesListFilter{
		Statuses: []string{library.SeriesListStatusHasErrors},
	})
	if len(statusBadges) != 1 || statusBadges[0].Label != "Has errors" {
		t.Fatalf("status badge label=%+v want Has errors", statusBadges)
	}
}

func TestFilterSelectToggleMultiValue(t *testing.T) {
	t.Parallel()
	sel := listFilterSelect{
		Name: "status", AriaLabel: "Status",
		Options: []listFilterOpt{
			{Value: "wanted", Label: "wanted", Selected: true},
			{Value: "downloaded", Label: "downloaded", Selected: true},
			{Value: "ignored", Label: "ignored", Selected: false},
		},
	}
	annotateFilterSelect(httptest.NewRequest("GET", "/videos?status=wanted&status=downloaded", nil), &sel)
	if sel.SelectedCount != 2 {
		t.Fatalf("SelectedCount=%d want 2", sel.SelectedCount)
	}
	for _, opt := range sel.Options {
		if !opt.Selected && opt.Href == "" {
			t.Fatalf("missing href on option %q", opt.Value)
		}
	}
}

func TestFilterSelectStudioToggleAddsSecondValue(t *testing.T) {
	t.Parallel()
	sel := listFilterSelect{
		Name: "studio", AriaLabel: "Studio",
		Options: []listFilterOpt{
			{Value: "A", Label: "A", Selected: true},
			{Value: "B", Label: "B", Selected: false},
		},
	}
	req := httptest.NewRequest("GET", "/videos?studio=A", nil)
	annotateFilterSelect(req, &sel)
	u, err := url.Parse(sel.Options[1].Href)
	if err != nil {
		t.Fatal(err)
	}
	vals := u.Query()["studio"]
	if len(vals) != 2 || vals[0] != "A" || vals[1] != "B" {
		t.Fatalf("toggle studio B should add second value, got %v href=%q", vals, sel.Options[1].Href)
	}
}
