package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

// browserPage is the Browser shell: type join + optional wide shell + Explorer body.
func (h *Handler) browserPage(w http.ResponseWriter, r *http.Request) {
	if v := strings.TrimSpace(r.URL.Query().Get("wide")); v == "1" || v == "0" {
		writeBrowserWideCookie(w, v == "1")
		q := r.URL.Query()
		q.Del("wide")
		u := "/browser"
		if enc := q.Encode(); enc != "" {
			u += "?" + enc
		}
		http.Redirect(w, r, u, http.StatusSeeOther)
		return
	}

	typ := parseBrowserType(r)
	writeBrowserTypeCookie(w, typ)
	wide := browserWide(r)

	q := r.URL.Query()
	q.Set("type", typ)
	q.Set("at", explorerAtBrowser)
	req := cloneRequestQuery(r, q)

	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	suggestions, _ := h.Library.ListMetaSuggestions()

	base := newPage("Browser", "browser", flashFromQuery(r))
	data := struct {
		pageBase
		Type                string
		Wide                bool
		WideHref            string
		SeriesLive          seriesListLiveData
		VideosLive          videosPageLiveData
		SourcesLive         sourcesListLiveData
		TasksLive           tasksListLiveData
		NotificationsLive   notificationsListLiveData
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		Suggestions         library.MetaSuggestions
		PackRoleOptions     []struct{ Value, Label string }
		ScanCronDescriptors []string
	}{
		pageBase:            base,
		Type:                typ,
		Wide:                wide,
		WideHref:            browserWideToggleHref(r, typ, !wide),
		Roots:               roots,
		Profiles:            profiles,
		Suggestions:         suggestions,
		PackRoleOptions:     library.PackRoleSelectOptions(),
		ScanCronDescriptors: scanCronDescriptors(),
	}

	switch typ {
	case explorerTypeVideos:
		live, err := h.loadVideosLive(w, req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data.VideosLive = live
	case explorerTypeSources:
		live, err := h.loadSourcesListLive(w, req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data.SourcesLive = live
	case explorerTypeTasks:
		live, err := h.loadTasksListLive(w, req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data.TasksLive = live
	case explorerTypeNotifications:
		live, err := h.loadNotificationsListLive(w, req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data.NotificationsLive = live
	default:
		data.Type = explorerTypeSeries
		live, err := h.loadSeriesListLive(w, req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		data.SeriesLive = live
	}
	render(w, "browser", data)
}

func browserWideToggleHref(r *http.Request, typ string, wantWide bool) string {
	q := url.Values{}
	q.Set("type", typ)
	if wantWide {
		q.Set("wide", "1")
	} else {
		q.Set("wide", "0")
	}
	for k, vs := range r.URL.Query() {
		switch k {
		case "type", "wide", "page", "through", "at":
			continue
		}
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	return "/browser?" + q.Encode()
}
