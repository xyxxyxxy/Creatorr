package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func hasTableColKey(defs []tableColDef, key string) bool {
	for _, d := range defs {
		if d.Key == key {
			return true
		}
	}
	return false
}
