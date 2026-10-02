package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

const (
	cookieStatusSeries = "creatorr_status_series"
	cookieSortSeries   = "creatorr_sort_series"
	cookieStatusVideos = "creatorr_status_videos"
	cookieSortVideos   = "creatorr_sort_videos"
)

func writeListPrefCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func readListPrefCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func encodeSortCookie(sort, dir string) string {
	sort = strings.TrimSpace(sort)
	dir = strings.TrimSpace(dir)
	if sort == "" {
		return ""
	}
	if dir == "" {
		return sort
	}
	return sort + ":" + dir
}

func parseSortCookie(raw string) (sort, dir string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+1:])
	}
	return raw, ""
}

func cloneRequestQuery(r *http.Request, q url.Values) *http.Request {
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return r2
}

// mergeSeriesListPrefs fills absent status/sort/dir from cookies so bare /series
// visits keep the last Status filter and Sort. Explicit query keys win (including
// status= to clear).
func mergeSeriesListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := false
	if _, ok := q["status"]; !ok {
		if v := readListPrefCookie(r, cookieStatusSeries); v != "" {
			q.Set("status", v)
			changed = true
		}
	}
	if _, ok := q["sort"]; !ok {
		sort, dir := parseSortCookie(readListPrefCookie(r, cookieSortSeries))
		if s := parseSeriesSort(sort); s != "" {
			q.Set("sort", s)
			changed = true
			if _, hasDir := q["dir"]; !hasDir {
				if d := parseSortDir(dir); d != "" {
					q.Set("dir", d)
				}
			}
		}
	}
	if !changed {
		return r
	}
	return cloneRequestQuery(r, q)
}

func writeSeriesListPrefs(w http.ResponseWriter, filter library.SeriesListFilter) {
	writeListPrefCookie(w, cookieStatusSeries, strings.TrimSpace(filter.Status))
	sort := filter.Sort
	if sort == "" {
		sort = library.SortTitle
	}
	dir := library.NormalizeSortDir(sort, filter.SortDir)
	writeListPrefCookie(w, cookieSortSeries, encodeSortCookie(sort, dir))
}

// mergeVideosListPrefs fills absent status/sort/dir from cookies for /videos.
func mergeVideosListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := false
	if _, ok := q["status"]; !ok {
		if raw := readListPrefCookie(r, cookieStatusVideos); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				st := strings.TrimSpace(part)
				if st != "" {
					q.Add("status", st)
					changed = true
				}
			}
		}
	}
	if _, ok := q["sort"]; !ok {
		sort, dir := parseSortCookie(readListPrefCookie(r, cookieSortVideos))
		if s := parseVideoSort(sort); s != "" {
			q.Set("sort", s)
			changed = true
			if _, hasDir := q["dir"]; !hasDir {
				if d := parseSortDir(dir); d != "" {
					q.Set("dir", d)
				}
			}
		}
	}
	if !changed {
		return r
	}
	return cloneRequestQuery(r, q)
}

func writeVideosListPrefs(w http.ResponseWriter, filter library.VideoListFilter) {
	writeListPrefCookie(w, cookieStatusVideos, strings.Join(filter.Statuses, ","))
	sort := filter.Sort
	if sort == "" {
		sort = library.SortAdded
	}
	dir := library.NormalizeSortDir(sort, filter.SortDir)
	writeListPrefCookie(w, cookieSortVideos, encodeSortCookie(sort, dir))
}

// clearQueryKey sets key to empty (marks intentional clear for cookie prefs) and
// drops page/through plus any extra keys.
func clearQueryKey(r *http.Request, key string, extraDrop ...string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	for _, k := range extraDrop {
		q.Del(k)
	}
	q.Set(key, "")
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}
