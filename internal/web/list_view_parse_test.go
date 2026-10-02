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
	if got := searchByPlaceholder(nil); got != "Search" {
		t.Fatalf("empty opts: got %q", got)
	}
	if got := searchByPlaceholder(qFieldOpts(library.QFieldPlot)); got != "Search by Plot" {
		t.Fatalf("plot: got %q", got)
	}
	if got := searchByPlaceholder(qFieldOpts("")); got != "Search by Title" {
		t.Fatalf("default title: got %q", got)
	}
	if got := searchByPlaceholder(sourceQFieldOpts(library.QFieldSourceLabel, true)); got != "Search by Name" {
		t.Fatalf("source name: got %q", got)
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

func TestBoolOnlySelectHasNo(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=sources&at=browser", nil)
	sel := boolOnlySelect(req, "schedule", "Schedule", library.SourceScheduleOn, library.SourceScheduleOff, nil)
	if sel.Options != nil || sel.PresenceField != "" {
		t.Fatalf("bool filter should be Has/No only: %#v", sel)
	}
	if sel.PresenceFilledLabel != "Has schedule" || sel.PresenceEmptyLabel != "No schedule" {
		t.Fatalf("labels: %q / %q", sel.PresenceFilledLabel, sel.PresenceEmptyLabel)
	}
	uHas, err := url.Parse(sel.PresenceFilledHref)
	if err != nil {
		t.Fatal(err)
	}
	if uHas.Query().Get("schedule") != library.SourceScheduleOn {
		t.Fatalf("Has href schedule=%q", uHas.Query().Get("schedule"))
	}
	uNo, err := url.Parse(sel.PresenceEmptyHref)
	if err != nil {
		t.Fatal(err)
	}
	if uNo.Query().Get("schedule") != library.SourceScheduleOff {
		t.Fatalf("No href schedule=%q", uNo.Query().Get("schedule"))
	}

	on := true
	selOn := boolOnlySelect(httptest.NewRequest(http.MethodGet, "/explorer/browse?type=sources&at=browser&schedule=on", nil),
		"schedule", "Schedule", library.SourceScheduleOn, library.SourceScheduleOff, &on)
	if !selOn.PresenceFilledSelected || selOn.PresenceEmptySelected {
		t.Fatalf("on selected flags: filled=%v empty=%v", selOn.PresenceFilledSelected, selOn.PresenceEmptySelected)
	}
	uClear, err := url.Parse(selOn.PresenceFilledHref)
	if err != nil {
		t.Fatal(err)
	}
	if uClear.Query().Get("schedule") != "" {
		t.Fatalf("active Has should clear schedule, href=%q", selOn.PresenceFilledHref)
	}
}

func TestSpecialKindFilterSelect(t *testing.T) {
	t.Parallel()
	opts := []listFilterOpt{{Value: library.PackRoleSpecialEpisode, Label: "Special episode"}}
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	sel := specialKindFilterSelect(req, "", opts)
	if sel.AriaLabel != "Special kind" || sel.PresenceField != "" {
		t.Fatalf("aria/presence: %#v", sel)
	}
	if len(sel.Options) != 1 || sel.Options[0].Value != library.PackRoleSpecialEpisode {
		t.Fatalf("concrete opts only: %#v", sel.Options)
	}
	uHas, err := url.Parse(sel.PresenceFilledHref)
	if err != nil {
		t.Fatal(err)
	}
	if uHas.Query().Get("kind") != library.VideoPackRoleAnySpecial {
		t.Fatalf("Has kind=%q", uHas.Query().Get("kind"))
	}
	uNo, err := url.Parse(sel.PresenceEmptyHref)
	if err != nil {
		t.Fatal(err)
	}
	if uNo.Query().Get("kind") != library.PackRoleRegular {
		t.Fatalf("No kind=%q", uNo.Query().Get("kind"))
	}

	selAny := specialKindFilterSelect(
		httptest.NewRequest(http.MethodGet, "/videos?kind=special", nil),
		library.VideoPackRoleAnySpecial, opts)
	if !selAny.PresenceFilledSelected || selAny.PresenceEmptySelected {
		t.Fatalf("any special flags: filled=%v empty=%v", selAny.PresenceFilledSelected, selAny.PresenceEmptySelected)
	}

	selEp := specialKindFilterSelect(
		httptest.NewRequest(http.MethodGet, "/videos?kind=episode", nil),
		library.PackRoleRegular, opts)
	if selEp.PresenceFilledSelected || !selEp.PresenceEmptySelected {
		t.Fatalf("regular flags: filled=%v empty=%v", selEp.PresenceFilledSelected, selEp.PresenceEmptySelected)
	}

	trailers := []listFilterOpt{{Value: "trailers", Label: "Feature: trailers", Selected: true}}
	selFeat := specialKindFilterSelect(
		httptest.NewRequest(http.MethodGet, "/videos?kind=trailers", nil),
		"trailers", trailers)
	if !selFeat.PresenceFilledSelected {
		t.Fatal("concrete special should mark Has selected")
	}
	uClear, err := url.Parse(selFeat.PresenceFilledHref)
	if err != nil {
		t.Fatal(err)
	}
	if uClear.Query().Get("kind") != "" {
		t.Fatalf("active Has should clear kind, href=%q", selFeat.PresenceFilledHref)
	}

	if got := specialKindChipLabel(library.PackRoleRegular); got != "No special kind" {
		t.Fatalf("regular chip: %q", got)
	}
	if got := specialKindChipLabel(library.VideoPackRoleAnySpecial); got != "Has special kind" {
		t.Fatalf("any chip: %q", got)
	}
	if got := specialKindChipLabel(library.PackRoleSpecialEpisode); got != "Special kind: Special episode" {
		t.Fatalf("special chip: %q", got)
	}
}
