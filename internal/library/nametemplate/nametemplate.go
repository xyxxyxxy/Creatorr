package nametemplate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Known tokens for Validate / Expand.
const (
	TokSeries  = "series"
	TokEpisode = "episode"
	TokTitle   = "title"
	TokID      = "id"
	TokDate    = "date"
	TokDomain  = "domain"
	TokYear    = "year"
	TokMonth   = "month"
	TokDay     = "day"
	TokHour    = "hour"
	TokMinute  = "minute"
)

var knownTokens = map[string]bool{
	TokSeries: true, TokEpisode: true, TokTitle: true, TokID: true,
	TokDate: true, TokDomain: true, TokYear: true, TokMonth: true, TokDay: true,
	TokHour: true, TokMinute: true,
}

// tokenRe matches {name} or {name:00} / {name:80} (pad zeros for ints; max runes for series/title).
var tokenRe = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)(?::([0-9]+))?\}`)

// Values holds fields for Expand.
type Values struct {
	Series  string
	Episode int
	Title   string
	ID      string
	// Date is release/upload day as YYYY-MM-DD for {date}; empty when unknown.
	Date string
	// Domain is source hostname (www. stripped) for {domain}; empty when unknown.
	Domain string
	// Year is the stored year-season (UTC calendar year). Undated year-season 0 renders as 0000 (min 4 digits).
	Year int
	// Month, Day are UTC calendar parts from release date (0 = unknown) for {month} {day}.
	Month int
	Day   int
	// Hour, Minute are UTC clock parts; HasClock false when undated (0 is valid midnight).
	Hour     int
	Minute   int
	HasClock bool
}

// Validate reports unknown tokens. Empty format is allowed (caller decides required).
func Validate(format string) error {
	matches := tokenRe.FindAllStringSubmatch(format, -1)
	for _, m := range matches {
		name := strings.ToLower(m[1])
		if !knownTokens[name] {
			return fmt.Errorf("unknown token {%s}", m[1])
		}
	}
	if strings.Contains(format, "{") || strings.Contains(format, "}") {
		cleaned := tokenRe.ReplaceAllString(format, "")
		if strings.Contains(cleaned, "{") || strings.Contains(cleaned, "}") {
			return fmt.Errorf("invalid token syntax in %q", format)
		}
	}
	return nil
}

// Expand replaces tokens. Numeric pads use the :00 suffix length.
func Expand(format string, v Values) (string, error) {
	if err := Validate(format); err != nil {
		return "", err
	}
	out := tokenRe.ReplaceAllStringFunc(format, func(tok string) string {
		m := tokenRe.FindStringSubmatch(tok)
		if m == nil {
			return tok
		}
		name := strings.ToLower(m[1])
		pad := m[2]
		switch name {
		case TokSeries:
			return v.Series
		case TokYear:
			return formatYear(v.Year, pad)
		case TokEpisode:
			return formatEpisode(v.Episode, pad)
		case TokTitle:
			return v.Title
		case TokID:
			return v.ID
		case TokDate:
			return v.Date
		case TokDomain:
			return v.Domain
		case TokMonth:
			return formatDatePart(v.Month, pad)
		case TokDay:
			return formatDatePart(v.Day, pad)
		case TokHour:
			return formatClockPart(v.Hour, v.HasClock, pad)
		case TokMinute:
			return formatClockPart(v.Minute, v.HasClock, pad)
		default:
			return tok
		}
	})
	return out, nil
}

func formatInt(n int, pad string) string {
	if pad == "" {
		return strconv.Itoa(n)
	}
	width := padWidth(pad)
	return fmt.Sprintf("%0*d", width, n)
}

// padWidth: decimal suffix when >0 (e.g. :04 → 4); else length of zero-string (:000 → 3).
func padWidth(pad string) int {
	if n, err := strconv.Atoi(pad); err == nil && n > 0 {
		return n
	}
	return len(pad)
}

// formatEpisode expands {episode}; bare form is unpadded.
func formatEpisode(n int, pad string) string {
	return formatInt(n, pad)
}

func formatYear(n int, pad string) string {
	if n != 0 {
		return formatInt(n, pad)
	}
	width := 4
	if pad != "" {
		width = padWidth(pad)
		if width < 4 {
			width = 4
		}
	}
	return fmt.Sprintf("%0*d", width, 0)
}

func formatDatePart(n int, pad string) string {
	if n <= 0 {
		return ""
	}
	return formatInt(n, pad)
}

func formatClockPart(n int, ok bool, pad string) string {
	if !ok {
		return ""
	}
	return formatInt(n, pad)
}

func titleMaxFromSuffix(pad string) int {
	if pad != "" {
		if n, err := strconv.Atoi(pad); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// SanitizeFilename hardens a path segment or stem for disk.
func SanitizeFilename(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	lastUnderscore := false
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if isSafeFilenameRune(r) {
			if r == ' ' || r == '\t' {
				if prevSpace || b.Len() == 0 {
					continue
				}
				b.WriteByte(' ')
				prevSpace = true
				lastUnderscore = false
				continue
			}
			prevSpace = false
			lastUnderscore = false
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 && !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
		prevSpace = false
	}
	s = strings.Trim(b.String(), " .")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "untitled"
	}
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		runes := []rune(s)
		s = string(runes[:maxRunes])
		s = strings.Trim(s, " .")
		if s == "" {
			s = "untitled"
		}
	}
	return s
}

func isSafeFilenameRune(r rune) bool {
	switch r {
	case '-', '_', '.', ',', '(', ')', '[', ']', '\'', ' ':
		return true
	}
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return true
	}
	return false
}

// ExpandAndSanitize expands tokens with per-field sanitize.
func ExpandAndSanitize(format string, v Values) (string, error) {
	if err := Validate(format); err != nil {
		return "", err
	}
	out := tokenRe.ReplaceAllStringFunc(format, func(tok string) string {
		m := tokenRe.FindStringSubmatch(tok)
		if m == nil {
			return tok
		}
		name := strings.ToLower(m[1])
		pad := m[2]
		switch name {
		case TokSeries:
			return SanitizeFilename(v.Series, titleMaxFromSuffix(pad))
		case TokYear:
			return formatYear(v.Year, pad)
		case TokEpisode:
			return formatEpisode(v.Episode, pad)
		case TokTitle:
			return SanitizeFilename(v.Title, titleMaxFromSuffix(pad))
		case TokID:
			return SanitizeFilename(v.ID, 64)
		case TokDate:
			if strings.TrimSpace(v.Date) == "" {
				return ""
			}
			return SanitizeFilename(v.Date, 32)
		case TokDomain:
			if strings.TrimSpace(v.Domain) == "" {
				return ""
			}
			return SanitizeFilename(v.Domain, 64)
		case TokMonth:
			return formatDatePart(v.Month, pad)
		case TokDay:
			return formatDatePart(v.Day, pad)
		case TokHour:
			return formatClockPart(v.Hour, v.HasClock, pad)
		case TokMinute:
			return formatClockPart(v.Minute, v.HasClock, pad)
		default:
			return tok
		}
	})
	return SanitizeFilename(out, 200), nil
}
