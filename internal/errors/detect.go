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

// Per-video membership / tier paywalls (not domain cookie/session failure).
// Site-shaped arms: add a new alt when another extractor grows a stable message.
var memberOnlyRe = regexp.MustCompile(`(?i)(` +
	// YouTube members-only
	`channel'?s?\s+members|` +
	`members\s+on\s+level|` +
	`join\s+this\s+channel\s+to\s+get\s+access|` +
	`members?[-\s]?only|` +
	// mde.tv tier (not "membership login required" / session expired)
	`outside\s+your\s+membership\s+tier` +
	`)`)

// Pause-worthy external-service failures (domain queue should stop).
var (
	// Strong cookie/session failure: wins over age-gate wording in the same stderr
	// (yt-dlp often prints both when a stale jar is used on an age-gated video).
	cookieStrongRe = regexp.MustCompile(`(?i)(` +
		`cookies?\s+are\s+no\s+longer\s+valid|` +
		`cookie.*expired|expired.*cookie|` +
		`re-?export\s+cookies|` +
		`token\s+refresh\s+failed|` +
		`missing\s+.*token|` +
		`missing\s+cookies` +
		`)`)

	// Weaker auth prompts. "Sign in to confirm your age" is age-only (see DetectPauseCode).
	cookieAuthRe = regexp.MustCompile(`(?i)(` +
		`sign\s+in\s+to\s+confirm|` +
		`login\s+required|` +
		`please\s+sign\s+in|` +
		`private\s+video` +
		`)`)

	// Tier paywalls ("outside your membership tier") must not match: those are
	// per-video MemberOnly product gaps, not domain cookie/session failure.
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
	// Require "no longer" on the "this video is … available" arm so YouTube members-only
	// ("This video is available to this channel's members…") does not match.
	videoUnavailableRe = regexp.MustCompile(`(?i)(` +
		`video\s+unavailable|` +
		`this\s+video\s+(?:is\s+)?no\s+longer\s+available|` +
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

// DetectMemberOnly reports per-video membership / tier paywall failures in yt-dlp stderr.
func DetectMemberOnly(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	return memberOnlyRe.MatchString(message)
}

// DetectVideoUnavailable reports clear per-video gone/removed failures in yt-dlp stderr.
// Narrow gate only: not cookie/rate/age/member. Used to enqueue Web Archive fallback.
func DetectVideoUnavailable(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	if DetectAgeRestricted(message) {
		return false
	}
	if DetectMemberOnly(message) {
		return false
	}
	if DetectPauseCode(message) != "" {
		return false
	}
	return videoUnavailableRe.MatchString(message)
}

// DetectPauseCode inspects external-tool stderr / error text.
// Returns CookieInvalid, RateLimited, or "" if not a pause trigger.
// Strong cookie/session lines beat age-gate text in the same blob.
func DetectPauseCode(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	if cookieStrongRe.MatchString(message) || cookieHTTPRe.MatchString(message) {
		return CodeCookieInvalid
	}
	if DetectAgeRestricted(message) {
		return ""
	}
	if DetectMemberOnly(message) {
		return ""
	}
	if cookieAuthRe.MatchString(message) {
		return CodeCookieInvalid
	}
	if rateLimitRe.MatchString(message) {
		return CodeRateLimited
	}
	return ""
}

// UpgradeCode replaces a generic failure code when message indicates pause, age, or member gate.
// CookieInvalid / RateLimited win over AgeRestricted / MemberOnly when both appear in the message.
// Keeps CookieInvalid / RateLimited / CookieMissing / remux/pack/verify / live-skip / archive unchanged.
func UpgradeCode(code, message string) string {
	switch code {
	case CodeCookieInvalid, CodeRateLimited, CodeCookieMissing, CodeRemuxFailed, CodePackFailed, CodeIntegrityCheckFailed,
		CodeLiveBroadcastSkipped, CodeArchiveFallbackQueued:
		return code
	case CodeAgeRestricted, CodeMemberOnly:
		// Prior per-video label may have been set before cookie lines were considered.
		if d := DetectPauseCode(message); d != "" {
			return d
		}
		return code
	}
	if d := DetectPauseCode(message); d != "" {
		return d
	}
	if DetectAgeRestricted(message) {
		return CodeAgeRestricted
	}
	if DetectMemberOnly(message) {
		return CodeMemberOnly
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
	case CodeMemberOnly:
		return "Members only"
	default:
		return "Domain issue"
	}
}

// IsYtDlpPauseCode reports whether a classified failure should soft-pause the domain lane.
// Only cookie/session and rate-limit/IP-block failures pause the hostname queue.
// Generic DownloadFailed / ResolveFailed stay per-task (and per-video for downloads);
// remux/pack/verify/age-gate/member-only are never pause codes.
func IsYtDlpPauseCode(code string) bool {
	switch code {
	case CodeCookieInvalid, CodeRateLimited:
		return true
	default:
		return false
	}
}
