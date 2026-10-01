package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestSearchByPlaceholder(t *testing.T) {
	t.Parallel()
	if got := searchByPlaceholder(""); got != "Search by..." {
		t.Fatalf("empty q_field: got %q", got)
	}
	if got := searchByPlaceholder(library.QFieldPlot); got != "Search by..." {
		t.Fatalf("plot: got %q", got)
	}
}

func TestSeriesSortOptsSelectedTogglesDir(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/series", nil)
	opts := seriesSortOpts(req, library.SortTitle, library.SortDirAsc)
	var title *listFilterOpt
	for i := range opts {
		if opts[i].Value == library.SortTitle {
			title = &opts[i]
			break
		}
	}
	if title == nil || !title.Selected {
		t.Fatal("title should be selected")
	}
	u, err := url.Parse(title.Href)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("dir") != library.SortDirDesc {
		t.Fatalf("re-select title from asc should link to desc, href=%q", title.Href)
	}

	reqDesc := httptest.NewRequest(http.MethodGet, "/series?dir=desc", nil)
	optsDesc := seriesSortOpts(reqDesc, library.SortTitle, library.SortDirDesc)
	for _, o := range optsDesc {
		if o.Value != library.SortTitle {
			continue
		}
		// Default for title is asc, so toggle back omits dir.
		if strings.Contains(o.Href, "dir=") {
			t.Fatalf("re-select title from desc should drop dir (default asc), href=%q", o.Href)
		}
		return
	}
	t.Fatal("title missing")
}

func TestToggleMultiSelectURL(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/videos?genre=A&page=2&not_empty=genres", nil)

	addB := toggleMultiSelectURL(req, "genre", "B", library.PresenceGenres)
	u, err := url.Parse(addB)
	if err != nil {
		t.Fatal(err)
	}
	genres := u.Query()["genre"]
	if len(genres) != 2 || genres[0] != "A" || genres[1] != "B" {
		t.Fatalf("add B: genre=%v href=%q", genres, addB)
	}
	if u.Query().Get("page") != "" {
		t.Fatalf("should drop page, href=%q", addB)
	}
	if u.Query().Has("not_empty") {
		t.Fatalf("non-empty multi should clear presence, href=%q", addB)
	}

	dropA := toggleMultiSelectURL(req, "genre", "A", library.PresenceGenres)
	u2, err := url.Parse(dropA)
	if err != nil {
		t.Fatal(err)
	}
	if len(u2.Query()["genre"]) != 0 {
		t.Fatalf("drop only A: genre=%v href=%q", u2.Query()["genre"], dropA)
	}
}

func TestFilterValueChipLabel(t *testing.T) {
	t.Parallel()
	if got := filterValueChipLabel("country", "123"); got != "Country: 123" {
		t.Fatalf("country: got %q", got)
	}
	if got := filterValueChipLabel("mpaa", "TV-MA"); got != "Rating: TV-MA" {
		t.Fatalf("mpaa: got %q", got)
	}
	sel := listFilterSelect{
		Name: "country", AriaLabel: "Country", PresenceField: library.PresenceCountry,
		Options: []listFilterOpt{{Value: "123", Label: "123"}},
	}
	annotateFilterSelect(httptest.NewRequest(http.MethodGet, "/videos", nil), &sel)
	if sel.Options[0].Label != "123" {
		t.Fatalf("menu label: got %q want bare value", sel.Options[0].Label)
	}
	if sel.PresenceFilledLabel != "Has country" || sel.PresenceEmptyLabel != "No country" {
		t.Fatalf("presence labels: %q / %q", sel.PresenceFilledLabel, sel.PresenceEmptyLabel)
	}

	reqOn := httptest.NewRequest(http.MethodGet, "/videos?not_empty=country", nil)
	annotateFilterSelect(reqOn, &sel)
	if !sel.PresenceFilledSelected || sel.PresenceEmptySelected {
		t.Fatalf("filled selected flags: filled=%v empty=%v", sel.PresenceFilledSelected, sel.PresenceEmptySelected)
	}
	u, err := url.Parse(sel.PresenceFilledHref)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Has("not_empty") {
		t.Fatalf("active Has should clear not_empty, href=%q", sel.PresenceFilledHref)
	}
}
