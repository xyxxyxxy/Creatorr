package notify

import (
	"fmt"
	"slices"

	apprise "github.com/unraid/apprise-go"
)

// Event ids stored on notify_channels.events and used by SendEvent.
const (
	EventCookieInvalid  = "cookie_invalid"
	EventRateLimited    = "rate_limited"
	EventYtDlpFailed    = "ytdlp_failed"
	EventVerifyFailed   = "verify_failed"
	EventFileSyncIssues = "file_sync_issues"
	EventPOTProvider    = "pot_provider"
	EventDownloadDigest = "download_digest"
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

// IsUnreadEvent reports whether event stays unread until read / Apprise OK.
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

// UnreadEvents returns alert + warning event ids (SQL IN lists, mark-all).
func UnreadEvents() []string {
	out := make([]string, 0, len(AlertEvents)+len(WarningEvents))
	out = append(out, AlertEvents...)
	out = append(out, WarningEvents...)
	return out
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
	default:
		return apprise.NotifyInfo
	}
}
