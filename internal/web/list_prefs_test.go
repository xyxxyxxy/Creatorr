package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestMergeVideosListPrefsSkipsStatusOnBrowser(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=videos&at=browser", nil)
	r.AddCookie(&http.Cookie{Name: cookieStatusVideos, Value: "downloaded"})
	got := mergeVideosListPrefs(r)
	if got.URL.Query().Get("status") != "" || len(got.URL.Query()["status"]) > 0 {
		t.Fatalf("browser should not merge status cookie, q=%v", got.URL.Query())
	}
}

func TestMergeSeriesListPrefsSkipsStatusOnBrowser(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=series&at=browser", nil)
	r.AddCookie(&http.Cookie{Name: cookieStatusSeries, Value: library.SeriesListStatusMonitored})
	got := mergeSeriesListPrefs(r)
	if got.URL.Query().Get("status") != "" {
		t.Fatalf("browser should not merge status cookie, q=%v", got.URL.Query())
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

func TestClearOperatorFiltersURLKeepsExplorerScope(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=sources&at=series-detail&series_id=9&kind=feed&q=x&domain=example.com&sort=label&view=table&page=2", nil)
	href := clearOperatorFiltersURL(r)
	u, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("type") != "sources" || q.Get("at") != "series-detail" || q.Get("series_id") != "9" {
		t.Fatalf("scope dropped: %q", href)
	}
	if q.Get("sort") != "label" || q.Get("view") != "table" {
		t.Fatalf("sort/view dropped: %q", href)
	}
	if q.Get("kind") != "" || q.Get("q") != "" || q.Get("domain") != "" || q.Get("page") != "" {
		t.Fatalf("operator filters should clear: %q", href)
	}
	if _, ok := q["status"]; !ok || q.Get("status") != "" {
		t.Fatalf("want status=: %q", href)
	}
}

func TestEncodeParseSortCookie(t *testing.T) {
	raw := encodeSortCookie(library.SortAdded, library.SortDirDesc)
	sort, dir := parseSortCookie(raw)
	if sort != library.SortAdded || dir != library.SortDirDesc {
		t.Fatalf("got %q %q", sort, dir)
	}
}
