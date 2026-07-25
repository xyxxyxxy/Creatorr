package library

import (
	"encoding/json"
	"sort"
	"strings"
)

// IgnoreReasonMediaType is recorded when a video is ignored for excluded media_type.
const IgnoreReasonMediaType = "media_type"

// YouTubeMediaTypeSeed is the fixed suggestion set from yt-dlp's YouTube extractor.
var YouTubeMediaTypeSeed = []string{"clip", "livestream", "short", "video"}

// NormalizeMediaType trims a raw yt-dlp media_type; empty means missing (never filterable).
func NormalizeMediaType(raw string) string {
	return strings.TrimSpace(raw)
}

// NormalizeAutoIgnoreMediaTypes cleans an exclude list: trim, drop empty/unknown, dedupe, sort alpha.
func NormalizeAutoIgnoreMediaTypes(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "unknown") {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// AutoIgnoreMediaTypesJSON encodes a normalized exclude list as JSON (always a JSON array).
func AutoIgnoreMediaTypesJSON(raw []string) string {
	norm := NormalizeAutoIgnoreMediaTypes(raw)
	if len(norm) == 0 {
		return "[]"
	}
	b, err := json.Marshal(norm)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseAutoIgnoreMediaTypesJSON decodes series.auto_ignore_media_types (invalid → empty).
func ParseAutoIgnoreMediaTypesJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return nil
	}
	return NormalizeAutoIgnoreMediaTypes(vals)
}

// MediaTypeExcluded reports whether a known (non-empty) media type is in the exclude set.
// Missing/empty type never matches.
func MediaTypeExcluded(exclude []string, mediaType string) bool {
	mediaType = NormalizeMediaType(mediaType)
	if mediaType == "" {
		return false
	}
	for _, e := range NormalizeAutoIgnoreMediaTypes(exclude) {
		if e == mediaType {
			return true
		}
	}
	return false
}

// MediaTypeMatchFilter builds a yt-dlp --match-filters expression from an exclude list.
// Empty exclude → empty string (omit the flag). Absent media_type still passes.
func MediaTypeMatchFilter(exclude []string) string {
	norm := NormalizeAutoIgnoreMediaTypes(exclude)
	if len(norm) == 0 {
		return ""
	}
	parts := make([]string, 0, len(norm))
	for _, e := range norm {
		// Quote values that need it; simple alphanumerics are fine unquoted.
		if needsMatchFilterQuote(e) {
			parts = append(parts, `media_type!='`+strings.ReplaceAll(e, `'`, `\'`)+`'`)
		} else {
			parts = append(parts, "media_type!="+e)
		}
	}
	return strings.Join(parts, " & ")
}

func needsMatchFilterQuote(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return true
	}
	return false
}

// MergeMediaTypeSuggestions returns YouTube seed ∪ customs, sorted unique (never unknown).
func MergeMediaTypeSuggestions(customs []string) []string {
	seen := make(map[string]struct{}, len(YouTubeMediaTypeSeed)+len(customs))
	out := make([]string, 0, len(YouTubeMediaTypeSeed)+len(customs))
	for _, v := range YouTubeMediaTypeSeed {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range NormalizeAutoIgnoreMediaTypes(customs) {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
