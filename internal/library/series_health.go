package library

// SeriesWarnLevel is a virtual series health status for list/detail UI.
// Higher severity overwrites lower: error > incomplete > none.
type SeriesWarnLevel string

const (
	SeriesWarnNone       SeriesWarnLevel = ""
	SeriesWarnIncomplete SeriesWarnLevel = "incomplete" // full scan stalled, no tip schedule
	SeriesWarnError      SeriesWarnLevel = "error"      // video download error or source scan_error
)

func argsToInt64(args []any) []int64 {
	out := make([]int64, 0, len(args))
	for _, a := range args {
		if id, ok := a.(int64); ok {
			out = append(out, id)
		}
	}
	return out
}

// SeriesVideoErrorFlags reports whether a series has any videos in error statuses.
type SeriesVideoErrorFlags struct {
	HasDownloadError   bool // wanted_download_error
	DownloadErrorCount int
	HasVerifyFailed    bool // integrity_check_failed
	VerifyFailedCount  int
}
