package web

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type videosPageLiveData struct {
	Videos          []seriesVideoRow
	Page            PageInfo
	Load            ListLoad
	ListMode        ListMode
	FilterTotal     int
	VideoFilter     listViewToolbar
	FilterActive    bool
	ViewMode        string
	SeriesTitles    map[int64]string
	BulkEditBusy    bool
	TableCols       []tableCol
	TableColsCookie string
	ShowSelectAll   bool
	InfiniteID      string
	RowsID          string
	OOB             bool
}

func (h *Handler) videosPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Target") == "videos-list-infinite" {
		h.renderVideosInfiniteChunk(w, r)
		return
	}
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	suggestions, _ := h.Library.ListMetaSuggestions()
	render(w, "videos", struct {
		pageBase
		Live            videosPageLiveData
		Suggestions     library.MetaSuggestions
		PackRoleOptions []struct{ Value, Label string }
	}{
		pageBase:        newPage("Videos", "videos", flashFromQuery(r)),
		Live:            data,
		Suggestions:     suggestions,
		PackRoleOptions: library.PackRoleSelectOptions(),
	})
}

func (h *Handler) videosLive(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Target") == "videos-list-infinite" {
		h.renderVideosInfiniteChunk(w, r)
		return
	}
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "videos_live", data)
}

func (h *Handler) renderVideosInfiniteChunk(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !data.Load.Append {
		render(w, "videos_live", data)
		return
	}
	render(w, "videos_infinite_chunk", data)
}

func (h *Handler) loadVideosLive(w http.ResponseWriter, r *http.Request) (videosPageLiveData, error) {
	r = mergeVideosListPrefs(r)
	filter := parseVideoListFilter(r, nil, true)
	if filter.Sort == "" {
		filter.Sort = library.SortAdded
	}
	writeVideosListPrefs(w, filter)
	viewMode, writeCookie := resolveViewMode(r, cookieModeVideos, viewList)
	if writeCookie {
		writeViewCookie(w, cookieModeVideos, viewMode)
	}
	total, err := h.Library.CountVideosFiltered(0, filter)
	if err != nil {
		return videosPageLiveData{}, err
	}

	mode := libraryListMode(viewMode)
	const liveTarget = "videos-list-live"
	const infiniteID = "videos-list-infinite"
	const rowsID = "videos-list-rows"

	var load ListLoad
	var limit, offset int
	switch mode {
	case ListModeInfinite:
		load = resolveInfiniteLoad(r, total, liveTarget, infiniteID, "page")
		limit, offset = infiniteLimitOffset(load)
	default:
		load = resolvePaginatedLoad(r, total, VideoPageSize, liveTarget, "page")
		limit = load.PageSize
		offset = OffsetSize(load.Page.Page, load.PageSize)
	}

	list, err := h.Library.ListVideosPageFiltered(0, filter, limit, offset)
	if err != nil {
		return videosPageLiveData{}, err
	}
	active, _ := h.Queue.ListActive()
	byVideo := map[int64][]queue.Task{}
	for _, t := range active {
		if t.VideoID.Valid {
			vid := t.VideoID.Int64
			byVideo[vid] = append(byVideo[vid], t)
		}
	}
	rows := h.buildSeriesVideoRows(list, byVideo, nil)
	seriesIDs := make([]int64, 0, len(list))
	seen := map[int64]struct{}{}
	for _, v := range list {
		if _, ok := seen[v.SeriesID]; ok {
			continue
		}
		seen[v.SeriesID] = struct{}{}
		seriesIDs = append(seriesIDs, v.SeriesID)
	}
	titles, _ := h.Library.SeriesTitles(seriesIDs)

	var sources []library.Source
	if filter.SeriesID > 0 {
		if ser, err := h.Library.GetSeries(filter.SeriesID, false); err == nil {
			sources = ser.Sources
		}
	}

	toolbar := listViewToolbar{
		Query:            filter.Title,
		QueryPlaceholder: searchByPlaceholder(filter.QField),
		AriaLabel:        "Video filters",
		QFieldOpts:       qFieldOpts(filter.QField),
		SortOpts:         videoSortOpts(r, filter.Sort, filter.SortDir, library.SortAdded),
		SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
		ViewOpts:         viewOpts(r, viewMode),
		ShowView:         true,
		FromDay:          filter.FromDay,
		ToDay:            filter.ToDay,
		ShowDateRange:    true,
		Selects:          videoFilterSelects(h, r, 0, filter, sources, true),
		FilterActive:     filter.Active(),
		Badges:           videoListBadges(r, filter, true, titles),
		ClearAllHref:     "",
		LiveTarget:       liveTarget,
		FormAction:       "/videos",
		VideoBulkMode:    true,
		DateClearHref:    dropQueryKeys(r, "from", "to", "page", "through"),
	}
	annotateUploadPresence(r, &toolbar)
	if filter.Active() {
		toolbar.ClearAllHref = clearOperatorFiltersURL(r)
	}

	bulkBusy, _ := h.Library.BulkEditVideosBusy()
	showSelectAll := total > len(rows)
	if mode == ListModePaginated {
		showSelectAll = load.Page.Show
	}
	return videosPageLiveData{
		Videos:          rows,
		Page:            load.Page,
		Load:            load,
		ListMode:        mode,
		FilterTotal:     total,
		VideoFilter:     toolbar,
		FilterActive:    filter.Active(),
		ViewMode:        viewMode,
		SeriesTitles:    titles,
		BulkEditBusy:    bulkBusy,
		TableCols:       parseTableColsCookie(r, cookieColsVideos, videoTableColDefs(true)),
		TableColsCookie: cookieColsVideos,
		ShowSelectAll:   showSelectAll,
		InfiniteID:      infiniteID,
		RowsID:          rowsID,
	}, nil
}
