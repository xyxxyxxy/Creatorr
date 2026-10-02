package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type seriesVideoRow struct {
	library.Video
	TaskInd             taskIndicatorView
	DownloadRunning     bool
	DeliveryQueued      bool
	Deleting            bool
	SizeLabel           string
	ResolutionLabel     string
	DurationLabel       string
	MediaTypeLabel      string
	StatusLabel         string
	ThumbURL            string
	DomainActive        bool
	DomainDisabledTitle string
	PackRoleBadge       string
}

// buildSeriesVideoRows enriches videos for video_list_row (series, source, history).
// domainBySource may be nil - then domain active is resolved from each video SourceURL.
func (h *Handler) buildSeriesVideoRows(vidList []library.Video, byVideo map[int64][]queue.Task, domainBySource map[int64]struct {
	active bool
	title  string
}) []seriesVideoRow {
	if len(vidList) == 0 {
		return nil
	}
	if byVideo == nil {
		byVideo = map[int64][]queue.Task{}
	}
	ids := make([]int64, 0, len(vidList))
	for _, v := range vidList {
		ids = append(ids, v.ID)
	}
	sizes, _ := h.Library.VideoSizeBytesMap(ids)
	thumbs, _ := h.Library.VideoThumbPathMap(ids)
	jsonPaths, _ := h.Library.VideoJSONPathMap(ids)
	domainCache := map[string]struct {
		active bool
		title  string
	}{}
	videos := make([]seriesVideoRow, 0, len(vidList))
	for _, v := range vidList {
		tasks := byVideo[v.ID]
		dlRunning := videoTaskRunning(tasks)
		sizeLabel := "-"
		if n, ok := sizes[v.ID]; ok {
			sizeLabel = library.FormatBytes(n)
		}
		thumbURL := ""
		if _, ok := thumbs[v.ID]; ok {
			thumbURL = fmt.Sprintf("/series/%d/videos/%d/thumb", v.SeriesID, v.ID)
		}
		dAct := true
		disTitle := ""
		if v.SourceID.Valid {
			if domainBySource != nil {
				if dom, ok := domainBySource[v.SourceID.Int64]; ok {
					dAct = dom.active
					disTitle = dom.title
				}
			} else if v.SourceURL.Valid {
				host := queue.DomainFromURL(v.SourceURL.String)
				if host != "" {
					if cached, ok := domainCache[host]; ok {
						dAct, disTitle = cached.active, cached.title
					} else {
						dAct, _ = domains.IsActive(h.Queue.DB, host)
						if !dAct {
							disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
						}
						domainCache[host] = struct {
							active bool
							title  string
						}{active: dAct, title: disTitle}
					}
				}
			}
		}
		best := pickBestTask(tasks)
		mediaTypeLabel := strings.TrimSpace(v.MediaType)
		if mediaTypeLabel == "" {
			mediaTypeLabel = "-"
		}
		videos = append(videos, seriesVideoRow{
			Video:               v,
			TaskInd:             h.videoIndicator(v.ID, best, v.Status),
			DownloadRunning:     dlRunning,
			DeliveryQueued:      videoDeliveryQueued(tasks),
			Deleting:            taskIsFileDelete(best),
			SizeLabel:           sizeLabel,
			ResolutionLabel:     library.PeekResolutionLabel(v.Width, v.Height, jsonPaths[v.ID]),
			DurationLabel:       formatDurationClock(library.PeekDurationSeconds(v.DurationSeconds, jsonPaths[v.ID])),
			MediaTypeLabel:      mediaTypeLabel,
			StatusLabel:         videoStatusLabel(v.Status),
			ThumbURL:            thumbURL,
			DomainActive:        dAct,
			DomainDisabledTitle: disTitle,
			PackRoleBadge:       library.PackRoleBadgeLabel(v.PackRole),
		})
	}
	return videos
}

type seriesVideosLiveData struct {
	SeriesID           int64
	Videos             []seriesVideoRow
	VideosPage         PageInfo
	FilterTotal        int
	BulkEditBusy       bool
	ProgressTotal      int64
	DownloadedCount    int64
	ErrorCount         int64
	WantedCount        int64
	Monitored          bool
	DownloadErrorCount int
	VideoFilter        listViewToolbar
	FilterActive       bool
	ViewMode           string
	TableCols          []tableCol
	TableColsCookie    string
	OOB                bool
}

// listViewToolbar is the shared filter/sort/view chrome for library lists.
type listViewToolbar struct {
	Query                string
	QueryPlaceholder     string
	AriaLabel            string
	QFieldOpts           []listFilterOpt
	SortOpts             []listFilterOpt
	SortDir              string // asc|desc resolved for UI
	ViewOpts             []listFilterOpt
	ShowView             bool
	FromDay              string
	ToDay                string
	ShowDateRange        bool
	DateClearHref        string
	UploadEmptyHref      string
	UploadFilledHref     string
	UploadEmptySelected  bool
	UploadFilledSelected bool
	Selects              []listFilterSelect
	FilterActive         bool
	Badges               []listViewBadge
	ClearAllHref         string
	LiveTarget           string
	FormAction           string
	SeriesBulkMode       bool
	VideoBulkMode        bool
}

func (h *Handler) loadSeriesVideosLive(w http.ResponseWriter, r *http.Request, ser *library.Series, byVideo map[int64][]queue.Task) (seriesVideosLiveData, error) {
	id := ser.ID
	filter := parseSeriesVideoListFilter(r, ser.Sources)
	if filter.Sort == "" {
		filter.Sort = library.SortUpload
	}
	viewMode, writeCookie := resolveViewMode(r, cookieModeSeriesVideos, viewList)
	if writeCookie {
		writeViewCookie(w, cookieModeSeriesVideos, viewMode)
	}
	videoTotal, _ := h.Library.CountVideosFiltered(id, filter)
	load := resolvePaginatedLoad(r, videoTotal, VideoPageSize, "series-videos-live", "page")
	videosPageInfo := load.Page
	vidList, err := h.Library.ListVideosPageFiltered(id, filter, load.PageSize, OffsetSize(videosPageInfo.Page, load.PageSize))
	if err != nil {
		return seriesVideosLiveData{}, err
	}

	domainBySource := map[int64]struct {
		active bool
		title  string
	}{}
	for _, src := range ser.Sources {
		host := queue.DomainFromURL(src.URL)
		dAct, _ := domains.IsActive(h.Queue.DB, host)
		disTitle := ""
		if !dAct {
			disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
		}
		domainBySource[src.ID] = struct {
			active bool
			title  string
		}{active: dAct, title: disTitle}
	}

	videos := h.buildSeriesVideoRows(vidList, byVideo, domainBySource)

	videosPageInfo.LiveTarget = "series-videos-live"
	videoFilter := listViewToolbar{
		Query:            filter.Title,
		QueryPlaceholder: searchByPlaceholder(filter.QField),
		AriaLabel:        "Video filters",
		QFieldOpts:       qFieldOpts(filter.QField),
		SortOpts:         videoSortOpts(r, filter.Sort, filter.SortDir, library.SortUpload),
		SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
		ViewOpts:         viewOpts(r, viewMode),
		ShowView:         true,
		FromDay:          filter.FromDay,
		ToDay:            filter.ToDay,
		ShowDateRange:    true,
		Selects:          videoFilterSelects(h, r, id, filter, ser.Sources, false),
		FilterActive:     filter.Active(),
		Badges:           videoListBadges(r, filter, false, nil),
		ClearAllHref:     "",
		LiveTarget:       "series-videos-live",
		FormAction:       fmt.Sprintf("/series/%d", id),
		VideoBulkMode:    true,
		DateClearHref:    dropQueryKeys(r, "from", "to", "page"),
	}
	annotateUploadPresence(r, &videoFilter)
	if filter.Active() {
		videoFilter.ClearAllHref = clearOperatorFiltersURL(r)
	}

	bulkBusy, _ := h.Library.BulkEditVideosBusy()
	dlErrCount, _ := h.Library.CountSeriesDownloadErrors(id)
	return seriesVideosLiveData{
		SeriesID:           id,
		Videos:             videos,
		VideosPage:         videosPageInfo,
		FilterTotal:        videoTotal,
		BulkEditBusy:       bulkBusy,
		ProgressTotal:      ser.ProgressTotal(),
		DownloadedCount:    ser.DownloadedCount,
		ErrorCount:         ser.ErrorCount(),
		WantedCount:        ser.WantedCount,
		Monitored:          ser.Monitored,
		DownloadErrorCount: dlErrCount,
		VideoFilter:        videoFilter,
		FilterActive:       filter.Active(),
		ViewMode:           viewMode,
		TableCols:          parseTableColsCookie(r, cookieColsSeriesVideos, videoTableColDefs(false)),
		TableColsCookie:    cookieColsSeriesVideos,
	}, nil
}

func (h *Handler) seriesVideosLive(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ser, err := h.Library.GetSeries(id, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	activeTasks, _ := h.Queue.ListActiveForSeries(id)
	_, _, byVideo := seriesActivityMaps(activeTasks)
	data, err := h.loadSeriesVideosLive(w, r, ser, byVideo)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "series_videos_live", data)
}

// parseSeriesVideoListFilter reads video list filters for a series page (series locked).
func parseSeriesVideoListFilter(r *http.Request, sources []library.Source) library.VideoListFilter {
	return parseVideoListFilter(r, sources, false)
}

func seriesVideoFilterQuery(filter library.VideoListFilter, page int) string {
	return encodeVideoListFilter(filter, page, "")
}

func sourceFilterLabel(src library.Source) string {
	if src.Label.Valid && strings.TrimSpace(src.Label.String) != "" {
		return src.Label.String
	}
	u := DisplayURL(strings.TrimSpace(src.URL))
	if len(u) > 48 {
		return u[:45] + "…"
	}
	if u != "" {
		return u
	}
	return fmt.Sprintf("Source #%d", src.ID)
}
