package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func parseSourceIDList(r *http.Request) []int64 {
	_ = r.ParseForm()
	raw := r.Form["source_id"]
	if len(raw) == 0 {
		raw = r.Form["source_ids"]
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

func sourcesBulkRedirect(r *http.Request, okKey string, n, skipped int, errMsg string) string {
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil && u != nil {
				redir = u.RequestURI()
			}
		}
	}
	if redir == "" {
		redir = "/browser?type=sources"
	}
	u, err := url.Parse(redir)
	if err != nil || u == nil {
		u, _ = url.Parse("/browser?type=sources")
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
		return "/browser?type=sources"
	}
	return out
}

func (h *Handler) sourcesDeleteImpactJSON(w http.ResponseWriter, r *http.Request) {
	ids := parseSourceIDList(r)
	indexed, downloaded, err := h.Library.CountVideosForSources(ids)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"indexed":    indexed,
		"downloaded": downloaded,
	})
}

func (h *Handler) sourcesIDsJSON(w http.ResponseWriter, r *http.Request) {
	filter := parseSourceListFilter(r)
	if !sourcesSeriesDetail(r) {
		r = mergeSourcesListPrefs(r)
		filter = parseSourceListFilter(r)
	} else {
		sid := filter.SeriesID
		filter = library.SourceListFilter{
			SeriesID: sid,
			Sort:     library.SortSourceLabel,
			SortDir:  library.SortDirAsc,
		}
	}
	ids, err := h.Library.ListSourceIDsFiltered(filter)
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

func (h *Handler) actionBulkScanSources(w http.ResponseWriter, r *http.Request) {
	ids := parseSourceIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, sourcesBulkRedirect(r, "", 0, 0, "select at least one source"), http.StatusSeeOther)
		return
	}
	queued, skipped := 0, 0
	for _, id := range ids {
		if _, err := h.Library.EnqueueScanSource(id, queue.OriginManual); err != nil {
			skipped++
			continue
		}
		queued++
	}
	if queued == 0 && skipped > 0 {
		http.Redirect(w, r, sourcesBulkRedirect(r, "", 0, skipped, "no eligible sources for scan"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, sourcesBulkRedirect(r, "bulk_scan_sources", queued, skipped, ""), http.StatusSeeOther)
}

func (h *Handler) actionBulkDeleteSources(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("confirm_delete") != "1" {
		http.Redirect(w, r, sourcesBulkRedirect(r, "", 0, 0, "confirm delete to remove sources"), http.StatusSeeOther)
		return
	}
	ids := parseSourceIDList(r)
	if len(ids) == 0 {
		http.Redirect(w, r, sourcesBulkRedirect(r, "", 0, 0, "select at least one source"), http.StatusSeeOther)
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		src, err := h.Library.GetSourceByID(id)
		if err != nil {
			skipped++
			continue
		}
		if err := h.Library.DeleteSource(src.SeriesID, id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	if deleted == 0 && skipped > 0 {
		http.Redirect(w, r, sourcesBulkRedirect(r, "", 0, skipped, "no sources deleted"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, sourcesBulkRedirect(r, "bulk_sources_deleted", deleted, skipped, ""), http.StatusSeeOther)
}
