package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/stats"
)

func (h *Handler) statsPage(w http.ResponseWriter, r *http.Request) {
	retention, _ := settings.Get(h.Queue.DB, settings.KeyStatsRetentionDays)
	retention = settings.NormalizeStatsRetention(retention)
	retentionLabel := retention + " days"
	for _, o := range settings.StatsRetentionOptions() {
		if o.Value == retention {
			retentionLabel = strings.ToLower(o.Label)
			break
		}
	}
	rootCount := 0
	if roots, err := h.Library.ListRoots(); err == nil {
		rootCount = len(roots)
	}

	render(w, "stats", struct {
		pageBase
		RetentionLabel string
		RootCount      int
	}{
		pageBase:       newPage("Stats", "stats", nil),
		RetentionLabel: retentionLabel,
		RootCount:      rootCount,
	})
}

func (h *Handler) statsSeriesJSON(w http.ResponseWriter, r *http.Request) {
	payload, err := stats.LoadChart(h.Queue.DB)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) statsLibrarySizeJSON(w http.ResponseWriter, r *http.Request) {
	payload, err := stats.LoadLibrarySize(h.Queue.DB, r.URL.Query().Get("group"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}
