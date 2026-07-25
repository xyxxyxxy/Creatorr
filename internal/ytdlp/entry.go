package ytdlp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Entry is one listed/resolved video from yt-dlp.
type Entry struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	WebpageURL   string  `json:"webpage_url"`
	UploadDate   string  `json:"upload_date"`
	Description  string  `json:"description"`
	ThumbnailURL string  `json:"thumbnail_url"`
	MediaType    string  `json:"media_type,omitempty"`
	Duration     float64 `json:"duration,omitempty"` // seconds; omit when unknown
}

// entriesFromInfo maps a yt-dlp -J dump (flat playlist or single video) to entries.
// Mixed playlists keep top-level videos and skip nested playlists. Channel roots
// (only tab playlists, each with nested entries) prefer the Videos tab.
func entriesFromInfo(info map[string]any) []Entry {
	if info == nil {
		return nil
	}
	if t, _ := info["_type"].(string); t == "playlist" {
		raw, _ := info["entries"].([]any)
		var videos []Entry
		var tabs []map[string]any
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if et, _ := m["_type"].(string); et == "playlist" {
				tabs = append(tabs, m)
				continue
			}
			e := entryFromMap(m)
			if e.ID == "" {
				continue
			}
			videos = append(videos, e)
		}
		if len(videos) > 0 {
			return videos
		}
		if tab := pickVideosTab(tabs); tab != nil {
			return entriesFromInfo(tab)
		}
		var out []Entry
		for _, tab := range tabs {
			out = append(out, entriesFromInfo(tab)...)
		}
		return out
	}
	e := entryFromMap(info)
	if e.ID == "" {
		return nil
	}
	return []Entry{e}
}

// pickVideosTab chooses a YouTube-style "Videos" tab playlist from channel tabs.
func pickVideosTab(tabs []map[string]any) map[string]any {
	for _, m := range tabs {
		u := strings.ToLower(strField(m, "webpage_url", "url"))
		if strings.Contains(u, "/videos") {
			return m
		}
	}
	for _, m := range tabs {
		title := strings.ToLower(strings.TrimSpace(strField(m, "title")))
		if strings.HasSuffix(title, " - videos") || title == "videos" {
			return m
		}
	}
	return nil
}

func entryFromMap(m map[string]any) Entry {
	id := strField(m, "id", "display_id")
	title := strField(m, "title")
	if title == "" {
		title = id
	}
	if title == "" {
		title = "video"
	}
	url := strField(m, "webpage_url", "url")
	if url != "" && !strings.HasPrefix(url, "http") {
		url = ""
	}
	date := uploadTime(m)
	desc := strField(m, "description")
	if len(desc) > 4000 {
		desc = desc[:4000]
	}
	return Entry{
		ID:           id,
		Title:        title,
		WebpageURL:   url,
		UploadDate:   date,
		Description:  desc,
		ThumbnailURL: thumbURL(m),
		MediaType:    strings.TrimSpace(strField(m, "media_type")),
		Duration:     durationSeconds(m),
	}
}

func durationSeconds(m map[string]any) float64 {
	switch v := m["duration"].(type) {
	case float64:
		if v > 0 {
			return v
		}
	case int:
		if v > 0 {
			return float64(v)
		}
	case int64:
		if v > 0 {
			return float64(v)
		}
	case json.Number:
		f, err := v.Float64()
		if err == nil && f > 0 {
			return f
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil && f > 0 {
			return f
		}
	}
	return 0
}

// uploadTime prefers yt-dlp timestamp / release_timestamp (unix → RFC3339 UTC).
// Falls back to upload_date / release_date (YYYYMMDD) as midnight UTC RFC3339.
func uploadTime(m map[string]any) string {
	if sec, ok := unixField(m, "timestamp", "release_timestamp"); ok {
		return time.Unix(sec, 0).UTC().Format(time.RFC3339)
	}
	raw := strField(m, "upload_date", "release_date")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) == 8 {
		t, err := time.ParseInLocation("20060102", raw, time.UTC)
		if err != nil {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return ""
}

func unixField(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			if v > 0 {
				return int64(v), true
			}
		case json.Number:
			n, err := v.Int64()
			if err == nil && n > 0 {
				return n, true
			}
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err == nil && n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return fmt.Sprintf("%.0f", v)
		case json.Number:
			return v.String()
		}
	}
	return ""
}

// thumbURL picks the highest-resolution thumbnail: yt-dlp orders "thumbnails"
// smallest-first, so the last entry with a usable URL wins.
func thumbURL(m map[string]any) string {
	if thumbs, ok := m["thumbnails"].([]any); ok {
		for i := len(thumbs) - 1; i >= 0; i-- {
			t, ok := thumbs[i].(map[string]any)
			if !ok {
				continue
			}
			if u, ok := t["url"].(string); ok && u != "" {
				return u
			}
		}
	}
	if u, ok := m["thumbnail"].(string); ok {
		return u
	}
	return ""
}
