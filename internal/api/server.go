package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	"github.com/xyxxyxxy/Creatorr/internal/cookies"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/events"
	"github.com/xyxxyxxy/Creatorr/internal/health"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
)

// Server implements the generated OpenAPI ServerInterface.
type Server struct {
	Health  *health.Checker
	Queue   *queue.Store
	Library *library.Store
	Events  *events.Hub
}

var _ gen.ServerInterface = (*Server)(nil)

func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	rep := s.Health.Run(r.Context())
	checks := make([]gen.HealthCheck, 0, len(rep.Checks))
	for _, c := range rep.Checks {
		ch := gen.HealthCheck{
			Name:   c.Name,
			Status: gen.HealthStatus(c.Status),
		}
		if c.Message != "" {
			msg := c.Message
			ch.Message = &msg
		}
		checks = append(checks, ch)
	}
	writeJSON(w, http.StatusOK, gen.HealthResponse{
		Status: gen.HealthStatus(rep.Status),
		Checks: checks,
	})
}

func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.Queue.ListActive()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list tasks failed", err.Error())
		return
	}
	out := make([]gen.Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, mapTask(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) EnqueueTask(w http.ResponseWriter, r *http.Request) {
	var body gen.EnqueueTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := queue.EnqueueParams{
		Kind:   body.Kind,
		Domain: body.Domain,
	}
	if body.SeriesId != nil {
		p.SeriesID = *body.SeriesId
	}
	if body.VideoId != nil {
		p.VideoID = *body.VideoId
	}
	if body.Message != nil {
		p.Message = *body.Message
	}
	if body.Priority != nil {
		p.Priority = *body.Priority
	}
	id, err := s.Queue.Enqueue(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "enqueue failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: id})
}

func (s *Server) CancelAllTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.Queue.CancelAll()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "cancel-all failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"cancelled": int64(len(tasks))})
}

func (s *Server) CancelTask(w http.ResponseWriter, r *http.Request, id gen.TaskId) {
	_, err := s.Queue.CancelWithMessage(int64(id), "Cancelled")
	if err != nil {
		writeErr(w, http.StatusNotFound, apperrors.CodeNotFound, "task not cancellable", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) SetDomainActive(w http.ResponseWriter, r *http.Request, domain gen.Domain) {
	var body gen.SetActiveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	host := string(domain)
	if err := domains.SetActive(s.Queue.DB, host, body.Active); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "set domain active failed", err.Error())
		return
	}
	if !body.Active {
		_, _ = s.Queue.CancelDomain(host, "Domain deactivated")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) SetDomainPaused(w http.ResponseWriter, r *http.Request, domain gen.Domain) {
	var body gen.SetPausedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	host := string(domain)
	if err := domains.SetPaused(s.Queue.DB, host, body.Paused); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "set domain paused failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListPausedDomains(w http.ResponseWriter, r *http.Request) {
	items, err := domains.ListPaused(s.Queue.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list paused domains failed", err.Error())
		return
	}
	if items == nil {
		items = []string{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) ListHistory(w http.ResponseWriter, r *http.Request, params gen.ListHistoryParams) {
	var statuses []string
	if params.Status != nil {
		statuses = []string{string(*params.Status)}
	}
	limit, offset := 100, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	items, err := s.Queue.ListHistory(queue.HistoryFilter{Statuses: statuses}, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list history failed", err.Error())
		return
	}
	out := make([]gen.HistoryItem, 0, len(items))
	for _, t := range items {
		out = append(out, mapHistoryTask(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ListSettings(w http.ResponseWriter, r *http.Request) {
	entries, err := settings.All(s.Queue.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list settings failed", err.Error())
		return
	}
	out := make([]gen.SettingEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, gen.SettingEntry{Key: e.Key, Label: e.Label, Value: e.Value, Help: e.Help})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body gen.UpdateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	if err := settings.SetMany(s.Queue.DB, body.Values); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "update settings failed", err.Error())
		return
	}
	s.ListSettings(w, r)
}

func (s *Server) ListCookies(w http.ResponseWriter, r *http.Request) {
	list, err := cookies.List(s.Queue.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list cookies failed", err.Error())
		return
	}
	out := make([]gen.Cookie, 0, len(list))
	for _, c := range list {
		out = append(out, gen.Cookie{
			Domain:    c.Domain,
			Content:   c.Content,
			UpdatedAt: parseTime(c.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) UpsertCookie(w http.ResponseWriter, r *http.Request) {
	var body gen.UpsertCookieRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	if err := cookies.Upsert(s.Queue.DB, body.Domain, body.Content); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "save cookie failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) DeleteCookie(w http.ResponseWriter, r *http.Request, domain gen.Domain) {
	if err := cookies.Delete(s.Queue.DB, string(domain)); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "delete cookie failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mapTask(t queue.Task) gen.Task {
	gt := gen.Task{
		Id:        t.ID,
		Kind:      t.Kind,
		Status:    gen.TaskStatus(t.Status),
		Domain:    t.Domain,
		CreatedAt: parseTime(t.CreatedAt),
	}
	if t.Message != "" {
		m := t.Message
		gt.Message = &m
	}
	if t.ErrorCode != "" {
		c := t.ErrorCode
		gt.ErrorCode = &c
	}
	if t.ErrorMessage != "" {
		m := t.ErrorMessage
		gt.ErrorMessage = &m
	}
	if t.QueuePos > 0 {
		p := t.QueuePos
		gt.QueuePosition = &p
	}
	if t.SeriesID.Valid {
		v := t.SeriesID.Int64
		gt.SeriesId = &v
	}
	if t.VideoID.Valid {
		v := t.VideoID.Int64
		gt.VideoId = &v
	}
	if t.Progress.Valid {
		v := t.Progress.Float64
		gt.Progress = &v
	}
	if t.StartedAt.Valid {
		ts := parseTime(t.StartedAt.String)
		gt.StartedAt = &ts
	}
	if t.FinishedAt.Valid {
		ts := parseTime(t.FinishedAt.String)
		gt.FinishedAt = &ts
	}
	return gt
}

func mapHistoryTask(t queue.Task) gen.HistoryItem {
	item := gen.HistoryItem{
		Id:        t.ID,
		CreatedAt: parseTime(t.CreatedAt),
		Kind:      t.Kind,
		Status:    gen.HistoryItemStatus(t.Status),
		Message:   t.Message,
	}
	if t.FinishedAt.Valid && t.FinishedAt.String != "" {
		ts := parseTime(t.FinishedAt.String)
		item.FinishedAt = &ts
	}
	if t.ErrorCode != "" {
		c := t.ErrorCode
		item.Code = &c
	}
	if t.Detail != "" {
		d := t.Detail
		item.Detail = &d
	}
	if t.Domain != "" {
		d := t.Domain
		item.Domain = &d
	}
	if t.SeriesID.Valid {
		v := t.SeriesID.Int64
		item.SeriesId = &v
	}
	if t.VideoID.Valid {
		v := t.VideoID.Int64
		item.VideoId = &v
	}
	return item
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message, detail string) {
	er := gen.ErrorResponse{Code: code, Message: message}
	if detail != "" {
		er.Detail = &detail
	}
	writeJSON(w, status, er)
}
