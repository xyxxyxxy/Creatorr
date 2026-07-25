package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Event types for GET /api/events (SSE).
const (
	TypeTaskUpdated           = "task.updated"
	TypeTaskDone              = "task.done"
	TypeTaskFailed            = "task.failed"
	TypeNotificationCreated   = "notification.created"
	TypeNotificationRead      = "notification.read"
)

// Event is one SSE payload.
type Event struct {
	Type           string         `json:"type"`
	At             string         `json:"at"`
	TaskID         int64          `json:"task_id,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Domain         string         `json:"domain,omitempty"`
	SeriesID       int64          `json:"series_id,omitempty"`
	VideoID        int64          `json:"video_id,omitempty"`
	Status         string         `json:"status,omitempty"`
	Message        string         `json:"message,omitempty"`
	Progress       *float64       `json:"progress"` // nil clears bar / shows spinner; omitempty would hide clears
	Code           string         `json:"code,omitempty"`
	Result         string         `json:"result,omitempty"`
	NotificationID int64          `json:"notification_id,omitempty"`
	UnreadCount    *int           `json:"unread_count,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

// Hub is an in-process pub/sub for SSE clients.
type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe returns a buffered channel; caller must Unsubscribe.
func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe closes and removes the subscriber channel.
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Publish sends e to all subscribers (non-blocking; drops if slow).
func (h *Hub) Publish(e Event) {
	if h == nil {
		return
	}
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// JSON encodes the event for SSE data lines.
func (e Event) JSON() []byte {
	b, err := json.Marshal(e)
	if err != nil {
		return []byte(`{"type":"error"}`)
	}
	return b
}

// N returns subscriber count (tests).
func (h *Hub) N() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// TaskUpdated publishes task.updated.
func (h *Hub) TaskUpdated(taskID int64, kind, domain, status, message string, seriesID, videoID int64, progress *float64) {
	h.Publish(Event{
		Type: TypeTaskUpdated, TaskID: taskID, Kind: kind, Domain: domain,
		Status: status, SeriesID: seriesID, VideoID: videoID, Message: message, Progress: progress,
	})
}

// TaskDone publishes task.done.
func (h *Hub) TaskDone(taskID int64, kind, domain, message string, seriesID, videoID int64) {
	h.Publish(Event{
		Type: TypeTaskDone, TaskID: taskID, Kind: kind, Domain: domain,
		Status: "done", SeriesID: seriesID, VideoID: videoID, Message: message,
	})
}

// TaskFailed publishes task.failed (also used for cancelled).
func (h *Hub) TaskFailed(taskID int64, kind, domain, message, code string, seriesID, videoID int64) {
	st := "failed"
	if code == "Cancelled" {
		st = "cancelled"
	}
	h.Publish(Event{
		Type: TypeTaskFailed, TaskID: taskID, Kind: kind, Domain: domain,
		Status: st, SeriesID: seriesID, VideoID: videoID, Message: message, Code: code,
	})
}

// NotificationCreated publishes notification.created.
func (h *Hub) NotificationCreated(notificationID int64, event string, unreadCount int) {
	uc := unreadCount
	h.Publish(Event{
		Type: TypeNotificationCreated, NotificationID: notificationID,
		Kind: event, UnreadCount: &uc,
	})
}

// NotificationRead publishes notification.read (after mark-one or mark-all).
func (h *Hub) NotificationRead(notificationID int64, unreadCount int) {
	uc := unreadCount
	h.Publish(Event{
		Type: TypeNotificationRead, NotificationID: notificationID, UnreadCount: &uc,
	})
}
