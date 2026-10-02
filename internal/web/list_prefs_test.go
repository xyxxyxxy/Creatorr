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
	for _, k := range []string{"level", "unread", "from", "to", "origin"} {
		if _, ok := q[k]; !ok || q.Get(k) != "" {
			t.Fatalf("want %s= clear marker: %q", k, href)
		}
	}
}

func TestClearAllBlocksNotificationFilterCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, clearOperatorFiltersURL(
		httptest.NewRequest(http.MethodGet, "/browser?type=notifications&level=alert&unread=1&from=2024-01-01&sort=when&dir=desc", nil),
	), nil)
	r.AddCookie(&http.Cookie{
		Name:  cookieFilterNotifications,
		Value: "level=alert&unread=1&from=2024-01-01",
	})
	got := mergeNotificationsListPrefs(r)
	q := got.URL.Query()
	if q.Get("level") != "" || q.Get("unread") != "" || q.Get("from") != "" {
		t.Fatalf("clear markers should block cookie restore, q=%v", q)
	}
	if q.Get("sort") != "when" || q.Get("dir") != "desc" {
		t.Fatalf("sort/dir should stay: %q/%q", q.Get("sort"), q.Get("dir"))
	}
}

func TestMergeTasksListPrefsFromFilterCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/browser?type=tasks", nil)
	r.AddCookie(&http.Cookie{
		Name:  cookieFilterTasks,
		Value: "status=done&status=failed&domain=example.com&kind=download&origin=user&from=2024-01-01&to=2024-01-31",
	})
	r.AddCookie(&http.Cookie{Name: cookieSortTasks, Value: "created:asc"})
	got := mergeTasksListPrefs(r)
	q := got.URL.Query()
	if got := q["status"]; len(got) != 2 || got[0] != "done" || got[1] != "failed" {
		t.Fatalf("status=%v", got)
	}
	if q.Get("domain") != "example.com" || q.Get("kind") != "download" || q.Get("origin") != "user" {
		t.Fatalf("domain/kind/origin=%q/%q/%q", q.Get("domain"), q.Get("kind"), q.Get("origin"))
	}
	if q.Get("from") != "2024-01-01" || q.Get("to") != "2024-01-31" {
		t.Fatalf("from/to=%q/%q", q.Get("from"), q.Get("to"))
	}
	if q.Get("sort") != "created" || q.Get("dir") != "asc" {
		t.Fatalf("sort=%q dir=%q", q.Get("sort"), q.Get("dir"))
	}
}

func TestMergeTasksListPrefsQueryWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/browser?type=tasks&status=&domain=other.example", nil)
	r.AddCookie(&http.Cookie{
		Name:  cookieFilterTasks,
		Value: "status=done&domain=example.com",
	})
	got := mergeTasksListPrefs(r)
	q := got.URL.Query()
	if q.Get("status") != "" || len(q["status"]) != 1 {
		t.Fatalf("status clear should win, got %v", q["status"])
	}
	if q.Get("domain") != "other.example" {
		t.Fatalf("domain=%q", q.Get("domain"))
	}
}

func TestMergeNotificationsListPrefsFromFilterCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/browser?type=notifications", nil)
	r.AddCookie(&http.Cookie{
		Name:  cookieFilterNotifications,
		Value: "level=alert&unread=1&from=2024-02-01&to=2024-02-28",
	})
	got := mergeNotificationsListPrefs(r)
	q := got.URL.Query()
	if q.Get("level") != "alert" || q.Get("unread") != "1" {
		t.Fatalf("level/unread=%q/%q", q.Get("level"), q.Get("unread"))
	}
	if q.Get("from") != "2024-02-01" || q.Get("to") != "2024-02-28" {
		t.Fatalf("from/to=%q/%q", q.Get("from"), q.Get("to"))
	}
}

func TestEncodeFilterPrefCookie(t *testing.T) {
	v := url.Values{}
	v.Add("status", "done")
	v.Set("domain", "example.com")
	raw := encodeFilterPrefCookie(v)
	back, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Get("status") != "done" || back.Get("domain") != "example.com" {
		t.Fatalf("roundtrip=%v", back)
	}
	if encodeFilterPrefCookie(nil) != "" || encodeFilterPrefCookie(url.Values{}) != "" {
		t.Fatal("empty should encode blank")
	}
}
