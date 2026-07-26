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

	recentVids, _ := h.Library.ListRecentVideos(VideoPageSize)
	recentRows := h.buildSeriesVideoRows(recentVids, nil, nil)
	seriesIDs := make([]int64, 0, len(recentVids))
	seen := map[int64]struct{}{}
	for _, v := range recentVids {
		if _, ok := seen[v.SeriesID]; ok {
			continue
		}
		seen[v.SeriesID] = struct{}{}
		seriesIDs = append(seriesIDs, v.SeriesID)
	}
	seriesTitles, _ := h.Library.SeriesTitles(seriesIDs)

	render(w, "overview", struct {
		pageBase
		SeriesCount                int
		VideoCount                 int
		SizeHuman                  string
		RecentVideos               []seriesVideoRow
		SeriesTitles               map[int64]string
		Roots                      []library.RootFolder
		Profiles                   []library.QualityProfile
		ScanCronDescriptors        []string
		AutoIgnoreMediaTypeOptions []string
		AllSourceURLs              []string
		CanStream                  bool
		StreamDisabledReason       string
	}{
		pageBase:                   newPage("Overview", "overview", flashFromQuery(r)),
		SeriesCount:                totals.SeriesCount,
		VideoCount:                 totals.VideoCount,
		SizeHuman:                  library.FormatBytes(totals.SizeBytes),
		RecentVideos:               recentRows,
		SeriesTitles:               seriesTitles,
		Roots:                      roots,
		Profiles:                   profiles,
		ScanCronDescriptors:        scanCronDescriptors(),
		AutoIgnoreMediaTypeOptions: autoIgnoreMediaTypeOptions(h),
		AllSourceURLs:              allSourceURLs,
		CanStream:                  canStream,
		StreamDisabledReason:       streamReason,
	})
}
