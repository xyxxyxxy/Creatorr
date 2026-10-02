package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	explorerTypeSeries        = "series"
	explorerTypeVideos        = "videos"
	explorerTypeSources       = "sources"
	explorerTypeTasks         = "tasks"
	explorerTypeNotifications = "notifications"

	cookieBrowserType = "creatorr_browser_type"
	cookieBrowserWide = "creatorr_browser_wide"

	explorerAtSeries       = "series"
	explorerAtVideos       = "videos"
	explorerAtBrowser      = "browser"
	explorerAtSeriesDetail = "series-detail"
	explorerAtTasks        = "tasks"
)

// explorerBrowse is the single Explorer fragment API.
func (h *Handler) explorerBrowse(w http.ResponseWriter, r *http.Request) {
	typ := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if typ == "" {
		typ = explorerTypeSeries
	}
	switch typ {
	case explorerTypeSeries:
		h.explorerBrowseSeries(w, r)
	case explorerTypeVideos:
		h.explorerBrowseVideos(w, r)
	case explorerTypeSources:
		h.explorerBrowseSources(w, r)
	case explorerTypeTasks:
		h.explorerBrowseTasks(w, r)
	case explorerTypeNotifications:
		h.explorerBrowseNotifications(w, r)
	default:
		http.Error(w, "unknown explorer type", http.StatusBadRequest)
	}
}

func (h *Handler) explorerBrowseSeries(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("HX-Target")
	if target == "series-list-infinite" {
		data, err := h.loadSeriesListLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !data.Load.Append {
			maybeExplorerPushURL(w, r, explorerTypeSeries)
			render(w, "series_list_live", data)
			return
		}
		render(w, "series_infinite_chunk", data)
		return
	}
	data, err := h.loadSeriesListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeSeries)
	render(w, "series_list_live", data)
}

func (h *Handler) explorerBrowseVideos(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("HX-Target")
	if target == "videos-list-infinite" {
		data, err := h.loadVideosLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !data.Load.Append {
			maybeExplorerPushURL(w, r, explorerTypeVideos)
			render(w, "videos_live", data)
			return
		}
		render(w, "videos_infinite_chunk", data)
		return
	}
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeVideos)
	render(w, "videos_live", data)
}

func explorerAtFrom(r *http.Request, fallback string) string {
	at := strings.TrimSpace(r.URL.Query().Get("at"))
	if at == "" {
		return fallback
	}
	return at
}

// explorerFragmentPath is the HTMX fragment URL when type+at scope the Explorer (Browser shell, series-detail sources, /explorer/browse).
func explorerFragmentPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/explorer/browse"
	}
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("type")) == "" || strings.TrimSpace(q.Get("at")) == "" {
		return r.URL.Path
	}
	return "/explorer/browse"
}

func applyExplorerToolbar(tb *listViewToolbar, typ, at string) {
	if tb == nil {
		return
	}
	tb.FormAction = "/explorer/browse"
	tb.ExplorerType = typ
	tb.ExplorerAt = at
	tb.Hidden = append([]listHiddenField{
		{Name: "type", Value: typ},
		{Name: "at", Value: at},
	}, tb.Hidden...)
}

type listHiddenField struct {
	Name, Value string
}

func rewriteExplorerInfinite(load *ListLoad, typ string, seriesID int64) {
	if load == nil {
		return
	}
	if load.NextHref != "" {
		load.NextHref = rewriteToExplorerBrowse(load.NextHref, typ, seriesID)
	}
	rewriteExplorerPageInfo(&load.Page, typ, seriesID)
}

func rewriteExplorerPageInfo(page *PageInfo, typ string, seriesID int64) {
	if page == nil {
		return
	}
	page.FirstHref = rewriteToExplorerBrowse(page.FirstHref, typ, seriesID)
	page.PrevHref = rewriteToExplorerBrowse(page.PrevHref, typ, seriesID)
	page.NextHref = rewriteToExplorerBrowse(page.NextHref, typ, seriesID)
	page.LastHref = rewriteToExplorerBrowse(page.LastHref, typ, seriesID)
}

func rewriteToExplorerBrowse(raw, typ string, seriesID int64) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("type", typ)
	at := q.Get("at")
	if at == "" {
		switch typ {
		case explorerTypeSeries:
			at = explorerAtSeries
		case explorerTypeVideos:
			at = explorerAtVideos
		case explorerTypeSources:
			if seriesID > 0 {
				at = explorerAtSeriesDetail
			} else {
				at = explorerAtBrowser
			}
		default:
			at = explorerAtBrowser
		}
		q.Set("at", at)
	}
	if seriesID > 0 {
		q.Set("series_id", strconv.FormatInt(seriesID, 10))
	}
	u.Path = "/explorer/browse"
	u.RawQuery = q.Encode()
	return u.String()
}

func maybeExplorerPushURL(w http.ResponseWriter, r *http.Request, typ string) {
	if r.Header.Get("HX-Request") == "" {
		return
	}
	// Infinite append: client owns through on the host URL.
	if strings.HasSuffix(r.Header.Get("HX-Target"), "-infinite") {
		return
	}
	if push := explorerCanonicalURL(r, typ); push != "" {
		w.Header().Set("HX-Push-Url", push)
	}
}

func explorerCanonicalURL(r *http.Request, typ string) string {
	at := explorerAtFrom(r, "")
	q := r.URL.Query()
	out := url.Values{}
	for k, vs := range q {
		if k == "at" {
			continue
		}
		if at != explorerAtBrowser && k == "type" {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	var path string
	switch at {
	case explorerAtBrowser:
		out.Set("type", typ)
		path = "/browser"
	case explorerAtVideos:
		path = "/videos"
	case explorerAtTasks:
		path = "/tasks"
		out.Del("type")
	case explorerAtSeriesDetail:
		sid := strings.TrimSpace(q.Get("series_id"))
		if sid == "" {
			return ""
		}
		path = "/series/" + sid
		out.Del("series_id")
		out.Del("type")
	default:
		path = "/series"
	}
	enc := out.Encode()
	if enc == "" {
		return path
	}
	return path + "?" + enc
}

func redirectExplorerLive(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("type", typ)
		if q.Get("at") == "" {
			if typ == explorerTypeVideos {
				q.Set("at", explorerAtVideos)
			} else {
				q.Set("at", explorerAtSeries)
			}
		}
		u := "/explorer/browse"
		if enc := q.Encode(); enc != "" {
			u += "?" + enc
		}
		http.Redirect(w, r, u, http.StatusPermanentRedirect)
	}
}

func parseBrowserType(r *http.Request) string {
	t := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	switch t {
	case explorerTypeSeries, explorerTypeVideos, explorerTypeSources, explorerTypeTasks, explorerTypeNotifications:
		return t
	}
	if c := readListPrefCookie(r, cookieBrowserType); c != "" {
		switch c {
		case explorerTypeSeries, explorerTypeVideos, explorerTypeSources, explorerTypeTasks, explorerTypeNotifications:
			return c
		}
	}
	return explorerTypeSeries
}

func writeBrowserTypeCookie(w http.ResponseWriter, typ string) {
	writeListPrefCookie(w, cookieBrowserType, typ)
}

func browserWide(r *http.Request) bool {
	return readListPrefCookie(r, cookieBrowserWide) == "1"
}

func writeBrowserWideCookie(w http.ResponseWriter, on bool) {
	if on {
		writeListPrefCookie(w, cookieBrowserWide, "1")
	} else {
		writeListPrefCookie(w, cookieBrowserWide, "")
	}
}
