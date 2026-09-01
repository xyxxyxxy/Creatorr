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
)

func (h *Handler) seriesArtFlags(ser *library.Series) library.SeriesArtFlags {
	root, err := h.Library.GetRoot(ser.RootID)
	if err != nil {
		return library.SeriesArtFlags{}
	}
	return library.SeriesArtFlagsForDir(library.SeriesDir(root.Path, ser.Title))
}

func (h *Handler) actionSaveSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(ct), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
			if sid > 0 {
				http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		_ = r.ParseForm()
	}
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if _, err := h.Library.GetSeries(sid, false); err != nil {
		http.NotFound(w, r)
		return
	}
	tmpDir, err := os.MkdirTemp("", "creatorr-meta-upload-*")
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	artSrc := map[string]string{}
	artClear := map[string]bool{}
	roles := []string{library.ArtPoster, library.ArtBanner, library.ArtFanart, library.ArtClearlogo}
	for _, role := range roles {
		// Prefer a newly chosen file over clear or an ephemeral prefetch path.
		f, hdr, err := r.FormFile(role)
		if err == nil && hdr != nil && strings.TrimSpace(hdr.Filename) != "" {
			ext := strings.ToLower(filepath.Ext(hdr.Filename))
			if ext == "" {
				ext = ".jpg"
			}
			dest := filepath.Join(tmpDir, role+ext)
			out, createErr := os.Create(dest)
			if createErr != nil {
				_ = f.Close()
				http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(createErr.Error())), http.StatusSeeOther)
				return
			}
			_, copyErr := io.Copy(out, f)
			_ = out.Close()
			_ = f.Close()
			if copyErr != nil {
				http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(copyErr.Error())), http.StatusSeeOther)
				return
			}
			artSrc[role] = dest
			continue
		}
		if r.FormValue("clear_"+role) == "1" {
			artClear[role] = true
			continue
		}
		if pref := strings.TrimSpace(r.FormValue("prefetch_" + role)); pref != "" && fileExistsWeb(pref) {
			artSrc[role] = pref
		}
	}

	uidType := ""
	uidVal := ""
	var prefetchTID int64
	if draftTID := strings.TrimSpace(r.FormValue("prefetch_task_id")); draftTID != "" {
		if tid, err := strconv.ParseInt(draftTID, 10, 64); err == nil && tid > 0 {
			prefetchTID = tid
			if d, err := h.Library.ReadPrefetchDraft(sid, tid); err == nil {
				uidType = d.UniqueIDType
				uidVal = d.UniqueIDValue
			}
		}
	}

	err = h.Library.SaveSeriesMetadata(sid, library.SaveSeriesMetadataParams{
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
		ArtSrc:        artSrc,
		ArtClear:      artClear,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	if prefetchTID > 0 {
		_ = h.Library.ClearPrefetchDraft(sid, prefetchTID)
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?ok=metadata", sid), http.StatusSeeOther)
}

func fileExistsWeb(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (h *Handler) actionSeriesMetadataPrefetch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	fetchURL := strings.TrimSpace(r.FormValue("fetch_url"))
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tid, err := h.Library.EnqueueSeriesMetaPrefetch(sid, fetchURL)
	if err != nil {
		if r.Header.Get("HX-Request") == "true" {
			render(w, "series_metadata_body", h.withMetaSuggestions(seriesMetadataView{
				Series: ser, Art: h.seriesArtFlags(ser), PrefetchArt: map[string]string{},
				PrefetchDraft: library.PrefetchDraft{Error: err.Error()}, FetchURL: fetchURL,
			}))
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", sid, urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	view := h.withMetaSuggestions(seriesMetadataView{
		Series: ser, Art: h.seriesArtFlags(ser), PrefetchArt: map[string]string{},
		PrefetchTaskID: tid, PrefetchPending: true, FetchURL: fetchURL, Open: true,
	})
	if r.Header.Get("HX-Request") == "true" {
		render(w, "series_metadata_body", view)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/series/%d?prefetch_task=%d&meta=1", sid, tid), http.StatusSeeOther)
}

func (h *Handler) seriesMetadataPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		http.NotFound(w, r)
		return
	}
	if task.SeriesID.Valid && task.SeriesID.Int64 != sid {
		http.NotFound(w, r)
		return
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		// No DOM swap while still working - stops the modal from flashing every poll.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	art := h.seriesArtFlags(ser)
	fetchURL := queue.URLFromPayload(task.Payload)
	draft := library.PrefetchDraft{}
	switch task.Status {
	case queue.StatusFailed, queue.StatusCancelled:
		draft.Error = task.ErrorMessage
		if draft.Error == "" {
			draft.Error = "Prefetch failed"
		}
	case queue.StatusDone:
		if d, err := h.Library.ReadPrefetchDraft(sid, tid); err == nil {
			draft = d
		}
	}
	merged := applyPrefetchDraft(ser, draft)
	render(w, "series_metadata_body", h.withMetaSuggestions(seriesMetadataView{
		Series: merged, Art: art, PrefetchArt: prefetchArtMap(draft), PrefetchDraft: draft, PrefetchTaskID: tid, FetchURL: fetchURL, Open: true,
	}))
}

// seriesMetadataBody returns the Metadata modal body from saved series state (no draft).
// Optional ?discard=<task_id> cancels a pending prefetch and clears its cache draft.
func (h *Handler) seriesMetadataBody(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if discardStr := strings.TrimSpace(r.URL.Query().Get("discard")); discardStr != "" {
		if tid, err := strconv.ParseInt(discardStr, 10, 64); err == nil && tid > 0 {
			h.discardSeriesMetaPrefetch(sid, tid)
		}
	}
	render(w, "series_metadata_body", h.withMetaSuggestions(seriesMetadataView{
		Series: ser, Art: h.seriesArtFlags(ser), PrefetchArt: map[string]string{},
	}))
}

func (h *Handler) discardSeriesMetaPrefetch(seriesID, taskID int64) {
	if h.Queue != nil {
		if task, err := h.Queue.GetTask(taskID); err == nil && task != nil {
			if task.SeriesID.Valid && task.SeriesID.Int64 == seriesID {
				switch task.Status {
				case queue.StatusPending, queue.StatusRunning:
					_, _ = h.Queue.CancelWithMessage(taskID, "Metadata fetch discarded")
				}
			}
		}
	}
	if h.Library != nil {
		_ = h.Library.ClearPrefetchDraft(seriesID, taskID)
	}
}

type seriesMetadataView struct {
	Series          *library.Series
	Art             library.SeriesArtFlags
	ArtMtimes       map[string]int64
	PrefetchArt     map[string]string
	PrefetchTaskID  int64
	PrefetchPending bool
	PrefetchDraft   library.PrefetchDraft
	FetchURL        string
	Open            bool
	Suggestions     library.MetaSuggestions
}

func (h *Handler) withMetaSuggestions(v seriesMetadataView) seriesMetadataView {
	if h.Library != nil {
		if s, err := h.Library.ListMetaSuggestions(); err == nil {
			v.Suggestions = s
		}
		if v.Series != nil {
			if root, err := h.Library.GetRoot(v.Series.RootID); err == nil {
				dir := library.SeriesDir(root.Path, v.Series.Title)
				v.Art = library.SeriesArtFlagsForDir(dir)
				v.ArtMtimes = library.SeriesArtMtimes(dir)
			}
		}
	}
	if v.ArtMtimes == nil {
		v.ArtMtimes = map[string]int64{}
	}
	if v.PrefetchArt == nil {
		v.PrefetchArt = map[string]string{}
	}
	return v
}

func prefetchArtMap(d library.PrefetchDraft) map[string]string {
	if d.ArtFiles == nil {
		return map[string]string{}
	}
	return d.ArtFiles
}

func applyPrefetchDraft(ser *library.Series, d library.PrefetchDraft) *library.Series {
	out := *ser
	meta := out.Meta
	if d.Plot != "" {
		meta.Plot = d.Plot
	}
	if d.SortTitle != "" {
		meta.SortTitle = d.SortTitle
	}
	if d.OriginalTitle != "" {
		meta.OriginalTitle = d.OriginalTitle
	}
	if d.Studio != "" {
		meta.Studio = d.Studio
	}
	if d.UniqueIDType != "" {
		meta.UniqueIDType = d.UniqueIDType
	}
	if d.UniqueIDValue != "" {
		meta.UniqueIDValue = d.UniqueIDValue
	}
	if len(d.Actors) > 0 {
		meta.Actors = d.Actors
	}
	out.Meta = meta
	return &out
}

func (h *Handler) seriesArtFile(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	role := chi.URLParam(r, "role")
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root, err := h.Library.GetRoot(ser.RootID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dir := library.SeriesDir(root.Path, ser.Title)
	path := ""
	switch role {
	case library.ArtPoster, library.ArtBanner, library.ArtFanart, library.ArtClearlogo:
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			p := filepath.Join(dir, role+ext)
			if fileExistsWeb(p) {
				path = p
				break
			}
		}
	default:
		http.NotFound(w, r)
		return
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeFile(w, r, path)
}

func (h *Handler) seriesPrefetchArtFile(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	role := chi.URLParam(r, "role")
	switch role {
	case library.ArtPoster, library.ArtBanner, library.ArtFanart, library.ArtClearlogo:
	default:
		http.NotFound(w, r)
		return
	}
	if _, err := h.Library.GetSeries(sid, false); err != nil {
		http.NotFound(w, r)
		return
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil || (task.SeriesID.Valid && task.SeriesID.Int64 != sid) {
		http.NotFound(w, r)
		return
	}
	draft, err := h.Library.ReadPrefetchDraft(sid, tid)
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
	cacheRoot = filepath.Clean(filepath.Join(cacheRoot, "series-meta", strconv.FormatInt(sid, 10)))
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(cacheRoot, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, clean)
}
