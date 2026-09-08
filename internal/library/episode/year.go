package episode

import (
	"strings"
	"time"
)

// YearFromUpload returns the UTC calendar year used as {year} (year-season, e.g. 2026).
// Undated or unparseable upload returns 0. Accepts RFC3339 / RFC3339Nano.
func YearFromUpload(upload string) int {
	t, ok := parseUploadTime(upload)
	if !ok {
		return 0
	}
	return t.UTC().Year()
}

// YearFromCalendarDay parses YYYY-MM-DD as UTC midnight and returns the year, or 0.
func YearFromCalendarDay(dayYYYYMMDD string) int {
	dayYYYYMMDD = strings.TrimSpace(dayYYYYMMDD)
	if dayYYYYMMDD == "" {
		return 0
	}
	t, ok := parseUploadTime(dayYYYYMMDD + "T00:00:00Z")
	if !ok {
		return 0
	}
	return t.UTC().Year()
}

func parseUploadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
