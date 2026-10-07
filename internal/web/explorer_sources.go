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
	SourcesBulkMode bool
	InfiniteID      string
	RowsID          string
	SeriesID        int64 // locked scope when > 0
	SeriesTitle     string
	ImportNullCount int
	ShowSeriesCol       bool
	ShowRowActions      bool // scan/edit/delete join (Browser + series-detail)
	ShowToolbar         bool // Browser Sources only; series-detail is a plain list
	Suggestions         library.MetaSuggestions
	PackRoleOptions     []struct{ Value, Label string }
	ScanCronDescriptors []string
	OOB                 bool
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
	StatusSummary       string // last-scan phrase for list second line
	StatusInd           sourceStatusView
	VideoCount          int
	Redirect            string
	LiveTarget          string
	ShowSeries          bool
	ShowRowActions      bool
	Selectable          bool // multi-select checkbox (Browser only)
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
	q := r.URL.Query()
	f := library.SourceListFilter{
		Q:         strings.TrimSpace(q.Get("q")),
		QField:    library.NormalizeSourceQField(q.Get("q_field")),
		Domains:   parseMultiQuery(q, "domain"),
		Studios:   parseMultiQuery(q, "studio"),
		Countries: parseMultiQuery(q, "country"),
		MPAAs:     parseMultiQuery(q, "mpaa"),
		Genres:     parseMultiQuery(q, "genre"),
		Tags:       parseMultiQuery(q, "tag"),
		Actors:     parseMultiQuery(q, "actor"),
		ActorRoles: parseMultiQuery(q, "actor_role"),
		SeriesIDs:  parseMultiInt64(q, "series"),
		Sort:       parseSourceSort(q.Get("sort")),
		SortDir:    parseSortDir(q.Get("dir")),
	}
	// series_id = series-detail lock; series = browser operator filter (like Videos).
	if sid, err := strconv.ParseInt(q.Get("series_id"), 10, 64); err == nil && sid > 0 {
		f.SeriesID = sid
	}
	f.FullScanDone = parseExclusiveBoolFilter(q, "full_scan", func(raw string) (bool, bool) {
		switch strings.ToLower(raw) {
		case library.SourceFullScanDone:
			return true, true
		case library.SourceFullScanIncomplete:
			return false, true
		default:
			return false, false
		}
	})
	switch strings.ToLower(strings.TrimSpace(q.Get("schedule"))) {
	case library.SourceScheduleOn:
		v := true
		f.ScheduleOn = &v
	case library.SourceScheduleOff:
		v := false
		f.ScheduleOn = &v
	}
	f.IndexAsIgnored = parseExclusiveBoolFilter(q, "discovered", func(raw string) (bool, bool) {
		switch strings.ToLower(raw) {
		case library.SourceDiscoveredIgnored:
			return true, true
		case library.SourceDiscoveredWanted:
			return false, true
		default:
			return false, false
		}
	})
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
	case library.SortSourceLabel, "label", "url":
		return library.SortSourceLabel
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

func sourcesSeriesDetail(r *http.Request) bool {
	if explorerAtFrom(r, "") == explorerAtSeriesDetail {
		return true
	}
	sid, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("series_id")), 10, 64)
	return err == nil && sid > 0
}

func (h *Handler) loadSourcesListLive(w http.ResponseWriter, r *http.Request) (sourcesListLiveData, error) {
	filter := parseSourceListFilter(r)
	seriesDetail := sourcesSeriesDetail(r)
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
			SortDir:  library.SortDirAsc}
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
	sug, _ := h.Library.ListMetaSuggestions()

	at := explorerAtFrom(r, explorerAtBrowser)
	if seriesDetail {
		at = explorerAtSeriesDetail
	}
	showSeries := !seriesDetail
	showActions := true

	now := time.Now().UTC()
	rows := make([]sourcesExplorerRow, 0, len(list))
	redir := r.URL.RequestURI()
	vcounts := map[int64]int{}
	seenSeries := map[int64]bool{}
	for _, src := range list {
		if seenSeries[src.SeriesID] {
			continue
		}
		seenSeries[src.SeriesID] = true
		m, _ := h.Library.CountVideosBySource(src.SeriesID)
		for k, v := range m {
			vcounts[k] = v
		}
	}
	bySource := map[int64][]queue.Task{}
	if h.Queue != nil {
		var activeTasks []queue.Task
		if seriesDetail && filter.SeriesID > 0 {
			activeTasks, _ = h.Queue.ListActiveForSeries(filter.SeriesID)
		} else {
			activeTasks, _ = h.Queue.ListActive()
		}
		_, bySource, _ = seriesActivityMaps(activeTasks)
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
		best := pickBestTask(bySource[src.ID])
		statusInd := buildSourceStatus(sourceStatusParams{
			Src: src.Source, Best: best, HasError: hasError, ErrMsg: errMsg, ErrCode: errCode, Stalled: stalled,
			SeriesMonitored: src.SeriesMonitored, DomainActive: dAct, DomainDisabledTitle: disTitle,
			ScanCronLabel: cronLabel, Summary: summary, HasScanned: hasScanned, HistoryID: taskID,
			Now: now, LastTipScannedAt: tipAt})
		vc := vcounts[src.ID]
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
			StatusSummary:       summary,
			StatusInd:           statusInd,
			VideoCount:          vc,
			Redirect:            redir,
			LiveTarget:          sourcesLiveTarget,
			ShowSeries:          showSeries,
			ShowRowActions:      showActions,
			Selectable:          !seriesDetail})
	}

	var toolbar listViewToolbar
	if !seriesDetail {
		titleIDs := append([]int64{}, filter.SeriesIDs...)
		if filter.SeriesID > 0 {
			titleIDs = append(titleIDs, filter.SeriesID)
		}
		seriesTitles, _ := h.Library.SeriesTitles(titleIDs)
		qfOpts := sourceQFieldOpts(filter.QField, showSeries)
		toolbar = listViewToolbar{
			Query:            filter.Q,
			QueryPlaceholder: searchByPlaceholder(qfOpts),
			AriaLabel:        "Source filters",
			QFieldOpts:       qfOpts,
			SortOpts:         sourcesSortOpts(r, filter.Sort, filter.SortDir),
			SortDir:          library.NormalizeSortDir(filter.Sort, filter.SortDir),
			ViewOpts:         sourcesViewOpts(r, viewMode),
			ShowView:         true,
			Selects:          sourcesFilterSelects(h, r, filter, showSeries),
			FilterActive:     filter.MenuActive(),
			Badges:           sourcesListBadges(r, filter, seriesTitles),
			LiveTarget:       sourcesLiveTarget,
			FormAction:       "/explorer/browse",
			SourcesBulkMode:  true,
		}
		if filter.MenuActive() {
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
		TableCols: annotateTableColsSort(
			parseTableColsCookie(r, "creatorr_cols_sources", sourcesTableColDefs(showSeries)),
			toolbar.SortOpts, toolbar.SortDir),
		TableColsCookie: "creatorr_cols_sources",
		ShowSelectAll:   !seriesDetail && total > len(rows),
		SourcesBulkMode: !seriesDetail,
		InfiniteID:      sourcesInfiniteID,
		RowsID:          sourcesRowsID,
		SeriesID:        filter.SeriesID,
		ShowSeriesCol:       showSeries,
		ShowRowActions:      showActions,
		ShowToolbar:         !seriesDetail,
		Suggestions:         sug,
		PackRoleOptions:     library.PackRoleSelectOptions(),
		ScanCronDescriptors: scanCronDescriptors()}
	if seriesDetail {
		if ser, err := h.Library.GetSeries(filter.SeriesID, false); err == nil && ser != nil {
			out.SeriesTitle = ser.Title
		}
		out.ImportNullCount, _ = h.Library.CountVideosWithNullSource(filter.SeriesID)
	}
	if !seriesDetail {
		rewriteExplorerInfinite(&out.Load, explorerTypeSources, 0)
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
		{Value: library.SortSourceLastScanned, Label: "Last scanned", Selected: cur == library.SortSourceLastScanned, Icon: "clock"}}
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
		{Value: viewTable, Label: "Table", Selected: cur == viewTable, Icon: "table"}}
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
	if showSeries {
		list, _ := h.Library.ListSeriesFiltered(library.SeriesListFilter{}, 0, 0)
		opts := make([]listFilterOpt, 0, len(list))
		seriesSel := selectedInt64Set(filter.SeriesIDs)
		for _, ser := range list {
			opts = append(opts, listFilterOpt{
				Value:    strconv.FormatInt(ser.ID, 10),
				Label:    ser.Title,
				Selected: seriesSel[ser.ID],
			})
		}
		if len(opts) > 0 {
			selects = append(selects, listFilterSelect{Name: "series", AriaLabel: "Series", Options: opts})
		}
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
		domainSel := selectedSet(filter.Domains)
		for _, d := range domains {
			domOpts = append(domOpts, listFilterOpt{Value: d, Label: d, Selected: domainSel[strings.ToLower(d)]})
		}
		selects = append(selects, listFilterSelect{
			Name: "domain", AriaLabel: "Domain", Options: domOpts})
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
			Name:      "full_scan",
			AriaLabel: "Full scan",
			Options: []listFilterOpt{
				{Value: library.SourceFullScanDone, Label: "Done", Selected: fullScan == library.SourceFullScanDone},
				{Value: library.SourceFullScanIncomplete, Label: "Incomplete", Selected: fullScan == library.SourceFullScanIncomplete}}})
	}

	showSchedule := !scoped || (facets.HasScheduleOn && facets.HasScheduleOff)
	if showSchedule {
		selects = append(selects, boolOnlySelect(r, "schedule", "Schedule",
			library.SourceScheduleOn, library.SourceScheduleOff, filter.ScheduleOn))
	}

	discovered := ""
	if filter.IndexAsIgnored != nil {
		if *filter.IndexAsIgnored {
			discovered = library.SourceDiscoveredIgnored
		} else {
			discovered = library.SourceDiscoveredWanted
		}
	}
	showDiscovered := !scoped || (facets.HasDiscoveredWanted && facets.HasDiscoveredIgnored)
	if showDiscovered {
		selects = append(selects, listFilterSelect{
			Name:      "discovered",
			AriaLabel: "Discovered",
			Options: []listFilterOpt{
				{Value: library.SourceDiscoveredWanted, Label: "Wanted", Selected: discovered == library.SourceDiscoveredWanted},
				{Value: library.SourceDiscoveredIgnored, Label: "Ignored", Selected: discovered == library.SourceDiscoveredIgnored}}})
	}

	if showSeries {
		selects = append(selects, boolOnlySelect(r, "monitored", "Series monitored", "1", "0", filter.SeriesMonitored))
	}
	selects = append(selects, presenceOnlySelect(r, library.PresenceScanError, "Scan error"))

	studioSel := selectedSet(filter.Studios)
	if studios, _ := h.Library.DistinctSourceScalar("studio"); len(studios) > 0 {
		opts := make([]listFilterOpt, 0, len(studios))
		for _, s := range studios {
			opts = append(opts, listFilterOpt{Value: s, Label: s, Selected: studioSel[strings.ToLower(s)]})
		}
		selects = append(selects, listFilterSelect{Name: "studio", AriaLabel: "Studio", Options: opts})
	}
	countrySel := selectedSet(filter.Countries)
	if countries, _ := h.Library.DistinctSourceScalar("country"); len(countries) > 0 {
		opts := make([]listFilterOpt, 0, len(countries))
		for _, s := range countries {
			opts = append(opts, listFilterOpt{Value: s, Label: s, Selected: countrySel[strings.ToLower(s)]})
		}
		selects = append(selects, listFilterSelect{Name: "country", AriaLabel: "Country", Options: opts})
	}
	mpaaSel := selectedSet(filter.MPAAs)
	if mpaas, _ := h.Library.DistinctSourceScalar("mpaa"); len(mpaas) > 0 {
		opts := make([]listFilterOpt, 0, len(mpaas))
		for _, s := range mpaas {
			opts = append(opts, listFilterOpt{Value: s, Label: s, Selected: mpaaSel[strings.ToLower(s)]})
		}
		selects = append(selects, listFilterSelect{Name: "mpaa", AriaLabel: "Content rating", Options: opts})
	}
	genreSel := selectedSet(filter.Genres)
	if genres, _ := h.Library.DistinctSourceJSONStrings("genres"); len(genres) > 0 {
		opts := make([]listFilterOpt, 0, len(genres))
		for _, g := range genres {
			opts = append(opts, listFilterOpt{Value: g, Label: g, Selected: genreSel[strings.ToLower(g)]})
		}
		selects = append(selects, listFilterSelect{Name: "genre", AriaLabel: "Genre", Options: opts})
	}
	tagSel := selectedSet(filter.Tags)
	if tags, _ := h.Library.DistinctSourceJSONStrings("tags"); len(tags) > 0 {
		opts := make([]listFilterOpt, 0, len(tags))
		for _, g := range tags {
			opts = append(opts, listFilterOpt{Value: g, Label: g, Selected: tagSel[strings.ToLower(g)]})
		}
		selects = append(selects, listFilterSelect{Name: "tag", AriaLabel: "Tag", Options: opts})
	}
	actorSel := selectedSet(filter.Actors)
	if actors, _ := h.Library.DistinctSourceActorNames(); len(actors) > 0 {
		opts := make([]listFilterOpt, 0, len(actors))
		for _, a := range actors {
			opts = append(opts, listFilterOpt{Value: a, Label: a, Selected: actorSel[strings.ToLower(a)]})
		}
		selects = append(selects, listFilterSelect{Name: "actor", AriaLabel: "Actor", Options: opts})
	}
	roleSel := selectedSet(filter.ActorRoles)
	if roles, _ := h.Library.DistinctSourceActorRoles(); len(roles) > 0 {
		opts := make([]listFilterOpt, 0, len(roles))
		for _, a := range roles {
			opts = append(opts, listFilterOpt{Value: a, Label: a, Selected: roleSel[strings.ToLower(a)]})
		}
		selects = append(selects, listFilterSelect{Name: "actor_role", AriaLabel: "Actor role", Options: opts})
	}

	annotateFilterSelects(r, selects)
	return selects
}

func sourcesListBadges(r *http.Request, filter library.SourceListFilter, seriesTitles map[int64]string) []listViewBadge {
	var out []listViewBadge
	out = append(out, orJoinBadges(r, "series", "Series", "series", int64QueryValues(filter.SeriesIDs), func(s string) string {
		id, _ := strconv.ParseInt(s, 10, 64)
		if title := seriesTitles[id]; title != "" {
			return title
		}
		return "#" + s
	})...)
	out = append(out, orJoinBadges(r, "domain", "Domain", "domain", filter.Domains, nil)...)
	out = append(out, orJoinBadges(r, "studio", "Studio", "studio", filter.Studios, nil)...)
	out = append(out, orJoinBadges(r, "country", "Country", "country", filter.Countries, nil)...)
	out = append(out, orJoinBadges(r, "mpaa", "Rating", "mpaa", filter.MPAAs, nil)...)
	out = append(out, orJoinBadges(r, "genre", "Genre", "genre", filter.Genres, nil)...)
	out = append(out, orJoinBadges(r, "tag", "Tag", "tag", filter.Tags, nil)...)
	out = append(out, orJoinBadges(r, "actor", "Actor", "actor", filter.Actors, nil)...)
	out = append(out, orJoinBadges(r, "actor_role", "Actor role", "actor_role", filter.ActorRoles, nil)...)
	if filter.FullScanDone != nil {
		label := "Full scan: Incomplete"
		if *filter.FullScanDone {
			label = "Full scan: Done"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "full_scan", "page", "through")})
	}
	if filter.ScheduleOn != nil {
		label := "No schedule"
		if *filter.ScheduleOn {
			label = "Has schedule"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "schedule", "page", "through")})
	}
	if filter.IndexAsIgnored != nil {
		label := "Discovered: Wanted"
		if *filter.IndexAsIgnored {
			label = "Discovered: Ignored"
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "discovered", "page", "through")})
	}
	if filter.SeriesMonitored != nil {
		label := "No series monitored"
		if *filter.SeriesMonitored {
			label = "Has series monitored"
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
		tableColDef{Key: "discovered", Label: "Discovered", Default: true},
		tableColDef{Key: "name", Label: "Name", Default: true},
		tableColDef{Key: "url", Label: "URL", Default: true},
		tableColDef{Key: "domain", Label: "Domain", Default: true},
		tableColDef{Key: "last_scanned", Label: "Last scanned", Default: true},
		tableColDef{Key: "status", Label: "Status", Default: true},
	)
	return cols
}

func sourceQFieldOpts(current string, showSeries bool) []listFilterOpt {
	cur := library.NormalizeSourceQField(current)
	opts := []listFilterOpt{
		{Value: library.QFieldSourceLabel, Label: "Name", Selected: cur == library.QFieldSourceLabel},
		{Value: library.QFieldSourceURL, Label: "URL", Selected: cur == library.QFieldSourceURL}}
	if showSeries {
		opts = append(opts, listFilterOpt{
			Value: library.QFieldSourceSeries, Label: "Series", Selected: cur == library.QFieldSourceSeries})
	}
	return opts
}
