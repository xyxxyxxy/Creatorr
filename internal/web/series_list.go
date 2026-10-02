package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type seriesListRow struct {
	library.Series
	HasMonitoredSource bool
	Busy               bool
	BulkEditBusy       bool
	StatusInd          *seriesStatusView // health errors/warnings (list: title row; cards: poster)
	PosterURL          string
	BannerURL          string
	Line2              string
	KindIcon           string
	KindTip            string
	Redirect           string
	LiveTarget         string
}

type seriesListLiveData struct {
	Series          []seriesListRow
	Page            PageInfo
	Load            ListLoad
	ListMode        ListMode
	SeriesFilter    listViewToolbar
	FilterActive    bool
	BulkEditBusy    bool
	FilterTotal     int
	ViewMode        string
	TableCols       []tableCol
	TableColsCookie string
	ShowSelectAll   bool
	InfiniteID      string
	RowsID          string
	OOB             bool
}

func parseSeriesListFilter(r *http.Request) library.SeriesListFilter {
	q := r.URL.Query()
	f := library.SeriesListFilter{
		Title:   strings.TrimSpace(q.Get("q")),
		QField:  parseQField(r),
		Studio:  strings.TrimSpace(q.Get("studio")),
		Country: strings.TrimSpace(q.Get("country")),
		MPAA:    strings.TrimSpace(q.Get("mpaa")),
		Genres:  parseMultiQuery(q, "genre"),
		Tags:    parseMultiQuery(q, "tag"),
		Actors:  parseMultiQuery(q, "actor"),
		Sort:    parseSeriesSort(q.Get("sort")),
		SortDir: parseSortDir(q.Get("dir")),
	}
	f.Empty, f.NotEmpty = parsePresenceParams(q)
	if v := strings.TrimSpace(q.Get("root")); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.RootID = id
		}
	}
	if v := strings.TrimSpace(q.Get("quality")); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.QualityProfileID = id
		}
	}
	switch strings.ToLower(strings.TrimSpace(q.Get("delivery"))) {
	case library.DeliveryVideo:
		f.DeliveryMode = library.DeliveryVideo
	case library.DeliveryAudio:
		f.DeliveryMode = library.DeliveryAudio
	}
	switch strings.TrimSpace(q.Get("status")) {
	case library.SeriesListStatusMonitored,
		library.SeriesListStatusUnmonitored,
		library.SeriesListStatusComplete,
		library.SeriesListStatusIncomplete,
		library.SeriesListStatusHasErrors:
		f.Status = strings.TrimSpace(q.Get("status"))
	}
	if raw := strings.TrimSpace(q.Get("year")); raw != "" {
		if y, err := strconv.Atoi(raw); err == nil && y >= 1900 && y <= 2100 {
			f.PremieredYear = y
		}
	}
	return f
}

func (h *Handler) loadSeriesListLive(w http.ResponseWriter, r *http.Request) (seriesListLiveData, error) {
	r = mergeSeriesListPrefs(r)
	filter := parseSeriesListFilter(r)
	if filter.Sort == "" {
		filter.Sort = library.SortTitle
	}
	writeSeriesListPrefs(w, r, filter)
	viewMode, writeCookie := resolveViewMode(r, cookieModeSeries, viewList)
	if writeCookie {
		writeViewCookie(w, cookieModeSeries, viewMode)
	}
	total, err := h.Library.CountSeriesFiltered(filter)
	if err != nil {
		return seriesListLiveData{}, err
	}

	mode := libraryListMode(viewMode)
	const liveTarget = "series-list-live"
	const infiniteID = "series-list-infinite"
	const rowsID = "series-list-rows"

	var load ListLoad
	var limit, offset int
	switch mode {
	case ListModeInfinite:
		load = resolveInfiniteLoad(r, total, liveTarget, infiniteID, "page")
		limit, offset = infiniteLimitOffset(load)
	default:
		load = resolvePaginatedLoad(r, total, SeriesPageSize, liveTarget, "page")
		limit = load.PageSize
		offset = OffsetSize(load.Page.Page, load.PageSize)
	}

	list, err := h.Library.ListSeriesFiltered(filter, limit, offset)
	if err != nil {
		return seriesListLiveData{}, err
	}

	active, _ := h.Queue.ListActive()
	bySeries := map[int64][]queue.Task{}
	for _, t := range active {
		if t.SeriesID.Valid {
			sid := t.SeriesID.Int64
			bySeries[sid] = append(bySeries[sid], t)
		}
	}
	h.mergeFileDeleteIntoSeriesMap(bySeries)
	ids := make([]int64, 0, len(list))
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	errFlags, _ := h.Library.SeriesVideoErrorFlagsMap(ids)
	warnLevels, _ := h.Library.SeriesWarnLevels(ids)

	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	rootPath := map[int64]string{}
	for _, root := range roots {
		rootPath[root.ID] = root.Path
	}

	redir := r.URL.RequestURI()
	if redir == "" {
		redir = "/series"
	}
	bulkBusy, _ := h.Library.BulkEditSeriesBusy()

	rows := make([]seriesListRow, 0, len(list))
	for _, s := range list {
		best := pickBestTask(bySeries[s.ID])
		art := h.Library.SeriesArtFlagsFor(&s)
		posterURL := ""
		if art.Poster {
			posterURL = fmt.Sprintf("/series/%d/art/poster", s.ID)
		}
		bannerURL := ""
		if art.Banner {
			bannerURL = fmt.Sprintf("/series/%d/art/banner", s.ID)
		}
		kindIcon, kindTip := "", ""
		if s.IsAudio() {
			kindIcon, kindTip = "headphones", "Audio series"
		}
		qualityLabel := s.QualityProfileName
		if s.IsAudio() {
			qualityLabel = library.DefaultProfileName
		}
		line2Parts := []string{}
		if len(roots) > 1 {
			rootLabel := strings.TrimSpace(s.RootName)
			if rootLabel == "" {
				rootLabel = rootPath[s.RootID]
			}
			if rootLabel != "" {
				line2Parts = append(line2Parts, rootLabel)
			}
		}
		line2Parts = append(line2Parts, qualityLabel)
		var statusInd *seriesStatusView
		if v, ok := buildSeriesHealthStatus(errFlags[s.ID], warnLevels[s.ID]); ok {
			statusInd = &v
		}
		rows = append(rows, seriesListRow{
			Series:             s,
			HasMonitoredSource: s.Monitored,
			Busy:               best != nil,
			BulkEditBusy:       bulkBusy,
			StatusInd:          statusInd,
			PosterURL:          posterURL,
			BannerURL:          bannerURL,
			Line2:              strings.Join(line2Parts, " - "),
			KindIcon:           kindIcon,
			KindTip:            kindTip,
			Redirect:           redir,
			LiveTarget:         liveTarget,
		})
	}

	clearHref := ""
	if filter.Active() {
		clearHref = clearOperatorFiltersURL(r)
	}
	at := explorerAtFrom(r, explorerAtSeries)
	toolbar := listViewToolbar{
		Query:            filter.Title,
		QueryPlaceholder: searchByPlaceholder(filter.QField),
		AriaLabel:        "Series filters",
		QFieldOpts:       qFieldOpts(filter.QField),
		SortOpts:         seriesSortOpts(r, filter.Sort, filter.SortDir),
		SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
		ViewOpts:         viewOpts(r, viewMode),
		ShowView:         true,
		Selects:          seriesFilterSelects(h, r, filter, roots, profiles),
		FilterActive:     filter.Active(),
		Badges:           seriesListBadges(r, filter),
		ClearAllHref:     clearHref,
		LiveTarget:       liveTarget,
		FormAction:       "/explorer/browse",
		SeriesBulkMode:   true,
	}
	applyExplorerToolbar(&toolbar, explorerTypeSeries, at)

	showSelectAll := total > len(rows)
	if mode == ListModePaginated {
		showSelectAll = load.Page.Show
	}
	out := seriesListLiveData{
		Series:          rows,
		Page:            load.Page,
		Load:            load,
		ListMode:        mode,
		SeriesFilter:    toolbar,
		FilterActive:    filter.Active(),
		BulkEditBusy:    bulkBusy,
		FilterTotal:     total,
		ViewMode:        viewMode,
		TableCols:       parseTableColsCookie(r, cookieColsSeries, seriesTableColDefs()),
		TableColsCookie: cookieColsSeries,
		ShowSelectAll:   showSelectAll,
		InfiniteID:      infiniteID,
		RowsID:          rowsID,
	}
	rewriteExplorerInfinite(&out.Load, explorerTypeSeries, 0)
	out.Page = out.Load.Page
	return out, nil
}

func (h *Handler) seriesErrorCountJSON(w http.ResponseWriter, r *http.Request) {
	n, err := h.Library.CountSeriesWithError()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]int{"count": n})
}

func (h *Handler) renderSeriesInfiniteChunk(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadSeriesListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !data.Load.Append {
		render(w, "series_list_live", data)
		return
	}
	render(w, "series_infinite_chunk", data)
}

// tryRenderSeriesListLive renders #series-list-live when HTMX targeted it.
func (h *Handler) tryRenderSeriesListLive(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("HX-Target") != "series-list-live" {
		return false
	}
	req := r
	if cur := strings.TrimSpace(r.Header.Get("HX-Current-URL")); cur != "" {
		if u, perr := url.Parse(cur); perr == nil && u != nil {
			clone := r.Clone(r.Context())
			clone.URL = u
			clone.RequestURI = u.RequestURI()
			req = clone
		}
	}
	data, err := h.loadSeriesListLive(w, req)
	if err != nil {
		return false
	}
	render(w, "series_list_live", data)
	return true
}
