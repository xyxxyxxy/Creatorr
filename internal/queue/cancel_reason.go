package queue

import (
	"fmt"
	"strings"
)

// Cancel reason codes (closed set). Stored as human tasks.message via CancelReasonMessage.
const (
	CancelReasonManual             = "manual"
	CancelReasonShutdown           = "shutdown"
	CancelReasonVideoIgnored       = "video_ignored"
	CancelReasonVideoDeleted       = "video_deleted"
	CancelReasonSeriesDeleted      = "series_deleted"
	CancelReasonSourceDeleted      = "source_deleted"
	CancelReasonSeriesUnmonitored  = "series_unmonitored"
	CancelReasonMetadataDiscarded  = "metadata_discarded"
	CancelReasonSupersededImport   = "superseded_import"
	CancelReasonSupersededPack     = "superseded_pack"
	CancelReasonDomainDeactivated  = "domain_deactivated"
)

// CancelReasonMessage maps a cancel reason code to the tasks.message string.
// Empty or unknown codes return an error (fail closed).
func CancelReasonMessage(code string) (string, error) {
	switch strings.TrimSpace(code) {
	case CancelReasonManual:
		return "Cancelled (manual)", nil
	case CancelReasonShutdown:
		return "Cancelled (shutdown)", nil
	case CancelReasonVideoIgnored:
		return "Cancelled (video ignored)", nil
	case CancelReasonVideoDeleted:
		return "Cancelled (video deleted)", nil
	case CancelReasonSeriesDeleted:
		return "Cancelled (series deleted)", nil
	case CancelReasonSourceDeleted:
		return "Cancelled (source deleted)", nil
	case CancelReasonSeriesUnmonitored:
		return "Cancelled (series unmonitored)", nil
	case CancelReasonMetadataDiscarded:
		return "Metadata fetch discarded", nil
	case CancelReasonSupersededImport:
		return "Superseded by import", nil
	case CancelReasonSupersededPack:
		return "Superseded by new pack", nil
	case CancelReasonDomainDeactivated:
		return "Domain deactivated", nil
	default:
		return "", fmt.Errorf("cancel reason required: known closed enum")
	}
}

// KindResumableOnShutdown reports whether a running task should be left interrupted
// on process stop (boot RequeueStaleRunning) instead of Cancelled (shutdown).
// Interactive prefetch is UI-session bound and is not resumable.
func KindResumableOnShutdown(kind string) bool {
	return !IsPrefetchKind(kind)
}
