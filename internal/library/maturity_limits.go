package library

import (
	"fmt"
	"strconv"
	"strings"
)

// Maturity delay bounds on quality profiles (0 = that pass off).
const (
	MaxMaturityRedownloadHours = 168 // 7 days (API / clamp)
	MaxMaturitySidecarDays     = 365
	HoursPerDay                = 24
	MaxMaturitySidecarHours    = MaxMaturitySidecarDays * HoursPerDay

	// RecommendedMaturityMediaHours is the UI enable default for media delay.
	RecommendedMaturityMediaHours = 6
	// RecommendedMaturitySidecarDays is the UI enable default for sidecar delay.
	RecommendedMaturitySidecarDays = 14
)

// MaturityMediaHourPresets are legacy UI stops (hours). Index 0 = off.
var MaturityMediaHourPresets = []int{0, 1, 2, 6, 12, 24, 48}

// MaturitySidecarDayPresets are legacy UI stops (days). Index 0 = off.
var MaturitySidecarDayPresets = []int{0, 7, 14, 30, 90, 180, 365}

// ClampMaturityRedownloadHours clamps hours to 0..MaxMaturityRedownloadHours.
func ClampMaturityRedownloadHours(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxMaturityRedownloadHours {
		return MaxMaturityRedownloadHours
	}
	return n
}

// ClampMaturitySidecarHours clamps hours to 0..MaxMaturitySidecarHours.
func ClampMaturitySidecarHours(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxMaturitySidecarHours {
		return MaxMaturitySidecarHours
	}
	return n
}

// ClampMaturitySidecarDays clamps UI days to 0..MaxMaturitySidecarDays.
func ClampMaturitySidecarDays(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxMaturitySidecarDays {
		return MaxMaturitySidecarDays
	}
	return n
}

// MaturitySidecarDaysToHours converts UI days to DB hours.
func MaturitySidecarDaysToHours(days int) int {
	return ClampMaturitySidecarDays(days) * HoursPerDay
}

// MaturitySidecarHoursToDays converts DB hours to UI days (floor).
func MaturitySidecarHoursToDays(hours int) int {
	return ClampMaturitySidecarHours(hours) / HoursPerDay
}

// MaturityMediaHoursForPreset returns hours for a legacy preset index (clamped).
func MaturityMediaHoursForPreset(idx int) int {
	if idx < 0 {
		return MaturityMediaHourPresets[0]
	}
	if idx >= len(MaturityMediaHourPresets) {
		return MaturityMediaHourPresets[len(MaturityMediaHourPresets)-1]
	}
	return MaturityMediaHourPresets[idx]
}

// MaturityMediaPresetIndex returns the nearest legacy media preset index for hours.
func MaturityMediaPresetIndex(hours int) int {
	hours = ClampMaturityRedownloadHours(hours)
	best, bestDist := 0, absInt(hours-MaturityMediaHourPresets[0])
	for i, h := range MaturityMediaHourPresets {
		if dist := absInt(hours - h); dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}

// MaturityMediaLabel returns a short UI label for an hour count.
func MaturityMediaLabel(hours int) string {
	hours = ClampMaturityRedownloadHours(hours)
	if hours == 0 {
		return "off"
	}
	if hours%HoursPerDay == 0 {
		d := hours / HoursPerDay
		if d == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", d)
	}
	if hours == 1 {
		return "1h"
	}
	return fmt.Sprintf("%dh", hours)
}

// MaturitySidecarDaysForPreset returns days for a legacy slider index (clamped).
func MaturitySidecarDaysForPreset(idx int) int {
	if idx < 0 {
		return MaturitySidecarDayPresets[0]
	}
	if idx >= len(MaturitySidecarDayPresets) {
		return MaturitySidecarDayPresets[len(MaturitySidecarDayPresets)-1]
	}
	return MaturitySidecarDayPresets[idx]
}

// MaturitySidecarPresetIndex returns the nearest legacy preset index for days.
func MaturitySidecarPresetIndex(days int) int {
	days = ClampMaturitySidecarDays(days)
	best, bestDist := 0, absInt(days-MaturitySidecarDayPresets[0])
	for i, d := range MaturitySidecarDayPresets {
		if dist := absInt(days - d); dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}

// MaturitySidecarLabel returns a short UI label for a day count.
func MaturitySidecarLabel(days int) string {
	days = ClampMaturitySidecarDays(days)
	switch days {
	case 0:
		return "off"
	case 1:
		return "1 day"
	case 7:
		return "1 week"
	case 14:
		return "2 weeks"
	case 30:
		return "1 month"
	case 90:
		return "3 months"
	case 180:
		return "6 months"
	case 365:
		return "1 year"
	default:
		return fmt.Sprintf("%d days", days)
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ParseMaturityInt parses a form/API int string; empty → 0. Rejects out of range.
func ParseMaturityInt(raw string, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	if n < 0 || n > max {
		return 0, fmt.Errorf("must be between 0 and %d", max)
	}
	return n, nil
}
