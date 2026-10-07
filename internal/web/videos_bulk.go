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

func preferVideosList(r *http.Request) bool {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return false
	}
	u, err := url.Parse(ref)
	if err != nil || u == nil {
		return false
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "/videos" {
		return true
	}
	if path == "/browser" && u.Query().Get("type") == explorerTypeVideos {
		return true
	}
	return false
}

func videosListRedirect(r *http.Request, okKey, detail, errMsg string) string {
	q := url.Values{}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u != nil {
			for k, vs := range u.Query() {
				switch k {
				case "ok", "detail", "err", "n":
					continue
				}
				for _, v := range vs {
					q.Add(k, v)
				}
			}
		}
	}
	q.Set("type", explorerTypeVideos)
	if okKey != "" {
		q.Set("ok", okKey)
	}
	if detail != "" {
		q.Set("detail", detail)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	}
	enc := q.Encode()
	if enc == "" {
		return "/browser?type=videos"
	}
	return "/browser?" + enc
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
	// Series Videos glance only keeps ?page=.
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u != nil {
			if v := u.Query().Get("page"); v != "" && q.Get("page") == "" {
				q.Set("page", v)
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

// videoBulkRedirect sends the operator back to /videos or series detail after a bulk action.
func videoBulkRedirect(r *http.Request, seriesID int64, okKey, detail, errMsg string) string {
	if preferVideosList(r) || seriesID <= 0 {
		return videosListRedirect(r, okKey, detail, errMsg)
	}
	return seriesVideosRedirect(seriesID, r, okKey, detail, errMsg)
}

func (h *Handler) actionBulkWantVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
		return
	}
	updated, skipped, err := h.Library.WantVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	ok := "bulk_want"
	if sid > 0 {
		if ser, serr := h.Library.GetSeries(sid, false); serr == nil && !ser.Monitored {
			ok = "bulk_want_unmonitored"
		}
	}
	msg := "updated=" + strconv.Itoa(updated)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, videoBulkRedirect(r, sid, ok, msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkClearVideoDownloadErrors(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
		return
	}
	updated, _, err := h.Library.ClearVideoDownloadErrorsBulk(ids)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	redir := videoBulkRedirect(r, sid, "clear-error", "", "")
	http.Redirect(w, r, appendQuery(redir, "n="+strconv.Itoa(updated)), http.StatusSeeOther)
}

func (h *Handler) actionBulkIgnoreVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
		return
	}
	updated, skipped, err := h.Library.IgnoreVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "updated=" + strconv.Itoa(updated)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, videoBulkRedirect(r, sid, "bulk_ignore", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkRefreshSidecarsVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
		return
	}
	queued, skipped, err := h.Library.EnqueueRefreshSidecarsVideosBulk(ids)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	msg := "queued=" + strconv.Itoa(queued)
	if skipped > 0 {
		msg += " skipped=" + strconv.Itoa(skipped)
	}
	http.Redirect(w, r, videoBulkRedirect(r, sid, "bulk_refresh_sidecars", msg, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkEditVideosMetadata(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
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
	if v := strings.TrimSpace(r.FormValue("special_feature")); v != "" {
		p.SpecialFeature = &v
	}
	tid, err := h.Library.EnqueueBulkEditVideos(p)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, videoBulkRedirect(r, sid, "bulk_edit_queued", "task="+strconv.FormatInt(tid, 10), ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkDeleteVideos(w http.ResponseWriter, r *http.Request) {
	ids := parseVideoIDList(r)
	sid, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	if len(ids) == 0 {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", "select at least one video"), http.StatusSeeOther)
		return
	}
	tid, _, _, err := h.Library.EnqueueBulkDeleteVideos(ids)
	if err != nil {
		http.Redirect(w, r, videoBulkRedirect(r, sid, "", "", err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, videoBulkRedirect(r, sid, "bulk_delete_queued", "task="+strconv.FormatInt(tid, 10), ""), http.StatusSeeOther)
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

func (h *Handler) videosIDsJSON(w http.ResponseWriter, r *http.Request) {
	filter := parseVideoListFilter(r, nil, true)
	ids, err := h.Library.ListVideoIDsFiltered(0, filter)
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
