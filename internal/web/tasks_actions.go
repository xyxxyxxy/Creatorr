package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

func lanePageParam(domain string) string {
	var b strings.Builder
	b.WriteString("p_")
	for _, r := range strings.ToLower(domain) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "p_" {
		return "p_unknown"
	}
	return s
}

func (h *Handler) actionCancelTask(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	_, _ = h.Queue.CancelWithReason(id, queue.CancelReasonManual)
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" || !strings.HasPrefix(redir, "/") || strings.HasPrefix(redir, "//") {
		redir = "/tasks"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionCancelDomainTasks(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	if domain != "" {
		_, _ = h.Queue.CancelPendingDomain(domain)
	}
	http.Redirect(w, r, "/tasks", http.StatusSeeOther)
}

func (h *Handler) actionSetDomainPaused(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := formDomain(r)
	paused := r.FormValue("paused") == "1"
	if err := domains.SetPaused(h.Queue.DB, domain, paused); err != nil {
		redir := r.FormValue("redirect")
		if redir == "" {
			redir = "/tasks"
		}
		http.Redirect(w, r, redir+"?err="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	redir := r.FormValue("redirect")
	if redir == "" {
		redir = "/tasks"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
