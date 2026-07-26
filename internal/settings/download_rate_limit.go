package settings

import (
	"fmt"
	"math"
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

// downloadRateBytesPerSec converts a stored rate (e.g. 10M, off) to bytes/s.
// Unlimited (off) is +Inf. Empty or unrecognized → error.
func downloadRateBytesPerSec(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("rate required")
	}
	if RateLimitOff(s) {
		return math.Inf(1), nil
	}
	v, u := SplitDownloadRateLimit(s)
	if v == "" || u == "" || u == RateUnitOff {
		return 0, fmt.Errorf("invalid rate %q", raw)
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid rate %q", raw)
	}
	var mul float64
	switch u {
	case RateUnitK:
		mul = 1024
	case RateUnitM:
		mul = 1024 * 1024
	case RateUnitG:
		mul = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("invalid rate %q", raw)
	}
	return n * mul, nil
}

// ValidateStreamPlayRateAgainstDownload requires stream play rate ≥ download rate
// (Unlimited is highest). Both must be non-empty stored rates.
func ValidateStreamPlayRateAgainstDownload(download, streamPlay string) error {
	d, err := downloadRateBytesPerSec(download)
	if err != nil {
		return fmt.Errorf("download_rate_limit: %w", err)
	}
	s, err := downloadRateBytesPerSec(streamPlay)
	if err != nil {
		return fmt.Errorf("stream_play_rate_limit: %w", err)
	}
	if s < d {
		return fmt.Errorf("stream play rate limit cannot be lower than download rate limit")
	}
	return nil
}
