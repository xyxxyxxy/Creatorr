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
	OOB             bool
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
	data, err := h.loadTasksListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeTasks)
	render(w, "tasks_list_live", data)
}

func (h *Handler) loadTasksListLive(w http.ResponseWriter, r *http.Request) (tasksListLiveData, error) {
	r = mergeTasksListPrefs(r)
	filter := parseTasksExplorerFilter(r)
	writeTasksListPrefs(w, r, filter)

	viewMode, writeCookie := resolveViewMode(r, cookieModeTasks, viewList)
	if mode := coerceListTableView(viewMode); mode != viewMode {
		viewMode = mode
		writeCookie = true
	}
	if writeCookie {
		writeViewCookie(w, cookieModeTasks, viewMode)
	}

	total, err := h.Queue.CountTasks(filter)
	if err != nil {
		return tasksListLiveData{}, err
	}
	load := resolvePaginatedLoad(r, total, tasksExplorerPageSize, tasksListLiveTarget, "page")
	items, err := h.Queue.ListTasks(filter, load.PageSize, OffsetSize(load.Page.Page, load.PageSize))
	if err != nil {
		return tasksListLiveData{}, err
	}

	now := time.Now().UTC()
	at := explorerAtFrom(r, explorerAtBrowser)
	atTasks := at == explorerAtTasks
	redirect := explorerCanonicalURL(r, explorerTypeTasks)
	if redirect == "" {
		if atTasks {
			redirect = "/tasks"
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
		})
	}

	page := load.Page
	page.LiveTarget = tasksListLiveTarget
	rewriteExplorerPageInfo(&page, explorerTypeTasks, 0)

	fromDay, toDay := parseFilterDay(r.URL.Query().Get("from")), parseFilterDay(r.URL.Query().Get("to"))
	toolbar := listViewToolbar{
		AriaLabel:          "Task filters",
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
		FilterActive:       tasksFilterActive(filter, fromDay, toDay),
		LiveTarget:         tasksListLiveTarget,
		FormAction:         explorerFragmentPath(r),
	}
	applyExplorerToolbar(&toolbar, explorerTypeTasks, at)
	if toolbar.FilterActive {
		toolbar.ClearAllHref = clearOperatorFiltersURL(r)
		toolbar.Badges = h.tasksListBadges(r, filter, fromDay, toDay)
	}

	out := tasksListLiveData{
		Items:           rows,
		ListRows:        listRows,
		Page:            page,
		Load:            load,
		ListMode:        ListModePaginated,
		FilterTotal:     total,
		Filter:          toolbar,
		FilterActive:    toolbar.FilterActive,
		ViewMode:        viewMode,
		TableCols: annotateTableColsSort(
			parseTableColsCookie(r, cookieColsTasks, tasksTableColDefs()),
			toolbar.SortOpts, toolbar.SortDir),
		TableColsCookie: cookieColsTasks,
	}
	rewriteExplorerInfinite(&out.Load, explorerTypeTasks, 0)

	return out, nil
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
	origin := strings.TrimSpace(r.URL.Query().Get("origin"))
	if !queue.ValidOrigin(origin) {
		origin = ""
	}
	f := queue.TaskListFilter{
		Domain:  strings.TrimSpace(r.URL.Query().Get("domain")),
		Kind:    strings.TrimSpace(r.URL.Query().Get("kind")),
		Origin:  origin,
		From:    fromBound,
		To:      toBound,
		Sort:    queue.NormalizeTaskSort(r.URL.Query().Get("sort")),
		SortDir: parseSortDir(r.URL.Query().Get("dir")),
	}
	if _, ok := r.URL.Query()["status"]; ok {
		f.Statuses = normalizeTaskStatusQuery(r.URL.Query()["status"])
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
	return len(f.Statuses) > 0 || f.Domain != "" || f.Kind != "" || f.Origin != "" || fromDay != "" || toDay != ""
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
	for _, d := range domainsList {
		domOpts = append(domOpts, listFilterOpt{Value: d, Label: d, Selected: f.Domain == d})
	}
	kinds, _ := h.Queue.DistinctTaskKinds()
	kindOpts := make([]listFilterOpt, 0, len(kinds))
	for _, k := range kinds {
		kindOpts = append(kindOpts, listFilterOpt{Value: k, Label: k, Selected: f.Kind == k})
	}
	originOpts := []listFilterOpt{
		{Value: queue.OriginManual, Label: "manual", Selected: f.Origin == queue.OriginManual},
		{Value: queue.OriginScheduled, Label: "scheduled", Selected: f.Origin == queue.OriginScheduled},
		{Value: queue.OriginBoot, Label: "boot", Selected: f.Origin == queue.OriginBoot},
		{Value: queue.OriginTask, Label: "task", Selected: f.Origin == queue.OriginTask},
	}
	selects := []listFilterSelect{
		{Name: "status", AriaLabel: "Status", Options: statusOpts, Multi: true},
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
		parts := make([]string, 0, len(f.Statuses))
		for _, s := range f.Statuses {
			if l, ok := labels[s]; ok {
				parts = append(parts, l)
			} else {
				parts = append(parts, s)
			}
		}
		badges = append(badges, listViewBadge{Label: "Status: " + strings.Join(parts, ", "), Href: clearQueryKey(r, "status")})
	}
	if f.Domain != "" {
		badges = append(badges, listViewBadge{Label: "Domain: " + f.Domain, Href: clearQueryKey(r, "domain")})
	}
	if f.Kind != "" {
		badges = append(badges, listViewBadge{Label: "Kind: " + f.Kind, Href: clearQueryKey(r, "kind")})
	}
	if f.Origin != "" {
		badges = append(badges, listViewBadge{Label: "Origin: " + f.Origin, Href: clearQueryKey(r, "origin")})
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
	if d := strings.TrimSpace(filter.Domain); d != "" {
		v.Set("domain", d)
	}
	if k := strings.TrimSpace(filter.Kind); k != "" {
		v.Set("kind", k)
	}
	if o := strings.TrimSpace(filter.Origin); o != "" {
		v.Set("origin", o)
	}
	if d := parseFilterDay(r.URL.Query().Get("from")); d != "" {
		v.Set("from", d)
	}
	if d := parseFilterDay(r.URL.Query().Get("to")); d != "" {
		v.Set("to", d)
	}
	writeListPrefCookie(w, cookieFilterTasks, encodeFilterPrefCookie(v))
}
