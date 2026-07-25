package web

import "testing"

func TestHistoryBreadcrumbs(t *testing.T) {
	t.Parallel()
	ser := &seriesLink{ID: 1, Title: "Demo"}
	src := &sourceLink{ID: 2, Title: "Feed"}
	vid := &videoLink{ID: 3, SeriesID: 1, Title: "Ep"}

	got := historyBreadcrumbs(nil, nil, nil, "sync_files")
	if len(got) != 2 || got[0].Href != "/history" || got[0].Icon != "history" || got[1].Icon != "scroll-text" {
		t.Fatalf("orphan: %+v", got)
	}

	got = historyBreadcrumbs(ser, nil, vid, "download")
	if len(got) != 4 || got[0].Href != "/series" || got[2].Href != "/series/1/videos/3" || got[2].Icon != "film" {
		t.Fatalf("video path: %+v", got)
	}

	got = historyBreadcrumbs(ser, src, nil, "scan")
	if len(got) != 4 || got[2].Href != "/series/1/sources/2" || got[2].Icon != "rss" {
		t.Fatalf("source path: %+v", got)
	}

	// Video wins over source when both present.
	got = historyBreadcrumbs(ser, src, vid, "download")
	if len(got) != 4 || got[2].Icon != "film" {
		t.Fatalf("video preferred: %+v", got)
	}
}
