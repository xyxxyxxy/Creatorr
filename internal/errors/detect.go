package errors

import (
	"regexp"
	"strings"
)

// Per-video age gates (not domain cookie/session failure).
var ageRestrictRe = regexp.MustCompile(`(?i)(` +
	`verify\s+your\s+age|` +
	`confirm\s+your\s+age|` +
	`confirm\s+you.?re\s+an\s+adult|` +
	`age[-\s]?restricted|` +
	`sign\s+in\s+to\s+confirm\s+your\s+age` +
	`)`)

// Pause-worthy external-service failures (domain queue should stop).
var (
	cookieAuthRe = regexp.MustCompile(`(?i)(` +
		`cookies?\s+are\s+no\s+longer\s+valid|` +
		`cookie.*expired|expired.*cookie|` +
		`sign\s+in\s+to\s+confirm|` +
		`login\s+required|` +
		`please\s+sign\s+in|` +
		`re-?export\s+cookies|` +
		`token\s+refresh\s+failed|` +
		`missing\s+.*token|` +
		`missing\s+cookies` +
		`)`)

	// Tier paywalls ("outside your membership tier") must not match: those are
	// per-video product gaps, not domain cookie/session failure.
	cookieHTTPRe = regexp.MustCompile(`(?i)` +
		`HTTP\s*(?:Error\s*)?(401|403).*(cookie|login|auth|sign\s*in)|` +
		`(cookie|login|auth|sign\s*in).*HTTP\s*(?:Error\s*)?(401|403)`)

	rateLimitRe = regexp.MustCompile(`(?i)(` +
		`HTTP\s*Error\s*429|` +
		`status(?:\s+code)?\s*[:=]?\s*429|` +
		`\b429\b.{0,40}too many|` +
		`too many requests|` +
		`rate[-\s]?limit(?:ed|ing)?|` +
		`ratelimited|` +
		`ip[-\s]?(?:address\s+)?(?:has been |is )?blocked|` +
		`blocked your ip|` +
		`your ip (?:has been |is )?blocked|` +
		`temporarily blocked|` +
		`quota exceeded|` +
		`exceeded your?\s+(?:rate|quota)|` +
		`slow down` +
		`)`)
)

// DetectAgeRestricted reports per-video age-gate failures in yt-dlp stderr.
func DetectAgeRestricted(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	return ageRestrictRe.MatchString(message)
}

// IsSourceCountableDownloadError reports whether a download failure code
// counts toward source_download_error_threshold.
func IsSourceCountableDownloadError(code string) bool {
	return code != CodeAgeRestricted
}

// DetectPauseCode inspects external-tool stderr / error text.
// Returns CookieInvalid, RateLimited, or "" if not a pause trigger.
func DetectPauseCode(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	if DetectAgeRestricted(message) {
		return ""
	}
	if cookieAuthRe.MatchString(message) || cookieHTTPRe.MatchString(message) {
		return CodeCookieInvalid
	}
	if rateLimitRe.MatchString(message) {
		return CodeRateLimited
	}
	return ""
}

// UpgradeCode replaces a generic failure code when message indicates pause.
// Keeps CookieInvalid / RateLimited / CookieMissing unchanged.
func UpgradeCode(code, message string) string {
	switch code {
	case CodeCookieInvalid, CodeRateLimited, CodeCookieMissing, CodeRemuxFailed, CodePackFailed, CodeMediaVerifyFailed,
		CodeMediaTypeExcluded, CodeLiveBroadcastSkipped, CodeAgeRestricted:
		return code
	}
	if DetectAgeRestricted(message) {
		return CodeAgeRestricted
	}
	if d := DetectPauseCode(message); d != "" {
		return d
	}
	return code
}

// PauseMessage is the short user-facing message for cookie/rate-limit style codes.
func PauseMessage(code string) string {
	switch code {
	case CodeCookieInvalid:
		return "Cookies invalid"
	case CodeRateLimited:
		return "Rate limited or IP blocked"
	case CodeAgeRestricted:
		return "Age restricted"
	default:
		return "Domain issue"
	}
}

// IsYtDlpPauseCode reports whether a classified failure should soft-pause the domain lane.
// Remux/pack/verify are not yt-dlp-facing and return false.
func IsYtDlpPauseCode(code string) bool {
	switch code {
	case CodeCookieInvalid, CodeRateLimited, CodeDownloadFailed, CodeResolveFailed:
		return true
	default:
		return false
	}
}
