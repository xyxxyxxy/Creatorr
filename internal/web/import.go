package web

import (
	"encoding/json"
	"fmt"
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
	roots, err := h.Library.ListRoots()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	profiles, _ := h.Library.ListProfiles()
	incompleteFullScan, _ := h.Library.HasIncompleteFullScan()

	rootPath := map[int64]string{}
	for _, root := range roots {
		rootPath[root.ID] = root.Path
	}
	type seriesOpt struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		PosterURL string `json:"poster_url,omitempty"`
	}
	opts := make([]seriesOpt, 0, len(series))
	for _, s := range series {
		posterURL := ""
		if path := rootPath[s.RootID]; path != "" {
			if library.SeriesArtFlagsForDir(library.SeriesDir(path, s.Title)).Poster {
				posterURL = fmt.Sprintf("/series/%d/art/poster", s.ID)
			}
		}
		opts = append(opts, seriesOpt{ID: s.ID, Title: s.Title, PosterURL: posterURL})
	}
	render(w, "import", struct {
		pageBase
		ImportPath                 string
		Roots                      []library.RootFolder
		Profiles                   []library.QualityProfile
		Series                     []seriesOpt
		Videos                     []library.ImportPickerVideo
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		IncompleteFullScan         bool
	}{
		pageBase:                   newPage("Import", "import", nil),
		ImportPath:                 h.Library.ImportRoot,
		Roots:                      roots,
		Profiles:                   profiles,
		Series:                     opts,
		Videos:                     videos,
		ScanCronDescriptors:        scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions: autoIgnoreMediaTypeOptions(h),
		IncompleteFullScan:         incompleteFullScan,
	})
}

func (h *Handler) importFullScanStatus(w http.ResponseWriter, r *http.Request) {
	incomplete, err := h.Library.HasIncompleteFullScan()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"incomplete": incomplete})
}
