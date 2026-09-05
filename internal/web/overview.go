package web

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	totals, err := h.Library.OverviewTotals()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	roots, _ := h.Library.ListRoots()
	profiles, _ := h.Library.ListProfiles()

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
		SeriesCount         int
		VideoCount          int
		SizeHuman           string
		RunningTasks        []taskView
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
		RunningTasks:        h.runningTaskViews(),
		RecentVideos:        recentRows,
		SeriesTitles:        seriesTitles,
		Roots:               roots,
		Profiles:            profiles,
		ScanCronDescriptors: scanCronDescriptors(),
	})
}

// runningTaskViews returns status=running tasks across all domains (read-only overview).
func (h *Handler) runningTaskViews() []taskView {
	if h.Queue == nil {
		return nil
	}
	tasks, err := h.Queue.ListActive()
	if err != nil {
		return nil
	}
	titles := map[int64]string{}
	videoTitles := map[int64]string{}
	var out []taskView
	for _, t := range tasks {
		if t.Status != queue.StatusRunning {
			continue
		}
		tv := taskView{
			ID: t.ID, Status: t.Status, Kind: t.Kind, Domain: t.Domain, Message: t.Message,
		}
		if t.SeriesID.Valid {
			tv.SeriesID = t.SeriesID.Int64
			if title, ok := titles[tv.SeriesID]; ok {
				tv.SeriesTitle = title
			} else if ser, err := h.Library.GetSeries(tv.SeriesID, false); err == nil {
				titles[tv.SeriesID] = ser.Title
				tv.SeriesTitle = ser.Title
			}
		}
		if t.VideoID.Valid {
			tv.VideoID = t.VideoID.Int64
			if title, ok := videoTitles[tv.VideoID]; ok {
				tv.VideoTitle = title
			} else if v, err := h.Library.GetVideo(tv.VideoID); err == nil {
				videoTitles[tv.VideoID] = v.Title
				tv.VideoTitle = v.Title
				if tv.SeriesID == 0 {
					tv.SeriesID = v.SeriesID
				}
			}
		}
		if t.Progress.Valid {
			p := t.Progress.Float64
			tv.Progress = &p
		}
		out = append(out, tv)
	}
	return out
}
