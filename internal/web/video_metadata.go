package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

type videoMetadataView struct {
	Series             *library.Series
	Video              *library.Video
	Suggestions        library.MetaSuggestions
	PrefetchTaskID     int64
	PrefetchPending    bool
	PrefetchDraft      library.VideoPrefetchDraft
	PrefetchArt        map[string]string
	FetchURL           string
	HasPackAnchor      bool
	HasThumb           bool
	ThumbMtime         int64
	Open               bool
	SeasonEpisodeHint  string
	UploadDateDay      string // YYYY-MM-DD for type=date
	UploadDateTime     string // HH:MM for type=time; empty = optional unset
	ManagedTagItems    []string
	ManagedGenreItems  []string
	ManagedTagPipe     string
	ManagedGenrePipe   string
	OperatorTagItems   []string
	OperatorGenreItems []string
}

func (h *Handler) buildVideoMetadataView(ser *library.Series, video *library.Video) videoMetadataView {
	v := videoMetadataView{
		Series:      ser,
		Video:       video,
		PrefetchArt: map[string]string{},
	}
	if h.Library != nil {
		v.Suggestions, _ = h.Library.ListMetaSuggestions()
		if _, ok, _ := h.Library.HasPackAnchor(video.ID); ok {
			v.HasPackAnchor = true
		}
		if path, ok, _ := h.Library.VideoThumbPath(video.ID); ok && path != "" {
			v.HasThumb = true
			v.ThumbMtime = h.Library.VideoThumbMtime(video.ID)
		}
		h.applyVideoMetadataManagedLists(&v, nil)
	}
	if video.SourceURL.Valid {
		v.FetchURL = video.SourceURL.String
	}
	if video.Season.Valid && video.Episode.Valid {
		v.SeasonEpisodeHint = fmt.Sprintf("S%dE%d", video.Season.Int64, video.Episode.Int64)
	}
	if video.UploadDate.Valid {
		v.UploadDateDay, v.UploadDateTime = library.UploadFormParts(video.UploadDate.String)
	}
	return v
}

// applyVideoMetadataManagedLists sets managed/operator tag and genre rows for the metadata form.
// draftGenres supplies yt-dlp categories from an ephemeral prefetch draft when present.
func (h *Handler) applyVideoMetadataManagedLists(view *videoMetadataView, draftGenres []string) {
	if h.Library == nil || view == nil || view.Video == nil {
		return
	}
	_, _ = settings.MetadataDomainTagEnabled(h.Library.DB)
	_, _ = settings.MetadataGenresFromCategoriesEnabled(h.Library.DB)
	_ = draftGenres
	// Metadata editor is operator-editable: show full current lists as removable items.
	// Auto-fill remains in download/pack/prefetch flows, but modal rows are not locked.
	view.ManagedTagItems = nil
	view.ManagedTagPipe = ""
	view.ManagedGenreItems = nil
	view.ManagedGenrePipe = ""
	view.OperatorTagItems = library.ParseStringListFields(view.Video.Tags)
	view.OperatorGenreItems = library.ParseStringListFields(view.Video.Genres)
}

func applyVideoPrefetchDraft(video *library.Video, d library.VideoPrefetchDraft, lib *library.Store) *library.Video {
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
	if lib != nil {
		sourceURL := ""
		if out.SourceURL.Valid {
			sourceURL = out.SourceURL.String
		}
		domainOn, _ := settings.MetadataDomainTagEnabled(lib.DB)
		if domainOn {
			out.Tags = library.MergeDomainTag(out.Tags, sourceURL)
		}
		genresOn, _ := settings.MetadataGenresFromCategoriesEnabled(lib.DB)
		if genresOn && len(d.Genres) > 0 {
			out.Genres = library.MergeCategoryGenres(out.Genres, d.Genres)
		}
	}
	return &out
}

func videoPrefetchArtFromDraft(d library.VideoPrefetchDraft) map[string]string {
	out := map[string]string{}
	if d.ArtFiles == nil {
		return out
	}
	for role, path := range d.ArtFiles {
		path = strings.TrimSpace(path)
		if path == "" || !fileExistsWeb(path) {
			continue
		}
		out[role] = path
	}
	return out
}

func (h *Handler) actionSaveVideoMetadata(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(ct), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
			sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
			if sid > 0 && vid > 0 {
				http.Redirect(w, r, fmt.Sprintf("/series/%d/videos/%d?err=%s", sid, vid, urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		_ = r.ParseForm()
	}
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	video, err := h.Library.GetVideo(vid)
	if err != nil || video.SeriesID != sid {
		http.NotFound(w, r)
		return
	}
	redir := fmt.Sprintf("/series/%d/videos/%d", sid, vid)

	tmpDir, err := os.MkdirTemp("", "creatorr-video-meta-upload-*")
	if err != nil {
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	thumbSrc := ""
	thumbClear := false
	if f, hdr, ferr := r.FormFile(library.ArtThumb); ferr == nil && hdr != nil && strings.TrimSpace(hdr.Filename) != "" {
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		if ext == "" {
			ext = ".jpg"
		}
		dest := filepath.Join(tmpDir, library.ArtThumb+ext)
		out, createErr := os.Create(dest)
		if createErr != nil {
			_ = f.Close()
			http.Redirect(w, r, redir+"?err="+urlQuery(createErr.Error()), http.StatusSeeOther)
			return
		}
		_, copyErr := io.Copy(out, f)
		_ = out.Close()
		_ = f.Close()
		if copyErr != nil {
			http.Redirect(w, r, redir+"?err="+urlQuery(copyErr.Error()), http.StatusSeeOther)
			return
		}
		thumbSrc = dest
	} else if r.FormValue("clear_"+library.ArtThumb) == "1" {
		thumbClear = true
	} else if pref := strings.TrimSpace(r.FormValue("prefetch_" + library.ArtThumb)); pref != "" && fileExistsWeb(pref) {
		thumbSrc = pref
	}

	draftTID, _ := strconv.ParseInt(r.FormValue("prefetch_task_id"), 10, 64)
	uidType, uidVal := "", ""
	if draftTID > 0 {
		if d, err := h.Library.ReadVideoPrefetchDraft(vid, draftTID); err == nil {
			uidType = d.UniqueIDType
			uidVal = d.UniqueIDValue
		}
	}
	outcome, err := h.Library.SaveVideoMetadata(vid, library.SaveVideoMetadataParams{
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
		UploadDate:    library.CombineUploadFormDateTime(r.FormValue("upload_date"), r.FormValue("upload_time")),
		ThumbSrc:      thumbSrc,
		ThumbClear:    thumbClear,
	})
	if err != nil {
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	ok := "video-metadata"
	if outcome.RenameSkippedBusy {
		ok = "video-metadata-busy"
	}
	http.Redirect(w, r, redir+"?ok="+ok, http.StatusSeeOther)
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
			view.Video = applyVideoPrefetchDraft(video, draft, h.Library)
			h.applyVideoMetadataManagedLists(&view, draft.Genres)
			view.PrefetchArt = videoPrefetchArtFromDraft(draft)
		}
	}
	view.PrefetchDraft = draft
	render(w, "video_metadata_body", view)
}

// videoMetadataBody returns the Metadata modal body from saved video state (no draft).
// Optional ?discard=<task_id> cancels a pending prefetch and clears its cache draft.
func (h *Handler) videoMetadataBody(w http.ResponseWriter, r *http.Request) {
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
	if discardStr := strings.TrimSpace(r.URL.Query().Get("discard")); discardStr != "" {
		if tid, err := strconv.ParseInt(discardStr, 10, 64); err == nil && tid > 0 {
			h.discardVideoMetaPrefetch(vid, tid)
		}
	}
	render(w, "video_metadata_body", h.buildVideoMetadataView(ser, video))
}

func (h *Handler) discardVideoMetaPrefetch(videoID, taskID int64) {
	if h.Queue != nil {
		if task, err := h.Queue.GetTask(taskID); err == nil && task != nil {
			if task.VideoID.Valid && task.VideoID.Int64 == videoID {
				switch task.Status {
				case queue.StatusPending, queue.StatusRunning:
					_, _ = h.Queue.CancelWithMessage(taskID, "Metadata fetch discarded")
				}
			}
		}
	}
	if h.Library != nil {
		_ = h.Library.ClearVideoPrefetchDraft(videoID, taskID)
	}
}

func (h *Handler) videoPrefetchArtFile(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	role := chi.URLParam(r, "role")
	if role != library.ArtThumb {
		http.NotFound(w, r)
		return
	}
	video, err := h.Library.GetVideo(vid)
	if err != nil || video.SeriesID != sid {
		http.NotFound(w, r)
		return
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil || (task.VideoID.Valid && task.VideoID.Int64 != vid) {
		http.NotFound(w, r)
		return
	}
	draft, err := h.Library.ReadVideoPrefetchDraft(vid, tid)
	if err != nil || draft.ArtFiles == nil {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimSpace(draft.ArtFiles[role])
	if path == "" || !fileExistsWeb(path) {
		http.NotFound(w, r)
		return
	}
	cacheRoot := strings.TrimSpace(h.Library.CacheDir)
	if cacheRoot == "" {
		cacheRoot = filepath.Join("data", "cache")
	}
	cacheRoot = filepath.Clean(filepath.Join(cacheRoot, "video-meta", strconv.FormatInt(vid, 10)))
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(cacheRoot, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, clean)
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
