package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

const (
	cookieModeNotifications   = "creatorr_mode_notifications"
	cookieSortNotifications   = "creatorr_sort_notifications"
	cookieFilterNotifications = "creatorr_filter_notifications"
	cookieColsNotifications   = "creatorr_cols_notifications"
	notificationsLiveTarget   = "notifications-list-live"
	notificationsPageSize     = SeriesPageSize
)

type notificationsListLiveData struct {
	Items            []notifyExplorerRow
	Page             PageInfo
	Load             ListLoad
	ListMode         ListMode
	FilterTotal      int
	SelectableTotal  int // alert/warning rows (bulk read/unread)
	Filter           listViewToolbar
	FilterActive     bool
	ViewMode         string
	TableCols        []tableCol
	TableColsCookie  string
	ShowSelectAll    bool
	OOB              bool
}

type notifyExplorerRow struct {
	notifyHistoryView
	CanToggle  bool
	LiveTarget string
	Redirect   string
}

func (h *Handler) explorerBrowseNotifications(w http.ResponseWriter, r *http.Request) {
	data, err := h.loadNotificationsListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeNotifications)
	render(w, "notifications_list_live", data)
}

func (h *Handler) loadNotificationsListLive(w http.ResponseWriter, r *http.Request) (notificationsListLiveData, error) {
	r = mergeNotificationsListPrefs(r)
	filter := parseNotificationsExplorerFilter(r)
	writeNotificationsListPrefs(w, r, filter)

	viewMode, writeCookie := resolveViewMode(r, cookieModeNotifications, viewList)
	if mode := coerceListTableView(viewMode); mode != viewMode {
		viewMode = mode
		writeCookie = true
	}
	if writeCookie {
		writeViewCookie(w, cookieModeNotifications, viewMode)
	}

	total, err := notify.CountNotifications(h.Queue.DB, filter)
	if err != nil {
		return notificationsListLiveData{}, err
	}
	load := resolvePaginatedLoad(r, total, notificationsPageSize, notificationsLiveTarget, "page")
	items, err := notify.ListNotifications(h.Queue.DB, filter, load.PageSize, OffsetSize(load.Page.Page, load.PageSize))
	if err != nil {
		return notificationsListLiveData{}, err
	}

	now := time.Now().UTC()
	at := explorerAtFrom(r, explorerAtBrowser)
	redirect := explorerCanonicalURL(r, explorerTypeNotifications)
	if redirect == "" {
		redirect = "/browser?type=notifications"
	}
	rows := make([]notifyExplorerRow, 0, len(items))
	for _, n := range items {
		v := notificationToView(n, now)
		rows = append(rows, notifyExplorerRow{
			notifyHistoryView: v,
			CanToggle:         notify.IsUnreadEvent(n.Event),
			LiveTarget:        notificationsLiveTarget,
			Redirect:          redirect,
		})
	}

	page := load.Page
	page.LiveTarget = notificationsLiveTarget
	rewriteExplorerPageInfo(&page, explorerTypeNotifications, 0)

	fromDay, toDay := parseFilterDay(r.URL.Query().Get("from")), parseFilterDay(r.URL.Query().Get("to"))
	toolbar := listViewToolbar{
		AriaLabel:             "Notification filters",
		QueryPlaceholder:      "Search",
		SortOpts:              notificationsSortOpts(r, filter.Sort, filter.SortDir),
		SortDir:               notifySortDir(filter.Sort, filter.SortDir),
		ViewOpts:              sourcesViewOpts(r, viewMode),
		ShowView:              true,
		FromDay:               fromDay,
		ToDay:                 toDay,
		ShowDateRange:         true,
		ShowDatePresence:      false,
		DateRangeLabel:        "Created",
		Selects:               notificationsFilterSelects(r, filter),
		FilterActive:          notificationsFilterActive(filter, fromDay, toDay),
		LiveTarget:            notificationsLiveTarget,
		FormAction:            explorerFragmentPath(r),
		NotificationsBulkMode: true,
	}
	applyExplorerToolbar(&toolbar, explorerTypeNotifications, at)
	if toolbar.FilterActive {
		toolbar.ClearAllHref = clearOperatorFiltersURL(r)
		toolbar.Badges = notificationsListBadges(r, filter, fromDay, toDay)
	}

	cols := annotateTableColsSort(
		parseTableColsCookie(r, cookieColsNotifications, notificationsTableColDefs()),
		toolbar.SortOpts, toolbar.SortDir)
	selIDs, _ := notify.ListNotificationIDs(h.Queue.DB, filter, true)
	selTotal := len(selIDs)
	pageSel := 0
	for _, row := range rows {
		if row.CanToggle {
			pageSel++
		}
	}
	out := notificationsListLiveData{
		Items:           rows,
		Page:            page,
		Load:            load,
		ListMode:        ListModePaginated,
		FilterTotal:     total,
		SelectableTotal: selTotal,
		Filter:          toolbar,
		FilterActive:    toolbar.FilterActive,
		ViewMode:        viewMode,
		TableCols:       cols,
		TableColsCookie: cookieColsNotifications,
		ShowSelectAll:   selTotal > pageSel,
	}
	rewriteExplorerInfinite(&out.Load, explorerTypeNotifications, 0)
	return out, nil
}

func coerceListTableView(mode string) string {
	if mode != viewList && mode != viewTable {
		return viewList
	}
	return mode
}

func parseNotificationsExplorerFilter(r *http.Request) notify.ListFilter {
	fromDay := parseFilterDay(r.URL.Query().Get("from"))
	toDay := parseFilterDay(r.URL.Query().Get("to"))
	fromBound, toBound := dayRangeBounds(fromDay, toDay)
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	if level == "" {
		level = strings.TrimSpace(r.URL.Query().Get("nlevel"))
	}
	switch level {
	case notify.LevelInfo, notify.LevelWarning, notify.LevelAlert:
	default:
		level = ""
	}
	f := notify.ListFilter{
		Level:   level,
		From:    fromBound,
		To:      toBound,
		Sort:    parseNotifySort(r.URL.Query().Get("sort")),
		SortDir: parseSortDir(r.URL.Query().Get("dir")),
	}
	switch strings.TrimSpace(r.URL.Query().Get("unread")) {
	case "1", "yes":
		f.UnreadOnly = true
	case "0", "no":
		f.ReadOnly = true
	}
	return f
}

func parseNotifySort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "level":
		return "level"
	case "created", "when", "":
		return "created"
	default:
		return "created"
	}
}

func notifySortDir(sort, dir string) string {
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "asc" || dir == "desc" {
		return dir
	}
	return "desc"
}

func dayRangeBounds(fromDay, toDay string) (fromBound, toBound string) {
	if fromDay != "" && toDay != "" && fromDay > toDay {
		fromDay, toDay = toDay, fromDay
	}
	if fromDay != "" {
		if t, err := time.ParseInLocation("2006-01-02", fromDay, time.UTC); err == nil {
			fromBound = t.UTC().Format(time.RFC3339Nano)
		}
	}
	if toDay != "" {
		if t, err := time.ParseInLocation("2006-01-02", toDay, time.UTC); err == nil {
			toBound = t.Add(24*time.Hour - time.Nanosecond).UTC().Format(time.RFC3339Nano)
		}
	}
	return fromBound, toBound
}

func notificationsFilterActive(f notify.ListFilter, fromDay, toDay string) bool {
	return f.Level != "" || f.UnreadOnly || f.ReadOnly || fromDay != "" || toDay != ""
}

func notificationsFilterSelects(r *http.Request, f notify.ListFilter) []listFilterSelect {
	levelOpts := []listFilterOpt{
		{Value: notify.LevelAlert, Label: "Alert", Selected: f.Level == notify.LevelAlert},
		{Value: notify.LevelWarning, Label: "Warning", Selected: f.Level == notify.LevelWarning},
		{Value: notify.LevelInfo, Label: "Info", Selected: f.Level == notify.LevelInfo},
	}
	selects := []listFilterSelect{
		{Name: "level", AriaLabel: "Level", Options: levelOpts},
	}
	var unreadSel *bool
	switch {
	case f.UnreadOnly:
		v := true
		unreadSel = &v
	case f.ReadOnly:
		v := false
		unreadSel = &v
	}
	selects = append(selects, boolOnlySelect(r, "unread", "Unread", "1", "0", unreadSel))
	annotateFilterSelects(r, selects[:1])
	return selects
}

func notificationsSortOpts(r *http.Request, sort, dir string) []listFilterOpt {
	sort = parseNotifySort(sort)
	opts := []listFilterOpt{
		{Value: "created", Label: "Created", Selected: sort == "created", Icon: "clock"},
		{Value: "level", Label: "Level", Selected: sort == "level", Icon: "triangle-alert"},
	}
	annotateSortOpts(r, opts, "created", dir)
	return opts
}

func notificationsListBadges(r *http.Request, f notify.ListFilter, fromDay, toDay string) []listViewBadge {
	var badges []listViewBadge
	if f.Level != "" {
		badges = append(badges, listViewBadge{Label: "Level: " + f.Level, Href: clearQueryKey(r, "level", "nlevel")})
	}
	if f.UnreadOnly {
		badges = append(badges, listViewBadge{Label: "Unread", Href: clearQueryKey(r, "unread")})
	}
	if f.ReadOnly {
		badges = append(badges, listViewBadge{Label: "Read", Href: clearQueryKey(r, "unread")})
	}
	if fromDay != "" || toDay != "" {
		var label string
		if fromDay != "" && toDay != "" {
			label = "Created: " + fromDay + "–" + toDay
		} else if fromDay != "" {
			label = "Created from " + fromDay
		} else {
			label = "Created to " + toDay
		}
		badges = append(badges, listViewBadge{Label: label, Href: clearQueryKeys(r, "from", "to")})
	}
	return badges
}

func notificationsTableColDefs() []tableColDef {
	return []tableColDef{
		{Key: "level", Label: "Level", Default: true},
		{Key: "title", Label: "Title", Default: true},
		{Key: "created", Label: "Created", Default: true},
		{Key: "event", Label: "Event", Default: true},
		{Key: "task", Label: "Task", Default: true},
		{Key: "read", Label: "Read", Default: true},
	}
}

func mergeNotificationsListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := mergeAbsentQueryKeys(q, readListPrefCookie(r, cookieFilterNotifications),
		"level", "unread", "from", "to")
	if _, ok := q["sort"]; !ok {
		sort, dir := parseSortCookie(readListPrefCookie(r, cookieSortNotifications))
		if s := parseNotifySort(sort); sort != "" && s != "" {
			q.Set("sort", s)
			changed = true
			if _, hasDir := q["dir"]; !hasDir {
				if d := parseSortDir(dir); d != "" {
					q.Set("dir", d)
				}
			}
		}
	}
	if !changed {
		return r
	}
	return cloneRequestQuery(r, q)
}

func writeNotificationsListPrefs(w http.ResponseWriter, r *http.Request, filter notify.ListFilter) {
	sort := parseNotifySort(filter.Sort)
	dir := notifySortDir(sort, filter.SortDir)
	writeListPrefCookie(w, cookieSortNotifications, encodeSortCookie(sort, dir))
	v := url.Values{}
	if filter.Level != "" {
		v.Set("level", filter.Level)
	}
	switch {
	case filter.UnreadOnly:
		v.Set("unread", "1")
	case filter.ReadOnly:
		v.Set("unread", "0")
	}
	if d := parseFilterDay(r.URL.Query().Get("from")); d != "" {
		v.Set("from", d)
	}
	if d := parseFilterDay(r.URL.Query().Get("to")); d != "" {
		v.Set("to", d)
	}
	writeListPrefCookie(w, cookieFilterNotifications, encodeFilterPrefCookie(v))
}
