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
	State  string // off | eligible | hashed | monitored | failed
	Tip    string
	LastOK string // relative ago when media ok_at set; empty otherwise
}

// integrityIndicatorState picks the chip state from video + profile + hash + schedule + derived file fail.
// Priority: failed (status or any derived-failed file) → off → eligible → hashed → monitored.
func integrityIndicatorState(videoStatus string, verifyMedia, hasHash, scheduleOn, anyFileFailed bool) string {
	if videoStatus == "downloaded_integrity_failed" || anyFileFailed {
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

// integrityIndicatorTip is state-only (no last-checked clause).
func integrityIndicatorTip(state, videoStatus string, verifyMedia bool) string {
	return integrityIndicatorBaseTip(state, videoStatus, verifyMedia)
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
		if videoStatus != "downloaded" && videoStatus != "downloaded_integrity_failed" {
			return "No packed media"
		}
		if !verifyMedia {
			return "This series quality profile has 'File integrity' turned off"
		}
		return "No packed media"
	}
}

func buildIntegrityIndicatorView(state, videoStatus string, verifyMedia bool, lastOKAt string, now time.Time) integrityIndicatorView {
	lastOK := ""
	if strings.TrimSpace(lastOKAt) != "" {
		if _, ago := createdAgoPairShort(lastOKAt, now); ago != "" {
			lastOK = ago
		}
	}
	return integrityIndicatorView{
		State:  state,
		Tip:    integrityIndicatorTip(state, videoStatus, verifyMedia),
		LastOK: lastOK,
	}
}
