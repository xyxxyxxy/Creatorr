package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

const (
	viewList    = "list"
	viewCards   = "cards"
	viewGallery = "gallery"
	viewTable   = "table"
	// legacyViewThumbs is accepted in ?view= and cookies; canonicalizeViewMode maps it to cards.
	legacyViewThumbs = "thumbs"

	cookieModeSeries       = "creatorr_mode_series"
	cookieModeSeriesVideos = "creatorr_mode_series_videos"
	cookieModeVideos       = "creatorr_mode_videos"
)

// listViewBadge is one removable active-filter chip.
// When Group is set, consecutive same-Group badges render as a daisyUI join
// cluster (OR-within-field); Label is the visible value chip text.
type listViewBadge struct {
	Group      string // clustering key (e.g. status); empty = standalone chip
	GroupTitle string // join prefix (e.g. Status); falls back to Group
	Label      string // visible chip text (value when grouped; "Field: value" when standalone)
	AriaLabel  string // checkbox accessible name; empty falls back to Label
	Href       string // URL without this constraint
	ClearHref  string // grouped: URL clearing every value for the field (join prefix)
}

// listViewBadgeCluster is a standalone chip (Title empty) or a join group (Title set).
type listViewBadgeCluster struct {
	Title     string
	ClearHref string // join prefix clears the whole field
	Items     []listViewBadge
}

// orJoinBadges builds OR-within-field join chips (Group + value Label + dropQueryValue).
func orJoinBadges(r *http.Request, group, title, queryKey string, values []string, labelFn func(string) string) []listViewBadge {
	clearHref := clearQueryKey(r, queryKey)
	out := make([]listViewBadge, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		label := v
		if labelFn != nil {
			label = labelFn(v)
		}
		out = append(out, listViewBadge{
			Group:      group,
			GroupTitle: title,
			Label:      label,
			Href:       dropQueryValue(r, queryKey, v),
			ClearHref:  clearHref,
		})
	}
	return out
}

// clusterListViewBadges groups consecutive badges that share Group into join clusters.
func clusterListViewBadges(badges []listViewBadge) []listViewBadgeCluster {
	var out []listViewBadgeCluster
	for _, b := range badges {
		if b.Group == "" {
			out = append(out, listViewBadgeCluster{Items: []listViewBadge{b}})
			continue
		}
		n := len(out)
		if n > 0 && out[n-1].Title != "" && len(out[n-1].Items) > 0 && out[n-1].Items[0].Group == b.Group {
			out[n-1].Items = append(out[n-1].Items, b)
			continue
		}
		title := b.GroupTitle
		if title == "" {
			title = b.Group
		}
		out = append(out, listViewBadgeCluster{Title: title, ClearHref: b.ClearHref, Items: []listViewBadge{b}})
	}
	return out
}

// canonicalizeViewMode maps a raw view token to a first-class mode, or "" if unknown.
// Legacy "thumbs" becomes cards.
func canonicalizeViewMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case viewList, viewCards, viewGallery, viewTable:
		return strings.ToLower(strings.TrimSpace(raw))
	case legacyViewThumbs:
		return viewCards
	default:
		return ""
	}
}

// resolveViewMode reads ?view= or the scope cookie; defaultMode is list unless locked.
// When view is in the query, writeCookie is true so the handler can persist it.
func resolveViewMode(r *http.Request, cookieName, defaultMode string) (mode string, writeCookie bool) {
	if mode := canonicalizeViewMode(r.URL.Query().Get("view")); mode != "" {
		return mode, true
	}
	if c, err := r.Cookie(cookieName); err == nil {
		if mode := canonicalizeViewMode(c.Value); mode != "" {
			return mode, false
		}
	}
	if mode := canonicalizeViewMode(defaultMode); mode != "" {
		return mode, false
	}
	return viewList, false
}

func writeViewCookie(w http.ResponseWriter, name, mode string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    mode,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

func parseQField(r *http.Request) string {
	return library.NormalizeQField(r.URL.Query().Get("q_field"))
}

func parsePresenceParams(q url.Values) (empty, notEmpty []string) {
	empty = uniqueQueryVals(q["empty"])
	notEmpty = uniqueQueryVals(q["not_empty"])
	// Drop fields that appear in both.
	both := map[string]struct{}{}
	for _, e := range empty {
		for _, n := range notEmpty {
			if strings.EqualFold(e, n) {
				both[strings.ToLower(e)] = struct{}{}
			}
		}
	}
	if len(both) == 0 {
		return empty, notEmpty
	}
	filter := func(in []string) []string {
		var out []string
		for _, v := range in {
			if _, ok := both[strings.ToLower(v)]; !ok {
				out = append(out, v)
			}
		}
		return out
	}
	return filter(empty), filter(notEmpty)
}

func uniqueQueryVals(raw []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range raw {
		v := strings.TrimSpace(r)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func parseMultiQuery(q url.Values, name string) []string {
	return uniqueQueryVals(q[name])
}

func parseMultiInt64(q url.Values, name string) []int64 {
	var out []int64
	seen := map[int64]struct{}{}
	for _, raw := range q[name] {
		n := parseIntQuery(raw)
		if n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func parseMultiYear(q url.Values, name string) []int {
	var out []int
	seen := map[int]struct{}{}
	for _, raw := range q[name] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		y, err := strconv.Atoi(raw)
		if err != nil || y < 1900 || y > 2100 {
			continue
		}
		if _, ok := seen[y]; ok {
			continue
		}
		seen[y] = struct{}{}
		out = append(out, y)
	}
	return out
}

func selectedSet(vals []string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		m[strings.ToLower(v)] = true
	}
	return m
}

func selectedInt64Set(vals []int64) map[int64]bool {
	m := make(map[int64]bool, len(vals))
	for _, v := range vals {
		if v != 0 {
			m[v] = true
		}
	}
	return m
}

func selectedIntSet(vals []int) map[int]bool {
	m := make(map[int]bool, len(vals))
	for _, v := range vals {
		if v != 0 {
			m[v] = true
		}
	}
	return m
}

// parseExclusiveBoolFilter maps repeated query values to *bool when exactly one outcome is selected.
func parseExclusiveBoolFilter(q url.Values, name string, mapFn func(string) (bool, bool)) *bool {
	var seenTrue, seenFalse bool
	for _, raw := range q[name] {
		b, ok := mapFn(strings.TrimSpace(raw))
		if !ok {
			continue
		}
		if b {
			seenTrue = true
		} else {
			seenFalse = true
		}
	}
	if seenTrue && seenFalse {
		return nil
	}
	if seenTrue {
		v := true
		return &v
	}
	if seenFalse {
		v := false
		return &v
	}
	return nil
}

func parseVideoSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case library.SortAdded, library.SortAcquired, library.SortTitle, library.SortUpload, library.SortDuration:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func parseSeriesSort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case library.SortAdded, library.SortTitle, library.SortLastUpload,
		library.SortDownloaded, library.SortWanted, library.SortErrors, library.SortSize:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func parseSortDir(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case library.SortDirAsc, library.SortDirDesc:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

// dropQueryKeys returns the request path with listed query keys removed (all values).
// Dropping page also drops through (infinite keep-depth resets with paging).
func dropQueryKeys(r *http.Request, keys ...string) string {
	q := r.URL.Query()
	dropThrough := false
	for _, k := range keys {
		q.Del(k)
		if k == "page" {
			dropThrough = true
		}
	}
	if dropThrough {
		q.Del("through")
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

// dropQueryValue removes one value from a multi query key (e.g. one genre).
// Clearing the last status value sets status= so list prefs cookies can clear.
func dropQueryValue(r *http.Request, key, value string) string {
	q := r.URL.Query()
	vals := q[key]
	q.Del(key)
	for _, v := range vals {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(value)) {
			continue
		}
		q.Add(key, v)
	}
	if key == "status" && len(q[key]) == 0 {
		q.Set("status", "")
	}
	q.Del("page")
	q.Del("through")
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

func clearOperatorFiltersURL(r *http.Request, keepKeys ...string) string {
	keep := map[string]struct{}{}
	for _, k := range keepKeys {
		keep[k] = struct{}{}
	}
	// Always keep sort, view, page? Plan: Clear all drops filters + search; sort and view stay.
	keep["sort"] = struct{}{}
	keep["view"] = struct{}{}
	keep["dir"] = struct{}{}
	// Explorer / series-detail scope (not operator filters).
	keep["type"] = struct{}{}
	keep["at"] = struct{}{}
	keep["series_id"] = struct{}{}
	q := r.URL.Query()
	out := url.Values{}
	for k, vs := range q {
		if _, ok := keep[k]; ok {
			for _, v := range vs {
				out.Add(k, v)
			}
		}
	}
	// Empty markers so filter cookies do not re-apply after Clear all.
	for _, k := range []string{
		"status", "domain", "kind", "origin",
		"level", "nlevel", "unread",
		"from", "to",
	} {
		out.Set(k, "")
	}
	u := *r.URL
	enc := out.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

// applySelectOptionURLClearingPresence returns path with name set/cleared and matching presence dropped.
// Clearing status sets status= (empty) so list prefs cookies can clear.
func applySelectOptionURLClearingPresence(r *http.Request, name, value, presenceField string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	q.Del(name)
	if strings.TrimSpace(value) != "" {
		q.Set(name, value)
	} else if name == "status" {
		q.Set("status", "")
	}
	clearPresenceField(q, presenceField)
	u := *r.URL
	enc := q.Encode()
	if enc == "" {
		u.RawQuery = ""
	} else {
		u.RawQuery = enc
	}
	return u.RequestURI()
}

// toggleMultiSelectURL adds value to a multi query key, or drops it when already selected.
// Non-empty result clears the matching presence constraint (same as chip apply).
// Clearing the last status value sets status= so list prefs cookies can clear.
func toggleMultiSelectURL(r *http.Request, name, value, presenceField string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	want := strings.TrimSpace(value)
	vals := q[name]
	q.Del(name)
	found := false
	for _, v := range vals {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			found = true
			continue
		}
		q.Add(name, v)
	}
	if !found && want != "" {
		q.Add(name, want)
	}
	if len(q[name]) > 0 {
		clearPresenceField(q, presenceField)
	} else if name == "status" {
		q.Set("status", "")
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

func applyPresenceURL(r *http.Request, empty bool, field string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del("through")
	for _, key := range filterKeysForPresence(field) {
		q.Del(key)
	}
	clearPresenceField(q, field)
	if empty {
		q.Add("empty", field)
	} else {
		q.Add("not_empty", field)
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

func filterKeysForPresence(field string) []string {
	switch field {
	case library.PresenceGenres:
		return []string{"genre"}
	case library.PresenceTags:
		return []string{"tag"}
	case library.PresenceActors:
		return []string{"actor"}
	case library.PresenceStudio:
		return []string{"studio"}
	case library.PresenceCountry:
		return []string{"country"}
	case library.PresenceMPAA:
		return []string{"mpaa"}
	case library.PresenceMediaType:
		return []string{"media_type"}
	case library.PresenceUploadDate:
		return []string{"year", "from", "to"}
	case library.PresencePremiered:
		return []string{"year"}
	default:
		return nil
	}
}

func clearPresenceField(q url.Values, field string) {
	if field == "" {
		return
	}
	for _, key := range []string{"empty", "not_empty"} {
		vals := q[key]
		q.Del(key)
		for _, v := range vals {
			if !strings.EqualFold(strings.TrimSpace(v), field) {
				q.Add(key, v)
			}
		}
	}
}

func annotateFilterSelect(r *http.Request, sel *listFilterSelect) {
	n := 0
	for i := range sel.Options {
		sel.Options[i].Href = toggleMultiSelectURL(r, sel.Name, sel.Options[i].Value, sel.PresenceField)
		if sel.Options[i].Selected {
			n++
		}
	}
	sel.SelectedCount = n
	if sel.PresenceField != "" {
		sel.PresenceEmptyLabel = presenceBadgeLabel(sel.PresenceField, true)
		sel.PresenceFilledLabel = presenceBadgeLabel(sel.PresenceField, false)
		sel.PresenceEmptySelected = hasPresenceQuery(r, "empty", sel.PresenceField)
		sel.PresenceFilledSelected = hasPresenceQuery(r, "not_empty", sel.PresenceField)
		// Active Has/No clears that presence (toggle); inactive applies it.
		if sel.PresenceEmptySelected {
			sel.PresenceEmptyHref = dropQueryValue(r, "empty", sel.PresenceField)
		} else {
			sel.PresenceEmptyHref = applyPresenceURL(r, true, sel.PresenceField)
		}
		if sel.PresenceFilledSelected {
			sel.PresenceFilledHref = dropQueryValue(r, "not_empty", sel.PresenceField)
		} else {
			sel.PresenceFilledHref = applyPresenceURL(r, false, sel.PresenceField)
		}
	}
}

func annotateFilterSelects(r *http.Request, selects []listFilterSelect) {
	for i := range selects {
		annotateFilterSelect(r, &selects[i])
	}
}

func hasPresenceQuery(r *http.Request, key, field string) bool {
	want := strings.TrimSpace(field)
	for _, v := range r.URL.Query()[key] {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

// annotateUploadPresence sets Has/No upload-date links on the toolbar (toggle when active).
func annotateUploadPresence(r *http.Request, tb *listViewToolbar) {
	tb.UploadEmptySelected = hasPresenceQuery(r, "empty", library.PresenceUploadDate)
	tb.UploadFilledSelected = hasPresenceQuery(r, "not_empty", library.PresenceUploadDate)
	if tb.UploadEmptySelected {
		tb.UploadEmptyHref = dropQueryValue(r, "empty", library.PresenceUploadDate)
	} else {
		tb.UploadEmptyHref = applyPresenceURL(r, true, library.PresenceUploadDate)
	}
	if tb.UploadFilledSelected {
		tb.UploadFilledHref = dropQueryValue(r, "not_empty", library.PresenceUploadDate)
	} else {
		tb.UploadFilledHref = applyPresenceURL(r, false, library.PresenceUploadDate)
	}
}

func presenceOnlySelect(r *http.Request, field, aria string) listFilterSelect {
	sel := listFilterSelect{
		Name:          "presence_" + field,
		AriaLabel:     aria,
		PresenceField: field,
	}
	annotateFilterSelect(r, &sel)
	return sel
}

// boolOnlySelect is Has/No chrome for a two-state query param (yes/no or on/off).
// Has applies trueVal; No applies falseVal; pressing the active side clears the param.
func boolOnlySelect(r *http.Request, name, aria, trueVal, falseVal string, selected *bool) listFilterSelect {
	sel := listFilterSelect{
		Name:                name,
		AriaLabel:           aria,
		PresenceFilledLabel: "Has " + strings.ToLower(aria),
		PresenceEmptyLabel:  "No " + strings.ToLower(aria),
	}
	clearHref := dropQueryKeys(r, name, "page", "through")
	hasHref := applySelectOptionURLClearingPresence(r, name, trueVal, "")
	noHref := applySelectOptionURLClearingPresence(r, name, falseVal, "")
	switch {
	case selected != nil && *selected:
		sel.PresenceFilledSelected = true
		sel.PresenceFilledHref = clearHref
		sel.PresenceEmptyHref = noHref
	case selected != nil && !*selected:
		sel.PresenceEmptySelected = true
		sel.PresenceEmptyHref = clearHref
		sel.PresenceFilledHref = hasHref
	default:
		sel.PresenceFilledHref = hasHref
		sel.PresenceEmptyHref = noHref
	}
	return sel
}

// packRoleFilterPresence reports all-regular / all-special-ish for Has/No chrome.
func packRoleFilterPresence(roles []string) (allRegular, allSpecial bool) {
	if len(roles) == 0 {
		return false, false
	}
	allRegular = true
	allSpecial = true
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		switch role {
		case library.PackRoleRegular:
			allSpecial = false
		case library.VideoPackRoleAnySpecial:
			allRegular = false
		default:
			if library.IsSpecialPackRole(role) {
				allRegular = false
			} else {
				allRegular = false
				allSpecial = false
			}
		}
	}
	return allRegular, allSpecial
}

// specialKindFilterSelect is Has/No for any-special vs regular, plus accordion of concrete specials.
func specialKindFilterSelect(r *http.Request, packRoles []string, concreteOpts []listFilterOpt) listFilterSelect {
	sel := listFilterSelect{
		Name:                "kind",
		AriaLabel:           "Special kind",
		Options:             concreteOpts,
		PresenceFilledLabel: "Has special kind",
		PresenceEmptyLabel:  "No special kind",
	}
	allRegular, allSpecial := packRoleFilterPresence(packRoles)
	clearHref := dropQueryKeys(r, "kind", "page", "through")
	hasHref := applySelectOptionURLClearingPresence(r, "kind", library.VideoPackRoleAnySpecial, "")
	noHref := applySelectOptionURLClearingPresence(r, "kind", library.PackRoleRegular, "")
	switch {
	case allRegular && !allSpecial:
		sel.PresenceEmptySelected = true
		sel.PresenceEmptyHref = clearHref
		sel.PresenceFilledHref = hasHref
	case allSpecial && !allRegular:
		sel.PresenceFilledSelected = true
		sel.PresenceFilledHref = clearHref
		sel.PresenceEmptyHref = noHref
	default:
		sel.PresenceFilledHref = hasHref
		sel.PresenceEmptyHref = noHref
	}
	return sel
}

func specialKindChipLabel(packRole string) string {
	role := strings.TrimSpace(packRole)
	switch role {
	case library.PackRoleRegular:
		return "No special kind"
	case library.VideoPackRoleAnySpecial:
		return "Has special kind"
	default:
		for _, opt := range library.PackRoleSelectOptions() {
			if opt.Value == role {
				return "Special kind: " + opt.Label
			}
		}
		if label := library.PackRoleBadgeLabel(role); label != "" {
			return "Special kind: " + label
		}
		return "Special kind: " + role
	}
}

func presenceBadgeLabel(field string, empty bool) string {
	name := presenceFieldLabel(field)
	if empty {
		return "No " + name
	}
	return "Has " + name
}

// filterValueChipLabel is active-chip / Filter-menu copy for a concrete value
// on a presence-capable field (e.g. country → "Country: US").
func filterValueChipLabel(queryKey, value string) string {
	v := strings.TrimSpace(value)
	switch strings.ToLower(strings.TrimSpace(queryKey)) {
	case "studio":
		return "Studio: " + v
	case "country":
		return "Country: " + v
	case "mpaa":
		return "Rating: " + v
	case "media_type":
		return "Media: " + v
	case "genre":
		return "Genre: " + v
	case "tag":
		return "Tag: " + v
	case "actor":
		return "Actor: " + v
	default:
		return v
	}
}

func presenceFieldLabel(field string) string {
	switch strings.ToLower(field) {
	case library.PresencePlot:
		return "plot"
	case library.PresenceTagline:
		return "tagline"
	case library.PresenceNotes:
		return "notes"
	case library.PresenceStudio:
		return "studio"
	case library.PresenceCountry:
		return "country"
	case library.PresenceMPAA:
		return "content rating"
	case library.PresencePremiered:
		return "premiered"
	case library.PresenceGenres:
		return "genre"
	case library.PresenceTags:
		return "tag"
	case library.PresenceActors:
		return "actor"
	case library.PresenceSortTitle:
		return "sort title"
	case library.PresenceOriginalTitle:
		return "original title"
	case library.PresenceUploadDate:
		return "upload date"
	case library.PresenceMediaType:
		return "media type"
	case library.PresenceThumbnail:
		return "thumbnail"
	case library.PresenceScanError:
		return "scan error"
	default:
		return field
	}
}

func parseFilterDay(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return ""
	}
	return raw
}

func parseIntQuery(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
