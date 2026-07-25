package settings

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Download rate units for yt-dlp --limit-rate (K/M/G binary suffixes).
const (
	RateUnitK   = "K"
	RateUnitM   = "M"
	RateUnitG   = "G"
	RateUnitOff = "off"
)

var rateLimitRE = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*([kmg])$`)

// SplitDownloadRateLimit splits a stored yt-dlp rate string into number + unit.
// Unit is K, M, G, or off. Empty input → empty parts (override inherit).
// off / 0 / none / unlimited → unit off with empty value.
func SplitDownloadRateLimit(raw string) (value, unit string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	lower := strings.ToLower(s)
	if lower == "off" || lower == "0" || lower == "none" || lower == "unlimited" {
		return "", RateUnitOff
	}
	m := rateLimitRE.FindStringSubmatch(s)
	if m == nil {
		// Unrecognized: leave value empty so the form does not invent a bad number.
		return "", RateUnitM
	}
	return m[1], strings.ToUpper(m[2])
}

// CombineDownloadRateLimit builds a yt-dlp --limit-rate string from form parts.
// Empty value+unit → "" (override inherit). unit off → "off".
func CombineDownloadRateLimit(value, unit string) (string, error) {
	value = strings.TrimSpace(value)
	unit = strings.TrimSpace(unit)
	if value == "" && unit == "" {
		return "", nil
	}
	if strings.EqualFold(unit, RateUnitOff) {
		return RateUnitOff, nil
	}
	u := strings.ToUpper(unit)
	switch u {
	case RateUnitK, RateUnitM, RateUnitG:
	default:
		return "", fmt.Errorf("download_rate_limit: unit must be K, M, G, or off")
	}
	if value == "" {
		return "", fmt.Errorf("download_rate_limit: value required")
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("download_rate_limit: value must be a number > 0")
	}
	// Preserve integer form when possible (matches existing defaults like 10M).
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10) + u, nil
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + u, nil
}

// CombineDownloadRateLimitOverride builds a host override rate from form parts.
// Empty number inherits Domain defaults (unit is display-only unless Unlimited).
// Unlimited with empty number overrides to off when the default is not already off.
func CombineDownloadRateLimitOverride(value, unit, defaultRate string) (string, error) {
	value = strings.TrimSpace(value)
	unit = strings.TrimSpace(unit)
	if value == "" {
		if strings.EqualFold(unit, RateUnitOff) {
			_, defUnit := SplitDownloadRateLimit(defaultRate)
			if defUnit != RateUnitOff {
				return RateUnitOff, nil
			}
		}
		return "", nil
	}
	return CombineDownloadRateLimit(value, unit)
}
