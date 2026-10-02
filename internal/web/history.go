package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type historyView struct {
	ID           int64
	CreatedAt    string // absolute (title / hover) - finished_at when set
	CreatedAgo   string // relative display
	Kind         string
	Status       string // done|failed|cancelled
	Message      string
	Domain       string
	Code         string
	ErrorMessage string // tasks.error_message (failure detail)
	SeriesID     int64
	VideoID      int64
	Origin       string
	ParentTaskID int64
	ParentKind   string
}

type notifyHistoryView struct {
	ID         int64
	CreatedAt  string
	CreatedAgo string
	Event      string
	EventLabel string
	Title      string
	Body       string
	TaskID     int64
	ExternalOK bool
	Unread     bool
	Alert      bool
	Warning    bool
	Level      string
	ReadAt     string
	ReadAgo    string
}

func taskWhen(t queue.Task) string {
	if t.FinishedAt.Valid && t.FinishedAt.String != "" {
		return t.FinishedAt.String
	}
	return t.CreatedAt
}

// historyListMessage is the History → Tasks row text. Failed rows prefer a short
// root-cause line from error_message when the stored message is generic.
// Always one line (no embedded newlines), capped for the table cell.
func historyListMessage(t queue.Task) string {
	msg := strings.TrimSpace(t.Message)
	if t.Status != queue.StatusFailed {
		return firstErrorLine(msg)
	}
	detail := firstErrorLine(t.ErrorMessage)
	if detail == "" {
		return firstErrorLine(msg)
	}
	if msg == "" || isGenericFailMessage(msg) {
		return detail
	}
	if strings.Contains(strings.ToLower(msg), strings.ToLower(detail)) {
		return firstErrorLine(msg)
	}
	return firstErrorLine(msg + ": " + detail)
}

func isGenericFailMessage(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "download failed", "remux failed", "pack failed", "scan failed", "failed":
		return true
	default:
		return false
	}
}

func firstErrorLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	const max = 160
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func taskToHistoryView(t queue.Task, now time.Time) historyView {
	abs, ago := createdAgoPairCompact(taskWhen(t), now)
	v := historyView{
		ID:           t.ID,
		CreatedAt:    abs,
		CreatedAgo:   ago,
		Kind:         t.Kind,
		Status:       t.Status,
		Message:      historyListMessage(t),
		Domain:       t.Domain,
		Code:         t.ErrorCode,
		ErrorMessage: t.ErrorMessage,
		Origin:       t.Origin,
	}
	if t.SeriesID.Valid {
		v.SeriesID = t.SeriesID.Int64
	}
	if t.VideoID.Valid {
		v.VideoID = t.VideoID.Int64
	}
	if t.ParentTaskID.Valid {
		v.ParentTaskID = t.ParentTaskID.Int64
	}
	return v
}

func notificationToView(n notify.Notification, now time.Time) notifyHistoryView {
	abs, ago := createdAgoPairCompact(n.CreatedAt, now)
	label := notify.EventLabels[n.Event]
	if label == "" {
		label = n.Event
	}
	v := notifyHistoryView{
		ID:         n.ID,
		CreatedAt:  abs,
		CreatedAgo: ago,
		Event:      n.Event,
		EventLabel: label,
		Title:      n.Title,
		Body:       n.Body,
		ExternalOK: n.ExternalOK,
		Unread:     n.Unread(),
		Alert:      notify.IsAlertEvent(n.Event),
		Warning:    notify.IsWarningEvent(n.Event),
		Level:      notify.EventLevel(n.Event),
	}
	if n.TaskID.Valid {
		v.TaskID = n.TaskID.Int64
	}
	if n.ReadAt.Valid {
		absRead, agoRead := createdAgoPair(n.ReadAt.String, now)
		v.ReadAt = absRead
		v.ReadAgo = agoRead
	}
	return v
}

func isHistoryStatus(status string) bool {
	return status == queue.StatusDone || status == queue.StatusFailed || status == queue.StatusCancelled
}

// historyEventError reports timeline events that should render in text-error
// (video download holds, source scan failures). Cancelled tasks are neutral, not errors.
func historyEventError(event string) bool {
	switch event {
	case "download_failed", "integrity_check_failed",
		"file_externally_changed", "sidecar_externally_changed",
		library.SourceHistScanError:
		return true
	default:
		return false
	}
}

// historyEventNeutral reports cancelled (operator abort) events for muted timeline chrome.
func historyEventNeutral(event string) bool {
	return event == library.VideoHistCancelled // same string as SourceHistCancelled
}

// historyEventLabel is the Event column text. Cancelled video rows store
// event=cancelled with detail.kind (e.g. download); show that kind.
// Source cancel rows use detail.mode and show "scan".
// Integrity outcomes keep the stored event id here; video History then prefers
// tasks.kind when TaskKind is filled (Event = kind, Message = result).
func historyEventLabel(event, detail string) string {
	event = strings.TrimSpace(event)
	if event == library.VideoHistCancelled {
		if kind := historyDetailString(detail, "kind"); kind != "" {
			return historyKindDisplay(kind)
		}
		if mode := historyDetailString(detail, "mode"); mode != "" {
			return queue.KindScan
		}
		return event
	}
	return historyEventDisplay(event)
}

// historyEventDisplay normalizes legacy event ids; does not invent prose labels
// (result text stays in Message).
// historyEventDisplay normalizes leftover legacy event ids for display only.
func historyEventDisplay(event string) string {
	switch event {
	case library.VideoHistVerified:
		return library.VideoHistIntegrityChecked
	case "verify_failed":
		return library.VideoHistVerifyFailed
	default:
		return event
	}
}

// historyKindDisplay maps task kinds (including leftover legacy ids) for cancelled history rows.
func historyKindDisplay(kind string) string {
	switch kind {
	case "media_verify":
		return queue.KindIntegrityCheckInitial
	case "verify_all_media":
		return queue.KindIntegrityCheck
	default:
		return kind
	}
}

// historyMessageWithDetail appends size/hash deltas from detail JSON when present.
func historyMessageWithDetail(msg, detail string) string {
	msg = strings.TrimSpace(msg)
	detail = strings.TrimSpace(detail)
	if detail == "" || isEmptyJSONPayload(detail) {
		return msg
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return msg
	}
	var parts []string
	if oldSz, okOld := historyDetailInt64(raw, "old_size"); okOld {
		if newSz, okNew := historyDetailInt64(raw, "new_size"); okNew {
			parts = append(parts, fmt.Sprintf("%s → %s", library.FormatBytes(oldSz), library.FormatBytes(newSz)))
		}
	}
	oldH := strings.TrimSpace(fmt.Sprint(raw["old_hash"]))
	newH := strings.TrimSpace(fmt.Sprint(raw["new_hash"]))
	if oldH != "" && oldH != "<nil>" && newH != "" && newH != "<nil>" {
		parts = append(parts, truncHashPrefix(oldH)+"… → "+truncHashPrefix(newH)+"…")
	}
	if len(parts) == 0 {
		return msg
	}
	suffix := strings.Join(parts, "; ")
	if msg == "" {
		return suffix
	}
	return msg + " (" + suffix + ")"
}

func historyDetailInt64(raw map[string]any, key string) (int64, bool) {
	v, ok := raw[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func truncHashPrefix(h string) string {
	h = strings.TrimSpace(h)
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func historyDetailString(detail, key string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return ""
	}
	s, _ := raw[key].(string)
	return strings.TrimSpace(s)
}

// isEmptyJSONPayload reports blank, null, or empty object/array JSON payloads.
func isEmptyJSONPayload(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return true
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return false
	}
	switch x := v.(type) {
	case nil:
		return true
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	default:
		return false
	}
}

// historyTimeRange holds raw UI values (datetime-local UTC) and SQL bounds.
type historyTimeRange struct {
	FromUI string // YYYY-MM-DDTHH:MM for inputs, or ""
	ToUI   string
	From   string // inclusive RFC3339Nano UTC, or ""
	To     string
}

func parseHistoryTimeRangeValues(fromRaw, toRaw string) historyTimeRange {
	fromUI := strings.TrimSpace(fromRaw)
	toUI := strings.TrimSpace(toRaw)
	fromT, fromOK := parseHistoryDateTimeLocalUTC(fromUI)
	toT, toOK := parseHistoryDateTimeLocalUTC(toUI)
	if !fromOK {
		fromUI = ""
	}
	if !toOK {
		toUI = ""
	}
	var fromBound, toBound string
	if fromOK {
		fromBound = fromT.UTC().Format(time.RFC3339Nano)
	}
	if toOK {
		// Inclusive through end of selected minute.
		toBound = toT.UTC().Add(time.Minute - time.Nanosecond).Format(time.RFC3339Nano)
	}
	if fromOK && toOK && fromT.After(toT) {
		fromUI, toUI = toUI, fromUI
		fromBound = toT.UTC().Format(time.RFC3339Nano)
		toBound = fromT.UTC().Add(time.Minute - time.Nanosecond).Format(time.RFC3339Nano)
	}
	return historyTimeRange{FromUI: fromUI, ToUI: toUI, From: fromBound, To: toBound}
}

// parseHistoryDateTimeLocalUTC accepts YYYY-MM-DDTHH:MM as UTC; invalid → false.
func parseHistoryDateTimeLocalUTC(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (h *Handler) historyPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// Legacy #notifications deep links and nlevel filter → Notifications Explorer.
	if strings.Contains(r.URL.Fragment, "notification") || q.Get("nlevel") != "" || q.Get("npage") != "" {
		http.Redirect(w, r, "/browser?type=notifications", http.StatusFound)
		return
	}
	out := url.Values{}
	out.Set("type", "tasks")
	if d := strings.TrimSpace(q.Get("domain")); d != "" {
		out.Set("domain", d)
	}
	if k := strings.TrimSpace(q.Get("kind")); k != "" {
		out.Set("kind", k)
	}
	if o := strings.TrimSpace(q.Get("origin")); o != "" {
		out.Set("origin", o)
	}
	if st := strings.TrimSpace(q.Get("status")); st != "" {
		out.Add("status", st)
	} else {
		// Finished view for old History visitors.
		out.Add("status", queue.StatusDone)
		out.Add("status", queue.StatusFailed)
		out.Add("status", queue.StatusCancelled)
	}
	http.Redirect(w, r, "/browser?"+out.Encode(), http.StatusFound)
}
