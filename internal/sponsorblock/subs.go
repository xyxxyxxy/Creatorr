package sponsorblock

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	srtTimeRe = regexp.MustCompile(`(?i)^(\d{2}):(\d{2}):(\d{2}),(\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2}),(\d{3})`)
	vttTimeRe = regexp.MustCompile(`(?i)^(\d{2}):(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2})\.(\d{3})`)
)

// RemapSubtitleFile rewrites cue times through cuts (and optional cards) in place.
func RemapSubtitleFile(path string, cuts []Segment, cardDur float64, withCards bool) error {
	if len(cuts) == 0 {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out string
	switch ext {
	case ".srt":
		out = remapSRT(string(b), cuts, cardDur, withCards)
	case ".vtt":
		out = remapVTT(string(b), cuts, cardDur, withCards)
	default:
		return nil // soft-skip unsupported
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// RemapSubtitleFiles remaps each existing subtitle path.
func RemapSubtitleFiles(paths []string, cuts []Segment, cardDur float64, withCards bool) {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		_ = RemapSubtitleFile(p, cuts, cardDur, withCards)
	}
}

func remapSRT(in string, cuts []Segment, cardDur float64, withCards bool) string {
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(in))
	idx := 1
	for sc.Scan() {
		line := sc.Text()
		m := srtTimeRe.FindStringSubmatch(line)
		if m == nil {
			// drop bare cue numbers; rewrite sequentially when we emit a cue
			if strings.TrimSpace(line) != "" && isAllDigits(strings.TrimSpace(line)) {
				continue
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		start := hmsToSec(m[1], m[2], m[3], m[4])
		end := hmsToSec(m[5], m[6], m[7], m[8])
		ns, ne, ok := mapCue(start, end, cuts, cardDur, withCards)
		if !ok {
			// skip cue body until blank
			for sc.Scan() {
				if strings.TrimSpace(sc.Text()) == "" {
					break
				}
			}
			continue
		}
		fmt.Fprintf(&out, "%d\n", idx)
		idx++
		out.WriteString(formatSRT(ns) + " --> " + formatSRT(ne) + "\n")
	}
	return out.String()
}

func remapVTT(in string, cuts []Segment, cardDur float64, withCards bool) string {
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(in))
	for sc.Scan() {
		line := sc.Text()
		m := vttTimeRe.FindStringSubmatch(line)
		if m == nil {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		start := hmsToSec(m[1], m[2], m[3], m[4])
		end := hmsToSec(m[5], m[6], m[7], m[8])
		ns, ne, ok := mapCue(start, end, cuts, cardDur, withCards)
		if !ok {
			for sc.Scan() {
				if strings.TrimSpace(sc.Text()) == "" {
					break
				}
			}
			continue
		}
		rest := ""
		if i := strings.Index(line, "-->"); i >= 0 {
			after := strings.TrimSpace(line[i+3:])
			fields := strings.Fields(after)
			if len(fields) > 1 {
				rest = " " + strings.Join(fields[1:], " ")
			}
		}
		out.WriteString(formatVTT(ns) + " --> " + formatVTT(ne) + rest + "\n")
	}
	return out.String()
}

func mapCue(start, end float64, cuts []Segment, cardDur float64, withCards bool) (float64, float64, bool) {
	if withCards && cardDur > 0 {
		for _, c := range cuts {
			if start >= c.Start && end <= c.End {
				return 0, 0, false
			}
		}
		ns := MapTimeWithCards(start, cuts, cardDur)
		ne := MapTimeWithCards(end, cuts, cardDur)
		if ne <= ns {
			return 0, 0, false
		}
		return ns, ne, true
	}
	return RemapSubtitleCue(start, end, cuts)
}

func hmsToSec(hh, mm, ss, ms string) float64 {
	h, _ := strconv.Atoi(hh)
	m, _ := strconv.Atoi(mm)
	s, _ := strconv.Atoi(ss)
	milli, _ := strconv.Atoi(ms)
	return float64(h*3600+m*60+s) + float64(milli)/1000
}

func formatSRT(t float64) string {
	if t < 0 {
		t = 0
	}
	ms := int((t - float64(int(t))) * 1000)
	total := int(t)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func formatVTT(t float64) string {
	if t < 0 {
		t = 0
	}
	ms := int((t - float64(int(t))) * 1000)
	total := int(t)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
