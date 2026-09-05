package library

import "strings"

// IgnoreReasonMediaType is kept for historical scan/task detail (ignored_media_type_ids).
const IgnoreReasonMediaType = "media_type"

// NormalizeMediaType trims a raw yt-dlp media_type; empty means missing (never filterable).
func NormalizeMediaType(raw string) string {
	return strings.TrimSpace(raw)
}

// LiveBroadcastMatchFilter is always AND'd into archive download --match-filters.
// Incomplete-match `?` so missing is_live still passes (normal VODs).
const LiveBroadcastMatchFilter = "is_live!=?1"

// BuildDownloadMatchFilter returns the always-on live soft-skip filter.
func BuildDownloadMatchFilter() string {
	return LiveBroadcastMatchFilter
}
