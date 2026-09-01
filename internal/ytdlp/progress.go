package ytdlp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pctRe = regexp.MustCompile(`\[download\]\s+(\d+(?:\.\d+)?)%`)

// speedRe matches yt-dlp's live throughput on progress lines, e.g. "at 1.00MiB/s".
var speedRe = regexp.MustCompile(`\bat\s+(\d+(?:\.\d+)?(?:[KMGT]?i?B)/s)`)

// parseDownloadSpeed returns yt-dlp's rate substring (e.g. "1.00MiB/s") or "".
func parseDownloadSpeed(line string) string {
	m := speedRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1]
}

func appendSpeed(msg, speed string) string {
	if speed == "" {
		return msg
	}
	return msg + " · " + speed
}

// parseProgress extracts a human message and, when available, a 0..1 fraction
// from one line of yt-dlp's --newline stdout/stderr. Lines with no fraction
// (merging, generic destination notices, …) return a message with a nil
// fraction; callers should only forward events that have a fraction.
func parseProgress(line string) (message string, fraction *float64) {
	line = strings.TrimSpace(line)
	if m := pctRe.FindStringSubmatch(line); m != nil {
		pct, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return "", nil
		}
		frac := pct / 100.0
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		return appendSpeed(fmt.Sprintf("Downloading %.0f%%", pct), parseDownloadSpeed(line)), &frac
	}
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "[download]") && strings.Contains(low, "destination"):
		return "Downloading…", nil
	case strings.Contains(low, "[merger]") || strings.Contains(low, "merging"):
		return "Merging…", nil
	case strings.Contains(low, "[extractaudio]") || strings.Contains(low, "[videoconvertor]"):
		return "Converting…", nil
	}
	return "", nil
}
