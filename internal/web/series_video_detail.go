package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

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
		if errorHistoryID == 0 && v.HasError && v.HistoryID > 0 {
			errorHistoryID = v.HistoryID
		}
	}
	fillVideoHistoryTaskKinds(h.Queue, histViews)
	enrichVideoHistoryRenameMessages(h.Library, h.Queue, histViews)
	histTimeline := videoHistoryGroupsToTimeline(groupVideoHistoryByTask(histViews))
	t, _ := h.Queue.ActiveTaskForVideo(vid)
	statusTask, _ := h.Queue.ActiveNonIntegrityTaskForVideo(vid)
	integrityTask, _ := h.Queue.ActiveIntegrityTaskForVideo(vid)
	dlRunning := deliveryTaskActive(t) && t.Status == queue.StatusRunning
	deliveryQueued := deliveryTaskActive(t)
	deleting := taskIsFileDelete(t)
	statusTaskID, statusTaskKind := int64(0), ""
	if statusTask != nil {
		statusTaskID = statusTask.ID
		statusTaskKind = statusTask.Kind
	}
	integrityTaskID, integrityTaskKind := int64(0), ""
	if integrityTask != nil {
		integrityTaskID = integrityTask.ID
		integrityTaskKind = integrityTask.Kind
	}
	detailRows := videoDetailRows(h.Library, video)
	mediaResolution := ""
	if video.Width.Valid && video.Height.Valid && video.Width.Int64 > 0 && video.Height.Int64 > 0 {
		mediaResolution = fmt.Sprintf("%dx%d", video.Width.Int64, video.Height.Int64)
	}
	mediaFPS := ""
	if video.FPS.Valid && video.FPS.Float64 > 0 {
		mediaFPS = fmt.Sprintf("%g", video.FPS.Float64)
	}
	mediaRemux := ""
	if video.DownloadRemuxContainer.Valid {
		mediaRemux = strings.TrimSpace(video.DownloadRemuxContainer.String)
	}
	mediaDuration := ""
	if video.DurationSeconds.Valid && video.DurationSeconds.Int64 > 0 {
		mediaDuration = formatDetailDuration(float64(video.DurationSeconds.Int64))
	}
	fileRows := videoAllFileViews(h.Library, sid, vid)
	mediaRaw, mediaAudio, hasMediaPlay := videoMediaPlay(fileRows, sid, vid, ser.IsAudio())
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
	thumbURL := ""
	if _, ok, _ := h.Library.VideoThumbPath(vid); ok {
		thumbURL = fmt.Sprintf("/series/%d/videos/%d/thumb", sid, vid)
	}
	resolvedSourceURL := ""
	if video.SourceURL.Valid {
		resolvedSourceURL = library.DownloadURL(video.SourceURL.String, video.RemoteID)
	}
	hasSourceURL := video.SourceURL.Valid && strings.TrimSpace(video.SourceURL.String) != ""
	integrityInd := h.integrityIndicatorForVideo(ser, video, now)
	render(w, "video_detail", struct {
		pageBase
		Series              *library.Series
		Video               *library.Video
		ResolvedSourceURL   string
		HasSourceURL        bool
		ThumbURL            string
		MediaRawHref        string
		MediaIsAudio        bool
		HasMediaPlay        bool
		MediaResolution     string
		MediaFPS            string
		MediaRemux          string
		MediaDuration       string
		Files               []videoFileView
		DetailRows          []videoDetailRow
		History             []taskStageView
		HistoryPage         PageInfo
		ErrorHistoryID      int64
		TaskInd             taskIndicatorView
		IntegrityInd        integrityIndicatorView
		StatusTaskID        int64
		StatusTaskKind      string
		IntegrityTaskID     int64
		IntegrityTaskKind   string
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
		ResolvedSourceURL:   resolvedSourceURL,
		HasSourceURL:        hasSourceURL,
		ThumbURL:            thumbURL,
		MediaRawHref:        mediaRaw,
		MediaIsAudio:        mediaAudio,
		HasMediaPlay:        hasMediaPlay,
		MediaResolution:     mediaResolution,
		MediaFPS:            mediaFPS,
		MediaRemux:          mediaRemux,
		MediaDuration:       mediaDuration,
		Files:               fileRows,
		DetailRows:          detailRows,
		History:             histTimeline,
		HistoryPage:         histPageInfo,
		ErrorHistoryID:      errorHistoryID,
		TaskInd:             h.videoIndicator(vid, t, video.Status),
		IntegrityInd:        integrityInd,
		StatusTaskID:        statusTaskID,
		StatusTaskKind:      statusTaskKind,
		IntegrityTaskID:     integrityTaskID,
		IntegrityTaskKind:   integrityTaskKind,
		DownloadRunning:     dlRunning,
		DeliveryQueued:      deliveryQueued,
		DomainActive:        dAct,
		DomainDisabledTitle: disTitle,
		Deleting:            deleting,
		HasPackAnchor:       metaForm.HasPackAnchor,
		MetaForm:            metaForm,
	})
}

// integrityIndicatorForVideo loads profile / hash / cron / last-check for the Status chip.
func (h *Handler) integrityIndicatorForVideo(ser *library.Series, video *library.Video, now time.Time) integrityIndicatorView {
	verifyMedia := false
	if ser != nil {
		if p, err := h.Library.GetProfile(ser.QualityProfileID); err == nil && p != nil {
			verifyMedia = p.VerifyMedia
		}
	}
	hasHash, _ := h.Library.VideoMediaHasContentHash(video.ID)
	cron := ""
	if h.Queue != nil && h.Queue.DB != nil {
		cron, _ = settings.Get(h.Queue.DB, settings.KeyIntegrityCheckCron)
	}
	lastAt := ""
	if t, ok, err := h.Library.LastIntegrityCheckAt(video.ID); err == nil && ok {
		lastAt = t.UTC().Format(time.RFC3339)
	}
	state := integrityIndicatorState(video.Status, verifyMedia, hasHash, integrityScheduleOn(cron))
	return buildIntegrityIndicatorView(state, video.Status, verifyMedia, lastAt, now)
}
