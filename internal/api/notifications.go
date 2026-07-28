package api

import (
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request, params gen.ListNotificationsParams) {
	f := notify.ListFilter{}
	if params.Event != nil {
		f.Event = string(*params.Event)
	}
	if params.Level != nil {
		f.Level = string(*params.Level)
	}
	if params.UnreadOnly != nil {
		f.UnreadOnly = *params.UnreadOnly
	}
	limit, offset := 100, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	items, err := notify.ListNotifications(s.Queue.DB, f, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list notifications failed", err.Error())
		return
	}
	out := make([]gen.Notification, 0, len(items))
	for _, n := range items {
		out = append(out, mapNotification(n))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetNotification(w http.ResponseWriter, r *http.Request, id gen.TaskId) {
	n, err := notify.GetNotification(s.Queue.DB, int64(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, apperrors.CodeInternal, "notification not found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapNotification(n))
}

func (s *Server) GetNotificationUnreadCount(w http.ResponseWriter, r *http.Request) {
	n, err := notify.CountUnread(s.Queue.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "count unread failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gen.NotificationUnreadCount{Count: n})
}

func (s *Server) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id gen.TaskId) {
	if _, err := notify.GetNotification(s.Queue.DB, int64(id)); err != nil {
		writeErr(w, http.StatusNotFound, apperrors.CodeInternal, "notification not found", err.Error())
		return
	}
	if err := notify.MarkRead(s.Queue.DB, int64(id)); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "mark read failed", err.Error())
		return
	}
	n, err := notify.GetNotification(s.Queue.DB, int64(id))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "reload notification failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapNotification(n))
}

func (s *Server) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if _, err := notify.MarkAllRead(s.Queue.DB); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "mark all read failed", err.Error())
		return
	}
	uc, err := notify.CountUnread(s.Queue.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "count unread failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gen.NotificationUnreadCount{Count: uc})
}

func mapNotification(n notify.Notification) gen.Notification {
	out := gen.Notification{
		Id:         n.ID,
		CreatedAt:  parseTime(n.CreatedAt),
		Event:      gen.NotificationEvent(n.Event),
		Level:      gen.NotificationLevel(notify.EventLevel(n.Event)),
		Title:      n.Title,
		Body:       n.Body,
		ExternalOk: n.ExternalOK,
		Unread:     n.Unread(),
	}
	if n.TaskID.Valid {
		tid := n.TaskID.Int64
		out.TaskId = &tid
	}
	if n.ReadAt.Valid && n.ReadAt.String != "" {
		t := parseTime(n.ReadAt.String)
		out.ReadAt = &t
	}
	return out
}
