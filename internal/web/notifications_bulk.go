package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

func parseNotificationIDList(r *http.Request) []int64 {
	_ = r.ParseForm()
	raw := r.Form["notification_id"]
	if len(raw) == 0 {
		raw = r.Form["id"]
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

func (h *Handler) notificationsIDsJSON(w http.ResponseWriter, r *http.Request) {
	r = mergeNotificationsListPrefs(r)
	filter := parseNotificationsExplorerFilter(r)
	ids, err := notify.ListNotificationIDs(h.Queue.DB, filter, true)
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

func (h *Handler) actionBulkNotificationRead(w http.ResponseWriter, r *http.Request) {
	ids := parseNotificationIDList(r)
	wantRead := strings.TrimSpace(r.FormValue("read")) == "1"
	if len(ids) > 0 {
		if wantRead {
			_, _ = notify.MarkReadMany(h.Queue.DB, ids)
		} else {
			_, _ = notify.MarkUnreadMany(h.Queue.DB, ids)
		}
	}
	if r.Header.Get("HX-Request") != "" {
		data, err := h.loadNotificationsListLive(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		render(w, "notifications_list_live", data)
		return
	}
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		redir = "/browser?type=notifications"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
