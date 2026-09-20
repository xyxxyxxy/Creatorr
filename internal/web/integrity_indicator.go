package web

import (
	"strings"
	"time"
)

// Integrity indicator states for video detail File integrity row.
const (
	integrityIndOff       = "off"
	integrityIndEligible  = "eligible"
	integrityIndHashed    = "hashed"
	integrityIndMonitored = "monitored"
	integrityIndFailed    = "failed"
)

// integrityIndicatorView drives partials/integrity_indicator.html.
type integrityIndicatorView struct {
	State string // off | eligible | hashed | monitored | failed
	Tip   string
}

// integrityIndicatorState picks the chip state from video + profile + hash + schedule.
// Priority: failed → off → eligible → hashed → monitored.
func integrityIndicatorState(videoStatus string, verifyMedia, hasHash, scheduleOn bool) string {
	if videoStatus == "integrity_check_failed" {
		return integrityIndFailed
	}
	if videoStatus != "downloaded" {
		return integrityIndOff
	}
	if !verifyMedia {
		return integrityIndOff
	}
	if !hasHash {
		return integrityIndEligible
	}
	if !scheduleOn {
		return integrityIndHashed
	}
	return integrityIndMonitored
}

// integrityScheduleOn is true when integrity_check_cron is set (empty / never = off).
func integrityScheduleOn(cron string) bool {
	c := strings.TrimSpace(cron)
	return c != "" && !strings.EqualFold(c, "never")
}

// integrityIndicatorTip builds data-tip / aria-label text.
// lastCheckedAt empty string means omit the last-checked clause.
// Off never shows last-checked (stale history while profile/feature off).
func integrityIndicatorTip(state, videoStatus string, verifyMedia bool, lastCheckedAt string, now time.Time) string {
	base := integrityIndicatorBaseTip(state, videoStatus, verifyMedia)
	if state == integrityIndOff || lastCheckedAt == "" {
		return base
	}
	_, ago := createdAgoPairShort(lastCheckedAt, now)
	if ago == "" {
		return base
	}
	return base + " · Last checked " + ago
}

func integrityIndicatorBaseTip(state, videoStatus string, verifyMedia bool) string {
	switch state {
	case integrityIndFailed:
		return "Integrity check failed"
	case integrityIndEligible:
		return "'File integrity' eligible; no hash yet"
	case integrityIndHashed:
		return "Hash present; Integrity check schedule off"
	case integrityIndMonitored:
		return "'File integrity' monitored"
	default: // off
		if videoStatus != "downloaded" && videoStatus != "integrity_check_failed" {
			return "No packed media"
		}
		if !verifyMedia {
			return "This series quality profile has 'File integrity' turned off"
		}
		return "No packed media"
	}
}

func buildIntegrityIndicatorView(state, videoStatus string, verifyMedia bool, lastCheckedAt string, now time.Time) integrityIndicatorView {
	return integrityIndicatorView{
		State: state,
		Tip:   integrityIndicatorTip(state, videoStatus, verifyMedia, lastCheckedAt, now),
	}
}
