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
		Title:     strings.TrimSpace(q.Get("q")),
		QField:    parseQField(r),
		FromDay:   parseFilterDay(q.Get("from")),
		ToDay:     parseFilterDay(q.Get("to")),
		Studio:    strings.TrimSpace(q.Get("studio")),
		Country:   strings.TrimSpace(q.Get("country")),
		MPAA:      strings.TrimSpace(q.Get("mpaa")),
		Genres:    parseMultiQuery(q, "genre"),
		Tags:      parseMultiQuery(q, "tag"),
		Actors:    parseMultiQuery(q, "actor"),
		Sort:      parseVideoSort(q.Get("sort")),
		SortDir:   parseSortDir(q.Get("dir")),
		MediaType: strings.TrimSpace(q.Get("media_type")),
	}
	f.Empty, f.NotEmpty = parsePresenceParams(q)
	if raw := strings.TrimSpace(q.Get("year")); raw != "" {
		if y, err := strconv.Atoi(raw); err == nil && y >= 1900 && y <= 2100 {
			f.Year = y
		}
	}
	if raw := strings.TrimSpace(q.Get("kind")); raw != "" {
		switch raw {
		case library.PackRoleRegular:
			f.PackRole = library.PackRoleRegular
		case library.VideoPackRoleAnySpecial:
			f.PackRole = library.VideoPackRoleAnySpecial
		default:
			if err := library.ValidatePackRole(raw); err == nil && library.IsSpecialPackRole(raw) {
				f.PackRole = library.NormalizePackRole(raw)
			}
		}
	}
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
	if raw := strings.TrimSpace(q.Get("source")); raw != "" {
		if strings.EqualFold(raw, library.VideoSourceImportQuery) {
			f.SourceID = library.VideoSourceImport
		} else if sid, err := strconv.ParseInt(raw, 10, 64); err == nil && sid > 0 {
			if len(sources) == 0 {
				f.SourceID = sid
			} else {
				for _, src := range sources {
					if src.ID == sid {
						f.SourceID = sid
						break
					}
				}
			}
		}
	}
	if allowSeries {
		f.SeriesID = parseIntQuery(q.Get("series"))
	}
	if f.FromDay != "" && f.ToDay != "" && f.FromDay > f.ToDay {
		f.FromDay, f.ToDay = f.ToDay, f.FromDay
	}
	return f
}

func encodeVideoListFilter(filter library.VideoListFilter, page int, view string) string {
	q := url.Values{}
	if t := strings.TrimSpace(filter.Title); t != "" {
		q.Set("q", t)
	}
	if qf := library.NormalizeQField(filter.QField); qf != library.QFieldTitle {
		q.Set("q_field", qf)
	}
	if filter.Year > 0 {
		q.Set("year", strconv.Itoa(filter.Year))
	}
	for _, st := range filter.Statuses {
		q.Add("status", st)
	}
	if filter.SourceID == library.VideoSourceImport {
		q.Set("source", library.VideoSourceImportQuery)
	} else if filter.SourceID > 0 {
		q.Set("source", strconv.FormatInt(filter.SourceID, 10))
	}
	if filter.SeriesID > 0 {
		q.Set("series", strconv.FormatInt(filter.SeriesID, 10))
	}
	if role := strings.TrimSpace(filter.PackRole); role != "" {
		q.Set("kind", role)
	}
	if filter.FromDay != "" {
		q.Set("from", filter.FromDay)
	}
	if filter.ToDay != "" {
		q.Set("to", filter.ToDay)
	}
	if s := strings.TrimSpace(filter.Studio); s != "" {
		q.Set("studio", s)
	}
	if s := strings.TrimSpace(filter.Country); s != "" {
		q.Set("country", s)
	}
	if s := strings.TrimSpace(filter.MPAA); s != "" {
		q.Set("mpaa", s)
	}
	if s := strings.TrimSpace(filter.MediaType); s != "" {
		q.Set("media_type", s)
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
	if view == viewThumbs || view == viewTable {
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
	for _, st := range filter.Statuses {
		out = append(out, listViewBadge{Label: "Status: " + videoStatusLabel(st), Href: dropQueryValue(r, "status", st)})
	}
	if filter.SourceID == library.VideoSourceImport {
		out = append(out, listViewBadge{Label: "Source: Import", Href: dropQueryKeys(r, "source", "page")})
	} else if filter.SourceID > 0 {
		out = append(out, listViewBadge{Label: "Source: #" + strconv.FormatInt(filter.SourceID, 10), Href: dropQueryKeys(r, "source", "page")})
	}
	if showSeries && filter.SeriesID > 0 {
		title := seriesTitles[filter.SeriesID]
		if title == "" {
			title = "#" + strconv.FormatInt(filter.SeriesID, 10)
		}
		out = append(out, listViewBadge{Label: "Series: " + title, Href: dropQueryKeys(r, "series", "page")})
	}
	if filter.Year > 0 {
		out = append(out, listViewBadge{Label: "Year: " + strconv.Itoa(filter.Year), Href: dropQueryKeys(r, "year", "page")})
	}
	if role := strings.TrimSpace(filter.PackRole); role != "" {
		out = append(out, listViewBadge{Label: "Kind: " + role, Href: dropQueryKeys(r, "kind", "page")})
	}
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
	if s := strings.TrimSpace(filter.Studio); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("studio", s), Href: dropQueryKeys(r, "studio", "page")})
	}
	if s := strings.TrimSpace(filter.Country); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("country", s), Href: dropQueryKeys(r, "country", "page")})
	}
	if s := strings.TrimSpace(filter.MPAA); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("mpaa", s), Href: dropQueryKeys(r, "mpaa", "page")})
	}
	if s := strings.TrimSpace(filter.MediaType); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("media_type", s), Href: dropQueryKeys(r, "media_type", "page")})
	}
	for _, g := range filter.Genres {
		out = append(out, listViewBadge{Label: filterValueChipLabel("genre", g), Href: dropQueryValue(r, "genre", g)})
	}
	for _, t := range filter.Tags {
		out = append(out, listViewBadge{Label: filterValueChipLabel("tag", t), Href: dropQueryValue(r, "tag", t)})
	}
	for _, a := range filter.Actors {
		out = append(out, listViewBadge{Label: filterValueChipLabel("actor", a), Href: dropQueryValue(r, "actor", a)})
	}
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
	if includeSeries {
		list, _ := h.Library.ListSeriesFiltered(library.SeriesListFilter{}, 0, 0)
		opts := make([]listFilterOpt, 0, len(list))
		for _, ser := range list {
			opts = append(opts, listFilterOpt{
				Value:    strconv.FormatInt(ser.ID, 10),
				Label:    ser.Title,
				Selected: filter.SeriesID == ser.ID,
			})
		}
		if len(opts) > 0 {
			selects = append(selects, listFilterSelect{Name: "series", AriaLabel: "Series", EmptyLabel: "All series", Options: opts})
		}
	}
	scope := seriesID
	if scope == 0 && filter.SeriesID > 0 {
		scope = filter.SeriesID
	}
	years, _ := h.Library.DistinctVideoYears(scope)
	yearOpts := make([]listFilterOpt, 0, len(years))
	for _, y := range years {
		ys := strconv.Itoa(y)
		yearOpts = append(yearOpts, listFilterOpt{Value: ys, Label: ys, Selected: filter.Year == y})
	}
	if len(yearOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "year", AriaLabel: "Upload year", EmptyLabel: "All years", Options: yearOpts,
		})
	}
	statuses, _ := h.Library.DistinctVideoStatuses(scope)
	sel := ""
	if len(filter.Statuses) == 1 {
		sel = filter.Statuses[0]
	}
	statusOpts := make([]listFilterOpt, 0, len(statuses))
	for _, st := range statuses {
		statusOpts = append(statusOpts, listFilterOpt{Value: st, Label: videoStatusLabel(st), Selected: st == sel})
	}
	if len(statusOpts) > 0 {
		selects = append(selects, listFilterSelect{Name: "status", AriaLabel: "Status", EmptyLabel: "Any status", Options: statusOpts})
	}
	studios, _ := h.Library.DistinctVideoScalar(scope, "studio")
	studioOpts := make([]listFilterOpt, 0, len(studios))
	for _, s := range studios {
		studioOpts = append(studioOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.Studio, s)})
	}
	if len(studioOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "studio", AriaLabel: "Studio", EmptyLabel: "Any studio", Options: studioOpts,
			PresenceField: library.PresenceStudio,
		})
	}
	countries, _ := h.Library.DistinctVideoScalar(scope, "country")
	countryOpts := make([]listFilterOpt, 0, len(countries))
	for _, s := range countries {
		countryOpts = append(countryOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.Country, s)})
	}
	if len(countryOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "country", AriaLabel: "Country", EmptyLabel: "Any country", Options: countryOpts,
			PresenceField: library.PresenceCountry,
		})
	}
	mpaas, _ := h.Library.DistinctVideoScalar(scope, "mpaa")
	mpaaOpts := make([]listFilterOpt, 0, len(mpaas))
	for _, s := range mpaas {
		mpaaOpts = append(mpaaOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.MPAA, s)})
	}
	if len(mpaaOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "mpaa", AriaLabel: "Content rating", EmptyLabel: "Any rating", Options: mpaaOpts,
			PresenceField: library.PresenceMPAA,
		})
	}
	mts, _ := h.Library.DistinctVideoScalar(scope, "media_type")
	mtOpts := make([]listFilterOpt, 0, len(mts))
	for _, s := range mts {
		mtOpts = append(mtOpts, listFilterOpt{Value: s, Label: s, Selected: filter.MediaType == s})
	}
	if len(mtOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "media_type", AriaLabel: "Media type", EmptyLabel: "Any media type", Options: mtOpts,
			PresenceField: library.PresenceMediaType,
		})
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
			Name: "genre", AriaLabel: "Genre", EmptyLabel: "Any genre", Options: genreOpts, Multi: true,
			PresenceField: library.PresenceGenres,
		})
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
			Name: "tag", AriaLabel: "Tag", EmptyLabel: "Any tag", Options: tagOpts, Multi: true,
			PresenceField: library.PresenceTags,
		})
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
			Name: "actor", AriaLabel: "Actor", EmptyLabel: "Any actor", Options: actorOpts, Multi: true,
			PresenceField: library.PresenceActors,
		})
	}

	if seriesID > 0 || len(sources) > 0 {
		srcOpts := make([]listFilterOpt, 0, len(sources)+1)
		if seriesID > 0 {
			nullImportCount, _ := h.Library.CountVideosWithNullSource(seriesID)
			if nullImportCount > 0 {
				srcOpts = append(srcOpts, listFilterOpt{
					Value: library.VideoSourceImportQuery, Label: "Import",
					Selected: filter.SourceID == library.VideoSourceImport,
				})
			}
		}
		for _, src := range sources {
			srcOpts = append(srcOpts, listFilterOpt{
				Value: strconv.FormatInt(src.ID, 10), Label: sourceFilterLabel(src),
				Selected: filter.SourceID == src.ID,
			})
		}
		if len(srcOpts) > 0 {
			selects = append(selects, listFilterSelect{Name: "source", AriaLabel: "Source", EmptyLabel: "All sources", Options: srcOpts})
		}
	}
	kindOpts := []listFilterOpt{
		{Value: library.PackRoleRegular, Label: "regular episode", Selected: filter.PackRole == library.PackRoleRegular},
		{Value: library.VideoPackRoleAnySpecial, Label: "any special", Selected: filter.PackRole == library.VideoPackRoleAnySpecial},
	}
	for _, opt := range library.PackRoleSelectOptions() {
		kindOpts = append(kindOpts, listFilterOpt{
			Value: opt.Value, Label: opt.Label, Selected: filter.PackRole == opt.Value,
		})
	}
	selects = append(selects, listFilterSelect{Name: "kind", AriaLabel: "Kind", EmptyLabel: "Any kind", Options: kindOpts})

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
		{Value: library.SortTitle, Label: "Title", Selected: cur == library.SortTitle, Icon: "type"},
	}
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
		{Value: library.QFieldNotes, Label: "Notes", Selected: cur == library.QFieldNotes},
	}
}

func searchByPlaceholder(qField string) string {
	_ = qField
	return "Search by..."
}

func viewOpts(r *http.Request, current string) []listFilterOpt {
	opts := []listFilterOpt{
		{Value: viewList, Label: "List", Selected: current == viewList || current == ""},
		{Value: viewThumbs, Label: "Thumbs", Selected: current == viewThumbs},
		{Value: viewTable, Label: "Table", Selected: current == viewTable},
	}
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
	}
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
		if v == defaultSort {
			opts[i].Href = dropQueryKeys(r, "sort", "dir", "page")
		} else {
			opts[i].Href = applySortFieldURL(r, v)
		}
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
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

func seriesListBadges(r *http.Request, filter library.SeriesListFilter) []listViewBadge {
	var out []listViewBadge
	// Search (q) stays in the toolbar field only - not a chip.
	if filter.RootID > 0 {
		out = append(out, listViewBadge{Label: "Root: #" + strconv.FormatInt(filter.RootID, 10), Href: dropQueryKeys(r, "root", "page")})
	}
	if filter.QualityProfileID > 0 {
		out = append(out, listViewBadge{Label: "Quality: #" + strconv.FormatInt(filter.QualityProfileID, 10), Href: dropQueryKeys(r, "quality", "page")})
	}
	if filter.DeliveryMode == library.DeliveryVideo || filter.DeliveryMode == library.DeliveryAudio {
		out = append(out, listViewBadge{Label: "Delivery: " + filter.DeliveryMode, Href: dropQueryKeys(r, "delivery", "page")})
	}
	if filter.Status != "" {
		out = append(out, listViewBadge{Label: "Status: " + filter.Status, Href: dropQueryKeys(r, "status", "page")})
	}
	if filter.PremieredYear > 0 {
		out = append(out, listViewBadge{Label: "Premiered: " + strconv.Itoa(filter.PremieredYear), Href: dropQueryKeys(r, "year", "page")})
	}
	if s := strings.TrimSpace(filter.Studio); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("studio", s), Href: dropQueryKeys(r, "studio", "page")})
	}
	if s := strings.TrimSpace(filter.Country); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("country", s), Href: dropQueryKeys(r, "country", "page")})
	}
	if s := strings.TrimSpace(filter.MPAA); s != "" {
		out = append(out, listViewBadge{Label: filterValueChipLabel("mpaa", s), Href: dropQueryKeys(r, "mpaa", "page")})
	}
	for _, g := range filter.Genres {
		out = append(out, listViewBadge{Label: filterValueChipLabel("genre", g), Href: dropQueryValue(r, "genre", g)})
	}
	for _, t := range filter.Tags {
		out = append(out, listViewBadge{Label: filterValueChipLabel("tag", t), Href: dropQueryValue(r, "tag", t)})
	}
	for _, a := range filter.Actors {
		out = append(out, listViewBadge{Label: filterValueChipLabel("actor", a), Href: dropQueryValue(r, "actor", a)})
	}
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
	if len(roots) > 1 {
		opts := make([]listFilterOpt, 0, len(roots))
		for _, root := range roots {
			label := strings.TrimSpace(root.Name)
			if label == "" {
				label = root.Path
			}
			opts = append(opts, listFilterOpt{
				Value: strconv.FormatInt(root.ID, 10), Label: label, Selected: filter.RootID == root.ID,
			})
		}
		selects = append(selects, listFilterSelect{Name: "root", AriaLabel: "Root folder", EmptyLabel: "All roots", Options: opts})
	}
	if len(profiles) > 0 {
		opts := make([]listFilterOpt, 0, len(profiles))
		for _, p := range profiles {
			opts = append(opts, listFilterOpt{
				Value: strconv.FormatInt(p.ID, 10), Label: p.Name, Selected: filter.QualityProfileID == p.ID,
			})
		}
		selects = append(selects, listFilterSelect{Name: "quality", AriaLabel: "Quality profile", EmptyLabel: "All quality", Options: opts})
	}
	selects = append(selects, listFilterSelect{
		Name: "delivery", AriaLabel: "Delivery mode", EmptyLabel: "All delivery",
		Options: []listFilterOpt{
			{Value: library.DeliveryVideo, Label: "Video", Selected: filter.DeliveryMode == library.DeliveryVideo},
			{Value: library.DeliveryAudio, Label: "Audio", Selected: filter.DeliveryMode == library.DeliveryAudio},
		},
	})
	selects = append(selects, listFilterSelect{
		Name: "status", AriaLabel: "Status", EmptyLabel: "Any status",
		Options: []listFilterOpt{
			{Value: library.SeriesListStatusMonitored, Label: "Monitored", Selected: filter.Status == library.SeriesListStatusMonitored},
			{Value: library.SeriesListStatusUnmonitored, Label: "Unmonitored", Selected: filter.Status == library.SeriesListStatusUnmonitored},
			{Value: library.SeriesListStatusComplete, Label: "Complete", Selected: filter.Status == library.SeriesListStatusComplete},
			{Value: library.SeriesListStatusIncomplete, Label: "Incomplete", Selected: filter.Status == library.SeriesListStatusIncomplete},
			{Value: library.SeriesListStatusHasErrors, Label: "Has errors", Selected: filter.Status == library.SeriesListStatusHasErrors},
		},
	})
	years, _ := h.Library.DistinctSeriesPremieredYears()
	yearOpts := make([]listFilterOpt, 0, len(years))
	for _, y := range years {
		ys := strconv.Itoa(y)
		yearOpts = append(yearOpts, listFilterOpt{Value: ys, Label: ys, Selected: filter.PremieredYear == y})
	}
	if len(yearOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "year", AriaLabel: "Premiered", EmptyLabel: "Any year", Options: yearOpts,
			PresenceField: library.PresencePremiered,
		})
	} else {
		selects = append(selects, presenceOnlySelect(r, library.PresencePremiered, "Premiered"))
	}
	studios, _ := h.Library.DistinctSeriesScalar("studio")
	studioOpts := make([]listFilterOpt, 0, len(studios))
	for _, s := range studios {
		studioOpts = append(studioOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.Studio, s)})
	}
	if len(studioOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "studio", AriaLabel: "Studio", EmptyLabel: "Any studio", Options: studioOpts,
			PresenceField: library.PresenceStudio,
		})
	}
	countries, _ := h.Library.DistinctSeriesScalar("country")
	countryOpts := make([]listFilterOpt, 0, len(countries))
	for _, s := range countries {
		countryOpts = append(countryOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.Country, s)})
	}
	if len(countryOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "country", AriaLabel: "Country", EmptyLabel: "Any country", Options: countryOpts,
			PresenceField: library.PresenceCountry,
		})
	}
	mpaas, _ := h.Library.DistinctSeriesScalar("mpaa")
	mpaaOpts := make([]listFilterOpt, 0, len(mpaas))
	for _, s := range mpaas {
		mpaaOpts = append(mpaaOpts, listFilterOpt{Value: s, Label: s, Selected: strings.EqualFold(filter.MPAA, s)})
	}
	if len(mpaaOpts) > 0 {
		selects = append(selects, listFilterSelect{
			Name: "mpaa", AriaLabel: "Content rating", EmptyLabel: "Any rating", Options: mpaaOpts,
			PresenceField: library.PresenceMPAA,
		})
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
			Name: "genre", AriaLabel: "Genre", EmptyLabel: "Any genre", Options: genreOpts, Multi: true,
			PresenceField: library.PresenceGenres,
		})
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
			Name: "tag", AriaLabel: "Tag", EmptyLabel: "Any tag", Options: tagOpts, Multi: true,
			PresenceField: library.PresenceTags,
		})
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
			Name: "actor", AriaLabel: "Actor", EmptyLabel: "Any actor", Options: actorOpts, Multi: true,
			PresenceField: library.PresenceActors,
		})
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
