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
	TableCols       []tableCol
	TableColsCookie string
	ShowSelectAll   bool
	InfiniteID      string
	RowsID          string
	OOB             bool
}

// videosPage redirects the legacy /videos host to Browser Videos.
func (h *Handler) videosPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("type", explorerTypeVideos)
	u := "/browser"
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	http.Redirect(w, r, u, http.StatusMovedPermanently)
}

func (h *Handler) loadVideosLive(w http.ResponseWriter, r *http.Request) (videosPageLiveData, error) {
	r = mergeVideosListPrefs(r)
	filter := parseVideoListFilter(r, nil, true)
	if filter.Sort == "" {
		filter.Sort = library.SortAdded
	}
	writeVideosListPrefs(w, r, filter)
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
	lockedIDs, _ := h.Library.VideoIDsLockedByBulkEdit()
	for i := range rows {
		_, rows[i].BulkEditLocked = lockedIDs[rows[i].ID]
	}
	// Badge/chip labels need titles for every filtered series, not only rows on this page.
	seriesIDs := make([]int64, 0, len(filter.SeriesIDs)+len(list))
	seen := map[int64]struct{}{}
	for _, id := range filter.SeriesIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		seriesIDs = append(seriesIDs, id)
	}
	for _, v := range list {
		if _, ok := seen[v.SeriesID]; ok {
			continue
		}
		seen[v.SeriesID] = struct{}{}
		seriesIDs = append(seriesIDs, v.SeriesID)
	}
	titles, _ := h.Library.SeriesTitles(seriesIDs)

	var sources []library.Source
	if len(filter.SeriesIDs) == 1 {
		if ser, err := h.Library.GetSeries(filter.SeriesIDs[0], false); err == nil {
			sources = ser.Sources
		}
	}

	qfOpts := qFieldOpts(filter.QField)
	toolbar := listViewToolbar{
		Query:            filter.Title,
		QueryPlaceholder: searchByPlaceholder(qfOpts),
		AriaLabel:        "Video filters",
		QFieldOpts:       qfOpts,
		SortOpts:         videoSortOpts(r, filter.Sort, filter.SortDir, library.SortAdded),
		SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
		ViewOpts:         viewOpts(r, viewMode),
		ShowView:         true,
		FromDay:          filter.FromDay,
		ToDay:            filter.ToDay,
		ShowDateRange:    true,
		ShowDatePresence: true,
		DateRangeLabel:   "Upload date",
		Selects:          videoFilterSelects(h, r, 0, filter, sources, true),
		FilterActive:     filter.MenuActive(),
		Badges:           videoListBadges(r, filter, true, titles),
		ClearAllHref:     "",
		LiveTarget:       liveTarget,
		FormAction:       "/explorer/browse",
		VideoBulkMode:    true,
	}
	annotateUploadPresence(r, &toolbar)
	if filter.MenuActive() {
		toolbar.ClearAllHref = clearOperatorFiltersURL(r)
	}
	at := explorerAtFrom(r, explorerAtBrowser)
	if at == explorerAtVideos {
		// Legacy at=videos bookmarks push to Browser.
		at = explorerAtBrowser
	}
	applyExplorerToolbar(&toolbar, explorerTypeVideos, at)

	showSelectAll := total > len(rows)
	if mode == ListModePaginated {
		showSelectAll = load.Page.Show
	}
	out := videosPageLiveData{
		Videos:       rows,
		Page:         load.Page,
		Load:         load,
		ListMode:     mode,
		FilterTotal:  total,
		VideoFilter:  toolbar,
		FilterActive: filter.Active(),
		ViewMode:     viewMode,
		SeriesTitles: titles,
		TableCols: annotateTableColsSort(
			parseTableColsCookie(r, cookieColsVideos, videoTableColDefs(true)),
			toolbar.SortOpts, toolbar.SortDir),
		TableColsCookie: cookieColsVideos,
		ShowSelectAll:   showSelectAll,
		InfiniteID:      infiniteID,
		RowsID:          rowsID,
	}
	rewriteExplorerInfinite(&out.Load, explorerTypeVideos, 0)
	out.Page = out.Load.Page
	return out, nil
}
