// Package notify sends operator alerts via Apprise (github.com/unraid/apprise-go)
// and records them in the in-app notifications table via the fixed Creatorr channel.
//
// Channels: virtual creatorr://in-app (all events, read-only) plus notification_channels
// (Apprise URL + subscribed event ids).
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	apprise "github.com/unraid/apprise-go"
	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/events"
)

// sendFn sends one Apprise notification. Tests override this.
var sendFn = defaultSend

// eventsHub optional SSE publisher (set from main).
var eventsHub *events.Hub

// SetEventsHub wires SSE for notification.created (nil clears).
func SetEventsHub(h *events.Hub) {
	eventsHub = h
}

func publishCreated(database *db.DB, id int64, event string) {
	if eventsHub == nil || id <= 0 {
		return
	}
	n, _ := CountUnread(database)
	eventsHub.NotificationCreated(id, event, n)
}

func publishRead(database *db.DB, id int64) {
	if eventsHub == nil {
		return
	}
	n, _ := CountUnread(database)
	eventsHub.NotificationRead(id, n)
}

// SetSendFnForTest swaps the send implementation; returns the previous one.
func SetSendFnForTest(fn func(urls []string, title, body string, nt apprise.NotifyType) error) func(urls []string, title, body string, nt apprise.NotifyType) error {
	prev := sendFn
	if fn == nil {
		sendFn = defaultSend
	} else {
		sendFn = fn
	}
	return prev
}

func defaultSend(urls []string, title, body string, nt apprise.NotifyType) error {
	if len(urls) == 0 {
		return nil
	}
	return apprise.Send(urls, body,
		apprise.WithTitle(title),
		apprise.WithNotifyType(nt),
	)
}

// Send posts title/body to every URL with NotifyInfo (used by channel Test).
// Does not write an in-app notification row.
func Send(ctx context.Context, urls []string, title, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return sendFn(urls, title, body, apprise.NotifyInfo)
}

// SendEvent delivers to every channel subscribed to event: in-app inserts a
// notifications row; Apprise channels call sendFn. taskID is required (>0) for
// unread events (alert + warning); may be 0 for info digests (stored NULL).
// Info events like live_skipped may still pass task_id so the detail page links the task.
// Successful Apprise delivery sets external_ok and marks unread notifications read.
func SendEvent(ctx context.Context, database *db.DB, event, title, body string, taskID int64) error {
	if database == nil {
		return nil
	}
	event = AliasEvent(event)
	if !validEvent(event) {
		return fmt.Errorf("unknown notify event %q", event)
	}
	if IsUnreadEvent(event) && taskID <= 0 {
		return fmt.Errorf("task_id required for notify event %q", event)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	readAt := ""
	if !IsUnreadEvent(event) {
		readAt = now
	}

	channels, err := ListForEvent(database, event)
	if err != nil {
		return err
	}
	nt := notifyTypeFor(event)
	var notifID int64
	var first error
	anyAppriseOK := false
	for _, c := range channels {
		if err := ctx.Err(); err != nil {
			return err
		}
		if IsInAppChannel(c) {
			id, ierr := InsertNotification(database, event, title, body, taskID, false, readAt)
			if ierr != nil {
				return ierr
			}
			notifID = id
			continue
		}
		if err := sendFn([]string{c.URL}, title, body, nt); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		anyAppriseOK = true
	}
	if notifID == 0 {
		// Should not happen: in-app is always subscribed to AllEvents.
		id, ierr := InsertNotification(database, event, title, body, taskID, false, readAt)
		if ierr != nil {
			return ierr
		}
		notifID = id
	}
	if anyAppriseOK {
		markRead := ""
		if IsUnreadEvent(event) {
			markRead = now
		}
		_ = MarkExternalOK(database, notifID, markRead)
	}
	publishCreated(database, notifID, event)
	return first
}

// CookieInvalid notifies that cookie/auth failed for a domain (does not unmonitor).
func CookieInvalid(ctx context.Context, database *db.DB, taskID int64, domain, detail string) error {
	return DomainAlert(ctx, database, taskID, domain, EventCookieInvalid, detail)
}

// RateLimited notifies that rate limit / IP block hit for a domain (does not unmonitor).
func RateLimited(ctx context.Context, database *db.DB, taskID int64, domain, detail string) error {
	return DomainAlert(ctx, database, taskID, domain, EventRateLimited, detail)
}

// maxAlertDetail caps failure detail on alert/warning bodies (in-app + Apprise).
const maxAlertDetail = 2000

// truncateDetailTail keeps the end of detail. yt-dlp/ffmpeg put the real ERROR
// after progress noise; head-truncation made notification bodies look incomplete.
func truncateDetailTail(detail string) string {
	if len(detail) <= maxAlertDetail {
		return detail
	}
	return "…" + detail[len(detail)-maxAlertDetail:]
}

// DomainAlert sends a domain problem alert without changing monitored flags.
func DomainAlert(ctx context.Context, database *db.DB, taskID int64, domain, reason, detail string) error {
	detail = truncateDetailTail(detail)
	title := fmt.Sprintf("Domain issue (%s)", domain)
	body := fmt.Sprintf("Domain %s reported %s. Videos may be in wanted_download_error / wanted_source_error. Fix cookies or wait, then Retry on the source.\n\n%s", domain, reason, detail)
	return SendEvent(ctx, database, reason, title, body, taskID)
}

// YtDlpFailed notifies a site/yt-dlp (or media-task remux/pack) failure that is not cookie/rate.
func YtDlpFailed(ctx context.Context, database *db.DB, taskID int64, domain, detail string) error {
	detail = truncateDetailTail(detail)
	title := fmt.Sprintf("yt-dlp / site failure (%s)", domain)
	body := fmt.Sprintf("Domain %s: yt-dlp or related media task failed.\n\n%s", domain, detail)
	return SendEvent(ctx, database, EventYtDlpFailed, title, body, taskID)
}

// VerifyFailed notifies that packed media failed post-pack verify (file kept).
func VerifyFailed(ctx context.Context, database *db.DB, taskID int64, series, title, detail string) error {
	detail = truncateDetailTail(detail)
	label := strings.TrimSpace(series)
	if label == "" {
		label = "library"
	}
	vt := strings.TrimSpace(title)
	if vt == "" {
		vt = "video"
	}
	nTitle := fmt.Sprintf("Verify failed (%s)", label)
	body := fmt.Sprintf("%s: %s failed media verify. File kept; status verify_failed. Re-download to retry.\n\n%s", label, vt, detail)
	return SendEvent(ctx, database, EventVerifyFailed, nTitle, body, taskID)
}

// POTProvider notifies that the PO token sidecar/plugin had a problem while
// yt-dlp continued (warning level; download is not failed for this alone).
func POTProvider(ctx context.Context, database *db.DB, taskID int64, domain, detail string) error {
	detail = truncateDetailTail(detail)
	dom := strings.TrimSpace(domain)
	if dom == "" {
		dom = "unknown"
	}
	title := fmt.Sprintf("PO token provider (%s)", dom)
	body := fmt.Sprintf("Domain %s: PO token provider issue while the task continued. Check creatorr-po-token / plugin dirs / CREATORR_POT_PROVIDER_URL.\n\n%s", dom, detail)
	return SendEvent(ctx, database, EventPOTProvider, title, body, taskID)
}

// DigestItem is one completed media item in a download_digest.
type DigestItem struct {
	Domain    string
	Series    string
	Title     string
	Kind      string // archive | stream
	Beginning bool   // stream beginning cached
}

// DownloadDigest sends the backlog-cleared digest (no task_id).
func DownloadDigest(ctx context.Context, database *db.DB, items []DigestItem) error {
	if len(items) == 0 {
		return nil
	}
	title := fmt.Sprintf("%d download(s) finished", len(items))
	return SendEvent(ctx, database, EventDownloadDigest, title, FormatDigestBody(items), 0)
}

// LiveSkipped records an info notification when download soft-skips a
// currently live broadcast. taskID links notification detail → task (video history
// live_skipped uses the same task_id). Status stays wanted for later retry.
func LiveSkipped(ctx context.Context, database *db.DB, taskID int64, series, title string) error {
	label := strings.TrimSpace(series)
	vid := strings.TrimSpace(title)
	if vid != "" {
		if label != "" {
			label += " / "
		}
		label += vid
	}
	if label == "" {
		label = "(unknown)"
	}
	nTitle := "Live broadcast skipped"
	body := label + "\n\nCurrently live; download/pack deferred. Video stays wanted and will retry after the broadcast ends."
	return SendEvent(ctx, database, EventLiveSkipped, nTitle, body, taskID)
}

// FileSyncIssueItem is one media or sidecar row in a file_sync_issues digest.
type FileSyncIssueItem struct {
	Series string
	Title  string
	Detail string // optional; e.g. "nfo: episode.nfo" for sidecars
}

const fileSyncIssueListCap = 40

// FileSyncIssues sends one alert digest for missing media/sidecars and/or size
// mismatches found during a sync_files pass. No-op when both slices are empty.
func FileSyncIssues(ctx context.Context, database *db.DB, taskID int64, missing, changed []FileSyncIssueItem) error {
	if len(missing) == 0 && len(changed) == 0 {
		return nil
	}
	n := len(missing) + len(changed)
	title := fmt.Sprintf("%d library file issue(s)", n)
	return SendEvent(ctx, database, EventFileSyncIssues, title, FormatFileSyncIssuesBody(missing, changed), taskID)
}

// FormatFileSyncIssuesBody builds the file_sync_issues message body.
func FormatFileSyncIssuesBody(missing, changed []FileSyncIssueItem) string {
	var b strings.Builder
	writeSection := func(heading string, items []FileSyncIssueItem) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d):\n", heading, len(items))
		shown := items
		extra := 0
		if len(shown) > fileSyncIssueListCap {
			extra = len(shown) - fileSyncIssueListCap
			shown = shown[:fileSyncIssueListCap]
		}
		for _, it := range shown {
			label := strings.TrimSpace(it.Series)
			title := strings.TrimSpace(it.Title)
			if title != "" {
				if label != "" {
					label += " / "
				}
				label += title
			}
			if label == "" {
				label = "(unknown)"
			}
			if d := strings.TrimSpace(it.Detail); d != "" {
				label += " (" + d + ")"
			}
			fmt.Fprintf(&b, "- %s\n", label)
		}
		if extra > 0 {
			fmt.Fprintf(&b, "- +%d more\n", extra)
		}
		b.WriteByte('\n')
	}
	writeSection("Missing", missing)
	writeSection("Size changed", changed)
	body := strings.TrimSpace(b.String())
	if body != "" {
		body += "\n\nFiles kept where present. Media size mismatches set status verify_failed; sidecar issues keep video status. Re-download or regenerate manually to replace. No automatic re-download."
	}
	return body
}

// FormatDigestBody builds the download_digest message body.
func FormatDigestBody(items []DigestItem) string {
	var b strings.Builder
	for _, it := range items {
		label := it.Series
		if label == "" {
			label = it.Domain
		}
		if it.Title != "" {
			if label != "" {
				label += " / "
			}
			label += it.Title
		}
		if label == "" {
			label = "(unknown)"
		}
		suffix := "downloaded"
		switch it.Kind {
		case "stream":
			if it.Beginning {
				suffix = "stream, beginning cached"
			} else {
				suffix = "stream"
			}
		}
		fmt.Fprintf(&b, "- %s (%s)\n", label, suffix)
	}
	return strings.TrimRight(b.String(), "\n")
}
