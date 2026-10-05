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
	notificationsInfiniteID   = "notifications-list-infinite"
	notificationsRowsID       = "notifications-list-rows"
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
	InfiniteID       string
	RowsID           string
	OOB              bool
}

type notifyExplorerRow struct {
	notifyHistoryView
	CanToggle  bool
	LiveTarget string
	Redirect   string
}

func (h *Handler) explorerBrowseNotifications(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("HX-Target")
	if target == notificationsInfiniteID {
		data, err := h.loadNotificationsListLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !data.Load.Append {
			maybeExplorerPushURL(w, r, explorerTypeNotifications)
			render(w, "notifications_list_live", data)
			return
		}
		render(w, "notifications_infinite_chunk", data)
		return
	}
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
	mode := libraryListMode(viewMode)
	var load ListLoad
	var limit, offset int
	switch mode {
	case ListModeInfinite:
		load = resolveInfiniteLoad(r, total, notificationsLiveTarget, notificationsInfiniteID, "page")
		limit, offset = infiniteLimitOffset(load)
	default:
		load = resolvePaginatedLoad(r, total, notificationsPageSize, notificationsLiveTarget, "page")
		limit = load.PageSize
		offset = OffsetSize(load.Page.Page, load.PageSize)
	}
	items, err := notify.ListNotifications(h.Queue.DB, filter, limit, offset)
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
			CanToggle:         true,
			LiveTarget:        notificationsLiveTarget,
			Redirect:          redirect,
		})
	}

	page := load.Page
	page.LiveTarget = notificationsLiveTarget
	rewriteExplorerPageInfo(&page, explorerTypeNotifications, 0)

	fromDay, toDay := parseFilterDay(r.URL.Query().Get("from")), parseFilterDay(r.URL.Query().Get("to"))
	menuActive := notificationsFilterActive(filter, fromDay, toDay)
	toolbar := listViewToolbar{
		AriaLabel:             "Notification filters",
		Query:                 filter.Q,
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
		FilterActive:          menuActive,
		LiveTarget:            notificationsLiveTarget,
		FormAction:            explorerFragmentPath(r),
		NotificationsBulkMode: true,
	}
	applyExplorerToolbar(&toolbar, explorerTypeNotifications, at)
	if menuActive {
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
		ListMode:        mode,
		FilterTotal:     total,
		SelectableTotal: selTotal,
		Filter:          toolbar,
		FilterActive:    menuActive || strings.TrimSpace(filter.Q) != "",
		ViewMode:        viewMode,
		TableCols:       cols,
		TableColsCookie: cookieColsNotifications,
		ShowSelectAll:   selTotal > pageSel,
		InfiniteID:      notificationsInfiniteID,
		RowsID:          notificationsRowsID,
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
	q := r.URL.Query()
	var levels []string
	for _, raw := range q["level"] {
		switch strings.TrimSpace(raw) {
		case notify.LevelInfo, notify.LevelWarning, notify.LevelAlert:
			levels = append(levels, strings.TrimSpace(raw))
		}
	}
	if len(levels) == 0 {
		if legacy := strings.TrimSpace(q.Get("nlevel")); legacy != "" {
			switch legacy {
			case notify.LevelInfo, notify.LevelWarning, notify.LevelAlert:
				levels = append(levels, legacy)
			}
		}
	}
	f := notify.ListFilter{
		Levels:  uniqueQueryVals(levels),
		From:    fromBound,
		To:      toBound,
		Q:       strings.TrimSpace(q.Get("q")),
		Sort:    parseNotifySort(q.Get("sort")),
		SortDir: parseSortDir(q.Get("dir")),
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
	return len(f.Levels) > 0 || f.UnreadOnly || f.ReadOnly || fromDay != "" || toDay != ""
}

func notificationsFilterSelects(r *http.Request, f notify.ListFilter) []listFilterSelect {
	levelSel := selectedSet(f.Levels)
	levelOpts := []listFilterOpt{
		{Value: notify.LevelAlert, Label: "Alert", Selected: levelSel[notify.LevelAlert]},
		{Value: notify.LevelWarning, Label: "Warning", Selected: levelSel[notify.LevelWarning]},
		{Value: notify.LevelInfo, Label: "Info", Selected: levelSel[notify.LevelInfo]},
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
	badges = append(badges, orJoinBadges(r, "level", "Level", "level", f.Levels, nil)...)
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
	for _, lv := range filter.Levels {
		if lv = strings.TrimSpace(lv); lv != "" {
			v.Add("level", lv)
		}
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
