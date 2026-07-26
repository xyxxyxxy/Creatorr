package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	StreamTypeLabel     string
	DurationLabel       string
	MediaTypeLabel      string
	ThumbURL            string
	DomainActive        bool
	DomainDisabledTitle string
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
			TaskInd:             h.streamIndicator(v.ID, best, v.Status, v.StreamKind()),
			DownloadRunning:     dlRunning,
			DeliveryQueued:      videoDeliveryQueued(tasks),
			Deleting:            taskIsFileDelete(best),
			SizeLabel:           sizeLabel,
			ResolutionLabel:     h.Library.ResolveResolutionLabel(v.ID, v.Width, v.Height, jsonPaths[v.ID]),
			StreamTypeLabel:     library.StreamTypeListLabel(v.StreamKind(), v.StreamBeginningCached),
			DurationLabel:       formatDurationClock(h.Library.ResolveDurationSeconds(v.ID, v.DurationSeconds, jsonPaths[v.ID])),
			MediaTypeLabel:      mediaTypeLabel,
			ThumbURL:            thumbURL,
			DomainActive:        dAct,
			DomainDisabledTitle: disTitle,
		})
	}
	return videos
}

type seriesVideosLiveData struct {
	SeriesID    int64
	IsStream    bool
	Videos      []seriesVideoRow
	VideosPage  PageInfo
	VideoFilter struct {
		Query            string
		QueryPlaceholder string
		AriaLabel        string
		Selects          []listFilterSelect
		LiveTarget       string
		FormAction       string
	}
	FilterActive bool
	OOB          bool
}

func (h *Handler) loadSeriesVideosLive(r *http.Request, ser *library.Series, byVideo map[int64][]queue.Task) (seriesVideosLiveData, error) {
	id := ser.ID
	filter := parseSeriesVideoListFilter(r, ser.Sources)
	videoPage := ParsePage(r, "page")
	videoTotal, _ := h.Library.CountVideosFiltered(id, filter)
	videosPageInfo := NewPageInfoSize(r, "page", videoPage, videoTotal, VideoPageSize)
	vidList, err := h.Library.ListVideosPageFiltered(id, filter, VideoPageSize, OffsetSize(videosPageInfo.Page, VideoPageSize))
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
	var videoFilter struct {
		Query            string
		QueryPlaceholder string
		AriaLabel        string
		Selects          []listFilterSelect
		LiveTarget       string
		FormAction       string
	}
	videoFilter.Query = filter.Title
	videoFilter.QueryPlaceholder = "Search title"
	videoFilter.AriaLabel = "Video filters"
	videoFilter.LiveTarget = "series-videos-live"
	videoFilter.FormAction = fmt.Sprintf("/series/%d", id)
	years, hasUnknown, _ := h.Library.DistinctVideoYears(id)
	yearOpts := make([]listFilterOpt, 0, len(years)+1)
	for _, y := range years {
		ys := strconv.Itoa(y)
		yearOpts = append(yearOpts, listFilterOpt{
			Value:    ys,
			Label:    ys,
			Selected: filter.Year == y,
		})
	}
	if hasUnknown {
		yearOpts = append(yearOpts, listFilterOpt{
			Value:    "unknown",
			Label:    "Unknown",
			Selected: filter.Year == library.VideoYearUnknown,
		})
	}
	statuses, _ := h.Library.DistinctVideoStatuses(id)
	sel := ""
	if len(filter.Statuses) == 1 {
		sel = filter.Statuses[0]
	}
	statusOpts := make([]listFilterOpt, 0, len(statuses))
	for _, st := range statuses {
		statusOpts = append(statusOpts, listFilterOpt{
			Value:    st,
			Label:    videoStatusLabel(st),
			Selected: st == sel,
		})
	}
	srcOpts := make([]listFilterOpt, 0, len(ser.Sources))
	for _, src := range ser.Sources {
		srcOpts = append(srcOpts, listFilterOpt{
			Value:    strconv.FormatInt(src.ID, 10),
			Label:    sourceFilterLabel(src),
			Selected: filter.SourceID == src.ID,
		})
	}
	videoFilter.Selects = append(videoFilter.Selects,
		listFilterSelect{Name: "source", AriaLabel: "Source", EmptyLabel: "All sources", Options: srcOpts},
		listFilterSelect{Name: "status", AriaLabel: "Status", EmptyLabel: "All statuses", Options: statusOpts},
		listFilterSelect{Name: "year", AriaLabel: "Year", EmptyLabel: "All years", Options: yearOpts},
	)

	return seriesVideosLiveData{
		SeriesID:     id,
		IsStream:     ser.IsStream(),
		Videos:       videos,
		VideosPage:   videosPageInfo,
		VideoFilter:  videoFilter,
		FilterActive: filter.Active(),
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
	data, err := h.loadSeriesVideosLive(r, ser, byVideo)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "series_videos_live", data)
}

// parseSeriesVideoListFilter reads ?q= (title), ?year=, ?status=…, ?source=<id>, and optional ?from=&to= (YYYY-MM-DD UTC).
func parseSeriesVideoListFilter(r *http.Request, sources []library.Source) library.VideoListFilter {
	f := library.VideoListFilter{
		Title:   strings.TrimSpace(r.URL.Query().Get("q")),
		FromDay: parseFilterDay(r.URL.Query().Get("from")),
		ToDay:   parseFilterDay(r.URL.Query().Get("to")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("year")); raw != "" {
		if strings.EqualFold(raw, "unknown") {
			f.Year = library.VideoYearUnknown
		} else if y, err := strconv.Atoi(raw); err == nil && y >= 1900 && y <= 2100 {
			f.Year = y
		}
	}
	seen := map[string]struct{}{}
	for _, raw := range r.URL.Query()["status"] {
		st := strings.TrimSpace(raw)
		if st == "" {
			continue
		}
		if _, ok := seen[st]; ok {
			continue
		}
		seen[st] = struct{}{}
		f.Statuses = append(f.Statuses, st)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("source")); raw != "" {
		sid, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && sid > 0 {
			for _, src := range sources {
				if src.ID == sid {
					f.SourceID = sid
					break
				}
			}
		}
	}
	if f.FromDay != "" && f.ToDay != "" && f.FromDay > f.ToDay {
		f.FromDay, f.ToDay = f.ToDay, f.FromDay
	}
	return f
}

func parseFilterDay(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return ""
	}
	return raw
}

func seriesVideoFilterQuery(filter library.VideoListFilter, page int) string {
	q := url.Values{}
	if t := strings.TrimSpace(filter.Title); t != "" {
		q.Set("q", t)
	}
	if filter.Year == library.VideoYearUnknown {
		q.Set("year", "unknown")
	} else if filter.Year > 0 {
		q.Set("year", strconv.Itoa(filter.Year))
	}
	for _, st := range filter.Statuses {
		q.Add("status", st)
	}
	if filter.SourceID > 0 {
		q.Set("source", strconv.FormatInt(filter.SourceID, 10))
	}
	if filter.FromDay != "" {
		q.Set("from", filter.FromDay)
	}
	if filter.ToDay != "" {
		q.Set("to", filter.ToDay)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	return q.Encode()
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
