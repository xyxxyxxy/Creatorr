package web

import (
	"fmt"
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

	wantedSeries, _ := h.Library.ListMostWantedSeries(OverviewGalleryRow)
	wantedRows := make([]seriesListRow, 0, len(wantedSeries))
	for _, s := range wantedSeries {
		art := h.Library.SeriesArtFlagsFor(&s)
		posterURL := ""
		if art.Poster {
			posterURL = fmt.Sprintf("/series/%d/art/poster", s.ID)
		}
		wantedRows = append(wantedRows, seriesListRow{
			Series:    s,
			PosterURL: posterURL,
		})
	}

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
		DownloadedCount     int
		SizeHuman           string
		TasksLive           tasksListLiveData
		SeriesBrowseHref    string
		VideosBrowseHref    string
		WantedSeries        []seriesListRow
		RecentVideos        []seriesVideoRow
		SeriesTitles        map[int64]string
		Roots               []library.RootFolder
		Profiles            []library.QualityProfile
		ScanCronDescriptors []string
	}{
		pageBase:            newPage("Overview", "overview", flashFromQuery(r)),
		SeriesCount:         totals.SeriesCount,
		VideoCount:          totals.VideoCount,
		DownloadedCount:     totals.DownloadedCount,
		SizeHuman:           library.FormatBytes(totals.SizeBytes),
		TasksLive:           tasksLive,
		SeriesBrowseHref:    overviewSeriesBrowseHref,
		VideosBrowseHref:    overviewVideosBrowseHref,
		WantedSeries:        wantedRows,
		RecentVideos:        recentRows,
		SeriesTitles:        seriesTitles,
		Roots:               roots,
		Profiles:            profiles,
		ScanCronDescriptors: scanCronDescriptors(),
	})
}
