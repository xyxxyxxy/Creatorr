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
	StatusInd          *seriesStatusView // poster top-left: health errors/warnings only
	PosterURL          string
	Line2              string
	KindIcon           string
	KindTip            string
	Redirect           string
	LiveTarget         string
}

type seriesListLiveData struct {
	Series       []seriesListRow
	Page         PageInfo
	SeriesFilter struct {
		Query            string
		QueryPlaceholder string
		AriaLabel        string
		Selects          []listFilterSelect
		LiveTarget       string
		FormAction       string
	}
	FilterActive bool
	BulkEditBusy bool
	FilterTotal  int
	OOB          bool
}

func parseSeriesListFilter(r *http.Request) library.SeriesListFilter {
	q := r.URL.Query()
	f := library.SeriesListFilter{
		Title: strings.TrimSpace(q.Get("q")),
	}
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
	return f
}

func (h *Handler) loadSeriesListLive(r *http.Request) (seriesListLiveData, error) {
	filter := parseSeriesListFilter(r)
	total, err := h.Library.CountSeriesFiltered(filter)
	if err != nil {
		return seriesListLiveData{}, err
	}
	page := ParsePage(r, "page")
	pageInfo := NewPageInfoSize(r, "page", page, total, SeriesPageSize)
	list, err := h.Library.ListSeriesFiltered(filter, SeriesPageSize, OffsetSize(pageInfo.Page, SeriesPageSize))
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
	liveTarget := "series-list-live"
	bulkBusy, _ := h.Library.BulkEditSeriesBusy()

	rows := make([]seriesListRow, 0, len(list))
	for _, s := range list {
		best := pickBestTask(bySeries[s.ID])
		posterURL := ""
		if path := rootPath[s.RootID]; path != "" {
			if library.SeriesArtFlagsForDir(library.SeriesDir(path, s.Title)).Poster {
				posterURL = fmt.Sprintf("/series/%d/art/poster", s.ID)
			}
		}
		kindIcon, kindTip := "", ""
		if s.IsAudio() {
			kindIcon, kindTip = "headphones", "Audio series"
		}
		srcLabel := fmt.Sprintf("%d sources", s.SourceCount)
		if s.SourceCount == 1 {
			srcLabel = "1 source"
		}
		qualityLabel := s.QualityProfileName
		if s.IsAudio() {
			qualityLabel = library.DefaultProfileName
		}
		line2Parts := []string{qualityLabel}
		if len(roots) > 1 {
			rootLabel := strings.TrimSpace(s.RootName)
			if rootLabel == "" {
				rootLabel = rootPath[s.RootID]
			}
			if rootLabel != "" {
				line2Parts = append(line2Parts, rootLabel)
			}
		}
		line2Parts = append(line2Parts, srcLabel)
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
			Line2:              strings.Join(line2Parts, " · "),
			KindIcon:           kindIcon,
			KindTip:            kindTip,
			Redirect:           redir,
			LiveTarget:         liveTarget,
		})
	}

	pageInfo.LiveTarget = "series-list-live"

	var seriesFilter struct {
		Query            string
		QueryPlaceholder string
		AriaLabel        string
		Selects          []listFilterSelect
		LiveTarget       string
		FormAction       string
	}
	seriesFilter.Query = filter.Title
	seriesFilter.QueryPlaceholder = "Search title"
	seriesFilter.AriaLabel = "Series filters"
	seriesFilter.LiveTarget = "series-list-live"
	seriesFilter.FormAction = "/series"

	if len(roots) > 1 {
		opts := make([]listFilterOpt, 0, len(roots))
		for _, root := range roots {
			label := strings.TrimSpace(root.Name)
			if label == "" {
				label = root.Path
			}
			opts = append(opts, listFilterOpt{
				Value:    strconv.FormatInt(root.ID, 10),
				Label:    label,
				Selected: filter.RootID == root.ID,
			})
		}
		seriesFilter.Selects = append(seriesFilter.Selects, listFilterSelect{
			Name: "root", AriaLabel: "Root folder", EmptyLabel: "All roots", Options: opts,
		})
	}
	if len(profiles) > 0 {
		opts := make([]listFilterOpt, 0, len(profiles))
		for _, p := range profiles {
			opts = append(opts, listFilterOpt{
				Value:    strconv.FormatInt(p.ID, 10),
				Label:    p.Name,
				Selected: filter.QualityProfileID == p.ID,
			})
		}
		seriesFilter.Selects = append(seriesFilter.Selects, listFilterSelect{
			Name: "quality", AriaLabel: "Quality profile", EmptyLabel: "All quality", Options: opts,
		})
	}
	seriesFilter.Selects = append(seriesFilter.Selects, listFilterSelect{
		Name: "delivery", AriaLabel: "Delivery mode", EmptyLabel: "All delivery",
		Options: []listFilterOpt{
			{Value: library.DeliveryVideo, Label: "Video", Selected: filter.DeliveryMode == library.DeliveryVideo},
			{Value: library.DeliveryAudio, Label: "Audio", Selected: filter.DeliveryMode == library.DeliveryAudio},
		},
	})
	seriesFilter.Selects = append(seriesFilter.Selects, listFilterSelect{
		Name: "status", AriaLabel: "Status", EmptyLabel: "Any status",
		Options: []listFilterOpt{
			{Value: library.SeriesListStatusMonitored, Label: "Monitored", Selected: filter.Status == library.SeriesListStatusMonitored},
			{Value: library.SeriesListStatusUnmonitored, Label: "Unmonitored", Selected: filter.Status == library.SeriesListStatusUnmonitored},
			{Value: library.SeriesListStatusComplete, Label: "Complete", Selected: filter.Status == library.SeriesListStatusComplete},
			{Value: library.SeriesListStatusIncomplete, Label: "Incomplete", Selected: filter.Status == library.SeriesListStatusIncomplete},
			{Value: library.SeriesListStatusHasErrors, Label: "Has errors", Selected: filter.Status == library.SeriesListStatusHasErrors},
		},
	})

	return seriesListLiveData{
		Series:       rows,
		Page:         pageInfo,
		SeriesFilter: seriesFilter,
		FilterActive: filter.Active(),
		BulkEditBusy: bulkBusy,
		FilterTotal:  total,
	}, nil
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

func (h *Handler) seriesListLive(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadSeriesListLive(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	render(w, "series_list_live", data)
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
	data, err := h.loadSeriesListLive(req)
	if err != nil {
		return false
	}
	render(w, "series_list_live", data)
	return true
}
