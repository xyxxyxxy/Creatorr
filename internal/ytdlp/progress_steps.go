package ytdlp

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// formatsInfoRe matches yt-dlp "[info] … Downloading N format(s): id[+id…]".
// yt-dlp prints the literal text "format(s)" (parentheses included).
// That line is the ground truth after -f selection (including / fallbacks).
var formatsInfoRe = regexp.MustCompile(`(?i)downloading\s+(\d+)\s+format(?:s|\(s\))?\s*:\s*(\S+)`)

// StepProgress turns yt-dlp download lines into labeled per-format progress.
// The bar fraction is the raw 0..1 for the current format (resets each step);
// multi-format messages carry role + step (e.g. "Downloading video (1/2) 42% · 1.00MiB/s").
//
// Do not seed total from the -f selector: a primary `bv*+ba` with `/b` fallback
// may still resolve to one format. Trust the info line (and later Destinations).
type StepProgress struct {
	total   int // 0 = unknown until info line or a second format appears
	step    int // 1-based; 0 = not started
	role    string
	lastRaw float64
	peak    float64 // highest raw in this step (HLS fragments must not rewind the bar)
	seenPct bool
	hasDest bool // Destination lines drive steps when present
}

// Feed consumes one yt-dlp output line. ok means onProgress should be called.
// frac may be nil for message-only / busy UI (Destination, 0%, Merging…).
func (s *StepProgress) Feed(line string) (msg string, frac *float64, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil, false
	}
	low := strings.ToLower(line)

	if n := parseFormatsInfoTotal(line); n > 0 {
		s.raiseTotal(n)
		return "", nil, false
	}

	if strings.Contains(low, "[download]") && strings.Contains(low, "destination") {
		s.hasDest = true
		s.beginStep(roleFromPath(destinationPath(line)))
		// Nil fraction → busy indeterminate; do not send 0% (flips the bar every Dest).
		return s.message(nil, ""), nil, true
	}

	if strings.Contains(low, "[merger]") || strings.Contains(low, "merging formats") {
		return "Merging…", nil, true
	}
	if strings.Contains(low, "[extractaudio]") || strings.Contains(low, "[videoconvertor]") {
		return "Converting…", nil, true
	}

	if m := pctRe.FindStringSubmatch(line); m != nil {
		pct, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return "", nil, false
		}
		raw := pct / 100.0
		if raw < 0 {
			raw = 0
		}
		if raw > 1 {
			raw = 1
		}
		speed := parseDownloadSpeed(line)
		// Without Destination lines, a sharp drop means a new format file.
		if !s.hasDest && s.seenPct && raw+0.2 < s.lastRaw && s.lastRaw > 0.5 {
			s.beginStep("")
		} else if s.step == 0 {
			s.beginStep("")
		}
		s.lastRaw = raw
		s.seenPct = true
		s.applyRole()
		// Still at 0%: keep busy (nil). HLS fragment restarts stay at peak.
		if raw <= 0 && s.peak <= 0 {
			return s.message(nil, ""), nil, true
		}
		if raw < s.peak {
			raw = s.peak
		} else {
			s.peak = raw
		}
		return s.message(&raw, speed), &raw, true
	}
	return "", nil, false
}

// parseFormatsInfoTotal returns the best known format-file count from an info line.
// Prefers the id list after the colon (96+95 → 2) over a wrong leading N.
func parseFormatsInfoTotal(line string) int {
	m := formatsInfoRe.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		n = 0
	}
	ids := strings.TrimSpace(m[2])
	idCount := 0
	if ids != "" {
		idCount = strings.Count(ids, "+") + 1
	}
	if idCount > n {
		return idCount
	}
	return n
}

func (s *StepProgress) raiseTotal(n int) {
	if n > s.total {
		s.total = n
	}
}

func (s *StepProgress) beginStep(role string) {
	if s.step == 0 {
		s.step = 1
	} else {
		s.step++
	}
	s.raiseTotal(s.step)
	if s.total == 0 && s.step >= 2 {
		s.total = 2
	}
	s.lastRaw = 0
	s.peak = 0
	s.seenPct = false
	s.role = role
	s.applyRole()
}

func (s *StepProgress) applyRole() {
	// Multi-format: yt-dlp bv*+ba order is video then audio. Prefer step index
	// over Destination extension (.webm/.mp4 audio would otherwise stay "video").
	if s.total > 1 && s.step > 0 {
		if s.step == 1 {
			s.role = "video"
		} else if s.total == 2 {
			s.role = "audio"
		} else if s.role == "" {
			s.role = fmt.Sprintf("part%d", s.step)
		}
		return
	}
	// Single-format messages omit the role word.
	s.role = ""
}

func (s *StepProgress) message(pct *float64, speed string) string {
	var b strings.Builder
	b.WriteString("Downloading")
	total := s.total
	if total == 0 && s.step > 1 {
		total = s.step
	}
	// Single-format: plain "Downloading 72%". Multi-format: role + (n/m).
	if total > 1 {
		if s.role != "" {
			b.WriteByte(' ')
			b.WriteString(s.role)
		}
		if s.step > 0 {
			fmt.Fprintf(&b, " (%d/%d)", s.step, total)
		}
	}
	if pct != nil {
		fmt.Fprintf(&b, " %.0f%%", *pct*100)
		if speed != "" {
			fmt.Fprintf(&b, " · %s", speed)
		}
	} else if total <= 1 {
		b.WriteString("…")
	}
	return b.String()
}

func destinationPath(line string) string {
	const key = "destination:"
	low := strings.ToLower(line)
	i := strings.Index(low, key)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(line[i+len(key):])
}

func roleFromPath(path string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	if base == "" {
		return ""
	}
	switch filepath.Ext(base) {
	case ".m4a", ".opus", ".ogg", ".mp3", ".aac", ".wma", ".flac", ".wav", ".mka":
		return "audio"
	case ".mp4", ".m4v", ".mkv", ".mov", ".avi", ".flv", ".ts", ".webm":
		return "video"
	}
	return ""
}
