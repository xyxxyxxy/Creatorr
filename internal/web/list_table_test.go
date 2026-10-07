package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestListTableChromeTopPagerBesideColumns(t *testing.T) {
	// String-guard: Columns left of summary; multi-page pager on the right.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "list_table_chrome.html"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	iCols := strings.Index(body, `list_table_col_picker`)
	iSummary := strings.Index(body, `tabular-nums`)
	iPager := strings.Index(body, `pagination_join`)
	if iCols < 0 || iSummary < 0 || iCols > iSummary {
		t.Fatal("list_table_chrome must render Columns picker left of dataset summary")
	}
	if iPager < 0 || iPager < iSummary {
		t.Fatal("list_table_chrome must render pagination_join after summary (right side)")
	}
	if !strings.Contains(body, `.Page.Show`) {
		t.Fatal("top table pager must gate on Page.Show")
	}
	picker, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "partials", "list_table_col_picker.html"))
	if err != nil {
		t.Fatal(err)
	}
	pBody := string(picker)
	if strings.Contains(pBody, `dropdown-end`) || strings.Contains(pBody, `tooltip-left`) {
		t.Fatal("Columns picker on leading edge must open right (no dropdown-end / tooltip-left)")
	}
}

func TestParseTableColsCookieDefaultsAndMinOne(t *testing.T) {
	defs := seriesTableColDefs()
	r := httptest.NewRequest(http.MethodGet, "/series", nil)
	cols := parseTableColsCookie(r, cookieColsSeries, defs)
	vis := 0
	for _, c := range cols {
		if c.Visible {
			vis++
		}
	}
	if vis == 0 {
		t.Fatal("expected default visible columns")
	}
	if !tableColVisible(cols, "title") || !tableColVisible(cols, "progress") {
		t.Fatalf("essential defaults missing: %+v", cols)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/series", nil)
	r2.AddCookie(&http.Cookie{Name: cookieColsSeries, Value: ""})
	cols2 := parseTableColsCookie(r2, cookieColsSeries, defs)
	if !tableColVisible(cols2, "title") {
		t.Fatal("empty cookie should fall back to defaults with title")
	}

	r3 := httptest.NewRequest(http.MethodGet, "/series", nil)
	r3.AddCookie(&http.Cookie{Name: cookieColsSeries, Value: "bogus,also-bad"})
	cols3 := parseTableColsCookie(r3, cookieColsSeries, defs)
	if !tableColVisible(cols3, "title") {
		t.Fatal("invalid keys should fall back / keep at least one")
	}

	r4 := httptest.NewRequest(http.MethodGet, "/series", nil)
	r4.AddCookie(&http.Cookie{Name: cookieColsSeries, Value: "root,quality"})
	cols4 := parseTableColsCookie(r4, cookieColsSeries, defs)
	if !tableColVisible(cols4, "root") || tableColVisible(cols4, "title") {
		t.Fatalf("cookie should honor root,quality: %+v", cols4)
	}

	r5 := httptest.NewRequest(http.MethodGet, "/series", nil)
	r5.AddCookie(&http.Cookie{Name: cookieColsSeries, Value: "root%2Cquality"})
	cols5 := parseTableColsCookie(r5, cookieColsSeries, defs)
	if !tableColVisible(cols5, "root") || !tableColVisible(cols5, "quality") || tableColVisible(cols5, "title") {
		t.Fatalf("legacy %%2C cookie should decode: %+v", cols5)
	}
}

func TestVideoTableColDefsSeriesOptional(t *testing.T) {
	with := videoTableColDefs(true)
	without := videoTableColDefs(false)
	if !hasTableColKey(with, "series") {
		t.Fatal("library videos need series column")
	}
	if hasTableColKey(without, "series") {
		t.Fatal("series detail videos omit series column")
	}
}

func TestAnnotateTableColsSortJoinsKeyToSortOpt(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/series?view=table&sort=downloaded", nil)
	opts := seriesSortOpts(r, library.SortDownloaded, library.SortDirDesc)
	cols := parseTableColsCookie(r, cookieColsSeries, seriesTableColDefs())
	cols = annotateTableColsSort(cols, opts, library.SortDirDesc)

	var dl, monitored, title *tableCol
	for i := range cols {
		switch cols[i].Key {
		case "downloaded":
			dl = &cols[i]
		case "monitored":
			monitored = &cols[i]
		case "title":
			title = &cols[i]
		}
	}
	if dl == nil || dl.SortHref == "" || !dl.SortSelected || dl.SortDir != library.SortDirDesc {
		t.Fatalf("downloaded should be selected sort: %+v", dl)
	}
	if !strings.Contains(dl.SortHref, "sort=downloaded") {
		t.Fatalf("downloaded href should keep sort: %q", dl.SortHref)
	}
	if monitored == nil || monitored.SortHref != "" || monitored.SortSelected {
		t.Fatalf("monitored has no SortOpt: %+v", monitored)
	}
	if title == nil || title.SortHref == "" || title.SortSelected {
		t.Fatalf("title should be sortable but not selected: %+v", title)
	}
}

func TestAnnotateTableColsSortSourcesName(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/browser?type=sources&view=table&sort=name", nil)
	opts := sourcesSortOpts(r, library.SortSourceLabel, library.SortDirAsc)
	cols := annotateTableColsSort(
		parseTableColsCookie(r, "creatorr_cols_sources", sourcesTableColDefs(true)),
		opts, library.SortDirAsc)
	var name *tableCol
	for i := range cols {
		if cols[i].Key == "name" {
			name = &cols[i]
			break
		}
	}
	if name == nil || name.SortHref == "" || !name.SortSelected {
		t.Fatalf("name col should match SortSourceLabel after rename: %+v", name)
	}
}

func hasTableColKey(defs []tableColDef, key string) bool {
	for _, d := range defs {
		if d.Key == key {
			return true
		}
	}
	return false
}
