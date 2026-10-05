package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

// parseVideoListFilter reads video list query params.
// allowSeries: when true (library /videos), honor ?series=; otherwise series is route-locked.
func parseVideoListFilter(r *http.Request, sources []library.Source, allowSeries bool) library.VideoListFilter {
	q := r.URL.Query()
	f := library.VideoListFilter{
		Title:      strings.TrimSpace(q.Get("q")),
		QField:     parseQField(r),
		FromDay:    parseFilterDay(q.Get("from")),
		ToDay:      parseFilterDay(q.Get("to")),
		Studios:    parseMultiQuery(q, "studio"),
		Countries:  parseMultiQuery(q, "country"),
		MPAAs:      parseMultiQuery(q, "mpaa"),
		MediaTypes: parseMultiQuery(q, "media_type"),
		Genres:     parseMultiQuery(q, "genre"),
		Tags:       parseMultiQuery(q, "tag"),
		Actors:     parseMultiQuery(q, "actor"),
		Sort:       parseVideoSort(q.Get("sort")),
		SortDir:    parseSortDir(q.Get("dir")),
	}
	f.Empty, f.NotEmpty = parsePresenceParams(q)
	f.Years = parseMultiYear(q, "year")
	f.PackRoles = parseVideoPackRoles(q)
	seen := map[string]struct{}{}
	for _, raw := range q["status"] {
		st := strings.TrimSpace(raw)
		if st == "" {
			continue
		}
		if _, ok := seen[st]; ok {
			continue
		}
		seen[st] = struct{}{}
		f.Statuses = append(f.Statuses, st)
	}
	f.SourceIDs = parseVideoSourceIDs(q, sources)
	if allowSeries {
		f.SeriesIDs = parseMultiInt64(q, "series")
	}
	if f.FromDay != "" && f.ToDay != "" && f.FromDay > f.ToDay {
		f.FromDay, f.ToDay = f.ToDay, f.FromDay
	}
	return f
}

func parseVideoPackRoles(q url.Values) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, raw := range q["kind"] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var role string
		switch raw {
		case library.PackRoleRegular:
			role = library.PackRoleRegular
		case library.VideoPackRoleAnySpecial:
			role = library.VideoPackRoleAnySpecial
		default:
			if err := library.ValidatePackRole(raw); err == nil && library.IsSpecialPackRole(raw) {
				role = library.NormalizePackRole(raw)
			} else {
				continue
			}
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func parseVideoSourceIDs(q url.Values, sources []library.Source) []int64 {
	var out []int64
	seen := map[int64]struct{}{}
	for _, raw := range q["source"] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var id int64
		if strings.EqualFold(raw, library.VideoSourceImportQuery) {
			id = library.VideoSourceImport
		} else if sid, err := strconv.ParseInt(raw, 10, 64); err == nil && sid > 0 {
			if len(sources) > 0 {
				found := false
				for _, src := range sources {
					if src.ID == sid {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			id = sid
		} else {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func videoSourceQueryValues(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == library.VideoSourceImport {
			out = append(out, library.VideoSourceImportQuery)
		} else if id > 0 {
			out = append(out, strconv.FormatInt(id, 10))
		}
	}
	return out
}

func int64QueryValues(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, strconv.FormatInt(id, 10))
		}
	}
	return out
}

func intQueryValues(vals []int) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != 0 {
			out = append(out, strconv.Itoa(v))
		}
	}
	return out
}

func encodeVideoListFilter(filter library.VideoListFilter, page int, view string) string {
	q := url.Values{}
	if t := strings.TrimSpace(filter.Title); t != "" {
		q.Set("q", t)
	}
	if qf := library.NormalizeQField(filter.QField); qf != library.QFieldTitle {
		q.Set("q_field", qf)
	}
	for _, y := range filter.Years {
		if y >= 1900 && y <= 2100 {
			q.Add("year", strconv.Itoa(y))
		}
	}
	for _, st := range filter.Statuses {
		q.Add("status", st)
	}
	for _, sv := range videoSourceQueryValues(filter.SourceIDs) {
		q.Add("source", sv)
	}
	for _, sid := range filter.SeriesIDs {
		if sid > 0 {
			q.Add("series", strconv.FormatInt(sid, 10))
		}
	}
	for _, role := range filter.PackRoles {
		if role = strings.TrimSpace(role); role != "" {
			q.Add("kind", role)
		}
	}
	if filter.FromDay != "" {
		q.Set("from", filter.FromDay)
	}
	if filter.ToDay != "" {
		q.Set("to", filter.ToDay)
	}
	for _, s := range filter.Studios {
		if s = strings.TrimSpace(s); s != "" {
			q.Add("studio", s)
		}
	}
	for _, s := range filter.Countries {
		if s = strings.TrimSpace(s); s != "" {
			q.Add("country", s)
		}
	}
	for _, s := range filter.MPAAs {
		if s = strings.TrimSpace(s); s != "" {
			q.Add("mpaa", s)
		}
	}
	for _, s := range filter.MediaTypes {
		if s = strings.TrimSpace(s); s != "" {
			q.Add("media_type", s)
		}
	}
	for _, g := range filter.Genres {
		q.Add("genre", g)
	}
	for _, t := range filter.Tags {
		q.Add("tag", t)
	}
	for _, a := range filter.Actors {
		q.Add("actor", a)
	}
	for _, e := range filter.Empty {
		q.Add("empty", e)
	}
	for _, e := range filter.NotEmpty {
		q.Add("not_empty", e)
	}
	if s := parseVideoSort(filter.Sort); s != "" && s != library.SortUpload {
		q.Set("sort", s)
	}
	if viewPersistedInQuery(view) {
		q.Set("view", view)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	return q.Encode()
}

func videoListBadges(r *http.Request, filter library.VideoListFilter, showSeries bool, seriesTitles map[int64]string) []listViewBadge {
	var out []listViewBadge
	// Search (q) stays in the toolbar field only - not a chip.
	out = append(out, orJoinBadges(r, "status", "Status", "status", filter.Statuses, videoStatusLabel)...)
	out = append(out, orJoinBadges(r, "source", "Source", "source", videoSourceQueryValues(filter.SourceIDs), func(s string) string {
		if strings.EqualFold(s, library.VideoSourceImportQuery) {
			return "Import"
		}
		return "#" + s
	})...)
	if showSeries {
		out = append(out, orJoinBadges(r, "series", "Series", "series", int64QueryValues(filter.SeriesIDs), func(s string) string {
			id, _ := strconv.ParseInt(s, 10, 64)
			if title := seriesTitles[id]; title != "" {
				return title
			}
			return "#" + s
		})...)
	}
	out = append(out, orJoinBadges(r, "year", "Year", "year", intQueryValues(filter.Years), nil)...)
	out = append(out, orJoinBadges(r, "kind", "Special kind", "kind", filter.PackRoles, func(role string) string {
		return specialKindChipLabel(role)
	})...)
	if filter.FromDay != "" || filter.ToDay != "" {
		var label string
		if filter.FromDay != "" && filter.ToDay != "" {
			label = fmt.Sprintf("Upload: %s–%s", filter.FromDay, filter.ToDay)
		} else if filter.FromDay != "" {
			label = "Upload from " + filter.FromDay
		} else {
			label = "Upload to " + filter.ToDay
		}
		out = append(out, listViewBadge{Label: label, Href: dropQueryKeys(r, "from", "to", "page")})
	}
	out = append(out, orJoinBadges(r, "studio", "Studio", "studio", filter.Studios, nil)...)
	out = append(out, orJoinBadges(r, "country", "Country", "country", filter.Countries, nil)...)
	out = append(out, orJoinBadges(r, "mpaa", "Rating", "mpaa", filter.MPAAs, nil)...)
	out = append(out, orJoinBadges(r, "media_type", "Media", "media_type", filter.MediaTypes, nil)...)
	out = append(out, orJoinBadges(r, "genre", "Genre", "genre", filter.Genres, nil)...)
	out = append(out, orJoinBadges(r, "tag", "Tag", "tag", filter.Tags, nil)...)
	out = append(out, orJoinBadges(r, "actor", "Actor", "actor", filter.Actors, nil)...)
	for _, e := range filter.Empty {
		out = append(out, listViewBadge{Label: presenceBadgeLabel(e, true), Href: dropQueryValue(r, "empty", e)})
	}
	for _, e := range filter.NotEmpty {
		out = append(out, listViewBadge{Label: presenceBadgeLabel(e, false), Href: dropQueryValue(r, "not_empty", e)})
	}
	return out
}

func videoFilterSelects(h *Handler, r *http.Request, seriesID int64, filter library.VideoListFilter, sources []library.Source, includeSeries bool) []listFilterSelect {
	var selects []listFilterSelect
	seriesSel := selectedInt64Set(filter.SeriesIDs)
	if includeSeries {
		list, _ := h.Library.ListSeriesFiltered(library.SeriesListFilter{}, 0, 0)
		opts := make([]listFilterOpt, 0, len(list))
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
	scope := seriesID
	if scope == 0 && len(filter.SeriesIDs) == 1 {
		scope = filter.SeriesIDs[0]
	}
	yearSel := selectedIntSet(filter.Years)
	years, _ := h.Library.DistinctVideoYears(scope)
	yearOpts := make([]listFilterOpt, 0, len(years))
	for _, y := range years {
		ys := strconv.Itoa(y)
		yearOpts = append(yearOpts, listFilterOpt{Value: ys, Label: ys, Selected: yearSel[y]})
	}
	if len(yearOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "year", AriaLabel: "Upload year", Options: yearOpts})
	}
	statuses, _ := h.Library.DistinctVideoStatuses(scope)
	statusSelected := map[string]bool{}
	for _, s := range filter.Statuses {
		statusSelected[s] = true
	}
	statusOpts := make([]listFilterOpt, 0, len(statuses))
	for _, st := range statuses {
		statusOpts = append(statusOpts, listFilterOpt{Value: st, Label: videoStatusLabel(st), Selected: statusSelected[st]})
	}
	if len(statusOpts) > 0 {
		selects = append(selects, listFilterSelect{Name: "status", AriaLabel: "Status", Options: statusOpts})
	}
	studioSel := selectedSet(filter.Studios)
	studios, _ := h.Library.DistinctVideoScalar(scope, "studio")
	studioOpts := make([]listFilterOpt, 0, len(studios))
	for _, s := range studios {
		studioOpts = append(studioOpts, listFilterOpt{Value: s, Label: s, Selected: studioSel[strings.ToLower(s)]})
	}
	if len(studioOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "studio", AriaLabel: "Studio", Options: studioOpts,
			PresenceField: library.PresenceStudio})
	}
	countrySel := selectedSet(filter.Countries)
	countries, _ := h.Library.DistinctVideoScalar(scope, "country")
	countryOpts := make([]listFilterOpt, 0, len(countries))
	for _, s := range countries {
		countryOpts = append(countryOpts, listFilterOpt{Value: s, Label: s, Selected: countrySel[strings.ToLower(s)]})
	}
	if len(countryOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "country", AriaLabel: "Country", Options: countryOpts,
			PresenceField: library.PresenceCountry})
	}
	mpaaSel := selectedSet(filter.MPAAs)
	mpaas, _ := h.Library.DistinctVideoScalar(scope, "mpaa")
	mpaaOpts := make([]listFilterOpt, 0, len(mpaas))
	for _, s := range mpaas {
		mpaaOpts = append(mpaaOpts, listFilterOpt{Value: s, Label: s, Selected: mpaaSel[strings.ToLower(s)]})
	}
	if len(mpaaOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "mpaa", AriaLabel: "Content rating", Options: mpaaOpts,
			PresenceField: library.PresenceMPAA})
	}
	mediaSel := selectedSet(filter.MediaTypes)
	mts, _ := h.Library.DistinctVideoScalar(scope, "media_type")
	mtOpts := make([]listFilterOpt, 0, len(mts))
	for _, s := range mts {
		mtOpts = append(mtOpts, listFilterOpt{Value: s, Label: s, Selected: mediaSel[strings.ToLower(s)]})
	}
	if len(mtOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "media_type", AriaLabel: "Media type", Options: mtOpts,
			PresenceField: library.PresenceMediaType})
	}
	genres, _ := h.Library.DistinctVideoJSONStrings(scope, "genres")
	genreOpts := make([]listFilterOpt, 0, len(genres))
	genreSel := map[string]struct{}{}
	for _, g := range filter.Genres {
		genreSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range genres {
		_, sel := genreSel[strings.ToLower(g)]
		genreOpts = append(genreOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(genreOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "genre", AriaLabel: "Genre", Options: genreOpts,
			PresenceField: library.PresenceGenres})
	}
	tags, _ := h.Library.DistinctVideoJSONStrings(scope, "tags")
	tagOpts := make([]listFilterOpt, 0, len(tags))
	tagSel := map[string]struct{}{}
	for _, g := range filter.Tags {
		tagSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range tags {
		_, sel := tagSel[strings.ToLower(g)]
		tagOpts = append(tagOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(tagOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "tag", AriaLabel: "Tag", Options: tagOpts,
			PresenceField: library.PresenceTags})
	}
	actors, _ := h.Library.DistinctVideoActorNames(scope)
	actorOpts := make([]listFilterOpt, 0, len(actors))
	actorSel := map[string]struct{}{}
	for _, g := range filter.Actors {
		actorSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range actors {
		_, sel := actorSel[strings.ToLower(g)]
		actorOpts = append(actorOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(actorOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "actor", AriaLabel: "Actor", Options: actorOpts,
			PresenceField: library.PresenceActors})
	}

	if seriesID > 0 || len(sources) > 0 {
		sourceSel := selectedInt64Set(filter.SourceIDs)
		srcOpts := make([]listFilterOpt, 0, len(sources)+1)
		if seriesID > 0 {
			nullImportCount, _ := h.Library.CountVideosWithNullSource(seriesID)
			if nullImportCount > 0 {
				srcOpts = append(srcOpts, listFilterOpt{
					Value: library.VideoSourceImportQuery, Label: "Import",
					Selected: sourceSel[library.VideoSourceImport],
				})
			}
		}
		for _, src := range sources {
			srcOpts = append(srcOpts, listFilterOpt{
				Value: strconv.FormatInt(src.ID, 10), Label: sourceFilterLabel(src),
				Selected: sourceSel[src.ID],
			})
		}
		if len(srcOpts) > 0 {
			selects = append(selects, listFilterSelect{Name: "source", AriaLabel: "Source", Options: srcOpts})
		}
	}
	packRoleSel := selectedSet(filter.PackRoles)
	kindOpts := make([]listFilterOpt, 0, len(library.PackRoleSelectOptions()))
	for _, opt := range library.PackRoleSelectOptions() {
		kindOpts = append(kindOpts, listFilterOpt{
			Value: opt.Value, Label: opt.Label, Selected: packRoleSel[strings.ToLower(opt.Value)],
		})
	}
	selects = append(selects, specialKindFilterSelect(r, filter.PackRoles, kindOpts))

	selects = append(selects,
		presenceOnlySelect(r, library.PresencePlot, "Plot"),
		presenceOnlySelect(r, library.PresenceTagline, "Tagline"),
		presenceOnlySelect(r, library.PresenceNotes, "Notes"),
		presenceOnlySelect(r, library.PresenceSortTitle, "Sort title"),
		presenceOnlySelect(r, library.PresenceOriginalTitle, "Original title"),
		presenceOnlySelect(r, library.PresenceThumbnail, "Thumbnail"),
	)

	annotateFilterSelects(r, selects)
	return selects
}

func videoSortOpts(r *http.Request, current, curDir, defaultSort string) []listFilterOpt {
	cur := current
	if cur == "" {
		cur = defaultSort
	}
	opts := []listFilterOpt{
		{Value: library.SortUpload, Label: "Upload date", Selected: cur == library.SortUpload, Icon: "calendar-days"},
		{Value: library.SortAdded, Label: "Date added", Selected: cur == library.SortAdded, Icon: "calendar-plus"},
		{Value: library.SortAcquired, Label: "Acquired", Selected: cur == library.SortAcquired, Icon: "download"},
		{Value: library.SortDuration, Label: "Duration", Selected: cur == library.SortDuration, Icon: "timer"},
		{Value: library.SortTitle, Label: "Title", Selected: cur == library.SortTitle, Icon: "type"}}
	annotateSortOpts(r, opts, defaultSort, curDir)
	return opts
}

func qFieldOpts(current string) []listFilterOpt {
	cur := library.NormalizeQField(current)
	return []listFilterOpt{
		{Value: library.QFieldTitle, Label: "Title", Selected: cur == library.QFieldTitle},
		{Value: library.QFieldPlot, Label: "Plot", Selected: cur == library.QFieldPlot},
		{Value: library.QFieldSortTitle, Label: "Sort title", Selected: cur == library.QFieldSortTitle},
		{Value: library.QFieldOriginalTitle, Label: "Original title", Selected: cur == library.QFieldOriginalTitle},
		{Value: library.QFieldTagline, Label: "Tagline", Selected: cur == library.QFieldTagline},
		{Value: library.QFieldNotes, Label: "Notes", Selected: cur == library.QFieldNotes}}
}

func searchByPlaceholder(opts []listFilterOpt) string {
	for _, o := range opts {
		if o.Selected {
			if label := strings.TrimSpace(o.Label); label != "" {
				return "Search by " + label
			}
			break
		}
	}
	return "Search"
}

func viewOpts(r *http.Request, current string) []listFilterOpt {
	opts := []listFilterOpt{
		{Value: viewList, Label: "List", Selected: current == viewList || current == ""},
		{Value: viewCards, Label: "Card", Selected: current == viewCards},
		{Value: viewGallery, Label: "Gallery", Selected: current == viewGallery},
		{Value: viewTable, Label: "Table", Selected: current == viewTable}}
	annotateViewOpts(r, opts)
	return opts
}

func seriesSortOpts(r *http.Request, current, curDir string) []listFilterOpt {
	cur := current
	if cur == "" {
		cur = library.SortTitle
	}
	opts := []listFilterOpt{
		{Value: library.SortTitle, Label: "Title", Selected: cur == library.SortTitle, Icon: "type"},
		{Value: library.SortAdded, Label: "Date added", Selected: cur == library.SortAdded, Icon: "calendar-plus"},
		{Value: library.SortLastUpload, Label: "Last upload", Selected: cur == library.SortLastUpload, Icon: "calendar-days"},
		{Value: library.SortDownloaded, Label: "Downloaded", Selected: cur == library.SortDownloaded, Icon: "download"},
		{Value: library.SortWanted, Label: "Wanted", Selected: cur == library.SortWanted, Icon: "circle-dashed"},
		{Value: library.SortErrors, Label: "Errors", Selected: cur == library.SortErrors, Icon: "circle-alert"},
		{Value: library.SortSize, Label: "Size", Selected: cur == library.SortSize, Icon: "hard-drive"}}
	annotateSortOpts(r, opts, library.SortTitle, curDir)
	return opts
}

// annotateSortOpts sets Href: other fields apply with field-default dir; the
// selected field toggles asc/desc on re-select.
func annotateSortOpts(r *http.Request, opts []listFilterOpt, defaultSort, curDir string) {
	for i := range opts {
		v := opts[i].Value
		if opts[i].Selected {
			opts[i].Href = sortDirToggleURL(r, v, curDir)
			continue
		}
		// Always set sort= so cookie prefs see an explicit choice (incl. page default).
		opts[i].Href = applySortFieldURL(r, v)
	}
}

func applySortFieldURL(r *http.Request, sort string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	q.Del("dir") // reset direction to the field default
	q.Set("sort", sort)
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

func sortDirToggleURL(r *http.Request, sort, curDir string) string {
	resolved := strings.ToLower(strings.TrimSpace(sort))
	cur := library.NormalizeSortDir(resolved, curDir)
	next := library.SortDirAsc
	if cur == library.SortDirAsc {
		next = library.SortDirDesc
	}
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	def := library.DefaultSortDir(resolved)
	if next == def {
		q.Del("dir")
	} else {
		q.Set("dir", next)
	}
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

func annotateViewOpts(r *http.Request, opts []listFilterOpt) {
	for i := range opts {
		opts[i].Href = applyViewURL(r, opts[i].Value)
	}
}

func applyViewURL(r *http.Request, view string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	if view == "" {
		view = viewList
	}
	// Always set view so changing the control updates the mode cookie.
	q.Set("view", view)
	path := explorerFragmentPath(r)
	u := url.URL{Path: path}
	if enc := q.Encode(); enc != "" {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

func seriesStatusFilterLabel(status string) string {
	switch status {
	case library.SeriesListStatusMonitored:
		return "Monitored"
	case library.SeriesListStatusUnmonitored:
		return "Unmonitored"
	case library.SeriesListStatusComplete:
		return "Complete"
	case library.SeriesListStatusIncomplete:
		return "Incomplete"
	case library.SeriesListStatusHasErrors:
		return "Has errors"
	default:
		return status
	}
}

func seriesListBadges(r *http.Request, filter library.SeriesListFilter) []listViewBadge {
	var out []listViewBadge
	// Search (q) stays in the toolbar field only - not a chip.
	out = append(out, orJoinBadges(r, "root", "Root", "root", int64QueryValues(filter.RootIDs), func(s string) string {
		return "#" + s
	})...)
	out = append(out, orJoinBadges(r, "quality", "Quality", "quality", int64QueryValues(filter.QualityProfileIDs), func(s string) string {
		return "#" + s
	})...)
	out = append(out, orJoinBadges(r, "delivery", "Delivery", "delivery", filter.DeliveryModes, nil)...)
	out = append(out, orJoinBadges(r, "status", "Status", "status", filter.Statuses, seriesStatusFilterLabel)...)
	out = append(out, orJoinBadges(r, "year", "Premiered", "year", intQueryValues(filter.PremieredYears), nil)...)
	out = append(out, orJoinBadges(r, "studio", "Studio", "studio", filter.Studios, nil)...)
	out = append(out, orJoinBadges(r, "country", "Country", "country", filter.Countries, nil)...)
	out = append(out, orJoinBadges(r, "mpaa", "Rating", "mpaa", filter.MPAAs, nil)...)
	out = append(out, orJoinBadges(r, "genre", "Genre", "genre", filter.Genres, nil)...)
	out = append(out, orJoinBadges(r, "tag", "Tag", "tag", filter.Tags, nil)...)
	out = append(out, orJoinBadges(r, "actor", "Actor", "actor", filter.Actors, nil)...)
	for _, e := range filter.Empty {
		out = append(out, listViewBadge{Label: presenceBadgeLabel(e, true), Href: dropQueryValue(r, "empty", e)})
	}
	for _, e := range filter.NotEmpty {
		out = append(out, listViewBadge{Label: presenceBadgeLabel(e, false), Href: dropQueryValue(r, "not_empty", e)})
	}
	return out
}

func seriesFilterSelects(h *Handler, r *http.Request, filter library.SeriesListFilter, roots []library.RootFolder, profiles []library.QualityProfile) []listFilterSelect {
	var selects []listFilterSelect
	rootSel := selectedInt64Set(filter.RootIDs)
	if len(roots) > 1 {
		opts := make([]listFilterOpt, 0, len(roots))
		for _, root := range roots {
			label := strings.TrimSpace(root.Name)
			if label == "" {
				label = root.Path
			}
			opts = append(opts, listFilterOpt{
				Value: strconv.FormatInt(root.ID, 10), Label: label, Selected: rootSel[root.ID],
			})
		}
		selects = append(selects, listFilterSelect{Name: "root", AriaLabel: "Root folder", Options: opts})
	}
	qualitySel := selectedInt64Set(filter.QualityProfileIDs)
	if len(profiles) > 0 {
		opts := make([]listFilterOpt, 0, len(profiles))
		for _, p := range profiles {
			opts = append(opts, listFilterOpt{
				Value: strconv.FormatInt(p.ID, 10), Label: p.Name, Selected: qualitySel[p.ID],
			})
		}
		selects = append(selects, listFilterSelect{Name: "quality", AriaLabel: "Quality profile", Options: opts})
	}
	deliverySel := selectedSet(filter.DeliveryModes)
	statusSel := selectedSet(filter.Statuses)
	selects = append(selects, listFilterSelect{
		Name: "delivery", AriaLabel: "Delivery mode", Options: []listFilterOpt{
			{Value: library.DeliveryVideo, Label: "Video", Selected: deliverySel[strings.ToLower(library.DeliveryVideo)]},
			{Value: library.DeliveryAudio, Label: "Audio", Selected: deliverySel[strings.ToLower(library.DeliveryAudio)]},
		},
	})
	selects = append(selects, listFilterSelect{
		Name: "status", AriaLabel: "Status", Options: []listFilterOpt{
			{Value: library.SeriesListStatusMonitored, Label: "Monitored", Selected: statusSel[library.SeriesListStatusMonitored]},
			{Value: library.SeriesListStatusUnmonitored, Label: "Unmonitored", Selected: statusSel[library.SeriesListStatusUnmonitored]},
			{Value: library.SeriesListStatusComplete, Label: "Complete", Selected: statusSel[library.SeriesListStatusComplete]},
			{Value: library.SeriesListStatusIncomplete, Label: "Incomplete", Selected: statusSel[library.SeriesListStatusIncomplete]},
			{Value: library.SeriesListStatusHasErrors, Label: "Has errors", Selected: statusSel[library.SeriesListStatusHasErrors]},
		},
	})
	yearSel := selectedIntSet(filter.PremieredYears)
	years, _ := h.Library.DistinctSeriesPremieredYears()
	yearOpts := make([]listFilterOpt, 0, len(years))
	for _, y := range years {
		ys := strconv.Itoa(y)
		yearOpts = append(yearOpts, listFilterOpt{Value: ys, Label: ys, Selected: yearSel[y]})
	}
	if len(yearOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "year", AriaLabel: "Premiered", Options: yearOpts,
			PresenceField: library.PresencePremiered})
	} else {
		selects = append(selects, presenceOnlySelect(r, library.PresencePremiered, "Premiered"))
	}
	studioSel := selectedSet(filter.Studios)
	studios, _ := h.Library.DistinctSeriesScalar("studio")
	studioOpts := make([]listFilterOpt, 0, len(studios))
	for _, s := range studios {
		studioOpts = append(studioOpts, listFilterOpt{Value: s, Label: s, Selected: studioSel[strings.ToLower(s)]})
	}
	if len(studioOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "studio", AriaLabel: "Studio", Options: studioOpts,
			PresenceField: library.PresenceStudio})
	}
	countrySel := selectedSet(filter.Countries)
	countries, _ := h.Library.DistinctSeriesScalar("country")
	countryOpts := make([]listFilterOpt, 0, len(countries))
	for _, s := range countries {
		countryOpts = append(countryOpts, listFilterOpt{Value: s, Label: s, Selected: countrySel[strings.ToLower(s)]})
	}
	if len(countryOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "country", AriaLabel: "Country", Options: countryOpts,
			PresenceField: library.PresenceCountry})
	}
	mpaaSel := selectedSet(filter.MPAAs)
	mpaas, _ := h.Library.DistinctSeriesScalar("mpaa")
	mpaaOpts := make([]listFilterOpt, 0, len(mpaas))
	for _, s := range mpaas {
		mpaaOpts = append(mpaaOpts, listFilterOpt{Value: s, Label: s, Selected: mpaaSel[strings.ToLower(s)]})
	}
	if len(mpaaOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "mpaa", AriaLabel: "Content rating", Options: mpaaOpts,
			PresenceField: library.PresenceMPAA})
	}
	genres, _ := h.Library.DistinctSeriesJSONStrings("genres")
	genreOpts := make([]listFilterOpt, 0, len(genres))
	genreSel := map[string]struct{}{}
	for _, g := range filter.Genres {
		genreSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range genres {
		_, sel := genreSel[strings.ToLower(g)]
		genreOpts = append(genreOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(genreOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "genre", AriaLabel: "Genre", Options: genreOpts,
			PresenceField: library.PresenceGenres})
	}
	tags, _ := h.Library.DistinctSeriesJSONStrings("tags")
	tagOpts := make([]listFilterOpt, 0, len(tags))
	tagSel := map[string]struct{}{}
	for _, g := range filter.Tags {
		tagSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range tags {
		_, sel := tagSel[strings.ToLower(g)]
		tagOpts = append(tagOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(tagOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "tag", AriaLabel: "Tag", Options: tagOpts,
			PresenceField: library.PresenceTags})
	}
	actors, _ := h.Library.DistinctSeriesActorNames()
	actorOpts := make([]listFilterOpt, 0, len(actors))
	actorSel := map[string]struct{}{}
	for _, g := range filter.Actors {
		actorSel[strings.ToLower(g)] = struct{}{}
	}
	for _, g := range actors {
		_, sel := actorSel[strings.ToLower(g)]
		actorOpts = append(actorOpts, listFilterOpt{Value: g, Label: g, Selected: sel})
	}
	if len(actorOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "actor", AriaLabel: "Actor", Options: actorOpts,
			PresenceField: library.PresenceActors})
	}
	selects = append(selects,
		presenceOnlySelect(r, library.PresencePlot, "Plot"),
		presenceOnlySelect(r, library.PresenceTagline, "Tagline"),
		presenceOnlySelect(r, library.PresenceNotes, "Notes"),
		presenceOnlySelect(r, library.PresenceSortTitle, "Sort title"),
		presenceOnlySelect(r, library.PresenceOriginalTitle, "Original title"),
	)
	annotateFilterSelects(r, selects)
	return selects
}
