package web

import (
	"encoding/json"
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	roots, err := h.Library.ListRoots()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	profiles, _ := h.Library.ListProfiles()
	incompleteFullScan, _ := h.Library.HasIncompleteFullScan()

	render(w, "import", struct {
		pageBase
		ImportPath                 string
		Roots                      []library.RootFolder
		Profiles                   []library.QualityProfile
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		IncompleteFullScan         bool
	}{
		pageBase:                   newPage("Import", "import", nil),
		ImportPath:                 h.Library.ImportRoot,
		Roots:                      roots,
		Profiles:                   profiles,
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
