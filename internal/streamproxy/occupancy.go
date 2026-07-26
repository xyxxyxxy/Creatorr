package streamproxy

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

const occupancyIdleTTL = 2 * time.Minute

type streamOccupancy struct {
	taskID    int64
	videoID   int64
	seriesID  int64
	domain    string
	token     string
	lastUse   time.Time
	hlsCancel context.CancelFunc
}

var (
	occMu   sync.Mutex
	occByKey = map[string]*streamOccupancy{}
)

func occupancyKey(videoID int64, token string) string {
	return hlsSessionKey(videoID, token)
}

// occupancyLastUseFresh reports whether stream_play occupancy for key was touched
// within ttl (Emby may hit durable cache URIs without touching the HLS session).
func occupancyLastUseFresh(key string, ttl time.Duration) bool {
	occMu.Lock()
	defer occMu.Unlock()
	o, ok := occByKey[key]
	if !ok {
		return false
	}
	return time.Since(o.lastUse) < ttl
}

// touchOccupancy ensures a running stream_play task for this video+token.
// Soft pause is ignored. Returns task ID (0 if queue unavailable).
func (h *Handler) touchOccupancy(videoID, seriesID int64, domain, token string) int64 {
	if h == nil || h.Queue == nil || videoID <= 0 || token == "" {
		return 0
	}
	if domain == "" {
		domain = "unknown"
	}
	key := occupancyKey(videoID, token)
	occMu.Lock()
	if o, ok := occByKey[key]; ok {
		o.lastUse = time.Now()
		if seriesID > 0 {
			o.seriesID = seriesID
		}
		if domain != "" && domain != "unknown" {
			o.domain = domain
		}
		tid := o.taskID
		occMu.Unlock()
		return tid
	}
	occMu.Unlock()

	tid, err := h.Queue.InsertRunning(queue.EnqueueParams{
		Kind:     queue.KindStreamPlay,
		Domain:   domain,
		SeriesID: seriesID,
		VideoID:  videoID,
		Message:  "Streaming playback",
		Payload:  map[string]any{"video_id": videoID},
	})
	if err != nil {
		slog.Warn("stream proxy", "msg", "stream_play InsertRunning failed", "err", err, "video_id", videoID)
		return 0
	}

	o := &streamOccupancy{
		taskID: tid, videoID: videoID, seriesID: seriesID, domain: domain,
		token: token, lastUse: time.Now(),
	}
	occMu.Lock()
	if existing, ok := occByKey[key]; ok {
		occMu.Unlock()
		// Race: another request won; finish our duplicate bookkeeping row.
		_ = h.Queue.Finish(tid, queue.StatusCancelled, "Superseded by concurrent play", "Cancelled", "")
		existing.lastUse = time.Now()
		return existing.taskID
	}
	occByKey[key] = o
	occMu.Unlock()

	h.Queue.RegisterRunning(tid, func() {
		h.onOccupancyCancel(key, tid)
	})
	if h.Events != nil {
		h.Events.TaskUpdated(tid, queue.KindStreamPlay, domain, queue.StatusRunning, "Streaming playback", seriesID, videoID, nil)
	}
	go reapOccupancy(h, key, o)
	return tid
}

func (h *Handler) onOccupancyCancel(key string, taskID int64) {
	occMu.Lock()
	o, ok := occByKey[key]
	if !ok || o.taskID != taskID {
		occMu.Unlock()
		return
	}
	delete(occByKey, key)
	cancel := o.hlsCancel
	occMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if h.Queue != nil {
		h.Queue.UnregisterRunning(taskID)
	}
}

// bindHLSCancel attaches the live mux cancel to occupancy so UI Cancel stops playback.
func (h *Handler) bindHLSCancel(videoID int64, token string, cancel context.CancelFunc) {
	if cancel == nil {
		return
	}
	key := occupancyKey(videoID, token)
	occMu.Lock()
	defer occMu.Unlock()
	if o, ok := occByKey[key]; ok {
		o.hlsCancel = cancel
	}
}

// clearHLSCancel drops the mux cancel when the session ends without ending occupancy
// (e.g. seek replace before new session binds).
func (h *Handler) clearHLSCancel(videoID int64, token string) {
	key := occupancyKey(videoID, token)
	occMu.Lock()
	defer occMu.Unlock()
	if o, ok := occByKey[key]; ok {
		o.hlsCancel = nil
	}
}

func (h *Handler) occupancyTaskID(videoID int64, token string) int64 {
	key := occupancyKey(videoID, token)
	occMu.Lock()
	defer occMu.Unlock()
	if o, ok := occByKey[key]; ok {
		return o.taskID
	}
	return 0
}

// occupancyForVideo returns one active stream_play occupancy for videoID (any token).
func (h *Handler) occupancyForVideo(videoID int64) *streamOccupancy {
	if videoID <= 0 {
		return nil
	}
	occMu.Lock()
	defer occMu.Unlock()
	for _, o := range occByKey {
		if o != nil && o.videoID == videoID {
			// Copy so caller can use fields without holding occMu.
			cp := *o
			return &cp
		}
	}
	return nil
}

var (
	streamCacheProgressMu   sync.Mutex
	streamCacheProgressLast = map[int64]streamCacheProgressStamp{}
)

type streamCacheProgressStamp struct {
	at  time.Time
	pct int
}

const streamCacheProgressMinInterval = 1500 * time.Millisecond

// publishPlaybackCacheProgress updates stream_play task progress from progressive cache
// and emits task.updated (throttled) so video detail Stream cache can live-refresh.
func (h *Handler) publishPlaybackCacheProgress(videoID int64) {
	if h == nil || h.Library == nil || videoID <= 0 {
		return
	}
	o := h.occupancyForVideo(videoID)
	if o == nil || o.taskID <= 0 {
		return
	}
	m, ok := h.Library.LoadPlaybackMeta(videoID)
	if !ok {
		return
	}
	dur := 0.0
	if sec := h.durationSeconds(videoID); sec > 0 {
		dur = float64(sec)
	}
	pctInt := library.StreamCachePercent(m.CachedSeconds, dur, m.Complete)
	streamCacheProgressMu.Lock()
	prev, seen := streamCacheProgressLast[videoID]
	now := time.Now()
	// Emit when % changes; throttle identical % (playlist re-fetch spam).
	if seen && pctInt == prev.pct && (pctInt >= 100 || now.Sub(prev.at) < streamCacheProgressMinInterval) {
		streamCacheProgressMu.Unlock()
		return
	}
	streamCacheProgressLast[videoID] = streamCacheProgressStamp{at: now, pct: pctInt}
	streamCacheProgressMu.Unlock()

	msg := "Streaming playback"
	if pctInt >= 0 {
		msg = "Caching stream " + strconv.Itoa(pctInt) + "%"
	} else if m.CachedSeconds > 0 {
		msg = "Caching stream"
	}
	var prog *float64
	if pctInt >= 0 {
		p := float64(pctInt) / 100
		prog = &p
	}
	if h.Queue != nil {
		_ = h.Queue.UpdateProgress(o.taskID, msg, prog)
	}
	if h.Events != nil {
		h.Events.TaskUpdated(o.taskID, queue.KindStreamPlay, o.domain, queue.StatusRunning, msg, o.seriesID, videoID, prog)
	}
}

func (h *Handler) withOccupancyTrace(ctx context.Context, taskID int64) context.Context {
	if h == nil || h.Queue == nil || taskID <= 0 {
		return ctx
	}
	return exectrace.With(ctx, func(line string) {
		_ = h.Queue.AppendCommand(taskID, line)
		h.Queue.Logs.Append(taskID, "$ "+line)
	})
}

// finishOccupancyDone ends occupancy as done (idle TTL).
func (h *Handler) finishOccupancyDone(key string, o *streamOccupancy) {
	if h == nil || o == nil {
		return
	}
	occMu.Lock()
	cur, ok := occByKey[key]
	if !ok || cur != o {
		occMu.Unlock()
		return
	}
	delete(occByKey, key)
	cancel := o.hlsCancel
	occMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if h.Queue != nil {
		h.Queue.UnregisterRunning(o.taskID)
		_ = h.Queue.Finish(o.taskID, queue.StatusDone, "Stream session ended", "", "")
	}
	if h.Events != nil {
		h.Events.TaskDone(o.taskID, queue.KindStreamPlay, o.domain, "Stream session ended", o.seriesID, o.videoID)
	}
	streamCacheProgressMu.Lock()
	delete(streamCacheProgressLast, o.videoID)
	streamCacheProgressMu.Unlock()
	releaseFlareIfIdle(h.Queue, o.domain)
}

// failOccupancy marks the stream_play task failed and drops occupancy.
func (h *Handler) failOccupancy(videoID int64, token, code, message, detail string) {
	key := occupancyKey(videoID, token)
	occMu.Lock()
	o, ok := occByKey[key]
	if !ok {
		occMu.Unlock()
		return
	}
	delete(occByKey, key)
	cancel := o.hlsCancel
	tid, domain, seriesID := o.taskID, o.domain, o.seriesID
	occMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if h.Queue != nil {
		h.Queue.UnregisterRunning(tid)
		_ = h.Queue.Finish(tid, queue.StatusFailed, message, code, detail)
		if detail != "" {
			_ = h.Queue.SetDetail(tid, detail)
		}
	}
	if h.Events != nil {
		h.Events.TaskFailed(tid, queue.KindStreamPlay, domain, message, code, seriesID, videoID)
	}
	streamCacheProgressMu.Lock()
	delete(streamCacheProgressLast, videoID)
	streamCacheProgressMu.Unlock()
	releaseFlareIfIdle(h.Queue, domain)
}

func releaseFlareIfIdle(q *queue.Store, domain string) {
	domain = strings.TrimSpace(domain)
	if q == nil || domain == "" || domain == "unknown" || domain == queue.SystemDomain {
		return
	}
	busy, err := q.HasPendingOrRunningDomain(domain)
	if err != nil || busy {
		return
	}
	ytdlp.ReleaseFlareSession(context.Background(), domain)
}

func reapOccupancy(h *Handler, key string, o *streamOccupancy) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		occMu.Lock()
		cur, ok := occByKey[key]
		if !ok || cur != o {
			occMu.Unlock()
			return
		}
		if time.Since(cur.lastUse) < occupancyIdleTTL {
			occMu.Unlock()
			continue
		}
		occMu.Unlock()
		h.finishOccupancyDone(key, o)
		slog.Info("stream proxy", "msg", "stream_play occupancy expired", "task_id", o.taskID, "video_id", o.videoID)
		return
	}
}
