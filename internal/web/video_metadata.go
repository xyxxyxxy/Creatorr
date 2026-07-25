package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/go-chi/chi/v5"
)

type videoMetadataView struct {
	Series            *library.Series
	Video             *library.Video
	Suggestions       library.MetaSuggestions
	PrefetchTaskID    int64
	PrefetchPending   bool
	PrefetchDraft     library.VideoPrefetchDraft
	FetchURL          string
	HasPackAnchor     bool
	Open              bool
	SeasonEpisodeHint string
	AiredHint         string
}

func (h *Handler) buildVideoMetadataView(ser *library.Series, video *library.Video) videoMetadataView {
	v := videoMetadataView{
		Series: ser,
		Video:  video,
	}
	if h.Library != nil {
		v.Suggestions, _ = h.Library.ListMetaSuggestions()
		if _, ok, _ := h.Library.HasPackAnchor(video.ID); ok {
			v.HasPackAnchor = true
		}
	}
	if video.SourceURL.Valid {
		v.FetchURL = video.SourceURL.String
	}
	if video.Season.Valid && video.Episode.Valid {
		v.SeasonEpisodeHint = fmt.Sprintf("S%dE%d", video.Season.Int64, video.Episode.Int64)
	}
	if video.UploadDate.Valid {
		day := library.UploadCalendarDate(video.UploadDate.String)
		if day == "" {
			day = video.UploadDate.String
		}
		v.AiredHint = day
	}
	return v
}

func applyVideoPrefetchDraft(video *library.Video, d library.VideoPrefetchDraft) *library.Video {
	if video == nil {
		return nil
	}
	out := *video
	if t := strings.TrimSpace(d.Title); t != "" {
		out.Title = t
	}
	if d.Plot != "" {
		out.Description = d.Plot
	}
	if d.SortTitle != "" {
		out.SortTitle = d.SortTitle
	}
	if d.OriginalTitle != "" {
		out.OriginalTitle = d.OriginalTitle
	}
	if d.Studio != "" {
		out.Studio = d.Studio
	}
	if d.UniqueIDType != "" {
		out.UniqueIDType = d.UniqueIDType
	}
	if d.UniqueIDValue != "" {
		out.UniqueIDValue = d.UniqueIDValue
	}
	if len(d.Actors) > 0 {
		out.Actors = d.Actors
	}
	if d.Tagline != "" {
		out.Tagline = d.Tagline
	}
	if d.Country != "" {
		out.Country = d.Country
	}
	if d.MPAA != "" {
		out.MPAA = d.MPAA
	}
	if len(d.Genres) > 0 {
		out.Genres = d.Genres
	}
	if len(d.Tags) > 0 {
		out.Tags = d.Tags
	}
	return &out
}

func (h *Handler) actionSaveVideoMetadata(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	video, err := h.Library.GetVideo(vid)
	if err != nil || video.SeriesID != sid {
		http.NotFound(w, r)
		return
	}
	draftTID, _ := strconv.ParseInt(r.FormValue("prefetch_task_id"), 10, 64)
	uidType, uidVal := "", ""
	if draftTID > 0 {
		if d, err := h.Library.ReadVideoPrefetchDraft(vid, draftTID); err == nil {
			uidType = d.UniqueIDType
			uidVal = d.UniqueIDValue
		}
	}
	err = h.Library.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{
		Title:         r.FormValue("title"),
		Plot:          r.FormValue("plot"),
		SortTitle:     r.FormValue("sorttitle"),
		OriginalTitle: r.FormValue("originaltitle"),
		Studio:        r.FormValue("studio"),
		Genres:        library.ParseStringListFields(r.Form["genre"]),
		Tags:          library.ParseStringListFields(r.Form["tag"]),
		UniqueIDType:  uidType,
		UniqueIDValue: uidVal,
		Actors:        library.ParseActorsFromFields(r.Form["actor_name"], r.Form["actor_role"]),
		Tagline:       r.FormValue("tagline"),
		Country:       r.FormValue("country"),
		MPAA:          r.FormValue("mpaa"),
	})
	redir := fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	if err != nil {
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redir+"?ok=video-metadata", http.StatusSeeOther)
}

func (h *Handler) actionVideoMetadataPrefetch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	fetchURL := strings.TrimSpace(r.FormValue("fetch_url"))
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
	view := h.buildVideoMetadataView(ser, video)
	view.FetchURL = fetchURL
	view.Open = true

	tid, err := h.Library.EnqueueVideoMetaPrefetch(vid, fetchURL)
	if err != nil {
		if r.Header.Get("HX-Request") == "true" {
			view.PrefetchDraft = library.VideoPrefetchDraft{Error: err.Error()}
			render(w, "video_metadata_body", view)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/series/%d/videos/%d?err=%s", sid, vid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	view.PrefetchTaskID = tid
	view.PrefetchPending = true
	if r.Header.Get("HX-Request") == "true" {
		render(w, "video_metadata_body", view)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d/videos/%d?meta_prefetch=%d", sid, vid, tid), http.StatusSeeOther)
}

func (h *Handler) videoMetadataPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
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
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		http.NotFound(w, r)
		return
	}
	if task.VideoID.Valid && task.VideoID.Int64 != vid {
		http.NotFound(w, r)
		return
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		w.WriteHeader(http.StatusNoContent)
		return
	}

	view := h.buildVideoMetadataView(ser, video)
	view.PrefetchTaskID = tid
	view.Open = true
	view.FetchURL = queue.URLFromPayload(task.Payload)

	draft := library.VideoPrefetchDraft{}
	switch task.Status {
	case queue.StatusFailed, queue.StatusCancelled:
		draft.Error = task.ErrorMessage
		if draft.Error == "" {
			draft.Error = "Prefetch failed"
		}
		if d, err := h.Library.ReadVideoPrefetchDraft(vid, tid); err == nil && d.Error != "" {
			draft = d
		}
	case queue.StatusDone:
		if d, err := h.Library.ReadVideoPrefetchDraft(vid, tid); err == nil {
			draft = d
			view.Video = applyVideoPrefetchDraft(video, draft)
		}
	}
	view.PrefetchDraft = draft
	render(w, "video_metadata_body", view)
}

func (h *Handler) actionRefreshSidecarsVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = fmt.Sprintf("/series/%d/videos/%d", sid, vid)
	}
	if err := h.errIfVideoDeleting(vid); err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	_, err := h.Library.EnqueueRefreshSidecarsVideo(vid)
	if err != nil {
		http.Redirect(w, r, appendQuery(redir, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendQuery(redir, "ok=refresh-sidecars"), http.StatusSeeOther)
}
