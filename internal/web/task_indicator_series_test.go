package web

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

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
		HasSourceError: true, SourceErrorCount: 2,
		HasDownloadError: true, DownloadErrorCount: 5,
	}, library.SeriesWarnNone)
	if !ok || v.Kind != "wanted_source_error" || v.Count != 2 {
		t.Fatalf("source wins over download: ok=%v v=%+v", ok, v)
	}
}
