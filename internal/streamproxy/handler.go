package streamproxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xyxxyxxy/Creatorr/internal/cookies"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/events"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// Handler proxies stream media for .strm playback (progressive, pipe, or HLS).
type Handler struct {
	Library *library.Store
	YtDlp   *ytdlp.Client
	Queue   *queue.Store
	Events  *events.Hub
	TmpRoot string
}

const urlsCacheTTL = 45 * time.Second

type urlsCacheEntry struct {
	urls ytdlp.UrlsResult
	at   time.Time
}

var (
	urlsCacheMu sync.Mutex
	urlsCache   = map[string]urlsCacheEntry{} // videoID|format
)

// Mount registers stream routes (outside OpenAPI / request timeout).
func (h *Handler) Mount(r chi.Router) {
	// HEAD: Emby probes .strm targets; 405 → "No compatible streams".
	for _, p := range []string{
		"/stream/videos/{id}",
		"/stream/videos/{id}/master.m3u8",
	} {
		r.Method(http.MethodGet, p, http.HandlerFunc(h.serveVideo))
		r.Method(http.MethodHead, p, http.HandlerFunc(h.serveVideo))
	}
	r.Method(http.MethodGet, "/stream/videos/{id}/hls", http.HandlerFunc(h.serveHLSAsset))
	r.Method(http.MethodHead, "/stream/videos/{id}/hls", http.HandlerFunc(h.serveHLSAsset))
	// Path form keeps segment URIs free of '&' (Emby/ffmpeg HLS parsers choke on query ampersands).
	local := "/stream/videos/{id}/hls/local/{sid}/{file}"
	r.Method(http.MethodGet, local, http.HandlerFunc(h.serveHLSLocal))
	r.Method(http.MethodHead, local, http.HandlerFunc(h.serveHLSLocal))
	begin := "/stream/videos/{id}/beginning/{file}"
	r.Method(http.MethodGet, begin, http.HandlerFunc(h.serveBeginningFile))
	r.Method(http.MethodHead, begin, http.HandlerFunc(h.serveBeginningFile))
	playback := "/stream/videos/{id}/playback/{file}"
	r.Method(http.MethodGet, playback, http.HandlerFunc(h.servePlaybackFile))
	r.Method(http.MethodHead, playback, http.HandlerFunc(h.servePlaybackFile))
}

type playCtx struct {
	videoID             int64
	seriesID            int64
	domain              string
	pageURL             string
	format              string
	jar                 string
	flare               string
	token               string
	taskID              int64
	streamPlayRateLimit string // yt-dlp --limit-rate for mux/pipe; never sleep
}

func (h *Handler) serveVideo(w http.ResponseWriter, r *http.Request) {
	// HEAD: Emby probes .strm - skip yt-dlp urls (often seconds). Content-Type is enough.
	if r.Method == http.MethodHead {
		h.serveVideoHead(w, r)
		return
	}
	// Warm pipe session: master can be served without a new urls resolve.
	if h.tryServeWarmPipeMaster(w, r) {
		return
	}
	pc, urls, cleanup, ok := h.resolvePlay(w, r)
	if !ok {
		return
	}
	defer cleanup()

	if urls.DurationSeconds <= 0 {
		if sec := h.durationSeconds(pc.videoID); sec > 0 {
			urls.DurationSeconds = float64(sec)
		}
	}
	if urls.DurationSeconds > 0 {
		urls.DurationSeconds = h.playDuration(pc.videoID, urls.DurationSeconds)
		_ = h.Library.EnsureStreamNFODuration(pc.videoID, int(urls.DurationSeconds+0.5))
	}

	// Emby cancels native CDN HLS on .strm. Pipe mux (with beginning / progressive handoff when cached).
	if h.tryServeCompletePlayback(w, r, pc.videoID, pc.token) {
		return
	}
	h.servePipeOrHLS(w, r, pc, urls)
}

// tryServeWarmPipeMaster returns true if an existing HLS session answered the master request.
func (h *Handler) tryServeWarmPipeMaster(w http.ResponseWriter, r *http.Request) bool {
	if h.Library == nil {
		return false
	}
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		return false
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		return false
	}
	sess := peekHLSSession(vid, token)
	if sess == nil {
		return false
	}
	wantStart := 0.0
	if _, handoff, _, ok := h.Library.DurableStreamPrefix(vid); ok {
		wantStart = handoff
	}
	if !hlsStartCompatible(sess.startSec, wantStart) {
		return false
	}
	// Beginning / progressive handoff: media playlist is cache-first; live segs may still be warming.
	if wantStart > 0 {
		if r.Method != http.MethodHead {
			h.touchOccupancyForVideo(vid, token)
		}
		h.writePipeMaster(w, vid, sess.id, token)
		return true
	}
	if err := waitHLSPlaylist(sess.dir, 3*time.Second, sess.done); err != nil {
		return false
	}
	if r.Method != http.MethodHead {
		h.touchOccupancyForVideo(vid, token)
	}
	h.writePipeMaster(w, vid, sess.id, token)
	return true
}

func (h *Handler) writePipeMaster(w http.ResponseWriter, videoID int64, sid, token string) {
	mediaURL := h.localMediaURI(videoID, sid, "index.m3u8", token)
	var master strings.Builder
	master.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	master.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=8000000\n")
	master.WriteString(mediaURL)
	master.WriteByte('\n')
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(master.String()))
}

// serveVideoHead validates play access and returns HLS Content-Type without resolving urls.
func (h *Handler) serveVideoHead(w http.ResponseWriter, r *http.Request) {
	if h.Library == nil {
		streamFail(w, http.StatusServiceUnavailable, "stream unavailable", nil)
		return
	}
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	v, err := h.Library.GetVideo(vid)
	if err != nil {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	ser, err := h.Library.GetSeries(v.SeriesID, false)
	if err != nil || !ser.IsStream() {
		streamFail(w, http.StatusBadRequest, "not a stream series", err)
		return
	}
	if v.Status != "streamable" {
		streamFail(w, http.StatusConflict, "video not streamable", nil)
		return
	}
	// .strm always points at master.m3u8 - Emby expects HLS on HEAD probe.
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	if sec := h.durationSeconds(vid); sec > 0 {
		_ = h.Library.EnsureStreamNFODuration(vid, sec)
	}
}

func (h *Handler) durationSeconds(videoID int64) int {
	if h.Library == nil {
		return 0
	}
	v, err := h.Library.GetVideo(videoID)
	if err != nil || v == nil {
		return 0
	}
	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		return int(v.DurationSeconds.Int64)
	}
	return 0
}

// playDuration returns playback length after SponsorBlock skips/cards when a plan sidecar exists.
// Prefer plan.SourceDuration so callers can pass yt-dlp source, DB play secs, or already-converted
// play length without double-applying cuts (pad/ENDLIST would shrink by ~skip each time).
func (h *Handler) playDuration(videoID int64, sourceDur float64) float64 {
	if h.Library == nil {
		return sourceDur
	}
	path, ok, err := h.Library.HasPackAnchor(videoID)
	if err != nil || !ok {
		return sourceDur
	}
	plan, found, err := sponsorblock.ReadPlan(path)
	if err != nil || !found || !plan.HasCuts() {
		return sourceDur
	}
	src := plan.SourceDuration
	if src <= 0 {
		src = sourceDur
	}
	if src <= 0 {
		return sourceDur
	}
	out := sponsorblock.PlaybackDuration(src, plan)
	if out > 0 {
		return out
	}
	return sourceDur
}

// loadStreamPlan loads the applied-cut plan beside strm/video when present.
func (h *Handler) loadStreamPlan(videoID int64) (sponsorblock.AppliedCutPlan, bool) {
	if h.Library == nil {
		return sponsorblock.AppliedCutPlan{}, false
	}
	path, ok, err := h.Library.HasPackAnchor(videoID)
	if err != nil || !ok {
		return sponsorblock.AppliedCutPlan{}, false
	}
	plan, found, err := sponsorblock.ReadPlan(path)
	if err != nil || !found {
		return sponsorblock.AppliedCutPlan{}, false
	}
	return plan, plan.HasCuts()
}

func urlsCacheKey(videoID int64, format string) string {
	return strconv.FormatInt(videoID, 10) + "|" + format
}

func getCachedUrls(videoID int64, format string) (ytdlp.UrlsResult, bool) {
	key := urlsCacheKey(videoID, format)
	urlsCacheMu.Lock()
	defer urlsCacheMu.Unlock()
	ent, ok := urlsCache[key]
	if !ok || time.Since(ent.at) >= urlsCacheTTL {
		return ytdlp.UrlsResult{}, false
	}
	return ent.urls, true
}

func putCachedUrls(videoID int64, format string, urls ytdlp.UrlsResult) {
	urlsCacheMu.Lock()
	urlsCache[urlsCacheKey(videoID, format)] = urlsCacheEntry{urls: urls, at: time.Now()}
	urlsCacheMu.Unlock()
}

func invalidateCachedUrls(videoID int64, format string) {
	urlsCacheMu.Lock()
	delete(urlsCache, urlsCacheKey(videoID, format))
	urlsCacheMu.Unlock()
}

func (h *Handler) resolvePlay(w http.ResponseWriter, r *http.Request) (playCtx, ytdlp.UrlsResult, func(), bool) {
	noop := func() {}
	if h.Library == nil {
		streamFail(w, http.StatusServiceUnavailable, "stream unavailable", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	v, err := h.Library.GetVideo(vid)
	if err != nil {
		streamFail(w, http.StatusNotFound, "not found", err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	ser, err := h.Library.GetSeries(v.SeriesID, false)
	if err != nil || !ser.IsStream() {
		streamFail(w, http.StatusBadRequest, "not a stream series", err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	if v.Status != "streamable" {
		streamFail(w, http.StatusConflict, "video not streamable", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	pageURL := ""
	if v.SourceURL.Valid {
		pageURL = strings.TrimSpace(v.SourceURL.String)
	}
	if pageURL == "" {
		streamFail(w, http.StatusBadRequest, "video has no source_url", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	domain := queue.DomainFromURL(pageURL)
	if ok, _ := domains.IsActive(h.Library.DB, domain); !ok {
		streamFail(w, http.StatusForbidden, "domain inactive", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	if h.YtDlp == nil {
		streamFail(w, http.StatusBadGateway, "yt-dlp not configured", nil)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	format := ""
	if p, err := h.Library.GetProfile(ser.QualityProfileID); err == nil {
		format = p.FormatSelector
	}
	tmpRoot := h.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	work, err := os.MkdirTemp(tmpRoot, "creatorr-stream-*")
	if err != nil {
		streamFail(w, http.StatusInternalServerError, "temp dir", err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	cleanup := func() { _ = os.RemoveAll(work) }

	jar, err := cookies.TempJarForURL(h.Library.DB, work, pageURL)
	if err != nil {
		cleanup()
		streamFail(w, http.StatusBadGateway, "cookies", err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	flare, err := domains.FlareSolverrURL(h.Library.DB, domain)
	if err != nil {
		cleanup()
		streamFail(w, http.StatusBadRequest, err.Error(), err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}

	// Resolve has no pace flags (fast start). Media mux/pipe uses stream_play_rate_limit only -
	// never download_rate_limit or sleep_requests (client waits on play bytes).
	streamRate := ""
	if lim, err := settings.LimitsForDomain(h.Library.DB, domain); err == nil {
		streamRate = lim.StreamPlayRateLimit
	}
	taskID := h.touchOccupancy(vid, v.SeriesID, domain, token)
	pcBase := playCtx{
		videoID: vid, seriesID: v.SeriesID, domain: domain,
		pageURL: pageURL, format: format,
		jar: jar, flare: flare, token: token, taskID: taskID,
		streamPlayRateLimit: streamRate,
	}

	if cached, ok := getCachedUrls(vid, format); ok {
		return pcBase, cached, cleanup, true
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = h.withOccupancyTrace(ctx, taskID)
	ctx = ytdlp.ContextWithPOTTracker(ctx,
		func(detail string) {
			if err := notify.POTProvider(context.WithoutCancel(ctx), h.Library.DB, taskID, domain, detail); err != nil {
				slog.Default().Warn("notify pot_provider", "task", taskID, "err", err)
			}
		},
		func(st ytdlp.POTStatus) {
			if h.Queue == nil || st.State == "" {
				return
			}
			_ = h.Queue.MergeDetailJSON(taskID, map[string]any{ytdlp.DetailKeyPOToken: st})
		},
	)
	urls, err := h.YtDlp.FetchUrls(ctx, ytdlp.UrlsOpts{
		URL:             pageURL,
		FormatSelector:  format,
		CookiesPath:     jar,
		FlareSolverrURL: flare,
	})
	if err != nil {
		cleanup()
		h.handlePlayYtDlpFail(r.Context(), pcBase, err)
		msg := "resolve failed"
		var ae *apperrors.AppError
		if errors.As(err, &ae) && ae != nil {
			msg = ae.Message
		}
		streamFail(w, http.StatusBadGateway, msg, err)
		return playCtx{}, ytdlp.UrlsResult{}, noop, false
	}
	putCachedUrls(vid, format, urls)
	kind := strings.TrimSpace(strings.ToLower(urls.Kind))
	if kind == "" {
		kind = ytdlp.UrlsKindProgressive
	}
	_ = h.Library.SetStreamURLsKind(vid, kind)
	return pcBase, urls, cleanup, true
}

// handlePlayYtDlpFail finishes stream_play failed and soft-pauses like worker tasks.
func (h *Handler) handlePlayYtDlpFail(ctx context.Context, pc playCtx, err error) {
	if err == nil {
		return
	}
	code, msg := classifyPlayError(err)
	detail := err.Error()
	tid := pc.taskID
	if tid == 0 {
		tid = h.occupancyTaskID(pc.videoID, pc.token)
	}
	h.failOccupancy(pc.videoID, pc.token, code, msg, detail)
	if tid <= 0 {
		return
	}
	var database = h.Library.DB
	if h.Library == nil {
		database = nil
	}
	if h.Queue != nil && h.Queue.DB != nil {
		database = h.Queue.DB
	}
	if database != nil {
		notify.SoftPauseAndAlert(ctx, database, slog.Default(), tid, pc.domain, code, detail)
	}
}

func classifyPlayError(err error) (code, message string) {
	var ae *apperrors.AppError
	if errors.As(err, &ae) && ae != nil {
		code = apperrors.UpgradeCode(ae.Code, ae.Error())
		if code == apperrors.CodeCookieInvalid || code == apperrors.CodeRateLimited {
			return code, apperrors.PauseMessage(code)
		}
		return code, ae.Message
	}
	if d := apperrors.DetectPauseCode(err.Error()); d != "" {
		return d, apperrors.PauseMessage(d)
	}
	// Unclassified yt-dlp play failures map to ResolveFailed (same auto-pause family).
	return apperrors.CodeResolveFailed, "Resolve failed"
}

func (h *Handler) touchOccupancyForVideo(videoID int64, token string) {
	if h == nil || h.Library == nil || videoID <= 0 || token == "" {
		return
	}
	v, err := h.Library.GetVideo(videoID)
	if err != nil || v == nil {
		return
	}
	domain := "unknown"
	if v.SourceURL.Valid {
		domain = queue.DomainFromURL(v.SourceURL.String)
	}
	h.touchOccupancy(videoID, v.SeriesID, domain, token)
}

func (h *Handler) serveProgressive(w http.ResponseWriter, r *http.Request, pc playCtx, urls ytdlp.UrlsResult) {
	err := proxyProgressive(w, r, urls.URL, urls.Headers)
	if err == errUpstreamForbidden {
		invalidateCachedUrls(pc.videoID, pc.format)
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		ctx = ytdlp.ContextWithPOTTracker(ctx,
			func(detail string) {
				if err := notify.POTProvider(context.WithoutCancel(ctx), h.Library.DB, pc.taskID, pc.domain, detail); err != nil {
					slog.Default().Warn("notify pot_provider", "task", pc.taskID, "err", err)
				}
			},
			func(st ytdlp.POTStatus) {
				if h.Queue == nil || st.State == "" {
					return
				}
				_ = h.Queue.MergeDetailJSON(pc.taskID, map[string]any{ytdlp.DetailKeyPOToken: st})
			},
		)
		urls2, err2 := h.YtDlp.FetchUrls(ctx, ytdlp.UrlsOpts{
			URL: pc.pageURL, FormatSelector: pc.format,
			CookiesPath: pc.jar, FlareSolverrURL: pc.flare,
		})
		if err2 != nil || urls2.Kind != ytdlp.UrlsKindProgressive || strings.TrimSpace(urls2.URL) == "" {
			streamFail(w, http.StatusBadGateway, "upstream forbidden", err2)
			return
		}
		putCachedUrls(pc.videoID, pc.format, urls2)
		if err2 := proxyProgressive(w, r, urls2.URL, urls2.Headers); err2 != nil {
			slog.Warn("stream proxy", "msg", "upstream retry failed", "err", err2)
		}
		return
	}
	if err != nil {
		slog.Warn("stream proxy", "msg", "upstream proxy failed", "err", err)
	}
}

func (h *Handler) servePipeOrHLS(w http.ResponseWriter, r *http.Request, pc playCtx, urls ytdlp.UrlsResult) {
	// .m3u8 clients (Emby) must never receive Matroska bytes - that yields "No compatible streams".
	mustHLS := strings.HasSuffix(r.URL.Path, ".m3u8")
	startSec := 0.0
	hasPrefix := false
	if h.Library != nil {
		if _, handoff, _, ok := h.Library.DurableStreamPrefix(pc.videoID); ok {
			hasPrefix = true
			startSec = handoff
			h.Library.TouchPlaybackCacheAccess(pc.videoID)
		}
		if h.Library.PlaybackCacheEnabled() {
			_, _ = h.Library.EnsurePlaybackCacheSeeded(pc.videoID)
		}
	}
	triedFresh := false
	for {
		sess, err := h.ensureHLSSession(pc, urls, pc.jar, startSec)
		if err != nil {
			slog.Warn("stream proxy", "msg", "hls session start failed", "err", err, "must_hls", mustHLS)
			h.handlePlayYtDlpFail(r.Context(), pc, err)
			if mustHLS {
				streamFail(w, http.StatusGatewayTimeout, "hls unavailable", err)
				return
			}
			h.servePipeMatroska(w, r, pc, urls)
			return
		}
		if hasPrefix {
			// Durable prefix on disk - master immediately; media playlist is cache-first VOD handoff.
			h.writePipeMaster(w, pc.videoID, sess.id, pc.token)
			return
		}
		if err := waitHLSPlaylist(sess.dir, hlsPlaylistWait, sess.done); err != nil {
			slog.Warn("stream proxy", "msg", "hls playlist wait failed", "err", err, "sid", sess.id, "must_hls", mustHLS)
			dropHLSSession(pc.videoID, pc.token, sess)
			hadReuse := urls.VideoURL != "" || urls.AudioURL != ""
			if !triedFresh && hadReuse {
				slog.Warn("stream proxy", "msg", "hls retry without reused CDN urls", "video_id", pc.videoID)
				invalidateCachedUrls(pc.videoID, pc.format)
				urls = clearPipeReuse(urls)
				triedFresh = true
				continue
			}
			if mustHLS {
				streamFail(w, http.StatusGatewayTimeout, "hls playlist timeout", err)
				return
			}
			h.servePipeMatroska(w, r, pc, urls)
			return
		}
		h.writePipeMaster(w, pc.videoID, sess.id, pc.token)
		return
	}
}

func clearPipeReuse(urls ytdlp.UrlsResult) ytdlp.UrlsResult {
	urls.VideoURL = ""
	urls.AudioURL = ""
	urls.VideoHeaders = nil
	urls.AudioHeaders = nil
	return urls
}

func dropHLSSession(videoID int64, token string, sess *hlsSession) {
	key := hlsSessionKey(videoID, token)
	hlsMu.Lock()
	if cur, ok := hlsSessions[key]; ok && cur == sess {
		delete(hlsSessions, key)
	}
	hlsMu.Unlock()
	sess.cancel()
	_ = os.RemoveAll(sess.dir)
}

func (h *Handler) servePipeMatroska(w http.ResponseWriter, r *http.Request, pc playCtx, urls ytdlp.UrlsResult) {
	w.Header().Set("Content-Type", "video/x-matroska")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	opts := ytdlp.StreamOptsFromUrls(ytdlp.StreamOpts{
		URL: pc.pageURL, FormatSelector: pc.format,
		CookiesPath: pc.jar, FlareSolverrURL: pc.flare,
		LimitRate: pc.streamPlayRateLimit,
	}, urls)
	err := h.YtDlp.PipeStream(r.Context(), opts, flushWriter{w: w, f: flusher})
	if err != nil && r.Context().Err() == nil {
		slog.Warn("stream proxy", "msg", "pipe stream failed", "err", err)
	}
}

func (h *Handler) serveHLSLocal(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	sid := chi.URLParam(r, "sid")
	name := chi.URLParam(r, "file")
	sess := findSession(vid, token, sid)
	if sess == nil {
		streamFail(w, http.StatusNotFound, "session not found", nil)
		return
	}
	path, ok := safeSessionFile(sess.dir, name)
	if !ok {
		streamFail(w, http.StatusBadRequest, "bad file", nil)
		return
	}
	if r.Method != http.MethodHead {
		h.touchOccupancyForVideo(vid, token)
	}
	low := strings.ToLower(name)
	ct := "application/octet-stream"
	switch {
	case strings.HasSuffix(low, ".m3u8"):
		liveBase := h.localHLSDir(vid, sid)
		dur := sess.durationSec
		if dur <= 0 && h.Library != nil {
			if sec := h.durationSeconds(vid); sec > 0 {
				dur = float64(sec)
				sess.durationSec = dur
			}
		}
		h.promotePlaybackCache(vid, sess.dir, dur)
		var body []byte
		if h.Library != nil {
			if dir, _, _, ok := h.Library.DurableStreamPrefix(vid); ok {
				prefixBase := h.durablePrefixURL(vid, dir)
				skipLive := 0
				if strings.Contains(filepath.ToSlash(dir), "/playback-cache/") {
					if m, mok := h.Library.LoadPlaybackMeta(vid); mok {
						skipLive = m.LiveSegsCopied
					}
				}
				body = buildHandoffPlaylist(dir, sess.dir, prefixBase, liveBase, token, dur, skipLive)
			}
		}
		if body == nil {
			data, err := os.ReadFile(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			body = rewriteLocalHLSPlaylist(data, liveBase, token, dur)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(body)
		return
	case strings.HasSuffix(low, ".m4s"), strings.HasSuffix(low, ".mp4"):
		// CMAF/fMP4: video/mp4 is widely accepted (Emby/ffmpeg); video/iso.segment is not.
		ct = "video/mp4"
	case strings.HasSuffix(low, ".ts"):
		ct = "video/mp2t"
		if _, err := os.Stat(path); err != nil {
			if err := sess.ensureSessionSegment(name); err != nil {
				slog.Warn("stream proxy", "msg", "hls segment unavailable", "file", name, "err", err)
				http.NotFound(w, r)
				return
			}
		}
		h.promotePlaybackCache(vid, sess.dir, sess.durationSec)
	}
	serveFile(w, r, path, ct)
}

// promotePlaybackCache copies new live segs into progressive cache and emits stream_play progress.
func (h *Handler) promotePlaybackCache(videoID int64, liveDir string, durationSec float64) {
	if h == nil || h.Library == nil || !h.Library.PlaybackCacheEnabled() {
		return
	}
	_ = h.Library.PromoteLiveSegmentsToPlayback(videoID, liveDir, durationSec)
	h.publishPlaybackCacheProgress(videoID)
}

// durablePrefixURL returns beginning/ or playback/ base for durable cache segs.
func (h *Handler) durablePrefixURL(videoID int64, dir string) string {
	if strings.Contains(filepath.ToSlash(dir), "/playback-cache/") {
		return h.absoluteURL("/stream/videos/" + strconv.FormatInt(videoID, 10) + "/playback/")
	}
	return h.beginningDirURL(videoID)
}

// tryServeCompletePlayback serves a static master when progressive cache is complete.
func (h *Handler) tryServeCompletePlayback(w http.ResponseWriter, r *http.Request, videoID int64, token string) bool {
	if h.Library == nil {
		return false
	}
	if sec := h.durationSeconds(videoID); sec > 0 {
		_ = h.Library.FinalizePlaybackCacheIfNearDuration(videoID, float64(sec))
	}
	m, ok := h.Library.LoadPlaybackMeta(videoID)
	if !ok || !m.Complete {
		return false
	}
	h.Library.TouchPlaybackCacheAccess(videoID)
	mediaURL := h.absoluteURL("/stream/videos/"+strconv.FormatInt(videoID, 10)+"/playback/index.m3u8") + "?token=" + token
	var master strings.Builder
	master.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	master.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=8000000\n")
	master.WriteString(mediaURL)
	master.WriteByte('\n')
	body := []byte(master.String())
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return true
	}
	h.touchOccupancyForVideo(videoID, token)
	_, _ = w.Write(body)
	return true
}

// beginningDirURL returns .../beginning/ (absolute when PublicBaseURL is set).
func (h *Handler) beginningDirURL(videoID int64) string {
	path := "/stream/videos/" + strconv.FormatInt(videoID, 10) + "/beginning/"
	return h.absoluteURL(path)
}

func (h *Handler) serveBeginningFile(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	if _, ok := h.Library.LoadBeginningMeta(vid); !ok {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "file")
	path, ok := safeSessionFile(h.Library.BeginningDir(vid), name)
	if !ok {
		streamFail(w, http.StatusBadRequest, "bad file", nil)
		return
	}
	if r.Method != http.MethodHead {
		h.touchOccupancyForVideo(vid, token)
	}
	h.serveCacheMediaFile(w, r, path, name)
}

func (h *Handler) servePlaybackFile(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	if _, ok := h.Library.LoadPlaybackMeta(vid); !ok {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "file")
	path, ok := safeSessionFile(h.Library.PlaybackCacheDir(vid), name)
	if !ok {
		streamFail(w, http.StatusBadRequest, "bad file", nil)
		return
	}
	h.Library.TouchPlaybackCacheAccess(vid)
	if r.Method != http.MethodHead {
		h.touchOccupancyForVideo(vid, token)
	}
	if strings.HasSuffix(strings.ToLower(name), ".m3u8") {
		if sec := h.durationSeconds(vid); sec > 0 {
			_ = h.Library.FinalizePlaybackCacheIfNearDuration(vid, float64(sec))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		base := h.absoluteURL("/stream/videos/" + strconv.FormatInt(vid, 10) + "/playback/")
		body := rewriteLocalHLSPlaylist(data, base, token, 0)
		// Preserve ENDLIST for complete caches (rewrite strips it when duration is 0).
		if m, ok := h.Library.LoadPlaybackMeta(vid); ok && m.Complete && !strings.Contains(string(body), "#EXT-X-ENDLIST") {
			body = append(body, []byte("#EXT-X-ENDLIST\n")...)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(body)
		return
	}
	h.serveCacheMediaFile(w, r, path, name)
}

func (h *Handler) serveCacheMediaFile(w http.ResponseWriter, r *http.Request, path, name string) {
	low := strings.ToLower(name)
	ct := "application/octet-stream"
	switch {
	case strings.HasSuffix(low, ".m3u8"):
		ct = "application/vnd.apple.mpegurl"
	case strings.HasSuffix(low, ".ts"):
		ct = "video/mp2t"
	case strings.HasSuffix(low, ".m4s"), strings.HasSuffix(low, ".mp4"):
		ct = "video/mp4"
	}
	serveFile(w, r, path, ct)
}

// localHLSDir returns .../hls/local/{sid}/ (absolute when PublicBaseURL is set).
func (h *Handler) localHLSDir(videoID int64, sid string) string {
	path := "/stream/videos/" + strconv.FormatInt(videoID, 10) + "/hls/local/" + sid + "/"
	return h.absoluteURL(path)
}

func (h *Handler) localMediaURI(videoID int64, sid, name, token string) string {
	return h.localHLSDir(videoID, sid) + name + "?token=" + token
}

func (h *Handler) absoluteURL(path string) string {
	base := ""
	if h.Library != nil {
		base = strings.TrimRight(strings.TrimSpace(h.Library.EffectivePublicBaseURL()), "/")
	}
	if base == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (h *Handler) servePipe(w http.ResponseWriter, r *http.Request, pc playCtx) {
	h.servePipeMatroska(w, r, pc, ytdlp.UrlsResult{Kind: ytdlp.UrlsKindPipe})
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if f.f != nil {
		f.f.Flush()
	}
	return n, err
}

func (h *Handler) serveHLSMaster(w http.ResponseWriter, r *http.Request, pc playCtx, urls ytdlp.UrlsResult) {
	baseProxy := hlsProxyPrefix(pc.videoID, pc.token)
	err := proxyUpstream(w, r, urls.URL, urls.Headers, true, urls.URL, baseProxy)
	if err == errUpstreamForbidden {
		streamFail(w, http.StatusBadGateway, "upstream forbidden", err)
		return
	}
	if err != nil {
		slog.Warn("stream proxy", "msg", "hls master failed", "err", err)
	}
}

func (h *Handler) serveHLSAsset(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !library.ValidStreamToken(h.Library.DB, token) {
		streamFail(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	vid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || vid <= 0 {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	enc := r.URL.Query().Get("u")
	upstream, err := decodeUpstream(enc)
	if err != nil {
		streamFail(w, http.StatusBadRequest, "bad upstream", err)
		return
	}
	v, err := h.Library.GetVideo(vid)
	if err != nil || v.Status != "streamable" {
		streamFail(w, http.StatusNotFound, "not found", err)
		return
	}
	ser, err := h.Library.GetSeries(v.SeriesID, false)
	if err != nil || !ser.IsStream() {
		streamFail(w, http.StatusBadRequest, "not a stream series", err)
		return
	}
	pageURL := ""
	if v.SourceURL.Valid {
		pageURL = strings.TrimSpace(v.SourceURL.String)
	}
	domain := queue.DomainFromURL(pageURL)
	if pageURL != "" {
		if ok, _ := domains.IsActive(h.Library.DB, domain); !ok {
			streamFail(w, http.StatusForbidden, "domain inactive", nil)
			return
		}
	}
	hdrs := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}
	if pageURL != "" {
		hdrs["Referer"] = pageURL
	}
	baseProxy := hlsProxyPrefix(vid, token)
	low := strings.ToLower(upstream)
	if strings.Contains(low, ".m3u8") || strings.Contains(low, "m3u8") {
		if err := proxyUpstream(w, r, upstream, hdrs, true, upstream, baseProxy); err != nil {
			if err == errUpstreamForbidden {
				streamFail(w, http.StatusBadGateway, "upstream forbidden", err)
				return
			}
			slog.Warn("stream proxy", "msg", "hls playlist proxy failed", "err", err)
		}
		return
	}
	if err := proxyUpstreamStream(w, r, upstream, hdrs); err != nil {
		if err == errUpstreamForbidden {
			streamFail(w, http.StatusBadGateway, "upstream forbidden", err)
			return
		}
		slog.Warn("stream proxy", "msg", "hls segment proxy failed", "err", err)
	}
}

func hlsProxyPrefix(videoID int64, token string) string {
	return "/stream/videos/" + strconv.FormatInt(videoID, 10) + "/hls?token=" + token + "&u="
}

func streamFail(w http.ResponseWriter, code int, msg string, err error) {
	if err != nil {
		slog.Warn("stream proxy", "status", code, "msg", msg, "err", err)
	} else {
		slog.Warn("stream proxy", "status", code, "msg", msg)
	}
	http.Error(w, msg, code)
}

var errUpstreamForbidden = errors.New("upstream forbidden")

func proxyProgressive(w http.ResponseWriter, r *http.Request, mediaURL string, hdrs map[string]string) error {
	return proxyUpstreamStream(w, r, mediaURL, hdrs)
}
