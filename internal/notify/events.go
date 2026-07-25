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
	EventDownloadDigest = "download_digest"
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
	EventCookieInvalid,
	EventRateLimited,
}

// EventLabels are short UI labels for event checkboxes.
var EventLabels = map[string]string{
	EventCookieInvalid:  "Cookie / auth failure",
	EventRateLimited:    "Rate limit / IP block",
	EventYtDlpFailed:    "yt-dlp / site failure",
	EventVerifyFailed:   "Verify failed",
	EventDownloadDigest: "Downloads finished (digest)",
}

// AlertEvents are unread-eligible notification events (vs info digests).
var AlertEvents = []string{
	EventCookieInvalid,
	EventRateLimited,
	EventYtDlpFailed,
	EventVerifyFailed,
}

func validEvent(id string) bool {
	return slices.Contains(AllEvents, id)
}

// IsAlertEvent reports whether event is an alert (unread tracking, requires task_id).
func IsAlertEvent(event string) bool {
	return slices.Contains(AlertEvents, event)
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
	case EventCookieInvalid, EventRateLimited:
		return apprise.NotifyWarning
	case EventYtDlpFailed, EventVerifyFailed:
		return apprise.NotifyFailure
	case EventDownloadDigest:
		return apprise.NotifySuccess
	default:
		return apprise.NotifyInfo
	}
}
