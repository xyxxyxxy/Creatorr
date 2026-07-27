package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

type historyView struct {
	ID         int64
	CreatedAt  string // absolute (title / hover) - finished_at when set
	CreatedAgo string // relative display
	Kind       string
	Status     string // done|failed|cancelled
	Message    string
	Domain     string
	Code       string
	SeriesID   int64
	VideoID    int64
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

type hiddenFilter struct {
	Name  string
	Value string
}

func taskWhen(t queue.Task) string {
	if t.FinishedAt.Valid && t.FinishedAt.String != "" {
		return t.FinishedAt.String
	}
	return t.CreatedAt
}

func taskToHistoryView(t queue.Task, now time.Time) historyView {
	abs, ago := createdAgoPair(taskWhen(t), now)
	v := historyView{
		ID:         t.ID,
		CreatedAt:  abs,
		CreatedAgo: ago,
		Kind:       t.Kind,
		Status:     t.Status,
		Message:    t.Message,
		Domain:     t.Domain,
		Code:       t.ErrorCode,
	}
	if t.SeriesID.Valid {
		v.SeriesID = t.SeriesID.Int64
	}
	if t.VideoID.Valid {
		v.VideoID = t.VideoID.Int64
	}
	return v
}

func notificationToView(n notify.Notification, now time.Time) notifyHistoryView {
	abs, ago := createdAgoPair(n.CreatedAt, now)
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
// (video download holds, source scan failures).
func historyEventError(event string) bool {
	switch event {
	case "download_failed", "source_failed", "verify_failed", "file_externally_changed",
		"sidecar_externally_changed",
		"wanted_download_error", "wanted_source_error", // legacy history rows
		library.SourceHistScanError:
		return true
	default:
		return false
	}
}

// historyEventLabel is the Event column text. Cancelled video rows store
// event=cancelled with detail.kind (download, pack_stream, …); show that kind.
// Source cancel rows use detail.mode and show "scan".
func historyEventLabel(event, detail string) string {
	event = strings.TrimSpace(event)
	if event != library.VideoHistCancelled {
		return event
	}
	if kind := historyDetailString(detail, "kind"); kind != "" {
		return kind
	}
	if mode := historyDetailString(detail, "mode"); mode != "" {
		return queue.KindScan
	}
	return event
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

func parseHistoryFilter(r *http.Request) queue.HistoryFilter {
	_, _, fromBound, toBound := parseHistoryTimeRange(r)
	f := queue.HistoryFilter{
		Domain: strings.TrimSpace(r.URL.Query().Get("domain")),
		Kind:   strings.TrimSpace(r.URL.Query().Get("kind")),
		From:   fromBound,
		To:     toBound,
	}
	if st := strings.TrimSpace(r.URL.Query().Get("status")); st != "" {
		f.Statuses = []string{st}
	}
	return f
}

func parseNotifyListFilter(r *http.Request) notify.ListFilter {
	_, _, fromBound, toBound := parseHistoryTimeRange(r)
	level := strings.TrimSpace(r.URL.Query().Get("nlevel"))
	switch level {
	case notify.LevelInfo, notify.LevelWarning, notify.LevelAlert:
	default:
		level = ""
	}
	return notify.ListFilter{
		Level: level,
		From:  fromBound,
		To:    toBound,
	}
}

// historyTimeRange holds raw UI values (datetime-local UTC) and SQL bounds.
type historyTimeRange struct {
	FromUI string // YYYY-MM-DDTHH:MM for inputs, or ""
	ToUI   string
	From   string // inclusive RFC3339Nano UTC, or ""
	To     string
}

// parseHistoryTimeRange reads shared from/to query params (UTC datetime-local).
// Empty bounds stay open. Invalid values ignored. If both set and from > to, swap.
func parseHistoryTimeRange(r *http.Request) (fromUI, toUI, fromBound, toBound string) {
	tr := parseHistoryTimeRangeValues(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	return tr.FromUI, tr.ToUI, tr.From, tr.To
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

func historyFilterActive(f queue.HistoryFilter) bool {
	return len(f.Statuses) > 0 || f.Domain != "" || f.Kind != "" || f.From != "" || f.To != ""
}

func notifyFilterActive(f notify.ListFilter) bool {
	return f.Level != "" || f.From != "" || f.To != ""
}

func (h *Handler) historyPage(w http.ResponseWriter, r *http.Request) {
	tr := parseHistoryTimeRangeValues(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	fromUI, toUI := tr.FromUI, tr.ToUI
	filter := parseHistoryFilter(r)
	nFilter := parseNotifyListFilter(r)
	now := time.Now().UTC()

	// Visiting History clears the unread badge (list then renders as read).
	_, _ = notify.MarkAllRead(h.Queue.DB)

	nTotal, err := notify.CountNotifications(h.Queue.DB, nFilter)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	nPage := ParsePage(r, "npage")
	nPageInfo := NewPageInfo(r, "npage", nPage, nTotal)
	nPageInfo.LiveTarget = "notification-history-live"
	nItems, err := notify.ListNotifications(h.Queue.DB, nFilter, PageSize, Offset(nPageInfo.Page))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	nViews := make([]notifyHistoryView, 0, len(nItems))
	for _, n := range nItems {
		nViews = append(nViews, notificationToView(n, now))
	}

	total, err := h.Queue.CountHistory(filter)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	page := ParsePage(r, "page")
	pageInfo := NewPageInfo(r, "page", page, total)
	pageInfo.LiveTarget = "history-live"
	items, err := h.Queue.ListHistory(filter, PageSize, Offset(pageInfo.Page))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]historyView, 0, len(items))
	for _, t := range items {
		views = append(views, taskToHistoryView(t, now))
	}

	statusSel := ""
	if len(filter.Statuses) == 1 {
		statusSel = filter.Statuses[0]
	}
	statusOpts := []listFilterOpt{
		{Value: queue.StatusDone, Label: "Success", Selected: statusSel == queue.StatusDone},
		{Value: queue.StatusFailed, Label: "Failure", Selected: statusSel == queue.StatusFailed},
		{Value: queue.StatusCancelled, Label: "Cancelled", Selected: statusSel == queue.StatusCancelled},
	}

	domains, _ := h.Queue.DistinctHistoryDomains()
	domainOpts := make([]listFilterOpt, 0, len(domains))
	for _, d := range domains {
		domainOpts = append(domainOpts, listFilterOpt{Value: d, Label: d, Selected: filter.Domain == d})
	}

	kinds, _ := h.Queue.DistinctHistoryKinds()
	kindOpts := make([]listFilterOpt, 0, len(kinds))
	for _, k := range kinds {
		kindOpts = append(kindOpts, listFilterOpt{Value: k, Label: k, Selected: filter.Kind == k})
	}

	selects := []listFilterSelect{
		{Name: "domain", AriaLabel: "Domain", EmptyLabel: "All domains", Options: domainOpts},
		{Name: "kind", AriaLabel: "Kind", EmptyLabel: "All kinds", Options: kindOpts},
		{Name: "status", AriaLabel: "Status", EmptyLabel: "All statuses", Options: statusOpts},
	}

	levelOpts := []listFilterOpt{
		{Value: notify.LevelInfo, Label: "Info", Selected: nFilter.Level == notify.LevelInfo},
		{Value: notify.LevelWarning, Label: "Warning", Selected: nFilter.Level == notify.LevelWarning},
		{Value: notify.LevelAlert, Label: "Alert", Selected: nFilter.Level == notify.LevelAlert},
	}
	notifySelects := []listFilterSelect{
		{Name: "nlevel", AriaLabel: "Type", EmptyLabel: "All types", Options: levelOpts},
	}

	rangeHidden := []hiddenFilter{}
	if fromUI != "" {
		rangeHidden = append(rangeHidden, hiddenFilter{Name: "from", Value: fromUI})
	}
	if toUI != "" {
		rangeHidden = append(rangeHidden, hiddenFilter{Name: "to", Value: toUI})
	}

	notifyHidden := append([]hiddenFilter{}, rangeHidden...)
	if statusSel != "" {
		notifyHidden = append(notifyHidden, hiddenFilter{Name: "status", Value: statusSel})
	}
	if filter.Domain != "" {
		notifyHidden = append(notifyHidden, hiddenFilter{Name: "domain", Value: filter.Domain})
	}
	if filter.Kind != "" {
		notifyHidden = append(notifyHidden, hiddenFilter{Name: "kind", Value: filter.Kind})
	}
	if pageInfo.Page > 1 {
		notifyHidden = append(notifyHidden, hiddenFilter{Name: "page", Value: strconv.Itoa(pageInfo.Page)})
	}

	taskHidden := append([]hiddenFilter{}, rangeHidden...)
	if nFilter.Level != "" {
		taskHidden = append(taskHidden, hiddenFilter{Name: "nlevel", Value: nFilter.Level})
	}
	if nPageInfo.Page > 1 {
		taskHidden = append(taskHidden, hiddenFilter{Name: "npage", Value: strconv.Itoa(nPageInfo.Page)})
	}

	rangeHiddenForTop := []hiddenFilter{}
	if statusSel != "" {
		rangeHiddenForTop = append(rangeHiddenForTop, hiddenFilter{Name: "status", Value: statusSel})
	}
	if filter.Domain != "" {
		rangeHiddenForTop = append(rangeHiddenForTop, hiddenFilter{Name: "domain", Value: filter.Domain})
	}
	if filter.Kind != "" {
		rangeHiddenForTop = append(rangeHiddenForTop, hiddenFilter{Name: "kind", Value: filter.Kind})
	}
	if nFilter.Level != "" {
		rangeHiddenForTop = append(rangeHiddenForTop, hiddenFilter{Name: "nlevel", Value: nFilter.Level})
	}

	render(w, "history", struct {
		pageBase
		Notifications      []notifyHistoryView
		NotifyPage         PageInfo
		NotifyFilter       notify.ListFilter
		NotifyFilterActive bool
		NotifySelects      []listFilterSelect
		NotifyHidden       []hiddenFilter
		Items              []historyView
		Page               PageInfo
		FilterActive       bool
		RangeFrom          string
		RangeTo            string
		RangeHidden        []hiddenFilter
		HistoryFilter      struct {
			HideQuery  bool
			AriaLabel  string
			Selects    []listFilterSelect
			Hidden     []hiddenFilter
			LiveTarget string
			FormAction string
		}
	}{
		pageBase:           newPage("History", "history", nil),
		Notifications:      nViews,
		NotifyPage:         nPageInfo,
		NotifyFilter:       nFilter,
		NotifyFilterActive: notifyFilterActive(nFilter),
		NotifySelects:      notifySelects,
		NotifyHidden:       notifyHidden,
		Items:              views,
		Page:               pageInfo,
		FilterActive:       historyFilterActive(filter),
		RangeFrom:          fromUI,
		RangeTo:            toUI,
		RangeHidden:        rangeHiddenForTop,
		HistoryFilter: struct {
			HideQuery  bool
			AriaLabel  string
			Selects    []listFilterSelect
			Hidden     []hiddenFilter
			LiveTarget string
			FormAction string
		}{
			HideQuery:  true,
			AriaLabel:  "Task filters",
			Selects:    selects,
			Hidden:     taskHidden,
			LiveTarget: "history-live",
			FormAction: "/history",
		},
	})
}
