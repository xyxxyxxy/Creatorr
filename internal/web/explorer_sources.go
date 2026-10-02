package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

const (
	cookieModeSources = "creatorr_mode_sources"
	cookieSortSources = "creatorr_sort_sources"
	sourcesLiveTarget = "sources-list-live"
	sourcesInfiniteID = "sources-list-infinite"
	sourcesRowsID     = "sources-list-rows"
)

type sourcesListLiveData struct {
	Sources         []sourcesExplorerRow
	Page            PageInfo
	Load            ListLoad
	ListMode        ListMode
	FilterTotal     int
	SourceFilter    listViewToolbar
	FilterActive    bool
	ViewMode        string
	TableCols       []tableCol
	TableColsCookie string
	ShowSelectAll   bool
	InfiniteID      string
	RowsID          string
	SeriesID        int64 // locked scope when > 0
	SeriesTitle     string
	ImportNullCount int
	ShowSeriesCol   bool
	ShowRowActions  bool // series-detail: scan/edit/delete join
	ShowToolbar     bool // Browser Sources only; series-detail is a plain list
	OOB             bool
}

type sourcesExplorerRow struct {
	library.SourceListRow
	SeriesMonitored     bool
	ScanActive          bool
	FullScanStalled     bool
	DomainHost          string
	DomainActive        bool
	DomainDisabledTitle string
	ScanCronLabel       string
	LastScannedAgo      string
	LastScannedTip      string
	StatusInd           sourceStatusView
	HasRetryable        bool
	VideoCount          int
	Redirect            string
	LiveTarget          string
	ShowSeries          bool
	ShowRowActions      bool
	OOB                 bool
}

func (h *Handler) explorerBrowseSources(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("HX-Target")
	if target == sourcesInfiniteID {
		data, err := h.loadSourcesListLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !data.Load.Append {
			maybeExplorerPushURL(w, r, explorerTypeSources)
			render(w, "sources_list_live", data)
			return
		}
		render(w, "sources_infinite_chunk", data)
		return
	}
	data, err := h.loadSourcesListLive(w, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	maybeExplorerPushURL(w, r, explorerTypeSources)
	render(w, "sources_list_live", data)
}

func parseSourceListFilter(r *http.Request) library.SourceListFilter {
	f := library.SourceListFilter{
		Q:       strings.TrimSpace(r.URL.Query().Get("q")),
		QField:  library.NormalizeSourceQField(r.URL.Query().Get("q_field")),
		Kind:    strings.TrimSpace(r.URL.Query().Get("kind")),
		Domain:  strings.TrimSpace(r.URL.Query().Get("domain")),
		Sort:    parseSourceSort(r.URL.Query().Get("sort")),
		SortDir: parseSortDir(r.URL.Query().Get("dir")),
	}
	if sid, err := strconv.ParseInt(r.URL.Query().Get("series_id"), 10, 64); err == nil && sid > 0 {
		f.SeriesID = sid
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("full_scan"))) {
	case library.SourceFullScanDone:
		v := true
		f.FullScanDone = &v
	case library.SourceFullScanIncomplete:
		v := false
		f.FullScanDone = &v
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("schedule"))) {
	case library.SourceScheduleOn:
		v := true
		f.ScheduleOn = &v
	case library.SourceScheduleOff:
		v := false
		f.ScheduleOn = &v
	}
	switch strings.TrimSpace(r.URL.Query().Get("monitored")) {
	case "1":
		v := true
		f.SeriesMonitored = &v
	case "0":
		v := false
		f.SeriesMonitored = &v
	}
	empty, notEmpty := parsePresenceParams(r.URL.Query())
	for _, e := range empty {
		if strings.EqualFold(e, library.PresenceScanError) {
			v := false
			f.HasError = &v
		}
	}
	for _, e := range notEmpty {
		if strings.EqualFold(e, library.PresenceScanError) {
			v := true
			f.HasError = &v
		}
	}
	return f
}

func parseSourceSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case library.SortTitle, library.SortSourceSeries:
		return library.SortSourceSeries
	case library.SortSourceLabel, "url":
		return library.SortSourceLabel
	case library.SortSourceKind:
		return library.SortSourceKind
	case library.SortSourceDomain:
		return library.SortSourceDomain
	case library.SortSourceLastScanned, "scanned":
		return library.SortSourceLastScanned
	default:
		return ""
	}
}

func mergeSourcesListPrefs(r *http.Request) *http.Request {
	q := r.URL.Query()
	changed := false
	if _, ok := q["sort"]; !ok {
		if raw := readListPrefCookie(r, cookieSortSources); raw != "" {
			sort, dir := parseSortCookie(raw)
			if s := parseSourceSort(sort); s != "" {
				q.Set("sort", s)
				changed = true
			}
			if dir != "" {
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

func writeSourcesListPrefs(w http.ResponseWriter, f library.SourceListFilter) {
	writeListPrefCookie(w, cookieSortSources, encodeSortCookie(f.Sort, f.SortDir))
}

func resolveSourcesViewMode(r *http.Request) (mode string, writeCookie bool) {
	mode, writeCookie = resolveViewMode(r, cookieModeSources, viewList)
	if mode == viewCards || mode == viewGallery || mode == legacyViewThumbs {
		return viewList, true
	}
	if mode != viewList && mode != viewTable {
		return viewList, true
	}
	return mode, writeCookie
}

func (h *Handler) loadSourcesListLive(w http.ResponseWriter, r *http.Request) (sourcesListLiveData, error) {
	filter := parseSourceListFilter(r)
	seriesDetail := filter.SeriesID > 0
	if !seriesDetail {
		r = mergeSourcesListPrefs(r)
		filter = parseSourceListFilter(r)
	}
	if filter.Sort == "" {
		filter.Sort = library.SortSourceSeries
	}

	var viewMode string
	if seriesDetail {
		// Series page: plain list of this series' sources (no Filter bar / view toggle).
		sid := filter.SeriesID
		filter = library.SourceListFilter{
			SeriesID: sid,
			Sort:     library.SortSourceLabel,
			SortDir:  library.SortDirAsc,
		}
		viewMode = viewList
	} else {
		writeSourcesListPrefs(w, filter)
		var writeCookie bool
		viewMode, writeCookie = resolveSourcesViewMode(r)
		if writeCookie {
			writeViewCookie(w, cookieModeSources, viewMode)
		}
	}

	total, err := h.Library.CountSourcesFiltered(filter)
	if err != nil {
		return sourcesListLiveData{}, err
	}

	var load ListLoad
	var limit, offset int
	var mode ListMode
	if seriesDetail {
		mode = ListModePaginated
		limit = total
		if limit < 1 {
			limit = 1
		}
		offset = 0
		load = ListLoad{LoadedCount: total}
	} else {
		mode = libraryListMode(viewMode)
		switch mode {
		case ListModeInfinite:
			load = resolveInfiniteLoad(r, total, sourcesLiveTarget, sourcesInfiniteID, "page")
			limit, offset = infiniteLimitOffset(load)
		default:
			load = resolvePaginatedLoad(r, total, SeriesPageSize, sourcesLiveTarget, "page")
			limit = load.PageSize
			offset = OffsetSize(load.Page.Page, load.PageSize)
		}
	}

	list, err := h.Library.ListSourcesFiltered(filter, limit, offset)
	if err != nil {
		return sourcesListLiveData{}, err
	}

	at := explorerAtFrom(r, explorerAtBrowser)
	if filter.SeriesID > 0 {
		at = explorerAtSeriesDetail
	}
	showSeries := filter.SeriesID == 0
	showActions := filter.SeriesID > 0

	now := time.Now().UTC()
	rows := make([]sourcesExplorerRow, 0, len(list))
	redir := r.URL.RequestURI()
	var vcounts map[int64]int
	if filter.SeriesID > 0 {
		vcounts, _ = h.Library.CountVideosBySource(filter.SeriesID)
	}
	for _, src := range list {
		active, _ := h.Library.HasActiveScanForSource(src.ID)
		stalled := !src.FullScanDone && !active
		host := queue.DomainFromURL(src.URL)
		dAct, _ := domains.IsActive(h.Queue.DB, host)
		disTitle := ""
		if !dAct {
			disTitle = "Domain " + host + " is inactive. Activate it under 'Settings → Queue / Domains'."
		}
		summary, _, errMsg, errCode, taskID, hasScanned, hasError := sourceStatusFields(h.Library, src.ID, now)
		tipAt, _ := h.Library.LatestTipScannedAt(src.ID)
		cronLabel := cronexpr.DescribeScan(src.ScanCron)
		statusInd := buildSourceStatus(sourceStatusParams{
			Src: src.Source, Best: nil, HasError: hasError, ErrMsg: errMsg, ErrCode: errCode, Stalled: stalled,
			SeriesMonitored: src.SeriesMonitored, DomainActive: dAct, DomainDisabledTitle: disTitle,
			ScanCronLabel: cronLabel, Summary: summary, HasScanned: hasScanned, HistoryID: taskID,
			Now: now, LastTipScannedAt: tipAt,
		})
		retryable, _ := h.Library.SourceHasRetryableVideos(src.ID)
		vc := 0
		if vcounts != nil {
			vc = vcounts[src.ID]
		}
		lastAgo, lastTip := "", ""
		if src.LastScannedAt != "" {
			lastTip, lastAgo = createdAgoPairShort(src.LastScannedAt, now)
		}
		rows = append(rows, sourcesExplorerRow{
			SourceListRow:       src,
			SeriesMonitored:     src.SeriesMonitored,
			ScanActive:          active,
			FullScanStalled:     stalled,
			DomainHost:          host,
			DomainActive:        dAct,
			DomainDisabledTitle: disTitle,
			ScanCronLabel:       cronLabel,
			LastScannedAgo:      lastAgo,
			LastScannedTip:      lastTip,
			StatusInd:           statusInd,
			HasRetryable:        retryable,
			VideoCount:          vc,
			Redirect:            redir,
			LiveTarget:          sourcesLiveTarget,
			ShowSeries:          showSeries,
			ShowRowActions:      showActions,
		})
	}

	var toolbar listViewToolbar
	if !seriesDetail {
		toolbar = listViewToolbar{
			Query:            filter.Q,
			QueryPlaceholder: searchByPlaceholder(filter.QField),
			AriaLabel:        "Source filters",
			QFieldOpts:       sourceQFieldOpts(filter.QField, showSeries),
			SortOpts:         sourcesSortOpts(r, filter.Sort, filter.SortDir),
			SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
			ViewOpts:         sourcesViewOpts(r, viewMode),
			ShowView:         true,
			Selects:          sourcesFilterSelects(h, r, filter, showSeries),
			FilterActive:     filter.Active(),
			Badges:           sourcesListBadges(r, filter),
			LiveTarget:       sourcesLiveTarget,
			FormAction:       "/explorer/browse",
		}
		if filter.Active() {
			toolbar.ClearAllHref = clearOperatorFiltersURL(r)
		}
		applyExplorerToolbar(&toolbar, explorerTypeSources, at)
	}

	out := sourcesListLiveData{
		Sources:         rows,
		Page:            load.Page,
		Load:            load,
		ListMode:        mode,
		FilterTotal:     total,
		SourceFilter:    toolbar,
		FilterActive:    !seriesDetail && filter.Active(),
		ViewMode:        viewMode,
		TableCols:       parseTableColsCookie(r, "creatorr_cols_sources", sourcesTableColDefs(showSeries)),
		TableColsCookie: "creatorr_cols_sources",
		InfiniteID:      sourcesInfiniteID,
		RowsID:          sourcesRowsID,
		SeriesID:        filter.SeriesID,
		ShowSeriesCol:   showSeries,
		ShowRowActions:  showActions,
		ShowToolbar:     !seriesDetail,
	}
	if filter.SeriesID > 0 {
		if ser, err := h.Library.GetSeries(filter.SeriesID, false); err == nil && ser != nil {
			out.SeriesTitle = ser.Title
		}
		out.ImportNullCount, _ = h.Library.CountVideosWithNullSource(filter.SeriesID)
	}
	if !seriesDetail {
		rewriteExplorerInfinite(&out.Load, explorerTypeSources, filter.SeriesID)
		out.Page = out.Load.Page
	}
	return out, nil
}

func sourcesSortOpts(r *http.Request, current, curDir string) []listFilterOpt {
	cur := current
	if cur == "" {
		cur = library.SortSourceSeries
	}
	opts := []listFilterOpt{
		{Value: library.SortSourceSeries, Label: "Series", Selected: cur == library.SortSourceSeries || cur == library.SortTitle, Icon: "tv"},
		{Value: library.SortSourceLabel, Label: "Name", Selected: cur == library.SortSourceLabel, Icon: "type"},
		{Value: library.SortSourceDomain, Label: "Domain", Selected: cur == library.SortSourceDomain, Icon: "globe"},
		{Value: library.SortSourceKind, Label: "Kind", Selected: cur == library.SortSourceKind, Icon: "shapes"},
		{Value: library.SortSourceLastScanned, Label: "Last scanned", Selected: cur == library.SortSourceLastScanned, Icon: "clock"},
	}
	annotateSortOpts(r, opts, library.SortSourceSeries, curDir)
	return opts
}

func sourcesViewOpts(r *http.Request, current string) []listFilterOpt {
	cur := current
	if cur == "" {
		cur = viewList
	}
	opts := []listFilterOpt{
		{Value: viewList, Label: "List", Selected: cur == viewList, Icon: "layout-list"},
		{Value: viewTable, Label: "Table", Selected: cur == viewTable, Icon: "table"},
	}
	annotateViewOpts(r, opts)
	return opts
}

func sourcesFilterSelects(h *Handler, r *http.Request, filter library.SourceListFilter, showSeries bool) []listFilterSelect {
	scoped := filter.SeriesID > 0
	var facets library.SourceFilterFacets
	if scoped {
		facets, _ = h.Library.SourceFilterFacetsForSeries(filter.SeriesID)
	}

	var selects []listFilterSelect
	kind := filter.Kind
	showKind := !scoped || len(facets.Kinds) >= 2
	if showKind {
		selects = append(selects, listFilterSelect{
			Name:       "kind",
			AriaLabel:  "Kind",
			EmptyLabel: "Any",
			Options: []listFilterOpt{
				{Value: library.SourceKindFeed, Label: "Feed", Selected: kind == library.SourceKindFeed},
				{Value: library.SourceKindSingle, Label: "Single", Selected: kind == library.SourceKindSingle},
			},
		})
	}

	var domains []string
	if scoped {
		domains = facets.Domains
	} else if list, err := h.Library.ListSourceDomains(); err == nil {
		domains = list
	}
	showDomain := len(domains) >= 2 || (!scoped && len(domains) > 0)
	if showDomain {
		domOpts := make([]listFilterOpt, 0, len(domains))
		for _, d := range domains {
			domOpts = append(domOpts, listFilterOpt{Value: d, Label: d, Selected: filter.Domain == d})
		}
		selects = append(selects, listFilterSelect{
			Name: "domain", AriaLabel: "Domain", EmptyLabel: "Any", Options: domOpts,
		})
	}

	fullScan := ""
	if filter.FullScanDone != nil {
		if *filter.FullScanDone {
			fullScan = library.SourceFullScanDone
		} else {
			fullScan = library.SourceFullScanIncomplete
		}
	}
	showFullScan := !scoped || (facets.HasFullScanDone && facets.HasFullScanIncomplete)
	if showFullScan {
		selects = append(selects, listFilterSelect{
			Name:       "full_scan",
			AriaLabel:  "Full scan",
			EmptyLabel: "Any",
			Options: []listFilterOpt{
				{Value: library.SourceFullScanDone, Label: "Done", Selected: fullScan == library.SourceFullScanDone},
				{Value: library.SourceFullScanIncomplete, Label: "Incomplete", Selected: fullScan == library.SourceFullScanIncomplete},
			},
		})
	}

	sched := ""
	if filter.ScheduleOn != nil {
		if *filter.ScheduleOn {
			sched = library.SourceScheduleOn
		} else {
			sched = library.SourceScheduleOff
		}
	}
	showSchedule := !scoped || (facets.HasScheduleOn && facets.HasScheduleOff)
	if showSchedule {
		selects = append(selects, listFilterSelect{
			Name:       "schedule",
			AriaLabel:  "Schedule",
			EmptyLabel: "Any",
			Options: []listFilterOpt{
				{Value: library.SourceScheduleOn, Label: "On", Selected: sched == library.SourceScheduleOn},
				{Value: library.SourceScheduleOff, Label: "Off", Selected: sched == library.SourceScheduleOff},
			},
		})
	}

	if showSeries {
		mon := ""
		if filter.SeriesMonitored != nil {
			if *filter.SeriesMonitored {
				mon = "1"
			} else {
				mon = "0"
			}
		}
		selects = append(selects, listFilterSelect{
			Name:       "monitored",
			AriaLabel:  "Series monitored",
			EmptyLabel: "Any",
			Options: []listFilterOpt{
				{Value: "1", Label: "Yes", Selected: mon == "1"},
				{Value: "0", Label: "No", Selected: mon == "0"},
			},
		})
	}
	selects = append(selects, presenceOnlySelect(r, library.PresenceScanError, "Scan error"))
	annotateFilterSelects(r, selects)
	return selects
}

func sourcesListBadges(r *http.Request, filter library.SourceListFilter) []listViewBadge {
	var out []listViewBadge
	if filter.Kind != "" {
		out = append(out, listViewBadge{Label: "Kind: " + filter.Kind, Href: dropQueryKeys(r, "kind", "page", "through")})
	}
	if d := strings.TrimSpace(filter.Domain); d != "" {
		out = append(out, listViewBadge{Label: "Domain: " + d, Href: dropQueryKeys(r, "domain", "page", "through")})
	}
	if filter.FullScanDone != nil {
		label := "Full scan: Incomplete"
		if *filter.FullScanDone {
			label = "Full scan: Done"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "full_scan", "page", "through")})
	}
	if filter.ScheduleOn != nil {
		label := "Schedule: Off"
		if *filter.ScheduleOn {
			label = "Schedule: On"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "schedule", "page", "through")})
	}
	if filter.SeriesMonitored != nil {
		label := "Series monitored: No"
		if *filter.SeriesMonitored {
			label = "Series monitored: Yes"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "monitored", "page", "through")})
	}
	empty, notEmpty := parsePresenceParams(r.URL.Query())
	for _, e := range empty {
		if strings.EqualFold(e, library.PresenceScanError) {
			out = append(out, listViewBadge{Label: presenceBadgeLabel(e, true), Href: dropQueryValue(r, "empty", e)})
		}
	}
	for _, e := range notEmpty {
		if strings.EqualFold(e, library.PresenceScanError) {
			out = append(out, listViewBadge{Label: presenceBadgeLabel(e, false), Href: dropQueryValue(r, "not_empty", e)})
		}
	}
	return out
}

func sourcesTableColDefs(showSeries bool) []tableColDef {
	cols := []tableColDef{}
	if showSeries {
		cols = append(cols, tableColDef{Key: "series", Label: "Series", Default: true})
	}
	cols = append(cols,
		tableColDef{Key: "name", Label: "Name", Default: true},
		tableColDef{Key: "url", Label: "URL", Default: true},
		tableColDef{Key: "domain", Label: "Domain", Default: true},
		tableColDef{Key: "kind", Label: "Kind", Default: true},
		tableColDef{Key: "last_scanned", Label: "Last scanned", Default: true},
		tableColDef{Key: "status", Label: "Status", Default: true},
	)
	return cols
}

func sourceQFieldOpts(current string, showSeries bool) []listFilterOpt {
	cur := library.NormalizeSourceQField(current)
	opts := []listFilterOpt{
		{Value: library.QFieldSourceURL, Label: "URL", Selected: cur == library.QFieldSourceURL},
		{Value: library.QFieldSourceLabel, Label: "Name", Selected: cur == library.QFieldSourceLabel},
	}
	if showSeries {
		opts = append(opts, listFilterOpt{
			Value: library.QFieldSourceSeries, Label: "Series", Selected: cur == library.QFieldSourceSeries,
		})
	}
	return opts
}
