package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func parseFileIDList(r *http.Request) []int64 {
	_ = r.ParseForm()
	raw := r.Form["file_id"]
	if len(raw) == 0 {
		raw = r.Form["file_ids"]
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

func filesBulkRedirect(r *http.Request, okKey string, n, skipped int, errMsg string) string {
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil && u != nil {
				redir = u.RequestURI()
			}
		}
	}
	if redir == "" {
		redir = "/browser?type=files"
	}
	u, err := url.Parse(redir)
	if err != nil || u == nil {
		u, _ = url.Parse("/browser?type=files")
	}
	q := u.Query()
	for _, k := range []string{"ok", "detail", "err", "n", "queued", "skipped"} {
		q.Del(k)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	} else if okKey != "" {
		q.Set("ok", okKey)
		if n > 0 {
			q.Set("n", strconv.Itoa(n))
		}
		if skipped > 0 {
			q.Set("skipped", strconv.Itoa(skipped))
		}
	}
	u.RawQuery = q.Encode()
	out := u.RequestURI()
	if out == "" {
		return "/browser?type=files"
	}
	return out
}

func (h *Handler) filesIDsJSON(w http.ResponseWriter, r *http.Request) {
	filter := parseFileListFilter(r)
	ids, err := h.Library.ListFileIDsFiltered(filter)
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

func (h *Handler) actionBulkCheckFileHash(w http.ResponseWriter, r *http.Request) {
	ids := parseFileIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, filesBulkRedirect(r, "", 0, 0, "select at least one file"), http.StatusSeeOther)
		return
	}
	queued, skipped := 0, 0
	for _, fid := range ids {
		f, err := h.Library.GetFile(fid)
		if err != nil {
			skipped++
			continue
		}
		if f.Missing() || strings.TrimSpace(f.Kind) == "nfo" || f.Kind == library.SeriesMetaFileRoleNFO {
			skipped++
			continue
		}
		if f.IsSeriesMeta() {
			if _, err := h.Library.EnqueueSeriesMetaFileHashCheck(fid); err != nil {
				skipped++
				continue
			}
			queued++
			continue
		}
		vid := int64(0)
		if f.VideoID.Valid {
			vid = f.VideoID.Int64
		}
		if vid <= 0 {
			skipped++
			continue
		}
		if err := h.errIfVideoDeleting(vid); err != nil {
			skipped++
			continue
		}
		if _, err := h.Library.EnqueueFileHashCheck(vid, fid); err != nil {
			skipped++
			continue
		}
		queued++
	}
	if queued == 0 && skipped > 0 {
		http.Redirect(w, r, filesBulkRedirect(r, "", 0, skipped, "no eligible files for integrity check"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, filesBulkRedirect(r, "bulk_check_file_hash", queued, skipped, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkDeleteVideoSidecar(w http.ResponseWriter, r *http.Request) {
	ids := parseFileIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, filesBulkRedirect(r, "", 0, 0, "select at least one file"), http.StatusSeeOther)
		return
	}
	deleted, skipped := 0, 0
	for _, fid := range ids {
		f, err := h.Library.GetFile(fid)
		if err != nil {
			skipped++
			continue
		}
		if !library.DeletableSidecarKind(f.Kind) || !f.VideoID.Valid || f.VideoID.Int64 <= 0 {
			skipped++
			continue
		}
		if err := h.Library.DeleteVideoSidecar(f.VideoID.Int64, fid); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	if deleted == 0 && skipped > 0 {
		http.Redirect(w, r, filesBulkRedirect(r, "", 0, skipped, "no deletable sidecars in selection"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, filesBulkRedirect(r, "bulk_sidecar_deleted", deleted, skipped, ""), http.StatusSeeOther)
}
