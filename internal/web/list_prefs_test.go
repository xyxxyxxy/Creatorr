package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestMergeSeriesListPrefsFromCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/series", nil)
	r.AddCookie(&http.Cookie{Name: cookieStatusSeries, Value: library.SeriesListStatusMonitored})
	r.AddCookie(&http.Cookie{Name: cookieSortSeries, Value: library.SortAdded + ":" + library.SortDirDesc})
	got := mergeSeriesListPrefs(r)
	q := got.URL.Query()
	if q.Get("status") != library.SeriesListStatusMonitored {
		t.Fatalf("status=%q", q.Get("status"))
	}
	if q.Get("sort") != library.SortAdded || q.Get("dir") != library.SortDirDesc {
		t.Fatalf("sort=%q dir=%q", q.Get("sort"), q.Get("dir"))
	}
}

func TestMergeSeriesListPrefsQueryWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/series?status=&sort="+library.SortTitle, nil)
	r.AddCookie(&http.Cookie{Name: cookieStatusSeries, Value: library.SeriesListStatusMonitored})
	r.AddCookie(&http.Cookie{Name: cookieSortSeries, Value: library.SortAdded + ":" + library.SortDirDesc})
	got := mergeSeriesListPrefs(r)
	q := got.URL.Query()
	if _, ok := q["status"]; !ok {
		t.Fatal("expected status key present for clear")
	}
	if q.Get("status") != "" {
		t.Fatalf("status should be cleared, got %q", q.Get("status"))
	}
	if q.Get("sort") != library.SortTitle {
		t.Fatalf("sort=%q", q.Get("sort"))
	}
	if q.Get("dir") != "" {
		t.Fatalf("dir should stay empty, got %q", q.Get("dir"))
	}
}

func TestMergeVideosListPrefsMultiStatus(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/videos", nil)
	r.AddCookie(&http.Cookie{Name: cookieStatusVideos, Value: "wanted,downloaded"})
	got := mergeVideosListPrefs(r)
	q := got.URL.Query()
	gotStatuses := q["status"]
	if len(gotStatuses) != 2 {
		t.Fatalf("statuses=%v", gotStatuses)
	}
}

func TestClearQueryKeyMarksStatus(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/series?status=monitored&page=2", nil)
	href := clearQueryKey(r, "status")
	req := httptest.NewRequest(http.MethodGet, "http://example.com"+href, nil)
	q := req.URL.Query()
	if _, ok := q["status"]; !ok {
		t.Fatalf("href %q missing status=", href)
	}
	if q.Get("status") != "" || q.Get("page") != "" {
		t.Fatalf("q=%v href=%q", q, href)
	}
}

func TestEncodeParseSortCookie(t *testing.T) {
	raw := encodeSortCookie(library.SortAdded, library.SortDirDesc)
	sort, dir := parseSortCookie(raw)
	if sort != library.SortAdded || dir != library.SortDirDesc {
		t.Fatalf("got %q %q", sort, dir)
	}
}
