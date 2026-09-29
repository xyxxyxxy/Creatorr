package web

import (
	"encoding/json"
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) importPage(w http.ResponseWriter, r *http.Request) {
	roots, err := h.Library.ListRoots()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	profiles, _ := h.Library.ListProfiles()
	importBusy, _ := importQueueBusy(h.Queue)

	render(w, "import", struct {
		pageBase
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		ScanCronDescriptors []string
		ImportBusy          bool
	}{
		pageBase:            newPage("Import", "import", nil),
		Roots:               roots,
		Profiles:            profiles,
		ScanCronDescriptors: scanCronDescriptors(),
		ImportBusy:          importBusy,
	})
}

func (h *Handler) importBusyStatus(w http.ResponseWriter, r *http.Request) {
	busy, err := importQueueBusy(h.Queue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"busy": busy})
}

func importQueueBusy(q *queue.Store) (bool, error) {
	busy, err := q.HasPendingOrRunningKind(queue.KindImport, queue.SystemDomain)
	if err != nil || busy {
		return busy, err
	}
	return q.HasPendingOrRunningKind(queue.KindImportPlan, queue.SystemDomain)
}
