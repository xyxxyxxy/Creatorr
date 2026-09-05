package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func parseSeriesIDList(r *http.Request) []int64 {
	_ = r.ParseForm()
	raw := r.Form["series_id"]
	if len(raw) == 0 {
		raw = r.Form["series_ids"]
	}
	var out []int64
	seen := map[int64]struct{}{}
	for _, s := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || id <= 0 {
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

func (h *Handler) actionBulkEditSeries(w http.ResponseWriter, r *http.Request) {
	ids := parseSeriesIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one series"), http.StatusSeeOther)
		return
	}
	p := library.BulkEditSeriesParams{SeriesIDs: ids}
	if v := strings.TrimSpace(r.FormValue("root_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			http.Redirect(w, r, "/series?err="+urlQuery("invalid root_id"), http.StatusSeeOther)
			return
		}
		p.RootID = &id
	}
	if v := strings.TrimSpace(r.FormValue("quality_profile_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			http.Redirect(w, r, "/series?err="+urlQuery("invalid quality_profile_id"), http.StatusSeeOther)
			return
		}
		p.QualityProfileID = &id
	}
	if v := strings.TrimSpace(r.FormValue("delivery_mode")); v != "" {
		m := library.NormalizeDeliveryMode(v)
		p.DeliveryMode = &m
	}
	if v := strings.TrimSpace(r.FormValue("monitored")); v == "1" || v == "0" {
		m := v == "1"
		p.Monitored = &m
	}
	tid, err := h.Library.EnqueueBulkEditSeries(p)
	if err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/series?ok=bulk_edit_queued&task="+strconv.FormatInt(tid, 10), http.StatusSeeOther)
}

func (h *Handler) actionBulkEditSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	ids := parseSeriesIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one series"), http.StatusSeeOther)
		return
	}
	p := library.BulkEditSeriesParams{SeriesIDs: ids}
	if v := strings.TrimSpace(r.FormValue("studio")); v != "" {
		p.Studio = &v
	}
	if v := strings.TrimSpace(r.FormValue("country")); v != "" {
		p.Country = &v
	}
	if v := strings.TrimSpace(r.FormValue("mpaa")); v != "" {
		p.MPAA = &v
	}
	if g := library.ParseStringListFields(r.Form["genre"]); len(g) > 0 {
		p.Genres = &g
	}
	if t := library.ParseStringListFields(r.Form["tag"]); len(t) > 0 {
		p.Tags = &t
	}
	if a := library.ParseActorsFromFields(r.Form["actor_name"], r.Form["actor_role"]); len(a) > 0 {
		p.Actors = &a
	}
	tid, err := h.Library.EnqueueBulkEditSeries(p)
	if err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/series?ok=bulk_edit_queued&task="+strconv.FormatInt(tid, 10), http.StatusSeeOther)
}

func (h *Handler) actionBulkSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	ids := parseSeriesIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one series"), http.StatusSeeOther)
		return
	}
	monitored := r.FormValue("monitored") == "1"
	updated, skipped, err := h.Library.SetSeriesMonitoredBulk(ids, monitored)
	if err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	msg := "updated=" + strconv.Itoa(updated)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, "/series?ok=bulk_monitored&detail="+urlQuery(msg), http.StatusSeeOther)
}

func (h *Handler) actionBulkDeleteSeries(w http.ResponseWriter, r *http.Request) {
	ids := parseSeriesIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one series"), http.StatusSeeOther)
		return
	}
	if r.FormValue("confirm_delete") != "1" {
		http.Redirect(w, r, "/series?err="+urlQuery("confirm delete required"), http.StatusSeeOther)
		return
	}
	tid, err := h.Library.EnqueueDeleteFiles(ids, nil)
	if err != nil {
		http.Redirect(w, r, "/series?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/series?ok=bulk_delete_queued&task="+strconv.FormatInt(tid, 10), http.StatusSeeOther)
}

func (h *Handler) seriesIDsJSON(w http.ResponseWriter, r *http.Request) {
	filter := parseSeriesListFilter(r)
	ids, err := h.Library.ListSeriesIDsFiltered(filter)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"ids": ids})
}

func (h *Handler) seriesBulkMetadataCommonJSON(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var body struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ids = body.IDs
	} else {
		ids = parseSeriesIDList(r)
	}
	meta, err := h.Library.CommonSeriesMetadata(ids)
	if err != nil {
		if errors.Is(err, library.ErrInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, library.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(meta)
}

func (h *Handler) seriesBulkSettingsCommonJSON(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var body struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ids = body.IDs
	} else {
		ids = parseSeriesIDList(r)
	}
	settings, err := h.Library.CommonSeriesSettings(ids)
	if err != nil {
		if errors.Is(err, library.ErrInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, library.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(settings)
}
