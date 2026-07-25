package web

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	series, err := h.Library.ListSeries()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	videos, err := h.Library.ListImportPickerVideos()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type seriesOpt struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	opts := make([]seriesOpt, 0, len(series))
	for _, s := range series {
		opts = append(opts, seriesOpt{ID: s.ID, Title: s.Title})
	}
	render(w, "import", struct {
		pageBase
		ImportPath string
		Series     []seriesOpt
		Videos     []library.ImportPickerVideo
	}{
		pageBase:   newPage("Import", "import", nil),
		ImportPath: h.Library.ImportRoot,
		Series:     opts,
		Videos:     videos,
	})
}
