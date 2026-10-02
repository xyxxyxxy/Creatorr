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
	viewList   = "list"
	viewThumbs = "thumbs"
	viewTable  = "table"

	cookieModeSeries       = "creatorr_mode_series"
	cookieModeSeriesVideos = "creatorr_mode_series_videos"
	cookieModeVideos       = "creatorr_mode_videos"
)

// listViewBadge is one removable active-filter chip.
type listViewBadge struct {
	Label string
	Href  string // URL without this constraint
}

type listViewOpt struct {
	Value, Label string
	Selected     bool
}

// resolveViewMode reads ?view= or the scope cookie; defaultMode is list unless locked.
// When view is in the query, writeCookie is true so the handler can persist it.
func resolveViewMode(r *http.Request, cookieName, defaultMode string) (mode string, writeCookie bool) {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	switch raw {
	case viewThumbs, viewList, viewTable:
		return raw, true
	}
	if c, err := r.Cookie(cookieName); err == nil {
		v := strings.ToLower(strings.TrimSpace(c.Value))
		if v == viewThumbs || v == viewList || v == viewTable {
			return v, false
		}
	}
	if defaultMode == viewThumbs {
		return viewThumbs, false
	}
	if defaultMode == viewTable {
		return viewTable, false
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
		library.SortDownloaded, library.SortWanted, library.SortErrors:
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
func dropQueryKeys(r *http.Request, keys ...string) string {
	q := r.URL.Query()
	for _, k := range keys {
		q.Del(k)
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
	q := r.URL.Query()
	out := url.Values{}
	for k, vs := range q {
		if _, ok := keep[k]; ok {
			for _, v := range vs {
				out.Add(k, v)
			}
		}
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
func applySelectOptionURLClearingPresence(r *http.Request, name, value, presenceField string) string {
	q := r.URL.Query()
	q.Del("page")
	q.Del(name)
	if strings.TrimSpace(value) != "" {
		q.Set(name, value)
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
func toggleMultiSelectURL(r *http.Request, name, value, presenceField string) string {
	q := r.URL.Query()
	q.Del("page")
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
	for i := range sel.Options {
		if sel.Multi {
			sel.Options[i].Href = toggleMultiSelectURL(r, sel.Name, sel.Options[i].Value, sel.PresenceField)
		} else {
			sel.Options[i].Href = applySelectOptionURLClearingPresence(r, sel.Name, sel.Options[i].Value, sel.PresenceField)
		}
	}
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
