package queue

import (
	"encoding/json"
	"strings"
)

const (
	KindScan                  = "scan"
	KindDownload              = "download"
	KindRescanMetadata        = "rescan_metadata"
	KindRefreshSidecars       = "refresh_sidecars"
	KindImport                = "import"
	KindImportPlan            = "import_plan"
	KindPrefetchSeriesMeta    = "prefetch_series_meta"
	KindPrefetchVideoMeta     = "prefetch_video_meta"
	KindPrefetchAddSeries     = "prefetch_add_series"
	KindPrefetchAddVideo      = "prefetch_add_video"
	KindProbeSourceTitle      = "probe_source_title"
	KindSyncFiles             = "sync_files"
	KindRetentionDelete       = "retention_delete"
	KindRenameEpisodes        = "rename_episodes"
	KindRegenerateNFO         = "regenerate_nfo"
	KindIntegrityCheck        = "integrity_check"
	KindDeleteFiles           = "delete_files"
	KindSponsorblockCut       = "sponsorblock_cut"
	KindIntegrityCheckInitial = "integrity_check_initial"
	KindYtDlpUpdate           = "ytdlp_update"
	KindBulkEditSeries        = "bulk_edit_series"
	KindBulkEditVideos        = "bulk_edit_videos"

	// SystemDomain is the queue lane for maintenance tasks.
	SystemDomain = "system"
)

// IsPrefetchKind is true for ClaimImmediate metadata prefetch / probe tasks.
// These do not occupy max_parallel_tasks slots.
func IsPrefetchKind(kind string) bool {
	return kind == KindPrefetchSeriesMeta || kind == KindPrefetchVideoMeta ||
		kind == KindPrefetchAddSeries || kind == KindPrefetchAddVideo ||
		kind == KindProbeSourceTitle
}

// IsInteractiveKind is true for tasks that must not wait behind other work
// (prefetch ClaimImmediate). Finish does not start domain cooldown.
// Prefetch kinds do not occupy parallel slots.
func IsInteractiveKind(kind string) bool {
	return IsPrefetchKind(kind)
}

// PayloadKeyDownloadNow marks a download for ClaimImmediate (skip queue / parallel / cooldown).
const PayloadKeyDownloadNow = "download_now"

// IsDownloadNowPayload reports payload.download_now truthy.
func IsDownloadNowPayload(payload string) bool {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return false
	}
	v, ok := m[PayloadKeyDownloadNow]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	default:
		return false
	}
}

// IsImmediateKind reports tasks claimed via ClaimImmediate (prefetch or download-now).
func IsImmediateKind(kind, payload string) bool {
	if IsInteractiveKind(kind) {
		return true
	}
	return kind == KindDownload && IsDownloadNowPayload(payload)
}

// sqlDownloadNowTrue is the SQLite predicate for tasks.payload.download_now (alias t).
const sqlDownloadNowTrue = `CAST(COALESCE(json_extract(t.payload, '$.` + PayloadKeyDownloadNow + `'), 0) AS INTEGER) != 0`

// Task origin: kick source (writable values only).
const (
	OriginManual    = "manual"
	OriginScheduled = "scheduled"
	OriginBoot      = "boot"
	OriginTask      = "task"
)

// ValidOrigin reports whether origin is a writable provenance value.
func ValidOrigin(origin string) bool {
	switch origin {
	case OriginManual, OriginScheduled, OriginBoot, OriginTask:
		return true
	default:
		return false
	}
}

// SourceIDFromPayload reads source_id from a task payload JSON (scan batches).
func SourceIDFromPayload(payload string) int64 {
	var p struct {
		SourceID int64 `json:"source_id"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return 0
	}
	return p.SourceID
}

// FileDeleteIDsFromPayload reads series_ids and video_ids from a delete_files payload.
func FileDeleteIDsFromPayload(payload string) (seriesIDs, videoIDs []int64) {
	var p struct {
		SeriesIDs []int64 `json:"series_ids"`
		VideoIDs  []int64 `json:"video_ids"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	return p.SeriesIDs, p.VideoIDs
}

// URLFromPayload reads url from a task payload JSON (series meta prefetch).
func URLFromPayload(payload string) string {
	var p struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.URL)
}

// DraftTokenFromPayload reads draft_token from prefetch_add_series / prefetch_add_video payload JSON.
func DraftTokenFromPayload(payload string) string {
	var p struct {
		DraftToken string `json:"draft_token"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.DraftToken)
}
