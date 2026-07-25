package web

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	totals, err := h.Library.OverviewTotals()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()
	allSourceURLs, _ := h.Library.ListAllSourceURLs()
	canStream, streamReason := h.streamGate()
	render(w, "overview", struct {
		pageBase
		SeriesCount          int
		VideoCount           int
		SizeHuman            string
		Roots                []library.RootFolder
		Profiles             []library.QualityProfile
		ScanCronDescriptors      []string
		AutoIgnoreMediaTypeOptions  []string
		AllSourceURLs            []string
		CanStream                bool
		StreamDisabledReason     string
	}{
		pageBase:                 newPage("Overview", "overview", flashFromQuery(r)),
		SeriesCount:              totals.SeriesCount,
		VideoCount:               totals.VideoCount,
		SizeHuman:                library.FormatBytes(totals.SizeBytes),
		Roots:                    roots,
		Profiles:                 profiles,
		ScanCronDescriptors:      scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions:  autoIgnoreMediaTypeOptions(h),
		AllSourceURLs:            allSourceURLs,
		CanStream:                canStream,
		StreamDisabledReason:     streamReason,
	})
}
