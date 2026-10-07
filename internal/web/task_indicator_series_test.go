package web

import (
	"strings"
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

func TestBuildSourceStatusScanErrorTipOmitsLongMessage(t *testing.T) {
	t.Parallel()
	long := "yt-dlp metadata failed: ERROR: [youtube] abc: This video is available to this channel's members on level: Gold"
	v := buildSourceStatus(sourceStatusParams{
		Src:        library.Source{ID: 19, FullScanDone: true},
		HasError:   true,
		ErrCode:    "ResolveFailed",
		ErrMsg:     long,
		Summary:    long,
		HasScanned: true,
		HistoryID:  99,
	})
	if v.Kind != "scan_error" || v.Label != "ResolveFailed" || v.Href != "/task/99" {
		t.Fatalf("scan_error chrome: %+v", v)
	}
	if strings.Contains(v.Title, "yt-dlp") || strings.Contains(v.Title, "Last scan:") {
		t.Fatalf("tip must omit long error body, got %q", v.Title)
	}
	if !strings.Contains(v.Title, "ResolveFailed") || !strings.Contains(v.Title, "Open task for full error") {
		t.Fatalf("tip=%q", v.Title)
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
