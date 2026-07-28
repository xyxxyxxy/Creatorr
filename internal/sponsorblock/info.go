package sponsorblock

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var ytIDRe = regexp.MustCompile(`(?i)(?:youtube\.com/(?:watch\?.*v=|embed/|shorts/|live/)|youtu\.be/)([A-Za-z0-9_-]{11})`)

// YouTubeVideoID extracts an 11-char YouTube id from a page URL or remote id.
func YouTubeVideoID(pageURL, remoteID string) string {
	remoteID = strings.TrimSpace(remoteID)
	if looksLikeYTID(remoteID) {
		return remoteID
	}
	pageURL = strings.TrimSpace(pageURL)
	if m := ytIDRe.FindStringSubmatch(pageURL); len(m) == 2 {
		return m[1]
	}
	return ""
}

func looksLikeYTID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// ChaptersFromInfoJSON reads yt-dlp extractor chapters (creator timeline).
func ChaptersFromInfoJSON(path string) ([]Chapter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw struct {
		Chapters []struct {
			StartTime float64 `json:"start_time"`
			EndTime   float64 `json:"end_time"`
			Title     string  `json:"title"`
		} `json:"chapters"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("info.json chapters: %w", err)
	}
	var out []Chapter
	for i, c := range raw.Chapters {
		start := c.StartTime
		end := c.EndTime
		if end <= start {
			if i+1 < len(raw.Chapters) {
				end = raw.Chapters[i+1].StartTime
			} else if raw.Duration > start {
				end = raw.Duration
			} else {
				end = start + 1
			}
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = fmt.Sprintf("Chapter %d", i+1)
		}
		out = append(out, Chapter{Start: start, End: end, Title: title})
	}
	return out, nil
}

// DurationFromInfoJSON returns duration seconds from info.json when present.
func DurationFromInfoJSON(path string) float64 {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var raw struct {
		Duration float64 `json:"duration"`
	}
	_ = json.Unmarshal(b, &raw)
	return raw.Duration
}
