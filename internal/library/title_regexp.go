package library

import (
	"fmt"
	"regexp"
	"strings"
)

// Skip / auto-ignore reasons on UpsertResult and scan detail.
const (
	SkipReasonTitleRegexpInclude = "title_regexp_include" // create skipped; not indexed
	SkipReasonTitleRegexpExclude = "title_regexp_exclude" // create skipped; not indexed
	IgnoreReasonIndexAsIgnored   = "index_as_ignored"
)

// ValidateTitleRegexp accepts empty (no filter) or a compilable Go regexp.
// field is used in the error message (e.g. title_regexp_include).
func ValidateTitleRegexp(field, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if field == "" {
		field = "title_regexp"
	}
	if _, err := regexp.Compile(raw); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalid, field, err)
	}
	return nil
}

// TitleMatchesFilter reports whether title matches pattern.
// Empty pattern means no filter (always matches).
func TitleMatchesFilter(pattern, title string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Invalid patterns are rejected on save; treat as non-match if one slips through.
		return false
	}
	return re.MatchString(title)
}

// TitlePassesFilters reports whether title may be indexed.
// Include (if set) must match; exclude (if set) must not. Exclude wins when both match.
func TitlePassesFilters(include, exclude, title string) (ok bool, skipReason string) {
	if !TitleMatchesFilter(include, title) {
		return false, SkipReasonTitleRegexpInclude
	}
	ex := strings.TrimSpace(exclude)
	if ex != "" && TitleMatchesFilter(ex, title) {
		return false, SkipReasonTitleRegexpExclude
	}
	return true, ""
}
