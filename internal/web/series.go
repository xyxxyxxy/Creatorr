package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/cookies"
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func videoTaskRunning(tasks []queue.Task) bool {
	for _, t := range tasks {
		if (t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindMediaVerify) && t.Status == queue.StatusRunning {
			return true
		}
	}
	return false
}

// videoDeliveryQueued is true when a download, sponsorblock_cut, or media_verify task is pending or running.
func videoDeliveryQueued(tasks []queue.Task) bool {
	for _, t := range tasks {
		if t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindMediaVerify {
			return true
		}
	}
	return false
}

func deliveryTaskActive(t *queue.Task) bool {
	return t != nil && (t.Kind == queue.KindDownload || t.Kind == queue.KindSponsorblockCut || t.Kind == queue.KindMediaVerify)
}

func (h *Handler) seriesList(w http.ResponseWriter, r *http.Request) {
	live, err := h.loadSeriesListLive(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	allSourceURLs, _ := h.Library.ListAllSourceURLs()
	render(w, "series_list", struct {
		pageBase
		Live                       seriesListLiveData
		Roots                      []library.RootFolder
		Profiles                   []library.QualityProfile
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		AllSourceURLs              []string
	}{
		pageBase:                   newPage("Series", "series", flashFromQuery(r)),
		Live:                       live,
		Roots:                      roots,
		Profiles:                   profiles,
		ScanCronDescriptors:        scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions: autoIgnoreMediaTypeOptions(h),
		AllSourceURLs:              allSourceURLs,
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
	if h.YtDlp == nil {
		slog.Info("probe source title skipped", "url", url, "err", "yt-dlp missing")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	tmp, err := os.MkdirTemp("", "creatorr-probe-*")
	if err != nil {
		slog.Error("probe source title temp dir", "err", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	defer os.RemoveAll(tmp)
	jar, err := cookies.TempJarForURL(h.Library.DB, tmp, url)
	if err != nil {
		slog.Warn("probe source title cookies", "url", url, "err", err)
		jar = ""
	}
	flare, err := domains.FlareSolverrURL(h.Library.DB, queue.DomainFromURL(url))
	if err != nil {
		slog.Warn("probe source title flaresolverr", "url", url, "err", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	authUser, authPass := "", ""
	if creds, err := settings.CredentialsForURL(h.Library.DB, url); err == nil {
		authUser, authPass = creds.Username, creds.Password
	}
	e, err := h.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
		URL: url, CookiesPath: jar, Username: authUser, Password: authPass,
		FlareSolverrURL: flare,
	})
	if err != nil {
		slog.Warn("probe source title failed", "url", url, "err", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	title := strings.TrimSpace(e.Title)
	if title == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(title))
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
	render(w, "series_detail", struct {
		pageBase
		Series                     *library.Series
		Sources                    []sourceRow
		SourcesPage                PageInfo
		SourceURLs                 []string
		VideosLive                 seriesVideosLiveData
		HasVideos                  bool
		MetaFiles                  []seriesMetaFileView
		CanScan                    bool
		ScanBlocked                string
		SeriesInd                  taskIndicatorView
		HasMonitoredSource         bool
		TaskIndicatorsPath         string
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		Roots                      []library.RootFolder
		Profiles                   []library.QualityProfile
		FolderRenameBusy           bool
		MetaForm                   seriesMetadataView
		Deleting                   bool
	}{
		pageBase:                   newPage(ser.Title, "series", flashFromQuery(r)),
		Series:                     ser,
		Sources:                    pageSrc,
		SourcesPage:                sourcesPage,
		SourceURLs:                 sourceURLs,
		VideosLive:                 videosLive,
		HasVideos:                  videoTotal > 0,
		MetaFiles:                  metaFiles,
		CanScan:                    canScan && !seriesDeleting,
		ScanBlocked:                blocked,
		SeriesInd:                  seriesInd,
		HasMonitoredSource:         ser.Monitored,
		TaskIndicatorsPath:         indicatorsQ,
		ScanCronDescriptors:        scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions: autoIgnoreMediaTypeOptions(h),
		Roots:                      roots,
		Profiles:                   profiles,
		FolderRenameBusy:           folderRenameBusy,
		MetaForm:                   metaForm,
		Deleting:                   seriesDeleting,
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

	render(w, "task_indicators_oob", struct {
		Indicators []taskIndicatorView
		Sources    []sourceLive
	}{Indicators: inds, Sources: srcLive})
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
			Event: historyEventLabel(e.Event, e.Detail), Message: e.Message, Detail: e.Detail,
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
		Series                     *library.Series
		Source                     *library.Source
		Title                      string
		SelfPath                   string
		LastScannedAt              string
		StatusSummary              string
		LastHistoryID              int64
		ErrorMessage               string
		ErrorCode                  string
		HasScanned                 bool
		HasError                   bool
		DomainHost                 string
		DomainActive               bool
		DomainDisabledTitle        string
		ScanActive                 bool
		ScanCronLabel              string
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		HasRetryable               bool
		VideoCount                 int
		History                    []videoHistoryView
		HistoryPage                PageInfo
	}{
		pageBase:                   newPage(title, "series", flashFromQuery(r)),
		Series:                     ser,
		Source:                     src,
		Title:                      title,
		SelfPath:                   selfPath,
		LastScannedAt:              lastAt,
		StatusSummary:              summary,
		LastHistoryID:              taskID,
		ErrorMessage:               errMsg,
		ErrorCode:                  errCode,
		HasScanned:                 hasScanned,
		HasError:                   hasError,
		DomainHost:                 host,
		DomainActive:               dAct,
		DomainDisabledTitle:        disTitle,
		ScanActive:                 scanActive,
		ScanCronLabel:              cronLabel,
		ScanCronDescriptors:        scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions: autoIgnoreMediaTypeOptions(h),
		HasRetryable:               retryable,
		VideoCount:                 videoTotal,
		History:                    histViews,
		HistoryPage:                histPageInfo,
	})
}

func autoIgnoreMediaTypeOptions(h *Handler) []string {
	if h == nil || h.Library == nil {
		return append([]string(nil), library.YouTubeMediaTypeSeed...)
	}
	opts, err := h.Library.ListAutoIgnoreMediaTypeSuggestions()
	if err != nil || len(opts) == 0 {
		return library.MergeMediaTypeSuggestions(nil)
	}
	return opts
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

// sourceStatusSummary is the non-error Status cell: "2 h 3 m ago (1 new)" or "never".
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
	HasTask    bool
	HistoryID  int64
	VideoID    int64
	VideoTitle string
	SeriesID   int64
}

func videoHistoryToView(e library.VideoHistoryEvent, now time.Time) videoHistoryView {
	abs, ago := createdAgoPair(e.CreatedAt, now)
	v := videoHistoryView{
		CreatedAt:  abs,
		CreatedAgo: ago,
		Event:      historyEventLabel(e.Event, e.Detail),
		Message:    e.Message,
		Detail:     e.Detail,
		VideoID:    e.VideoID,
	}
	if e.TaskID.Valid {
		v.HasTask = true
		v.TaskID = e.TaskID.Int64
		v.HistoryID = e.TaskID.Int64
	}
	return v
}

func (h *Handler) videoDetail(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	video, err := h.Library.GetVideo(vid)
	if err != nil || video.SeriesID != sid {
		http.NotFound(w, r)
		return
	}
	histTotal, _ := h.Library.CountVideoTimeline(vid)
	histPage := ParsePage(r, "page")
	histPageInfo := NewPageInfo(r, "page", histPage, histTotal)
	hist, _ := h.Library.ListVideoTimelinePage(vid, PageSize, Offset(histPageInfo.Page))
	now := time.Now().UTC()
	histViews := make([]videoHistoryView, 0, len(hist))
	var errorHistoryID int64
	for _, e := range hist {
		v := videoHistoryToView(e, now)
		histViews = append(histViews, v)
		if errorHistoryID == 0 && historyEventError(v.Event) && v.HistoryID > 0 {
			errorHistoryID = v.HistoryID
		}
	}
	histGroups := groupVideoHistoryByTask(histViews)
	t, _ := h.Queue.ActiveTaskForVideo(vid)
	dlRunning := deliveryTaskActive(t) && t.Status == queue.StatusRunning
	deliveryQueued := deliveryTaskActive(t)
	deleting := taskIsFileDelete(t)
	detailRows := videoDetailRows(h.Library, video)
	sizeLabel := "-"
	if n, ok, _ := h.Library.VideoSizeBytes(vid); ok {
		sizeLabel = library.FormatBytes(n)
	}
	fileRows := videoAllFileViews(h.Library, sid, vid)
	metaForm := h.buildVideoMetadataView(ser, video)
	if tidStr := r.URL.Query().Get("meta_prefetch"); tidStr != "" {
		if tid, err := strconv.ParseInt(tidStr, 10, 64); err == nil && tid > 0 {
			metaForm.PrefetchTaskID = tid
			metaForm.Open = true
			if task, err := h.Queue.GetTask(tid); err == nil && task != nil {
				switch task.Status {
				case queue.StatusPending, queue.StatusRunning:
					metaForm.PrefetchPending = true
					metaForm.FetchURL = queue.URLFromPayload(task.Payload)
				case queue.StatusDone:
					if d, err := h.Library.ReadVideoPrefetchDraft(vid, tid); err == nil {
						metaForm.PrefetchDraft = d
						metaForm.Video = applyVideoPrefetchDraft(video, d, h.Library)
						h.applyVideoMetadataManagedLists(&metaForm, d.Genres)
						metaForm.PrefetchArt = videoPrefetchArtFromDraft(d)
						metaForm.FetchURL = queue.URLFromPayload(task.Payload)
					}
				case queue.StatusFailed, queue.StatusCancelled:
					metaForm.PrefetchDraft = library.VideoPrefetchDraft{Error: task.ErrorMessage}
					if metaForm.PrefetchDraft.Error == "" {
						metaForm.PrefetchDraft.Error = "Prefetch failed"
					}
				}
			}
		}
	}
	dAct, disTitle := true, ""
	if video.SourceID.Valid {
		if src, err := h.Library.GetSourceByID(video.SourceID.Int64); err == nil {
			host := queue.DomainFromURL(src.URL)
			dAct, _ = domains.IsActive(h.Queue.DB, host)
			if !dAct {
				disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
			}
		}
	}
	render(w, "video_detail", struct {
		pageBase
		Series              *library.Series
		Video               *library.Video
		SizeLabel           string
		Files               []videoFileView
		DetailRows          []videoDetailRow
		History             []videoHistoryGroup
		HistoryPage         PageInfo
		ErrorHistoryID      int64
		TaskInd             taskIndicatorView
		DownloadRunning     bool
		DeliveryQueued      bool
		DomainActive        bool
		DomainDisabledTitle string
		Deleting            bool
		HasPackAnchor       bool
		MetaForm            videoMetadataView
	}{
		pageBase:            newPage(video.Title, "series", flashFromQuery(r)),
		Series:              ser,
		Video:               video,
		SizeLabel:           sizeLabel,
		Files:               fileRows,
		DetailRows:          detailRows,
		History:             histGroups,
		HistoryPage:         histPageInfo,
		ErrorHistoryID:      errorHistoryID,
		TaskInd:             h.videoIndicator(vid, t, video.Status),
		DownloadRunning:     dlRunning,
		DeliveryQueued:      deliveryQueued,
		DomainActive:        dAct,
		DomainDisabledTitle: disTitle,
		Deleting:            deleting,
		HasPackAnchor:       metaForm.HasPackAnchor,
		MetaForm:            metaForm,
	})
}

func (h *Handler) actionFetchAddSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sourceURL := strings.TrimSpace(r.FormValue("source_url"))
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if sourceURL == "" {
		writeJSON(http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}
	if sid, ok, err := h.Library.FindSeriesIDBySourceURL(sourceURL); err != nil {
		writeJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	} else if ok {
		name := fmt.Sprintf("#%d", sid)
		if ser, err := h.Library.GetSeries(sid, false); err == nil && ser != nil {
			name = ser.Title
		}
		writeJSON(http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("source URL already used by series %q", name),
		})
		return
	}
	if h.Queue == nil {
		writeJSON(http.StatusServiceUnavailable, map[string]string{"error": "queue is not available"})
		return
	}
	token, err := newAddSeriesDraftToken()
	if err != nil {
		writeJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tid, err := h.Library.EnqueueAddSeriesPrefetch(sourceURL, token)
	if err != nil {
		writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(http.StatusOK, map[string]any{"task_id": tid, "draft_token": token})
}

func newAddSeriesDraftToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) addSeriesPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Kind != queue.KindPrefetchAddSeries {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	token := queue.DraftTokenFromPayload(task.Payload)
	out := map[string]any{
		"status":      task.Status,
		"task_id":     tid,
		"draft_token": token,
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusFailed, queue.StatusCancelled:
		msg := task.ErrorMessage
		if msg == "" {
			msg = "Prefetch failed"
		}
		if token != "" {
			if d, err := h.Library.ReadAddSeriesDraft(token); err == nil && strings.TrimSpace(d.Error) != "" {
				msg = d.Error
			}
		}
		out["error"] = msg
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusDone:
		if token == "" {
			out["error"] = "draft token missing"
			writeJSON(http.StatusOK, out)
			return
		}
		draft, err := h.Library.ReadAddSeriesDraft(token)
		if err != nil {
			out["error"] = "draft not found"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Error) != "" {
			out["error"] = draft.Error
			writeJSON(http.StatusOK, out)
			return
		}
		title := library.SeriesTitleFromDraft(draft)
		if title == "" {
			out["error"] = "could not determine series title from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		out["title"] = title
		writeJSON(http.StatusOK, out)
		return
	default:
		out["error"] = "unexpected task status"
		writeJSON(http.StatusOK, out)
	}
}

func (h *Handler) actionFetchAddVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sourceURL := strings.TrimSpace(r.FormValue("url"))
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(r.FormValue("source_url"))
	}
	seriesID, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if sourceURL == "" {
		writeJSON(http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}
	if h.Queue == nil {
		writeJSON(http.StatusServiceUnavailable, map[string]string{"error": "queue is not available"})
		return
	}
	token, err := newAddSeriesDraftToken()
	if err != nil {
		writeJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tid, err := h.Library.EnqueueAddVideoPrefetch(sourceURL, token, seriesID)
	if err != nil {
		writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(http.StatusOK, map[string]any{"task_id": tid, "draft_token": token})
}

func (h *Handler) addVideoPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Kind != queue.KindPrefetchAddVideo {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	token := queue.DraftTokenFromPayload(task.Payload)
	out := map[string]any{
		"status":      task.Status,
		"task_id":     tid,
		"draft_token": token,
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusFailed, queue.StatusCancelled:
		msg := task.ErrorMessage
		if msg == "" {
			msg = "Prefetch failed"
		}
		if token != "" {
			if d, err := h.Library.ReadAddVideoDraft(token); err == nil && strings.TrimSpace(d.Error) != "" {
				msg = d.Error
			}
		}
		out["error"] = msg
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusDone:
		if token == "" {
			out["error"] = "draft token missing"
			writeJSON(http.StatusOK, out)
			return
		}
		draft, err := h.Library.ReadAddVideoDraft(token)
		if err != nil {
			out["error"] = "draft not found"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Error) != "" {
			out["error"] = draft.Error
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Title) == "" {
			out["error"] = "could not determine video title from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.UploadDate) == "" {
			out["error"] = "could not determine upload date from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		out["title"] = draft.Title
		out["remote_id"] = draft.RemoteID
		out["upload_date"] = draft.UploadDate
		out["source_url"] = draft.SourceURL
		writeJSON(http.StatusOK, out)
		return
	default:
		out["error"] = "unexpected task status"
		writeJSON(http.StatusOK, out)
	}
}

func (h *Handler) actionAddSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	rootID, _ := strconv.ParseInt(r.PostFormValue("root_id"), 10, 64)
	qpID, _ := strconv.ParseInt(r.PostFormValue("quality_profile_id"), 10, 64)
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	title := strings.TrimSpace(r.PostFormValue("title"))
	delivery := r.PostFormValue("delivery_mode")
	draftToken := strings.TrimSpace(r.PostFormValue("draft_token"))
	// Monitored defaults on at create; toggle only from series list.
	wantJSON := strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.FormValue("response") == "json" ||
		r.URL.Query().Get("response") == "json"

	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	redirErr := func(msg string) {
		if wantJSON {
			writeJSON(http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		http.Redirect(w, r, "/?err="+urlQuery(msg)+"&add=1", http.StatusSeeOther)
	}
	doneOK := func(ser *library.Series, warn string) {
		if wantJSON {
			out := map[string]any{"id": ser.ID, "title": ser.Title}
			if warn != "" {
				out["warning"] = warn
			}
			writeJSON(http.StatusCreated, out)
			return
		}
		if warn != "" {
			http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", ser.ID, urlQuery(warn)), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/series/%d", ser.ID), http.StatusSeeOther)
	}

	if sourceURL != "" {
		var draft library.PrefetchDraft
		if draftToken != "" {
			if d, err := h.Library.ReadAddSeriesDraft(draftToken); err == nil {
				draft = d
			}
		}
		if title == "" {
			title = library.SeriesTitleFromDraft(draft)
		}
		if title == "" {
			redirErr("title is required - fetch metadata again or enter a title")
			return
		}

		sched, err := parseFeedScanCron(r, "@weekly")
		if err != nil {
			redirErr(err.Error())
			return
		}
		scanCron := sched

		ser, err := h.Library.CreateSeries(library.CreateSeriesParams{
			Title:                title,
			SourceURL:            sourceURL,
			RootID:               rootID,
			QualityProfileID:     qpID,
			Monitored:            true,
			DeliveryMode:         delivery,
			ScanCutoff:           clampPastDate(strings.TrimSpace(r.FormValue("scan_cutoff"))),
			ScanCron:             scanCron,
			IndexAsIgnored:       r.FormValue("index_as_ignored") == "1",
			TitleRegexpInclude:   strings.TrimSpace(r.FormValue("title_regexp_include")),
			TitleRegexpExclude:   strings.TrimSpace(r.FormValue("title_regexp_exclude")),
			AutoIgnoreMediaTypes: library.NormalizeAutoIgnoreMediaTypes(r.Form["auto_ignore_media_types"]),
			SourceLabel:          strings.TrimSpace(r.FormValue("source_label")),
		})
		if err != nil {
			redirErr(err.Error())
			return
		}
		warn := ""
		if draft.Plot != "" || draft.Studio != "" || draft.OriginalTitle != "" || len(draft.ArtFiles) > 0 ||
			draft.UniqueIDValue != "" || len(draft.Actors) > 0 {
			if err := h.Library.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{
				Plot:          draft.Plot,
				SortTitle:     draft.SortTitle,
				OriginalTitle: draft.OriginalTitle,
				Studio:        draft.Studio,
				UniqueIDType:  draft.UniqueIDType,
				UniqueIDValue: draft.UniqueIDValue,
				Actors:        draft.Actors,
				ArtSrc:        draft.ArtFiles,
			}); err != nil {
				slog.Warn("add series metadata save", "series_id", ser.ID, "err", err)
				warn = "series created but metadata save failed: " + err.Error()
			}
		}
		if draftToken != "" {
			_ = h.Library.ClearAddSeriesDraft(draftToken)
		}
		doneOK(ser, warn)
		return
	}

	if title == "" {
		redirErr("title is required when creating manually")
		return
	}
	ser, err := h.Library.CreateSeries(library.CreateSeriesParams{
		Title:                title,
		RootID:               rootID,
		QualityProfileID:     qpID,
		Monitored:            true,
		DeliveryMode:         delivery,
		AutoIgnoreMediaTypes: library.NormalizeAutoIgnoreMediaTypes(r.Form["auto_ignore_media_types"]),
	})
	if err != nil {
		redirErr(err.Error())
		return
	}
	doneOK(ser, "")
}

func (h *Handler) actionUpdateSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	if _, err := h.Library.GetSeries(sid, false); err != nil {
		http.NotFound(w, r)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	rootID, _ := strconv.ParseInt(r.FormValue("root_id"), 10, 64)
	qpID, _ := strconv.ParseInt(r.FormValue("quality_profile_id"), 10, 64)
	dm := library.NormalizeDeliveryMode(r.FormValue("delivery_mode"))
	ex := library.NormalizeAutoIgnoreMediaTypes(r.Form["auto_ignore_media_types"])
	_, err := h.Library.UpdateSeries(sid, library.UpdateSeriesParams{
		Title:                &title,
		RootID:               &rootID,
		QualityProfileID:     &qpID,
		DeliveryMode:         &dm,
		AutoIgnoreMediaTypes: &ex,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=updated", sid), http.StatusSeeOther)
}

func (h *Handler) actionAddSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	kind := r.FormValue("kind")
	scanCron := ""
	if kind != library.SourceKindSingle {
		c, err := parseFeedScanCron(r, "@weekly")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
			return
		}
		scanCron = c
	}
	titleInclude := ""
	titleExclude := ""
	indexAsIgnored := false
	if kind != library.SourceKindSingle {
		titleInclude = strings.TrimSpace(r.FormValue("title_regexp_include"))
		titleExclude = strings.TrimSpace(r.FormValue("title_regexp_exclude"))
		indexAsIgnored = r.FormValue("index_as_ignored") == "1"
	}
	_, err := h.Library.AddSource(sid, library.AddSourceParams{
		URL:                strings.TrimSpace(r.FormValue("url")),
		Label:              strings.TrimSpace(r.FormValue("label")),
		Kind:               kind,
		ScanCron:           scanCron,
		IndexAsIgnored:     indexAsIgnored,
		TitleRegexpInclude: titleInclude,
		TitleRegexpExclude: titleExclude,
		ScanCutoff:         clampPastDate(strings.TrimSpace(r.FormValue("scan_cutoff"))),
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=source", sid), http.StatusSeeOther)
}

func (h *Handler) actionUpdateSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	label := strings.TrimSpace(r.FormValue("label"))
	cutoff := clampPastDate(strings.TrimSpace(r.FormValue("scan_cutoff")))
	redir := seriesSourceRedirect(r, sid, srcID)
	cur, err := h.Library.GetSource(sid, srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	p := library.UpdateSourceParams{
		Label:       &label,
		ScanCutoff:  &cutoff,
		ClearCutoff: cutoff == "",
	}
	if !cur.IsSingle() {
		if _, ok := r.Form["scan_cron"]; ok {
			cron, err := cronexpr.NormalizeScanCron(r.FormValue("scan_cron"))
			if err != nil {
				http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			p.ScanCron = &cron
		} else if _, ok := r.Form["scan_cron_schedule"]; ok {
			cron, err := cronexpr.NormalizeScanCron(r.FormValue("scan_cron_schedule"))
			if err != nil {
				http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			p.ScanCron = &cron
		}
	}
	idx := r.FormValue("index_as_ignored") == "1"
	if !cur.IsSingle() {
		p.IndexAsIgnored = &idx
		titleInclude := strings.TrimSpace(r.FormValue("title_regexp_include"))
		titleExclude := strings.TrimSpace(r.FormValue("title_regexp_exclude"))
		p.TitleRegexpInclude = &titleInclude
		p.TitleRegexpExclude = &titleExclude
	} else {
		off := false
		p.IndexAsIgnored = &off
	}
	_, err = h.Library.UpdateSource(sid, srcID, p)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=source-updated"), http.StatusSeeOther)
}

func (h *Handler) actionDeleteSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	if r.FormValue("confirm_delete") != "1" {
		http.Redirect(w, r, appendQuery(seriesSourceRedirect(r, sid, srcID), "err="+urlQuery("confirm delete to remove this source")), http.StatusSeeOther)
		return
	}
	if err := h.Library.DeleteSource(sid, srcID); err != nil {
		http.Redirect(w, r, appendQuery(seriesSourceRedirect(r, sid, srcID), "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=source-deleted", sid), http.StatusSeeOther)
}

func (h *Handler) actionDeleteSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if r.FormValue("delete_files") != "1" {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery("confirm delete to remove this series and its library files")), http.StatusSeeOther)
		return
	}
	if err := h.Library.DeleteSeries(sid, true); err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/series?ok=delete-queued", http.StatusSeeOther)
}

func (h *Handler) actionScanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	n, _, err := h.Library.EnqueueScansForSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=scan-for-new-%d", sid, n), http.StatusSeeOther)
}

func (h *Handler) actionScanSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	src, err := h.Library.GetSourceByID(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	okFlash := "scan-for-new"
	if !src.FullScanDone {
		okFlash = "history-scan"
	}
	_, err = h.Library.EnqueueScanSource(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok="+okFlash), http.StatusSeeOther)
}

func (h *Handler) actionFullRescanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	n, _, err := h.Library.FullRescanSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=restart-history-%d", sid, n), http.StatusSeeOther)
}

func (h *Handler) actionFullRescanSource(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	_, err := h.Library.FullRescanSource(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=restart-history"), http.StatusSeeOther)
}

func (h *Handler) actionMetadataRescanSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	_, err := h.Library.EnqueueMetadataRescanSeries(sid)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=metadata-rescan", sid), http.StatusSeeOther)
}

func (h *Handler) actionMetadataRescanVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfVideoDeleting(vid); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
		}
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	_, err := h.Library.EnqueueMetadataRescanVideo(vid)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	}
	if err != nil {
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	sep := "?"
	if strings.Contains(redir, "?") {
		sep = "&"
	}
	http.Redirect(w, r, redir+sep+"ok=metadata-rescan", http.StatusSeeOther)
}

func (h *Handler) actionSetSourceMonitored(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "source monitored flag removed; use domain active and series monitored", http.StatusGone)
}

func (h *Handler) actionSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if err := h.errIfSeriesDeleting(sid); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = "/series"
		}
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		errURL := redir + sep + "err=" + urlQuery(err.Error())
		if hxRequest(r) {
			hxRedirect(w, errURL)
			return
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}
	monitored := r.FormValue("monitored") == "1"
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/series"
	}
	if err := h.Library.SetSeriesMonitored(sid, monitored); err != nil {
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		errURL := redir + sep + "err=" + urlQuery(err.Error())
		if hxRequest(r) {
			hxRedirect(w, errURL)
			return
		}
		http.Redirect(w, r, errURL, http.StatusSeeOther)
		return
	}
	if hxRequest(r) {
		if h.tryRenderSeriesListLive(w, r) {
			return
		}
		// Detail page scan buttons depend on series monitored - full refresh.
		if strings.HasPrefix(redir, "/series/") {
			hxRedirect(w, redir)
			return
		}
		asButton := false
		if path, _, _ := strings.Cut(redir, "?"); path == "/series" {
			asButton = true
		}
		renderMonitorToggle(w, map[string]any{
			"Action":    "/actions/set-series-monitored",
			"SeriesID":  sid,
			"Monitored": monitored,
			"Redirect":  redir,
			"Title":     "Include this series in scans and download-wanted",
			"AsButton":  asButton,
		})
		return
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionWantVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.WantVideo(vid)
	h.finishVideoAction(w, r, sid, redir, err)
}

func (h *Handler) actionDownloadVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.EnqueueDownloadNow(vid)
	if err == nil {
		redir = appendQuery(redir, "ok=download")
	}
	h.finishVideoAction(w, r, sid, redir, err)
}

func (h *Handler) actionRetrySourceErrors(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	srcID, _ := strconv.ParseInt(r.FormValue("source_id"), 10, 64)
	redir := seriesSourceRedirect(r, sid, srcID)
	n, err := h.Library.RetrySourceErrors(srcID)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=retry&n="+strconv.FormatInt(int64(n), 10)), http.StatusSeeOther)
}

func (h *Handler) actionIgnoreVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	_, err := h.Library.IgnoreVideo(vid)
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, redir, nil)
}

func (h *Handler) actionDeleteVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	_, err := h.Library.DeleteVideo(vid)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d", sid)
	}
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, appendQuery(redir, "ok=delete-queued"), nil)
}

func (h *Handler) actionDeleteVideoSidecar(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	fid, _ := strconv.ParseInt(r.FormValue("file_id"), 10, 64)
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	}
	err := h.Library.DeleteVideoSidecar(vid, fid)
	if err != nil {
		h.finishVideoAction(w, r, sid, redir, err)
		return
	}
	h.finishVideoAction(w, r, sid, appendQuery(redir, "ok=sidecar-deleted"), nil)
}
