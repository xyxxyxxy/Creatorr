package web

import (
	"net/http"
	"net/http/httptest"

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

	recentVids, _ := h.Library.ListRecentVideos(FixedDefault)
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

	tasksReq := httptest.NewRequest(http.MethodGet, "/explorer/browse?type=tasks&at=overview&view=list", nil)
	tasksLive, err := h.loadTasksListLive(w, tasksReq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	render(w, "overview", struct {
		pageBase
		SeriesCount         int
		VideoCount          int
		SizeHuman           string
		TasksLive           tasksListLiveData
		VideosBrowseHref    string
		RecentVideos        []seriesVideoRow
		SeriesTitles        map[int64]string
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		ScanCronDescriptors []string
	}{
		pageBase:            newPage("Overview", "overview", flashFromQuery(r)),
		SeriesCount:         totals.SeriesCount,
		VideoCount:          totals.VideoCount,
		SizeHuman:           library.FormatBytes(totals.SizeBytes),
		TasksLive:           tasksLive,
		VideosBrowseHref:    overviewVideosBrowseHref,
		RecentVideos:        recentRows,
		SeriesTitles:        seriesTitles,
		Roots:               roots,
		Profiles:            profiles,
		ScanCronDescriptors: scanCronDescriptors(),
	})
}
