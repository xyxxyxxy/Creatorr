package ytdlp

import (
	"regexp"
	"strings"
)

const maxFailDetailLen = 1500

var (
	failLineRe = regexp.MustCompile(`(?i)^(ERROR:|WARNING:)`)
	// yt-dlp progress / fragment noise that drowns the real ERROR in tails.
	progressNoiseRe = regexp.MustCompile(`(?i)^\[download\].*(%\s|ETA\s|at\s+\d)`)
)

// formatFailDetail picks operator-useful failure text from yt-dlp stream output.
// Prefer ERROR:/WARNING: lines (plus short non-progress context after the last
// match); otherwise keep a line-boundary truncated tail. Cap length at maxFailDetailLen.
func formatFailDetail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := splitLinesKeep(raw)
	var kept []string
	lastFailIdx := -1
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if failLineRe.MatchString(trim) {
			kept = append(kept, trim)
			lastFailIdx = len(kept) - 1
			continue
		}
	}
	if lastFailIdx >= 0 {
		// Append up to 3 non-progress lines that follow the last ERROR/WARNING
		// in the original stream (extractor extras, exit hints).
		after := linesAfterLastFail(lines)
		extra := 0
		for _, line := range after {
			if extra >= 3 {
				break
			}
			trim := strings.TrimSpace(line)
			if trim == "" || progressNoiseRe.MatchString(trim) || failLineRe.MatchString(trim) {
				continue
			}
			kept = append(kept, trim)
			extra++
		}
		return truncateTailLines(strings.Join(kept, "\n"), maxFailDetailLen)
	}
	// No ERROR/WARNING: drop progress noise, then line-boundary truncate.
	var filtered []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || progressNoiseRe.MatchString(trim) {
			continue
		}
		filtered = append(filtered, trim)
	}
	if len(filtered) == 0 {
		return truncateTailLines(raw, maxFailDetailLen)
	}
	return truncateTailLines(strings.Join(filtered, "\n"), maxFailDetailLen)
}

func linesAfterLastFail(lines []string) []string {
	last := -1
	for i, line := range lines {
		if failLineRe.MatchString(strings.TrimSpace(line)) {
			last = i
		}
	}
	if last < 0 || last+1 >= len(lines) {
		return nil
	}
	return lines[last+1:]
}

func splitLinesKeep(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// truncateTailLines keeps the end of s up to max bytes, starting at a line boundary.
func truncateTailLines(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := s[len(s)-max:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i+1 < len(cut) {
		cut = cut[i+1:]
	}
	return strings.TrimSpace(cut)
}
