package web

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type videosPageLiveData struct {
	Videos       []seriesVideoRow
	Page         PageInfo
	FilterTotal  int
	VideoFilter  listViewToolbar
	FilterActive bool
	ViewMode     string
	SeriesTitles map[int64]string
	OOB          bool
}

func (h *Handler) videosPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "videos", struct {
		pageBase
		Live videosPageLiveData
	}{
		pageBase: newPage("Videos", "videos", flashFromQuery(r)),
		Live:     data,
	})
}

func (h *Handler) videosLive(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadVideosLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "videos_live", data)
}

func (h *Handler) loadVideosLive(w http.ResponseWriter, r *http.Request) (videosPageLiveData, error) {
	filter := parseVideoListFilter(r, nil, true)
	if filter.Sort == "" {
		filter.Sort = library.SortAdded
	}
	viewMode, writeCookie := resolveViewMode(r, cookieModeVideos, viewList)
	if writeCookie {
		writeViewCookie(w, cookieModeVideos, viewMode)
	}
	page := ParsePage(r, "page")
	total, err := h.Library.CountVideosFiltered(0, filter)
	if err != nil {
		return videosPageLiveData{}, err
	}
	pageInfo := NewPageInfoSize(r, "page", page, total, VideoPageSize)
	pageInfo.LiveTarget = "videos-list-live"
	list, err := h.Library.ListVideosPageFiltered(0, filter, VideoPageSize, OffsetSize(pageInfo.Page, VideoPageSize))
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
		LiveTarget:       "videos-list-live",
		FormAction:       "/videos",
		DateClearHref:    dropQueryKeys(r, "from", "to", "page"),
		UploadEmptyHref:  applyPresenceURL(r, true, library.PresenceUploadDate),
		UploadFilledHref: applyPresenceURL(r, false, library.PresenceUploadDate),
	}
	if filter.Active() {
		toolbar.ClearAllHref = clearOperatorFiltersURL(r)
	}

	return videosPageLiveData{
		Videos:       rows,
		Page:         pageInfo,
		FilterTotal:  total,
		VideoFilter:  toolbar,
		FilterActive: filter.Active(),
		ViewMode:     viewMode,
		SeriesTitles: titles,
	}, nil
}
