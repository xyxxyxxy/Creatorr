package web

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func TestBuildSourceStatusScheduleOffSaysDisabled(t *testing.T) {
	t.Parallel()
	off := buildSourceStatus(sourceStatusParams{
		Src:        library.Source{ID: 7, FullScanDone: true, ScanCron: ""},
		Summary:    "28 days ago (11 new)",
		HasScanned: true,
		HistoryID:  124,
	})
	if off.Kind != "unscheduled" || off.Label != "disabled" || off.Href != "/task/124" {
		t.Fatalf("schedule off: %+v", off)
	}
	never := buildSourceStatus(sourceStatusParams{
		Src:        library.Source{ID: 7, FullScanDone: true, ScanCron: "never"},
		Summary:    "28 days ago (11 new)",
		HasScanned: true,
		HistoryID:  124,
	})
	if never.Label != "disabled" {
		t.Fatalf("stored never: %+v", never)
	}
	on := buildSourceStatus(sourceStatusParams{
		Src:        library.Source{ID: 7, FullScanDone: true, ScanCron: "@weekly"},
		Summary:    "28 days ago (11 new)",
		HasScanned: true,
		HistoryID:  124,
	})
	if on.Kind != "scheduled" || on.Label != "28 days ago (11 new)" {
		t.Fatalf("schedule on: %+v", on)
	}
}

func TestBuildSeriesHealthStatusCounts(t *testing.T) {
	t.Parallel()
	v, ok := buildSeriesHealthStatus(library.SeriesVideoErrorFlags{
		HasDownloadError: true, DownloadErrorCount: 1,
	}, library.SeriesWarnNone)
	if !ok || v.Kind != "wanted_download_error" || v.Title != "Download error (1)" || v.Count != 1 {
		t.Fatalf("single download error: ok=%v v=%+v", ok, v)
	}
	v, ok = buildSeriesHealthStatus(library.SeriesVideoErrorFlags{
		HasDownloadError: true, DownloadErrorCount: 3,
	}, library.SeriesWarnNone)
	if !ok || v.Title != "Download error (3)" || v.Count != 3 {
		t.Fatalf("multi download error: ok=%v v=%+v", ok, v)
	}
	v, ok = buildSeriesHealthStatus(library.SeriesVideoErrorFlags{
		HasVerifyFailed: true, VerifyFailedCount: 2,
		HasDownloadError: true, DownloadErrorCount: 5,
	}, library.SeriesWarnNone)
	if !ok || v.Kind != "wanted_download_error" || v.Count != 5 {
		t.Fatalf("download wins over verify: ok=%v v=%+v", ok, v)
	}
}
