package web

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// videoThumb serves the on-disk thumbnail for a video (kind=thumb file), if present.
func (h *Handler) videoThumb(w http.ResponseWriter, r *http.Request) {
	seriesID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "vid"), 10, 64)
	v, err := h.Library.GetVideo(vid)
	if err != nil || v == nil || v.SeriesID != seriesID {
		http.NotFound(w, r)
		return
	}
	path, ok, err := h.Library.VideoThumbPath(vid)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}
