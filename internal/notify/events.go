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
	// EventAll is a channel subscription token (not a SendEvent id): match every
	// current and future event. Stored alone as ["all"] when selected.
	EventAll                 = "all"
	EventCookieInvalid       = "cookie_invalid"
	EventRateLimited         = "rate_limited"
	EventYtDlpFailed         = "ytdlp_failed"
	EventVerifyFailed        = "verify_failed"
	EventFileSyncIssues      = "file_sync_issues"
	EventPOTProvider         = "pot_provider"
	EventPathCollision       = "path_collision"
	EventDownloadDigest      = "download_digest"
	EventLiveSkipped         = "live_skipped"
	EventArchiveFallback     = "archive_fallback"
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
	EventArchiveFallback,
	EventYtDlpFailed,
	EventVerifyFailed,
	EventFileSyncIssues,
	EventCookieInvalid,
	EventRateLimited,
	EventPOTProvider,
	EventPathCollision,
}

// EventLabels are short UI labels for event checkboxes.
var EventLabels = map[string]string{
	EventAll:                 "All",
	EventCookieInvalid:       "Cookie / auth failure",
	EventRateLimited:         "Rate limit / IP block",
	EventYtDlpFailed:         "yt-dlp / site failure",
	EventVerifyFailed:        "Verify failed",
	EventFileSyncIssues:      "File sync issues",
	EventPOTProvider:         "PO token provider failure",
	EventPathCollision:       "Episode path collision",
	EventDownloadDigest:      "Downloads finished (digest)",
	EventLiveSkipped:         "Live broadcast skipped",
	EventArchiveFallback:     "Web Archive fallback used",
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
	EventPathCollision,
}

func validEvent(id string) bool {
	return slices.Contains(AllEvents, id)
}

func validChannelEvent(id string) bool {
	return id == EventAll || validEvent(id)
}

// HasAllSubscription reports whether channel events subscribe to every event
// (explicit EventAll token, or a legacy full explicit AllEvents set).
func HasAllSubscription(events []string) bool {
	if slices.Contains(events, EventAll) {
		return true
	}
	return isFullEventSet(events)
}

// Subscribes reports whether a channel's event list includes event (or EventAll).
func Subscribes(events []string, event string) bool {
	event = AliasEvent(strings.TrimSpace(event))
	if event == "" || event == EventAll {
		return false
	}
	if HasAllSubscription(events) {
		return true
	}
	return slices.Contains(events, event)
}

func isFullEventSet(events []string) bool {
	if len(events) != len(AllEvents) {
		return false
	}
	have := map[string]bool{}
	for _, e := range events {
		have[AliasEvent(e)] = true
	}
	for _, id := range AllEvents {
		if !have[id] {
			return false
		}
	}
	return true
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
	wantAll := false
	for _, id := range ids {
		if id == "" {
			continue
		}
		id = AliasEvent(id)
		if id == EventAll {
			wantAll = true
			continue
		}
		if !validChannelEvent(id) {
			return nil, fmt.Errorf("unknown notify event %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if wantAll || isFullEventSet(out) {
		return []string{EventAll}, nil
	}
	slices.Sort(out)
	return out, nil
}

func notifyTypeFor(event string) apprise.NotifyType {
	switch event {
	case EventCookieInvalid, EventRateLimited, EventPOTProvider, EventPathCollision:
		return apprise.NotifyWarning
	case EventYtDlpFailed, EventVerifyFailed, EventFileSyncIssues:
		return apprise.NotifyFailure
	case EventDownloadDigest:
		return apprise.NotifySuccess
	case EventLiveSkipped, EventArchiveFallback:
		return apprise.NotifyInfo
	default:
		return apprise.NotifyInfo
	}
}
