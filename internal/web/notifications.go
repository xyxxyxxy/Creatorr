package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

func (h *Handler) notificationDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	n, err := notify.GetNotification(h.Queue.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Opening detail marks alert notifications read (badge / History list).
	if n.Unread() {
		_ = notify.MarkRead(h.Queue.DB, id)
		if refreshed, gerr := notify.GetNotification(h.Queue.DB, id); gerr == nil {
			n = refreshed
		}
	}
	view := notificationToView(n, time.Now().UTC())
	var related []notifyFileSyncIssueSection
	var relatedTask *notifyRelatedTaskView
	messageBody := view.Body
	if view.Event == notify.EventDownloadDigest {
		messageBody = notify.DigestBodyDisplay(view.Body)
		related = digestRelatedSectionsFromBody(view.Body, h.resolveFileSyncNotifyRef, h.resolveDigestNotifyByTitles)
	}
	if view.TaskID > 0 && h.Queue != nil {
		if t, gerr := h.Queue.GetTask(view.TaskID); gerr == nil && t != nil {
			relatedTask = &notifyRelatedTaskView{
				ID:      t.ID,
				Kind:    t.Kind,
				Status:  t.Status,
				Domain:  t.Domain,
				Message: strings.TrimSpace(t.Message),
			}
			if view.Event == notify.EventFileSyncIssues {
				related = fileSyncNotifySectionsFromDetail(t.Detail, h.resolveFileSyncNotifyRef)
			}
		}
	}
	pageTitle := view.Title
	if strings.TrimSpace(pageTitle) == "" {
		pageTitle = view.EventLabel
	}
	render(w, "notification_detail", struct {
		pageBase
		Crumbs          []breadcrumb
		Item            notifyHistoryView
		MessageBody     string
		RelatedTask     *notifyRelatedTaskView
		RelatedSections []notifyFileSyncIssueSection
	}{
		pageBase: newPage(pageTitle, "history", nil),
		Crumbs: []breadcrumb{
			crumb("/history", "History", "history"),
			crumb("", pageTitle, "bell"),
		},
		Item:            view,
		MessageBody:     messageBody,
		RelatedTask:     relatedTask,
		RelatedSections: related,
	})
}

// notifyRelatedTaskView is high-level task chrome for notification Related to.
type notifyRelatedTaskView struct {
	ID      int64
	Kind    string
	Status  string
	Domain  string
	Message string
}

func (h *Handler) actionMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if id > 0 {
		_ = notify.MarkRead(h.Queue.DB, id)
	}
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		redir = "/history#notifications"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *Handler) actionMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	_, _ = notify.MarkAllRead(h.Queue.DB)
	redir := strings.TrimSpace(r.FormValue("redirect"))
	if redir == "" {
		redir = "/history#notifications"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
