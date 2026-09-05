package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func parseVideoIDList(r *http.Request) []int64 {
	_ = r.ParseForm()
	raw := r.Form["video_id"]
	if len(raw) == 0 {
		raw = r.Form["video_ids"]
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

func seriesVideosRedirect(seriesID int64, r *http.Request, okKey, detail, errMsg string) string {
	q := url.Values{}
	if okKey != "" {
		q.Set("ok", okKey)
	}
	if detail != "" {
		q.Set("detail", detail)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	}
	// Preserve list filters from Referer query when present.
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u != nil {
			for _, k := range []string{"q", "status", "source", "year", "page", "from", "to"} {
				if v := u.Query().Get(k); v != "" && q.Get(k) == "" {
					q.Set(k, v)
				}
			}
		}
	}
	path := fmt.Sprintf("/series/%d", seriesID)
	enc := q.Encode()
	if enc == "" {
		return path
	}
	return path + "?" + enc
}

func (h *Handler) actionBulkWantVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	updated, skipped, err := h.Library.WantVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "updated=" + strconv.Itoa(updated)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_want", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkIgnoreVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	updated, skipped, err := h.Library.IgnoreVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "updated=" + strconv.Itoa(updated)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_ignore", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkDownloadVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	queued, skipped, err := h.Library.EnqueueDownloadVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "queued=" + strconv.Itoa(queued)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_download", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkRefreshSidecarsVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	queued, skipped, err := h.Library.EnqueueRefreshSidecarsVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "queued=" + strconv.Itoa(queued)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_refresh_sidecars", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkEditVideosMetadata(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	p := library.BulkEditVideosParams{VideoIDs: ids}
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
	tid, err := h.Library.EnqueueBulkEditVideos(p)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_edit_queued", "task="+strconv.FormatInt(tid, 10), ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkDeleteVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 || sid <= 0 {
		http.Redirect(w, r, "/series?err="+urlQuery("select at least one video"), http.StatusSeeOther)
		return
	}
	tid, _, _, err := h.Library.EnqueueBulkDeleteVideos(ids)
	if err != nil {
		http.Redirect(w, r, seriesVideosRedirect(sid, r, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, seriesVideosRedirect(sid, r, "bulk_delete_queued", "task="+strconv.FormatInt(tid, 10), ""), http.StatusSeeOther)
}

func (h *Handler) seriesVideoIDsJSON(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ser, err := h.Library.GetSeries(sid, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	filter := parseSeriesVideoListFilter(r, ser.Sources)
	ids, err := h.Library.ListVideoIDsFiltered(sid, filter)
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

func (h *Handler) videoBulkMetadataCommonJSON(w http.ResponseWriter, r *http.Request) {
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
		ids = parseVideoIDList(r)
	}
	meta, err := h.Library.CommonVideoMetadata(ids)
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
