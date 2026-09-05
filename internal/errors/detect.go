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

	// Per-video gone / removed (not domain cookie or rate). Narrow gate for archive fallback.
	videoUnavailableRe = regexp.MustCompile(`(?i)(` +
		`video\s+unavailable|` +
		`this\s+video\s+(?:is\s+)?(?:no\s+longer\s+)?available|` +
		`this\s+video\s+has\s+been\s+removed|` +
		`has\s+been\s+removed\s+by\s+the\s+(?:uploader|user)|` +
		`video\s+has\s+been\s+removed|` +
		`has\s+been\s+deleted|` +
		`account\s+associated\s+with\s+this\s+video\s+has\s+been\s+terminated|` +
		`uploader\s+has\s+closed\s+their\s+youtube\s+account` +
		`)`)
)

// DetectAgeRestricted reports per-video age-gate failures in yt-dlp stderr.
func DetectAgeRestricted(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	return ageRestrictRe.MatchString(message)
}

// DetectVideoUnavailable reports clear per-video gone/removed failures in yt-dlp stderr.
// Narrow gate only: not cookie/rate/age. Used to enqueue Web Archive fallback.
func DetectVideoUnavailable(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	if DetectAgeRestricted(message) {
		return false
	}
	if DetectPauseCode(message) != "" {
		return false
	}
	return videoUnavailableRe.MatchString(message)
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
		CodeLiveBroadcastSkipped, CodeAgeRestricted, CodeArchiveFallbackQueued:
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
// Only cookie/session and rate-limit/IP-block failures pause the hostname queue.
// Generic DownloadFailed / ResolveFailed stay per-task (and per-video for downloads);
// remux/pack/verify/age-gate are never pause codes.
func IsYtDlpPauseCode(code string) bool {
	switch code {
	case CodeCookieInvalid, CodeRateLimited:
		return true
	default:
		return false
	}
}
