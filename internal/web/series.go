package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func videoTaskRunning(tasks []queue.Task) bool {
	for _, t := range tasks {
		if (t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindIntegrityCheckInitial) && t.Status == queue.StatusRunning {
			return true
		}
	}
	return false
}

// seriesFolderLockTip is the DisabledTitle for Edit series Title and Root folder.
const seriesFolderLockTip = "'Title' and 'Root folder' are locked while download, SponsorBlock cut, or media verify tasks for this series are pending or running. Wait or cancel those tasks first."

func editSeriesSettingsFields(ser *library.Series, roots []library.RootFolder, profiles []library.QualityProfile, busy, oob bool) map[string]any {
	return map[string]any{
		"DeliveryMode":       ser.DeliveryMode,
		"Title":              ser.Title,
		"TitleInfo":          "Changing the title updates Creatorr and renames the on-disk series folder immediately. Episode filenames that include the series name are not rewritten until you run 'Apply episode format' under 'Settings → Library'.",
		"TitleDisabled":      busy,
		"TitleDisabledTitle": seriesFolderLockTip,
		"Monitored":          ser.Monitored,
		"Roots":              roots,
		"RootID":             ser.RootID,
		"RootInfo":           "Where downloads and series NFO/art are written. Changing root moves the series folder on disk.",
		"RootDisabled":       busy,
		"RootDisabledTitle":  seriesFolderLockTip,
		"Profiles":           profiles,
		"ProfileID":          ser.QualityProfileID,
		"OOB":                oob,
	}
}

// videoDeliveryQueued is true when a download, sponsorblock_cut, or media_verify task is pending or running.
func videoDeliveryQueued(tasks []queue.Task) bool {
	for _, t := range tasks {
		if t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindIntegrityCheckInitial {
			return true
		}
	}
	return false
}

func deliveryTaskActive(t *queue.Task) bool {
	return t != nil && (t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindIntegrityCheckInitial)
}

func (h *Handler) seriesList(w http.ResponseWriter, r *http.Request) {
	live, err := h.loadSeriesListLive(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	suggestions, _ := h.Library.ListMetaSuggestions()
	render(w, "series_list", struct {
		pageBase
		Live                seriesListLiveData
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		Suggestions         library.MetaSuggestions
		ScanCronDescriptors []string
	}{
		pageBase:            newPage("Series", "series", flashFromQuery(r)),
		Live:                live,
		Roots:               roots,
		Profiles:            profiles,
		Suggestions:         suggestions,
		ScanCronDescriptors: scanCronDescriptors(),
	})
}

func (h *Handler) seriesAdd(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/series?add=1", http.StatusSeeOther)
}

func (h *Handler) actionProbeSourceTitle(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.URL.Query().Get("source_url"))
	if url == "" {
		url = strings.TrimSpace(r.URL.Query().Get("url"))
	}
	if url == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if h.Queue == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	domain := queue.DomainFromURL(url)
	if domain == "" {
		domain = queue.SystemDomain
	}
	tid, err := h.Queue.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindProbeSourceTitle,
		Domain:  domain,
		Payload: map[string]any{"url": url},
		Message: "Probing title…",
	})
	if err != nil {
		slog.Warn("probe source title enqueue", "url", url, "err", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-r.Context().Done():
			_, _ = h.Queue.CancelWithReason(tid, queue.CancelReasonManual)
			w.WriteHeader(http.StatusNoContent)
			return
		default:
		}
		task, gerr := h.Queue.GetTask(tid)
		if gerr != nil || task == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch task.Status {
		case queue.StatusDone:
			title := strings.TrimSpace(task.Detail)
			if title == "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(title))
			return
		case queue.StatusFailed, queue.StatusCancelled:
			w.WriteHeader(http.StatusNoContent)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, _ = h.Queue.CancelWithReason(tid, queue.CancelReasonManual)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) seriesDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ser, err := h.Library.GetSeries(id, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	canScan := false
	blocked := ""
	if len(ser.Sources) == 0 {
		blocked = "Add a source first."
	} else {
		for _, src := range ser.Sources {
			host := queue.DomainFromURL(src.URL)
			dAct, _ := domains.IsActive(h.Queue.DB, host)
			if dAct {
				canScan = true
				break
			}
		}
		if !canScan {
			blocked = "All source domains are inactive. Activate under 'Settings → Queue / Domains'."
		}
	}
	type sourceRow struct {
		library.Source
		SeriesID            int64
		SeriesMonitored     bool
		ScanActive          bool
		FullScanStalled     bool
		DomainHost          string
		DomainActive        bool
		DomainDisabledTitle string
		ScanCronLabel       string
		StatusInd           sourceStatusView
		HasRetryable        bool
		VideoCount          int
		LastScannedAgo      string
		LastScannedAt       string
		LastHistoryID       int64
		StatusSummary       string
		ErrorMessage        string
		HasScanned          bool
		HasError            bool
		OOB                 bool
	}
	activeTasks, _ := h.Queue.ListActiveForSeries(id)
	seriesTasks, bySource, byVideo := seriesActivityMaps(activeTasks)
	h.mergeFileDeleteForSeries(id, &seriesTasks, byVideo)
	seriesInd := indicatorFromTask(seriesIndicatorID(id), pickBestTask(seriesTasks), "Active tasks for this series")
	seriesDeleting := taskIsFileDelete(pickBestTask(seriesTasks))

	now := time.Now().UTC()
	vcounts, _ := h.Library.CountVideosBySource(id)
	srcRows := make([]sourceRow, 0, len(ser.Sources))
	for _, src := range ser.Sources {
		active, _ := h.Library.HasActiveScanForSource(src.ID)
		stalled := !src.FullScanDone && !active
		host := queue.DomainFromURL(src.URL)
		dAct, _ := domains.IsActive(h.Queue.DB, host)
		disTitle := ""
		if !dAct {
			disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
		}
		best := pickBestTask(bySource[src.ID])
		retryable, _ := h.Library.SourceHasRetryableVideos(src.ID)
		summary, lastAt, errMsg, errCode, taskID, hasScanned, hasError := sourceStatusFields(h.Library, src.ID, now)
		tipAt, _ := h.Library.LatestTipScannedAt(src.ID)
		cronLabel := cronexpr.DescribeScan(src.ScanCron)
		statusInd := buildSourceStatus(sourceStatusParams{
			Src: src, Best: best, HasError: hasError, ErrMsg: errMsg, ErrCode: errCode, Stalled: stalled,
			SeriesMonitored: ser.Monitored, DomainActive: dAct, DomainDisabledTitle: disTitle,
			ScanCronLabel: cronLabel, Summary: summary, HasScanned: hasScanned, HistoryID: taskID,
			Now: now, LastTipScannedAt: tipAt,
		})
		scannedAgo := ""
		if hasScanned {
			_, scannedAgo = createdAgoPair(lastAt, now)
		}
		srcRows = append(srcRows, sourceRow{
			Source: src, SeriesID: id, SeriesMonitored: ser.Monitored,
			ScanActive: active, FullScanStalled: stalled,
			DomainHost: host, DomainActive: dAct, DomainDisabledTitle: disTitle,
			ScanCronLabel: cronLabel,
			StatusInd:     statusInd, HasRetryable: retryable, VideoCount: vcounts[src.ID],
			LastScannedAgo: scannedAgo, LastScannedAt: lastAt, LastHistoryID: taskID,
			StatusSummary: summary, ErrorMessage: errMsg, HasScanned: hasScanned, HasError: hasError,
		})
	}
	pageSrc, sourcesPage := SlicePage(r, "sources_page", srcRows)

	videosLive, listErr := h.loadSeriesVideosLive(r, ser, byVideo)
	if listErr != nil {
		http.Error(w, listErr.Error(), 500)
		return
	}
	filter := parseSeriesVideoListFilter(r, ser.Sources)
	sourceURLs := make([]string, 0, len(ser.Sources))
	for _, src := range ser.Sources {
		sourceURLs = append(sourceURLs, src.URL)
	}
	indicatorsQ := fmt.Sprintf("/series/%d/task-indicators", id)
	if q := seriesVideoFilterQuery(filter, videosLive.VideosPage.Page); q != "" {
		indicatorsQ += "?" + q
	}
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	folderRenameBusy, _ := h.Library.SeriesHasBusyMediaTasks(id)
	metaForm := seriesMetadataView{
		Series:      ser,
		Art:         h.seriesArtFlags(ser),
		PrefetchArt: map[string]string{},
		Open:        r.URL.Query().Get("meta") == "1",
	}
	if tidStr := r.URL.Query().Get("prefetch_task"); tidStr != "" {
		if tid, err := strconv.ParseInt(tidStr, 10, 64); err == nil && tid > 0 {
			metaForm.PrefetchTaskID = tid
			metaForm.Open = true
			if task, err := h.Queue.GetTask(tid); err == nil && task != nil {
				metaForm.FetchURL = queue.URLFromPayload(task.Payload)
				switch task.Status {
				case queue.StatusPending, queue.StatusRunning:
					metaForm.PrefetchPending = true
				case queue.StatusDone:
					if d, err := h.Library.ReadPrefetchDraft(id, tid); err == nil {
						metaForm.PrefetchDraft = d
						metaForm.PrefetchArt = prefetchArtMap(d)
						metaForm.Series = applyPrefetchDraft(ser, d)
					}
				case queue.StatusFailed, queue.StatusCancelled:
					metaForm.PrefetchDraft.Error = task.ErrorMessage
					if metaForm.PrefetchDraft.Error == "" {
						metaForm.PrefetchDraft.Error = "Prefetch failed"
					}
				}
			}
		}
	}
	metaForm = h.withMetaSuggestions(metaForm)
	metaFiles := seriesMetaFileViews(h.Library, ser)
	videoTotal, _ := h.Library.CountVideos(id)
	editSettings := editSeriesSettingsFields(ser, roots, profiles, folderRenameBusy, false)
	render(w, "series_detail", struct {
		pageBase
		Series              *library.Series
		Sources             []sourceRow
		SourcesPage         PageInfo
		SourceURLs          []string
		VideosLive          seriesVideosLiveData
		HasVideos           bool
		MetaFiles           []seriesMetaFileView
		CanScan             bool
		ScanBlocked         string
		SeriesInd           taskIndicatorView
		HasMonitoredSource  bool
		TaskIndicatorsPath  string
		ScanCronDescriptors []string
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		FolderRenameBusy    bool
		EditSettings        map[string]any
		MetaForm            seriesMetadataView
		Deleting            bool
	}{
		pageBase:            newPage(ser.Title, "series", flashFromQuery(r)),
		Series:              ser,
		Sources:             pageSrc,
		SourcesPage:         sourcesPage,
		SourceURLs:          sourceURLs,
		VideosLive:          videosLive,
		HasVideos:           videoTotal > 0,
		MetaFiles:           metaFiles,
		CanScan:             canScan && !seriesDeleting,
		ScanBlocked:         blocked,
		SeriesInd:           seriesInd,
		HasMonitoredSource:  ser.Monitored,
		TaskIndicatorsPath:  indicatorsQ,
		ScanCronDescriptors: scanCronDescriptors(),
		Roots:               roots,
		Profiles:            profiles,
		FolderRenameBusy:    folderRenameBusy,
		EditSettings:        editSettings,
		MetaForm:            metaForm,
		Deleting:            seriesDeleting,
	})
}

func (h *Handler) seriesTaskIndicators(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ser, err := h.Library.GetSeries(id, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	activeTasks, _ := h.Queue.ListActiveForSeries(id)
	seriesTasks, bySource, byVideo := seriesActivityMaps(activeTasks)
	h.mergeFileDeleteForSeries(id, &seriesTasks, byVideo)
	filter := parseSeriesVideoListFilter(r, ser.Sources)
	videoPage := ParsePage(r, "page")
	videoTotal, _ := h.Library.CountVideosFiltered(id, filter)
	videosPageInfo := NewPageInfoSize(r, "page", videoPage, videoTotal, VideoPageSize)
	pageVids, _ := h.Library.ListVideosPageFiltered(id, filter, VideoPageSize, OffsetSize(videosPageInfo.Page, VideoPageSize))

	vidIDs := map[int64]struct{}{}
	for _, v := range pageVids {
		vidIDs[v.ID] = struct{}{}
	}
	for vid := range byVideo {
		vidIDs[vid] = struct{}{}
	}

	inds := make([]taskIndicatorView, 0, 1+len(vidIDs))
	si := indicatorFromTask(seriesIndicatorID(id), pickBestTask(seriesTasks), "Active tasks for this series")
	si.OOB = true
	inds = append(inds, si)
	for vid := range vidIDs {
		st := ""
		if v, err := h.Library.GetVideo(vid); err == nil && v != nil {
			st = v.Status
		}
		v := h.videoIndicator(vid, pickBestTask(byVideo[vid]), st)
		v.OOB = true
		inds = append(inds, v)
	}

	now := time.Now().UTC()
	type sourceLive struct {
		library.Source
		SeriesID            int64
		SeriesMonitored     bool
		ScanActive          bool
		FullScanStalled     bool
		DomainActive        bool
		DomainDisabledTitle string
		LastHistoryID       int64
		LastScannedAt       string
		StatusSummary       string
		ErrorMessage        string
		HasScanned          bool
		HasError            bool
		StatusInd           sourceStatusView
		OOB                 bool
	}
	srcLive := make([]sourceLive, 0, len(ser.Sources))
	for _, src := range ser.Sources {
		active, _ := h.Library.HasActiveScanForSource(src.ID)
		stalled := !src.FullScanDone && !active
		host := queue.DomainFromURL(src.URL)
		dAct, _ := domains.IsActive(h.Queue.DB, host)
		disTitle := ""
		if !dAct {
			disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
		}
		summary, lastAt, errMsg, errCode, taskID, hasScanned, hasError := sourceStatusFields(h.Library, src.ID, now)
		tipAt, _ := h.Library.LatestTipScannedAt(src.ID)
		cronLabel := cronexpr.DescribeScan(src.ScanCron)
		statusInd := buildSourceStatus(sourceStatusParams{
			Src: src, Best: pickBestTask(bySource[src.ID]), HasError: hasError, ErrMsg: errMsg, ErrCode: errCode, Stalled: stalled,
			SeriesMonitored: ser.Monitored, DomainActive: dAct, DomainDisabledTitle: disTitle,
			ScanCronLabel: cronLabel, Summary: summary, HasScanned: hasScanned, HistoryID: taskID,
			Now: now, LastTipScannedAt: tipAt,
		})
		statusInd.OOB = true
		srcLive = append(srcLive, sourceLive{
			Source: src, SeriesID: id, SeriesMonitored: ser.Monitored,
			ScanActive: active, FullScanStalled: stalled, LastHistoryID: taskID, LastScannedAt: lastAt,
			DomainActive: dAct, DomainDisabledTitle: disTitle,
			StatusSummary: summary, ErrorMessage: errMsg, HasScanned: hasScanned, HasError: hasError,
			StatusInd: statusInd, OOB: true,
		})
	}

	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	folderBusy, _ := h.Library.SeriesHasBusyMediaTasks(id)

	render(w, "task_indicators_oob", struct {
		Indicators   []taskIndicatorView
		Sources      []sourceLive
		EditSettings map[string]any
	}{
		Indicators:   inds,
		Sources:      srcLive,
		EditSettings: editSeriesSettingsFields(ser, roots, profiles, folderBusy, true),
	})
}

func (h *Handler) videoTaskIndicator(w http.ResponseWriter, r *http.Request) {
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	t, _ := h.Queue.ActiveTaskForVideo(vid)
	st := ""
	if v, err := h.Library.GetVideo(vid); err == nil && v != nil {
		st = v.Status
	}
	v := h.videoIndicator(vid, t, st)
	v.OOB = true
	render(w, "task_indicator", v)
}

func (h *Handler) sourceDetail(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	sourceID, _ := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	ser, err := h.Library.GetSeries(seriesID, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	src, err := h.Library.GetSource(seriesID, sourceID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	host := queue.DomainFromURL(src.URL)
	dAct, _ := domains.IsActive(h.Queue.DB, host)
	disTitle := ""
	if !dAct {
		disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
	}
	retryable, _ := h.Library.SourceHasRetryableVideos(src.ID)
	videoTotal, _ := h.Library.CountVideosForSource(src.ID)

	histTotal, err := h.Library.CountSourceHistory(src.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	histPage := ParsePage(r, "page")
	histPageInfo := NewPageInfo(r, "page", histPage, histTotal)
	histItems, err := h.Library.ListSourceHistoryPage(src.ID, PageSize, Offset(histPageInfo.Page))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now().UTC()
	histViews := make([]videoHistoryView, 0, len(histItems))
	for _, e := range histItems {
		abs, ago := createdAgoPair(e.CreatedAt, now)
		v := videoHistoryView{
			CreatedAt: abs, CreatedAgo: ago,
			Event: historyEventLabel(e.Event, e.Detail), Message: historyMessageWithDetail(e.Message, e.Detail), Detail: e.Detail,
			HasError: historyEventError(e.Event),
			Neutral:  historyEventNeutral(e.Event),
		}
		if e.TaskID > 0 {
			v.HasTask = true
			v.TaskID = e.TaskID
			v.HistoryID = e.TaskID
		}
		histViews = append(histViews, v)
	}

	title := DisplayURL(src.URL)
	if src.Label.Valid && strings.TrimSpace(src.Label.String) != "" {
		title = src.Label.String
	}
	summary, lastAt, errMsg, errCode, taskID, hasScanned, hasError := sourceStatusFields(h.Library, src.ID, now)
	cronLabel := cronexpr.DescribeScan(src.ScanCron)
	scanActive, _ := h.Library.HasActiveScanForSource(src.ID)
	selfPath := fmt.Sprintf("/series/%d/sources/%d", seriesID, sourceID)
	render(w, "source_detail", struct {
		pageBase
		Series              *library.Series
		Source              *library.Source
		Title               string
		SelfPath            string
		LastScannedAt       string
		StatusSummary       string
		LastHistoryID       int64
		ErrorMessage        string
		ErrorCode           string
		HasScanned          bool
		HasError            bool
		DomainHost          string
		DomainActive        bool
		DomainDisabledTitle string
		ScanActive          bool
		ScanCronLabel       string
		ScanCronDescriptors []string
		HasRetryable        bool
		VideoCount          int
		History             []videoHistoryView
		HistoryPage         PageInfo
	}{
		pageBase:            newPage(title, "series", flashFromQuery(r)),
		Series:              ser,
		Source:              src,
		Title:               title,
		SelfPath:            selfPath,
		LastScannedAt:       lastAt,
		StatusSummary:       summary,
		LastHistoryID:       taskID,
		ErrorMessage:        errMsg,
		ErrorCode:           errCode,
		HasScanned:          hasScanned,
		HasError:            hasError,
		DomainHost:          host,
		DomainActive:        dAct,
		DomainDisabledTitle: disTitle,
		ScanActive:          scanActive,
		ScanCronLabel:       cronLabel,
		ScanCronDescriptors: scanCronDescriptors(),
		HasRetryable:        retryable,
		VideoCount:          videoTotal,
		History:             histViews,
		HistoryPage:         histPageInfo,
	})
}

// createdAgoPair returns absolute tip text and relative "… ago" display (same as History).
func createdAgoPair(createdAt string, now time.Time) (absolute, ago string) {
	absolute = createdAt
	ago = createdAt
	if t, ok := parseActivityTime(createdAt); ok {
		absolute = formatAbsoluteTip(t)
		ago = formatAgo(t, now)
	}
	return absolute, ago
}

// createdAgoPairShort is like createdAgoPair but uses largest-unit prose ("1 hour ago").
func createdAgoPairShort(createdAt string, now time.Time) (absolute, ago string) {
	absolute = createdAt
	ago = createdAt
	if t, ok := parseActivityTime(createdAt); ok {
		absolute = formatAbsoluteTip(t)
		ago = formatAgoShort(t, now)
	}
	return absolute, ago
}

// sourceStatusSummary is the non-error Status cell: "2 hours ago (1 new)" or "never".
func sourceStatusFields(lib *library.Store, sourceID int64, now time.Time) (summary, lastScannedAt, errMsg, errCode string, taskID int64, hasScanned, hasError bool) {
	st, err := lib.LatestSourceScanStatus(sourceID)
	if err != nil || st.LastScannedAt == "" {
		return "never", "", "", "", 0, false, false
	}
	ago := st.LastScannedAt
	if t, ok := parseActivityTime(st.LastScannedAt); ok {
		ago = formatAgoShort(t, now)
	}
	if st.Event == library.SourceHistScanError {
		return st.LastErrorMessage, st.LastScannedAt, st.LastErrorMessage, st.LastErrorCode, st.TaskID, true, true
	}
	summary = ago
	if st.HasCreatedCount {
		summary = fmt.Sprintf("%s (%d new)", ago, st.CreatedCount)
	}
	return summary, st.LastScannedAt, "", "", st.TaskID, true, false
}

type videoHistoryView struct {
	CreatedAt  string
	CreatedAgo string
	Event      string
	Message    string
	Detail     string
	TaskID     int64
	TaskKind   string // tasks.kind when TaskID set (grouped Event label)
	HasTask    bool
	HasError   bool // from raw event before historyEventLabel remap
	Neutral    bool // cancelled: muted chrome, not success/fail
	HistoryID  int64
	VideoID    int64
	VideoTitle string
	SeriesID   int64
}
