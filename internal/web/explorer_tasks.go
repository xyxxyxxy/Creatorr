package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

const (
	cookieModeTasks       = "creatorr_mode_tasks"
	cookieSortTasks       = "creatorr_sort_tasks"
	cookieFilterTasks     = "creatorr_filter_tasks"
	cookieColsTasks       = "creatorr_cols_tasks"
	tasksListLiveTarget   = "tasks-list-live"
	tasksInfiniteID       = "tasks-list-infinite"
	tasksRowsID           = "tasks-list-rows"
	tasksExplorerPageSize = SeriesPageSize
)

type tasksListLiveData struct {
	Items           []taskExplorerRow
	ListRows        []taskView // list view: same layout as /tasks task_row
	Page            PageInfo
	Load            ListLoad
	ListMode        ListMode
	FilterTotal     int
	Filter          listViewToolbar
	FilterActive    bool
	ViewMode        string
	TableCols       []tableCol
	TableColsCookie string
	ShowToolbar     bool // false for Overview locked glance
	EmptyText       string
	InfiniteID      string
	RowsID          string
	OOB             bool
	BrowseHref      string // Overview: Browser Tasks URL matching the locked glance filter
}

// overviewVideosBrowseHref is Browser Videos with the Recent additions glance filter.
const overviewVideosBrowseHref = "/browser?type=videos&status=downloaded&sort=acquired&view=gallery"

// overviewSeriesBrowseHref is Browser Series with the Most wanted glance filter.
const overviewSeriesBrowseHref = "/browser?type=series&sort=wanted&view=gallery"

// browserTasksBrowseHref builds /browser?type=tasks with the overview glance filter.
func browserTasksBrowseHref(f queue.TaskListFilter) string {
	q := url.Values{}
	q.Set("type", explorerTypeTasks)
	for _, s := range f.Statuses {
		if s != "" {
			q.Add("status", s)
		}
	}
	if sort := queue.NormalizeTaskSort(f.Sort); sort != "" {
		q.Set("sort", sort)
	}
	if dir := strings.ToLower(strings.TrimSpace(f.SortDir)); dir == "asc" || dir == "desc" {
		q.Set("dir", dir)
	}
	return "/browser?" + q.Encode()
}

type taskExplorerRow struct {
	ID             int64
	Kind           string
	Status         string
	Domain         string
	Origin         string
	Message        string
	CreatedAt      string
	CreatedAgo     string
	DurationLabel  string
	InterruptCount int
	InterruptLabel string
	Progress       *float64
	ProgressLabel  string
	QueuePos       int
	QueuePosLabel  string
	ParentTaskID   int64
	SeriesID       int64
	SeriesTitle    string
	VideoID        int64
	VideoTitle     string
	Open           bool // pending|running
	LiveTarget     string
	Redirect       string
}

func (h *Handler) explorerBrowseTasks(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("HX-Target")
	if target == tasksInfiniteID {
		data, err := h.loadTasksListLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !data.Load.Append {
			maybeExplorerPushURL(w, r, explorerTypeTasks)
			render(w, "tasks_list_live", data)
			return
		}
		render(w, "tasks_infinite_chunk", data)
		return
	}
	data, err := h.loadTasksListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeTasks)
	render(w, "tasks_list_live", data)
}

func (h *Handler) loadTasksListLive(w http.ResponseWriter, r *http.Request) (tasksListLiveData, error) {
	at := explorerAtFrom(r, explorerAtBrowser)
	overview := at == explorerAtOverview
	if !overview {
		r = mergeTasksListPrefs(r)
	}
	filter := parseTasksExplorerFilter(r)
	if !overview {
		writeTasksListPrefs(w, r, filter)
	}

	viewMode := viewList
	if !overview {
		var writeCookie bool
		viewMode, writeCookie = resolveViewMode(r, cookieModeTasks, viewList)
		if mode := coerceListTableView(viewMode); mode != viewMode {
			viewMode = mode
			writeCookie = true
		}
		if writeCookie {
			writeViewCookie(w, cookieModeTasks, viewMode)
		}
	}

	var load ListLoad
	var items []queue.Task
	var total int
	var err error
	var listMode ListMode
	if overview {
		items, filter, total, err = h.loadOverviewTaskItems()
		if err != nil {
			return tasksListLiveData{}, err
		}
		load = resolveFixedLoad(total, OverviewTasksFixed)
		listMode = ListModeFixed
	} else {
		total, err = h.Queue.CountTasks(filter)
		if err != nil {
			return tasksListLiveData{}, err
		}
		listMode = libraryListMode(viewMode)
		var limit, offset int
		switch listMode {
		case ListModeInfinite:
			load = resolveInfiniteLoad(r, total, tasksListLiveTarget, tasksInfiniteID, "page")
			limit, offset = infiniteLimitOffset(load)
		default:
			load = resolvePaginatedLoad(r, total, tasksExplorerPageSize, tasksListLiveTarget, "page")
			limit = load.PageSize
			offset = OffsetSize(load.Page.Page, load.PageSize)
		}
		items, err = h.Queue.ListTasks(filter, limit, offset)
	}
	if err != nil {
		return tasksListLiveData{}, err
	}

	now := time.Now().UTC()
	atTasks := at == explorerAtTasks
	redirect := explorerCanonicalURL(r, explorerTypeTasks)
	if redirect == "" {
		if atTasks {
			redirect = "/tasks"
		} else if overview {
			redirect = "/"
		} else {
			redirect = "/browser?type=tasks"
		}
	}

	paused := map[string]bool{}
	if hosts, err := domains.ListPaused(h.Queue.DB); err == nil {
		for _, d := range hosts {
			paused[d] = true
		}
	}

	titles := map[int64]string{}
	videoTitles := map[int64]string{}
	rows := make([]taskExplorerRow, 0, len(items))
	listRows := make([]taskView, 0, len(items))
	for _, t := range items {
		er := h.taskToExplorerRow(t, now, titles, videoTitles, redirect)
		rows = append(rows, er)
		listRows = append(listRows, taskView{
			ID:          er.ID,
			Position:    er.QueuePos,
			Status:      er.Status,
			Kind:        er.Kind,
			Domain:      er.Domain,
			SeriesID:    er.SeriesID,
			SeriesTitle: er.SeriesTitle,
			VideoID:     er.VideoID,
			VideoTitle:  er.VideoTitle,
			Message:     er.Message,
			Progress:    er.Progress,
			LanePaused:  paused[er.Domain],
			Redirect:    redirect,
			NoActions:   overview,
		})
	}

	page := load.Page
	page.LiveTarget = tasksListLiveTarget
	if !overview {
		rewriteExplorerPageInfo(&page, explorerTypeTasks, 0)
	}

	fromDay, toDay := parseFilterDay(r.URL.Query().Get("from")), parseFilterDay(r.URL.Query().Get("to"))
	menuActive := tasksFilterActive(filter, fromDay, toDay)
	toolbar := listViewToolbar{
		AriaLabel:          "Task filters",
		Query:              filter.Q,
		QueryPlaceholder:   "Search",
		SortOpts:           tasksSortOpts(r, filter.Sort, filter.SortDir),
		SortDir:            tasksSortDir(filter.Sort, filter.SortDir),
		ViewOpts:           sourcesViewOpts(r, viewMode),
		ShowView:           true,
		FromDay:            fromDay,
		ToDay:              toDay,
		ShowDateRange:      true,
		ShowDatePresence:   false,
		DateRangeLabel:     "Created",
		Selects:            h.tasksFilterSelects(r, filter),
		FilterActive:       menuActive,
		LiveTarget:         tasksListLiveTarget,
		FormAction:         explorerFragmentPath(r),
	}
	if !overview {
		applyExplorerToolbar(&toolbar, explorerTypeTasks, at)
		if menuActive {
			toolbar.ClearAllHref = clearOperatorFiltersURL(r)
			toolbar.Badges = h.tasksListBadges(r, filter, fromDay, toDay)
		}
	}

	emptyText := "No tasks."

	out := tasksListLiveData{
		Items:           rows,
		ListRows:        listRows,
		Page:            page,
		Load:            load,
		ListMode:        listMode,
		FilterTotal:     total,
		Filter:          toolbar,
		FilterActive:    !overview && (menuActive || strings.TrimSpace(filter.Q) != ""),
		ViewMode:        viewMode,
		ShowToolbar:     !overview,
		EmptyText:       emptyText,
		InfiniteID:      tasksInfiniteID,
		RowsID:          tasksRowsID,
		TableCols: annotateTableColsSort(
			parseTableColsCookie(r, cookieColsTasks, tasksTableColDefs()),
			toolbar.SortOpts, toolbar.SortDir),
		TableColsCookie: cookieColsTasks,
	}
	if overview {
		out.BrowseHref = browserTasksBrowseHref(filter)
	}
	if !overview {
		rewriteExplorerInfinite(&out.Load, explorerTypeTasks, 0)
	}

	return out, nil
}

// loadOverviewTaskItems returns up to OverviewTasksFixed open tasks (running then
// queued). When the queues are idle, falls back to the newest finished tasks.
func (h *Handler) loadOverviewTaskItems() ([]queue.Task, queue.TaskListFilter, int, error) {
	open := queue.TaskListFilter{
		Statuses: []string{queue.StatusPending, queue.StatusRunning},
		Sort:     queue.TaskSortQueue,
	}
	n, err := h.Queue.CountTasks(open)
	if err != nil {
		return nil, open, 0, err
	}
	if n > 0 {
		items, err := h.Queue.ListTasks(open, OverviewTasksFixed, 0)
		return items, open, n, err
	}
	past := queue.TaskListFilter{
		Statuses: append([]string{}, queue.HistoryStatuses...),
		Sort:     queue.TaskSortCreated,
		SortDir:  "desc",
	}
	n, err = h.Queue.CountTasks(past)
	if err != nil {
		return nil, past, 0, err
	}
	items, err := h.Queue.ListTasks(past, OverviewTasksFixed, 0)
	return items, past, n, err
}

func (h *Handler) taskToExplorerRow(t queue.Task, now time.Time, titles, videoTitles map[int64]string, redirect string) taskExplorerRow {
	abs, ago := createdAgoPairCompact(t.CreatedAt, now)
	row := taskExplorerRow{
		ID:             t.ID,
		Kind:           t.Kind,
		Status:         t.Status,
		Domain:         t.Domain,
		Origin:         t.Origin,
		Message:        historyListMessage(t),
		CreatedAt:      abs,
		CreatedAgo:     ago,
		InterruptCount: t.InterruptCount,
		QueuePos:       t.QueuePos,
		Open:           t.Status == queue.StatusPending || t.Status == queue.StatusRunning,
		LiveTarget:     tasksListLiveTarget,
		Redirect:       redirect,
	}
	if t.InterruptCount > 0 {
		row.InterruptLabel = strconv.Itoa(t.InterruptCount)
	} else {
		row.InterruptLabel = "-"
	}
	if t.Status == queue.StatusPending && t.QueuePos > 0 {
		row.QueuePosLabel = "#" + strconv.Itoa(t.QueuePos)
	} else {
		row.QueuePosLabel = "-"
	}
	if t.Progress.Valid {
		p := t.Progress.Float64
		row.Progress = &p
		row.ProgressLabel = fmt.Sprintf("%.0f%%", p*100)
	} else {
		row.ProgressLabel = "-"
	}
	row.DurationLabel = taskDurationLabel(t, now)
	if t.ParentTaskID.Valid {
		row.ParentTaskID = t.ParentTaskID.Int64
	}
	if t.SeriesID.Valid {
		row.SeriesID = t.SeriesID.Int64
		if title, ok := titles[row.SeriesID]; ok {
			row.SeriesTitle = title
		} else if ser, err := h.Library.GetSeries(row.SeriesID, false); err == nil && ser != nil {
			row.SeriesTitle = ser.Title
			titles[row.SeriesID] = ser.Title
		}
	}
	if t.VideoID.Valid {
		row.VideoID = t.VideoID.Int64
		if title, ok := videoTitles[row.VideoID]; ok {
			row.VideoTitle = title
		} else if v, err := h.Library.GetVideo(row.VideoID); err == nil && v != nil {
			row.VideoTitle = v.Title
			videoTitles[row.VideoID] = row.VideoTitle
		}
	}
	return row
}

func taskDurationLabel(t queue.Task, now time.Time) string {
	switch t.Status {
	case queue.StatusPending:
		return "-"
	case queue.StatusRunning:
		if !t.StartedAt.Valid || t.StartedAt.String == "" {
			return "-"
		}
		start, err := time.Parse(time.RFC3339Nano, t.StartedAt.String)
		if err != nil {
			return "-"
		}
		return stageDurationLabel(now.Sub(start))
	default:
		end := now
		if t.FinishedAt.Valid && t.FinishedAt.String != "" {
			if ft, err := time.Parse(time.RFC3339Nano, t.FinishedAt.String); err == nil {
				end = ft
			}
		}
		start := end
		if t.StartedAt.Valid && t.StartedAt.String != "" {
			if st, err := time.Parse(time.RFC3339Nano, t.StartedAt.String); err == nil {
				start = st
			}
		} else if t.CreatedAt != "" {
			if ct, err := time.Parse(time.RFC3339Nano, t.CreatedAt); err == nil {
				start = ct
			}
		}
		if !end.After(start) {
			return "-"
		}
		return stageDurationLabel(end.Sub(start))
	}
}

func parseTasksExplorerFilter(r *http.Request) queue.TaskListFilter {
	fromDay := parseFilterDay(r.URL.Query().Get("from"))
	toDay := parseFilterDay(r.URL.Query().Get("to"))
	fromBound, toBound := dayRangeBounds(fromDay, toDay)
	q := r.URL.Query()
	f := queue.TaskListFilter{
		Domains: parseMultiQuery(q, "domain"),
		Kinds:   parseMultiQuery(q, "kind"),
		From:    fromBound,
		To:      toBound,
		Q:       strings.TrimSpace(q.Get("q")),
		Sort:    queue.NormalizeTaskSort(q.Get("sort")),
		SortDir: parseSortDir(q.Get("dir")),
	}
	for _, raw := range parseMultiQuery(q, "origin") {
		if queue.ValidOrigin(raw) {
			f.Origins = append(f.Origins, raw)
		}
	}
	if _, ok := q["status"]; ok {
		f.Statuses = normalizeTaskStatusQuery(q["status"])
	}
	return f
}

func normalizeTaskStatusQuery(raw []string) []string {
	allowed := map[string]struct{}{
		queue.StatusPending: {}, queue.StatusRunning: {},
		queue.StatusDone: {}, queue.StatusFailed: {}, queue.StatusCancelled: {},
	}
	seen := map[string]struct{}{}
	var out []string
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := allowed[s]; !ok {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func tasksSortDir(sort, dir string) string {
	sort = queue.NormalizeTaskSort(sort)
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "asc" || dir == "desc" {
		return dir
	}
	switch sort {
	case queue.TaskSortKind, queue.TaskSortDomain, queue.TaskSortStatus, queue.TaskSortParent:
		return "asc"
	default:
		return "desc"
	}
}

func tasksFilterActive(f queue.TaskListFilter, fromDay, toDay string) bool {
	return len(f.Statuses) > 0 || len(f.Domains) > 0 || len(f.Kinds) > 0 || len(f.Origins) > 0 || fromDay != "" || toDay != ""
}

func (h *Handler) tasksFilterSelects(r *http.Request, f queue.TaskListFilter) []listFilterSelect {
	statusSelected := map[string]bool{}
	for _, s := range f.Statuses {
		statusSelected[s] = true
	}
	// When Statuses empty (= all), none selected in UI.
	statusOpts := []listFilterOpt{
		{Value: queue.StatusPending, Label: "Queued", Selected: statusSelected[queue.StatusPending]},
		{Value: queue.StatusRunning, Label: "Running", Selected: statusSelected[queue.StatusRunning]},
		{Value: queue.StatusDone, Label: "Success", Selected: statusSelected[queue.StatusDone]},
		{Value: queue.StatusFailed, Label: "Failure", Selected: statusSelected[queue.StatusFailed]},
		{Value: queue.StatusCancelled, Label: "Cancelled", Selected: statusSelected[queue.StatusCancelled]},
	}
	domainsList, _ := h.Queue.DistinctTaskDomains()
	seenDom := map[string]struct{}{}
	for _, d := range domainsList {
		seenDom[d] = struct{}{}
	}
	if paused, err := domains.ListPaused(h.Queue.DB); err == nil {
		for _, d := range paused {
			if _, ok := seenDom[d]; ok {
				continue
			}
			domainsList = append(domainsList, d)
			seenDom[d] = struct{}{}
		}
	}
	domOpts := make([]listFilterOpt, 0, len(domainsList))
	domainSel := selectedSet(f.Domains)
	for _, d := range domainsList {
		domOpts = append(domOpts, listFilterOpt{Value: d, Label: d, Selected: domainSel[strings.ToLower(d)]})
	}
	kinds, _ := h.Queue.DistinctTaskKinds()
	kindSel := selectedSet(f.Kinds)
	kindOpts := make([]listFilterOpt, 0, len(kinds))
	for _, k := range kinds {
		kindOpts = append(kindOpts, listFilterOpt{Value: k, Label: k, Selected: kindSel[strings.ToLower(k)]})
	}
	originSel := selectedSet(f.Origins)
	originOpts := []listFilterOpt{
		{Value: queue.OriginManual, Label: "manual", Selected: originSel[queue.OriginManual]},
		{Value: queue.OriginScheduled, Label: "scheduled", Selected: originSel[queue.OriginScheduled]},
		{Value: queue.OriginBoot, Label: "boot", Selected: originSel[queue.OriginBoot]},
		{Value: queue.OriginTask, Label: "task", Selected: originSel[queue.OriginTask]},
	}
	selects := []listFilterSelect{
		{Name: "status", AriaLabel: "Status", Options: statusOpts},
		{Name: "domain", AriaLabel: "Domain", Options: domOpts},
		{Name: "kind", AriaLabel: "Kind", Options: kindOpts},
		{Name: "origin", AriaLabel: "Origin", Options: originOpts},
	}
	annotateFilterSelects(r, selects)
	return selects
}

func tasksSortOpts(r *http.Request, sort, dir string) []listFilterOpt {
	sort = queue.NormalizeTaskSort(sort)
	opts := []listFilterOpt{
		{Value: queue.TaskSortCreated, Label: "Created", Selected: sort == queue.TaskSortCreated, Icon: "clock"},
		{Value: queue.TaskSortDuration, Label: "Duration", Selected: sort == queue.TaskSortDuration, Icon: "timer"},
		{Value: queue.TaskSortInterrupted, Label: "Interrupted", Selected: sort == queue.TaskSortInterrupted, Icon: "rotate-ccw"},
		{Value: queue.TaskSortProgress, Label: "Progress", Selected: sort == queue.TaskSortProgress, Icon: "loader"},
		{Value: queue.TaskSortQueue, Label: "Queue", Selected: sort == queue.TaskSortQueue, Icon: "list-ordered"},
		{Value: queue.TaskSortKind, Label: "Kind", Selected: sort == queue.TaskSortKind, Icon: "shapes"},
		{Value: queue.TaskSortDomain, Label: "Domain", Selected: sort == queue.TaskSortDomain, Icon: "globe"},
		{Value: queue.TaskSortStatus, Label: "Status", Selected: sort == queue.TaskSortStatus, Icon: "circle-dot"},
		{Value: queue.TaskSortParent, Label: "Parent", Selected: sort == queue.TaskSortParent, Icon: "git-branch"},
	}
	annotateSortOpts(r, opts, queue.TaskSortCreated, dir)
	return opts
}

func (h *Handler) tasksListBadges(r *http.Request, f queue.TaskListFilter, fromDay, toDay string) []listViewBadge {
	var badges []listViewBadge
	if len(f.Statuses) > 0 {
		labels := map[string]string{
			queue.StatusPending: "Queued", queue.StatusRunning: "Running",
			queue.StatusDone: "Success", queue.StatusFailed: "Failure", queue.StatusCancelled: "Cancelled",
		}
		badges = append(badges, orJoinBadges(r, "status", "Status", "status", f.Statuses, func(s string) string {
			if l, ok := labels[s]; ok {
				return l
			}
			return s
		})...)
	}
	badges = append(badges, orJoinBadges(r, "domain", "Domain", "domain", f.Domains, nil)...)
	badges = append(badges, orJoinBadges(r, "kind", "Kind", "kind", f.Kinds, nil)...)
	badges = append(badges, orJoinBadges(r, "origin", "Origin", "origin", f.Origins, nil)...)
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

func tasksTableColDefs() []tableColDef {
	return []tableColDef{
		{Key: "kind", Label: "Kind", Default: true},
		{Key: "status", Label: "Status", Default: true},
		{Key: "domain", Label: "Domain", Default: true},
		{Key: "origin", Label: "Origin", Default: true},
		{Key: "parent", Label: "Parent", Default: true},
		{Key: "created", Label: "Created", Default: true},
		{Key: "duration", Label: "Duration", Default: true},
		{Key: "interrupted", Label: "Interrupted", Default: true},
		{Key: "progress", Label: "Progress", Default: true},
		{Key: "queue", Label: "Queue pos", Default: true},
		{Key: "message", Label: "Message", Default: true},
		{Key: "series", Label: "Series", Default: true},
		{Key: "video", Label: "Video", Default: true},
	}
}

func mergeTasksListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := mergeAbsentQueryKeys(q, readListPrefCookie(r, cookieFilterTasks),
		"status", "domain", "kind", "origin", "from", "to")
	if _, ok := q["sort"]; !ok {
		sort, dir := parseSortCookie(readListPrefCookie(r, cookieSortTasks))
		if s := queue.NormalizeTaskSort(sort); sort != "" {
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

func writeTasksListPrefs(w http.ResponseWriter, r *http.Request, filter queue.TaskListFilter) {
	sort := queue.NormalizeTaskSort(filter.Sort)
	dir := tasksSortDir(sort, filter.SortDir)
	writeListPrefCookie(w, cookieSortTasks, encodeSortCookie(sort, dir))
	v := url.Values{}
	for _, st := range filter.Statuses {
		if st = strings.TrimSpace(st); st != "" {
			v.Add("status", st)
		}
	}
	for _, d := range filter.Domains {
		if d = strings.TrimSpace(d); d != "" {
			v.Add("domain", d)
		}
	}
	for _, k := range filter.Kinds {
		if k = strings.TrimSpace(k); k != "" {
			v.Add("kind", k)
		}
	}
	for _, o := range filter.Origins {
		if o = strings.TrimSpace(o); o != "" {
			v.Add("origin", o)
		}
	}
	if d := parseFilterDay(r.URL.Query().Get("from")); d != "" {
		v.Set("from", d)
	}
	if d := parseFilterDay(r.URL.Query().Get("to")); d != "" {
		v.Set("to", d)
	}
	writeListPrefCookie(w, cookieFilterTasks, encodeFilterPrefCookie(v))
}
