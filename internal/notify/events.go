package notify

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	apprise "github.com/unraid/apprise-go"
)

// Event ids stored on notification_channels.events and used by SendEvent.
const (
	EventCookieInvalid  = "cookie_invalid"
	EventRateLimited    = "rate_limited"
	EventYtDlpFailed    = "ytdlp_failed"
	EventVerifyFailed   = "verify_failed"
	EventFileSyncIssues = "file_sync_issues"
	EventPOTProvider    = "pot_provider"
	EventDownloadDigest = "download_digest"
	EventLiveSkipped    = "live_skipped"
)

// Notification levels (in-app icon / API). Warning matches alert for unread behavior.
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelAlert   = "alert"
)

// Legacy event ids (read-time aliases only; never written on new Upsert).
const (
	legacyEventDownloadFailed = "download_failed"
	legacyEventDownloadsDone  = "downloads_done"
)

// AllEvents is the closed set of selectable channel events (UI checkboxes).
var AllEvents = []string{
	EventDownloadDigest,
	EventLiveSkipped,
	EventYtDlpFailed,
	EventVerifyFailed,
	EventFileSyncIssues,
	EventCookieInvalid,
	EventRateLimited,
	EventPOTProvider,
}

// EventLabels are short UI labels for event checkboxes.
var EventLabels = map[string]string{
	EventCookieInvalid:  "Cookie / auth failure",
	EventRateLimited:    "Rate limit / IP block",
	EventYtDlpFailed:    "yt-dlp / site failure",
	EventVerifyFailed:   "Verify failed",
	EventFileSyncIssues: "File sync issues",
	EventPOTProvider:    "PO token provider",
	EventDownloadDigest: "Downloads finished (digest)",
	EventLiveSkipped:    "Live broadcast skipped",
}

// AlertEvents are unread-eligible failure notifications (red megaphone in UI).
var AlertEvents = []string{
	EventCookieInvalid,
	EventRateLimited,
	EventYtDlpFailed,
	EventVerifyFailed,
	EventFileSyncIssues,
}

// WarningEvents are unread-eligible warnings (same in-app unread rules as alerts).
var WarningEvents = []string{
	EventPOTProvider,
}

func validEvent(id string) bool {
	return slices.Contains(AllEvents, id)
}

// IsAlertEvent reports whether event is an alert-level notification.
func IsAlertEvent(event string) bool {
	return slices.Contains(AlertEvents, event)
}

// IsWarningEvent reports whether event is a warning-level notification.
func IsWarningEvent(event string) bool {
	return slices.Contains(WarningEvents, event)
}

// IsUnreadEvent reports whether event stays unread until in-app acknowledgment.
func IsUnreadEvent(event string) bool {
	return IsAlertEvent(event) || IsWarningEvent(event)
}

// EventLevel returns info, warning, or alert for an event id.
func EventLevel(event string) string {
	switch {
	case IsAlertEvent(event):
		return LevelAlert
	case IsWarningEvent(event):
		return LevelWarning
	default:
		return LevelInfo
	}
}

// EventsForLevel returns canonical event ids for a level filter (empty level → nil).
func EventsForLevel(level string) []string {
	switch strings.TrimSpace(level) {
	case LevelAlert:
		return append([]string(nil), AlertEvents...)
	case LevelWarning:
		return append([]string(nil), WarningEvents...)
	case LevelInfo:
		out := make([]string, 0, len(AllEvents))
		for _, e := range AllEvents {
			if EventLevel(e) == LevelInfo {
				out = append(out, e)
			}
		}
		return out
	default:
		return nil
	}
}

// UnreadEvents returns alert + warning event ids (SQL IN lists, mark-all).
func UnreadEvents() []string {
	out := make([]string, 0, len(AlertEvents)+len(WarningEvents))
	out = append(out, AlertEvents...)
	out = append(out, WarningEvents...)
	return out
}

// EventsSortedByLevel returns AllEvents ordered alert → warning → info, then by label.
// Used for Settings channel event checkboxes.
func EventsSortedByLevel() []string {
	out := append([]string(nil), AllEvents...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := levelSortRank(out[i]), levelSortRank(out[j])
		if ri != rj {
			return ri < rj
		}
		li, lj := EventLabels[out[i]], EventLabels[out[j]]
		if li != lj {
			return li < lj
		}
		return out[i] < out[j]
	})
	return out
}

func levelSortRank(event string) int {
	switch EventLevel(event) {
	case LevelAlert:
		return 0
	case LevelWarning:
		return 1
	default:
		return 2
	}
}

// AliasEvent maps legacy channel event ids to canonical ids.
func AliasEvent(id string) string {
	switch id {
	case legacyEventDownloadFailed:
		return EventYtDlpFailed
	case legacyEventDownloadsDone:
		return EventDownloadDigest
	default:
		return id
	}
}

func NormalizeEvents(ids []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		id = AliasEvent(id)
		if !validEvent(id) {
			return nil, fmt.Errorf("unknown notify event %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

func notifyTypeFor(event string) apprise.NotifyType {
	switch event {
	case EventCookieInvalid, EventRateLimited, EventPOTProvider:
		return apprise.NotifyWarning
	case EventYtDlpFailed, EventVerifyFailed, EventFileSyncIssues:
		return apprise.NotifyFailure
	case EventDownloadDigest:
		return apprise.NotifySuccess
	case EventLiveSkipped:
		return apprise.NotifyInfo
	default:
		return apprise.NotifyInfo
	}
}
