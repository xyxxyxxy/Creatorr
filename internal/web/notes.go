package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (h *Handler) actionSaveVideoNotes(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	vid, _ := strconv.ParseInt(r.FormValue("video_id"), 10, 64)
	notes := r.FormValue("notes")
	if err := h.Library.UpdateVideoNotes(vid, notes); err != nil {
		h.respondNotesSaveError(w, r, err)
		return
	}
	h.respondNotesSaveOK(w)
}

func (h *Handler) actionSaveSeriesNotes(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	notes := r.FormValue("notes")
	if err := h.Library.UpdateSeriesNotes(sid, notes); err != nil {
		h.respondNotesSaveError(w, r, err)
		return
	}
	h.respondNotesSaveOK(w)
}

func (h *Handler) respondNotesSaveOK(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) respondNotesSaveError(w http.ResponseWriter, r *http.Request, err error) {
	msg := "Save failed."
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, library.ErrNotFound):
		msg = "Not found."
		code = http.StatusNotFound
	case errors.Is(err, library.ErrInvalid):
		code = http.StatusUnprocessableEntity
		if strings.Contains(err.Error(), "notes too long") {
			msg = "Notes too long."
		} else {
			msg = err.Error()
		}
	}
	if h.isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		render(w, "flash_toast_oob", flashErr(msg))
		return
	}
	http.Error(w, msg, code)
}
