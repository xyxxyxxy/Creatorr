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
	http.Redirect(w, r, safeTasksRedirect(r, ""), http.StatusSeeOther)
}

func (h *Handler) actionCancelDomainTasks(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	if domain != "" {
		_, _ = h.Queue.CancelPendingDomain(domain)
	}
	http.Redirect(w, r, safeTasksRedirect(r, ""), http.StatusSeeOther)
}

func (h *Handler) actionSkipDomainCooldown(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := settings.NormalizeDomain(r.FormValue("domain"))
	if domain != "" && domain != queue.SystemDomain {
		_ = h.Queue.ClearCooldown(domain)
	}
	http.Redirect(w, r, safeTasksRedirect(r, "ok=cooldown-skipped"), http.StatusSeeOther)
}

func (h *Handler) actionTaskToFront(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if id > 0 {
		_ = h.Queue.MoveToFront(id)
	}
	http.Redirect(w, r, safeTasksRedirect(r, "ok=task-to-front"), http.StatusSeeOther)
}

func (h *Handler) actionSetDomainPaused(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := formDomain(r)
	paused := r.FormValue("paused") == "1"
	if err := domains.SetPaused(h.Queue.DB, domain, paused); err != nil {
		http.Redirect(w, r, safeTasksRedirect(r, "err="+urlQuery(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, safeTasksRedirect(r, ""), http.StatusSeeOther)
}

// safeTasksRedirect returns form redirect when it is a same-origin /tasks path; else /tasks.
// optionalQuery is appended (e.g. ok=…).
func safeTasksRedirect(r *http.Request, optionalQuery string) string {
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" || !strings.HasPrefix(redir, "/") || strings.HasPrefix(redir, "//") {
		redir = "/tasks"
	} else {
		path := redir
		if i := strings.IndexByte(redir, '?'); i >= 0 {
			path = redir[:i]
		}
		if path != "/tasks" {
			redir = "/tasks"
		}
	}
	if optionalQuery == "" {
		return redir
	}
	return appendQuery(redir, optionalQuery)
}
