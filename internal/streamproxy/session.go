package streamproxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

const (
	hlsSessionIdleTTL    = 2 * time.Minute
	hlsPlaylistPollEvery = 200 * time.Millisecond
	hlsSegWaitTimeout    = 15 * time.Second // linear mux only; no seek-restart
)

// hlsPlaylistWait is how long servePipeOrHLS waits for index.m3u8 before giving up.
var hlsPlaylistWait = 45 * time.Second

type hlsSession struct {
	id          string
	videoID     int64
	token       string
	dir         string
	opts        ytdlp.StreamOpts
	startSec    float64 // live handoff offset; 0 = from start
	cancel      context.CancelFunc
	done        <-chan error
	durationSec float64 // known runtime; when >0 playlist is padded VOD+ENDLIST for Emby scrubber
	startedAt   time.Time
	lastUse     time.Time
}

var (
	hlsMu       sync.Mutex
	hlsSessions = map[string]*hlsSession{} // key: videoID|token
)

func hlsSessionKey(videoID int64, token string) string {
	return strconv.FormatInt(videoID, 10) + "|" + token
}

func newSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ensureHLSSession starts or reuses a local fMP4 HLS session for this video+token.
// cookiesSrc is copied into the session dir so resolvePlay cleanup cannot delete it early.
// startSec > 0 seeks the live mux (download-beginning handoff); mismatched sessions are replaced.
func (h *Handler) ensureHLSSession(pc playCtx, urls ytdlp.UrlsResult, cookiesSrc string, startSec float64) (*hlsSession, error) {
	key := hlsSessionKey(pc.videoID, pc.token)
	hlsMu.Lock()
	if s, ok := hlsSessions[key]; ok {
		if approxStartSec(s.startSec, startSec) {
			s.lastUse = time.Now()
			if urls.DurationSeconds > 0 {
				s.durationSec = urls.DurationSeconds
			}
			hlsMu.Unlock()
			_ = h.touchOccupancy(pc.videoID, pc.seriesID, pc.domain, pc.token)
			return s, nil
		}
		delete(hlsSessions, key)
		hlsMu.Unlock()
		h.clearHLSCancel(pc.videoID, pc.token)
		s.cancel()
		_ = os.RemoveAll(s.dir)
	} else {
		hlsMu.Unlock()
	}

	tmpRoot := h.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	sid := newSessionID()
	dir, err := os.MkdirTemp(tmpRoot, "creatorr-hls-"+sid+"-*")
	if err != nil {
		return nil, err
	}
	jar := ""
	if cookiesSrc != "" {
		data, err := os.ReadFile(cookiesSrc)
		if err == nil {
			jar = filepath.Join(dir, "cookies.txt")
			_ = os.WriteFile(jar, data, 0o600)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	tid := pc.taskID
	if tid == 0 {
		tid = h.touchOccupancy(pc.videoID, pc.seriesID, pc.domain, pc.token)
	} else {
		_ = h.touchOccupancy(pc.videoID, pc.seriesID, pc.domain, pc.token)
	}
	ctx = h.withOccupancyTrace(ctx, tid)
	opts := ytdlp.StreamOptsFromUrls(ytdlp.StreamOpts{
		URL: pc.pageURL, FormatSelector: pc.format,
		CookiesPath: jar, FlareSolverrURL: pc.flare,
		HLSDir: dir, HLSStartSec: startSec,
	}, urls)
	if h.YtDlp == nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("yt-dlp client missing")
	}
	done, err := h.YtDlp.StartHLSStream(ctx, opts)
	if err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, err
	}

	// durationSec is already play timeline length when set by serveVideo (playDuration once).
	s := &hlsSession{
		id: sid, videoID: pc.videoID, token: pc.token,
		dir: dir, opts: opts, startSec: startSec,
		cancel: cancel, done: done,
		durationSec: urls.DurationSeconds, startedAt: time.Now(), lastUse: time.Now(),
	}
	hlsMu.Lock()
	if existing, ok := hlsSessions[key]; ok {
		if approxStartSec(existing.startSec, startSec) {
			hlsMu.Unlock()
			cancel()
			_ = os.RemoveAll(dir)
			existing.lastUse = time.Now()
			return existing, nil
		}
		delete(hlsSessions, key)
		hlsMu.Unlock()
		h.clearHLSCancel(pc.videoID, pc.token)
		existing.cancel()
		_ = os.RemoveAll(existing.dir)
		hlsMu.Lock()
	}
	hlsSessions[key] = s
	hlsMu.Unlock()
	h.bindHLSCancel(pc.videoID, pc.token, cancel)
	go reapHLSSession(key, s)
	slog.Info("stream proxy", "msg", "hls session started", "video_id", pc.videoID, "sid", sid, "start_sec", startSec)
	return s, nil
}

func approxStartSec(a, b float64) bool {
	if a == b {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.05
}

func reapHLSSession(key string, s *hlsSession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		hlsMu.Lock()
		cur, ok := hlsSessions[key]
		if !ok || cur != s {
			hlsMu.Unlock()
			return
		}
		if time.Since(cur.lastUse) < hlsSessionIdleTTL {
			hlsMu.Unlock()
			continue
		}
		delete(hlsSessions, key)
		hlsMu.Unlock()
		s.cancel()
		_ = os.RemoveAll(s.dir)
		slog.Info("stream proxy", "msg", "hls session expired", "sid", s.id)
		return
	}
}

func waitHLSPlaylist(dir string, timeout time.Duration, done <-chan error) error {
	deadline := time.Now().Add(timeout)
	index := filepath.Join(dir, "index.m3u8")
	ticker := time.NewTicker(hlsPlaylistPollEvery)
	defer ticker.Stop()
	for {
		if data, err := os.ReadFile(index); err == nil && playlistHasMedia(data) {
			return nil
		}
		select {
		case err := <-done:
			if data, e := os.ReadFile(index); e == nil && playlistHasMedia(data) {
				return nil
			}
			if err != nil {
				return err
			}
			return errHLSPlaylistTimeout
		case <-ticker.C:
			if time.Now().After(deadline) {
				return errHLSPlaylistTimeout
			}
		}
	}
}

func playlistHasMedia(data []byte) bool {
	if strings.Contains(string(data), "EXT-X-MAP") || strings.Contains(string(data), ".m4s") {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return true
	}
	return false
}

var errHLSPlaylistTimeout = os.ErrDeadlineExceeded

// rewriteLocalHLSPlaylist rewrites relative media URIs to Creatorr local session URLs.
// dir is .../hls/local/{sid}/ (optionally absolute). Token is the only query param (no '&').
// Strips EVENT playlist type. Emits VOD + START at 0 before any #EXTINF.
// When durationSec > 0, pads future seg%05d.ts entries and ENDLIST so Emby reports
// full length (HLS .m3u8 ignores NFO). Missing padded segs wait via ensureSessionSegment.
func rewriteLocalHLSPlaylist(body []byte, dir, token string, durationSec float64) []byte {
	q := "?token=" + token
	var out strings.Builder
	wroteVOD := false
	wroteStart := false
	var sum float64
	lastSeg := -1
	targetDur := 4.0
	pendingINF := false
	pendingDur := 0.0
	injectHeaders := func() {
		if !wroteVOD {
			out.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
			wroteVOD = true
		}
		if !wroteStart {
			out.WriteString("#EXT-X-START:TIME-OFFSET=0\n")
			wroteStart = true
		}
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, "URI=\"") {
			injectHeaders()
			const key = `URI="`
			if i := strings.Index(line, key); i >= 0 {
				rest := line[i+len(key):]
				if j := strings.Index(rest, `"`); j >= 0 {
					name := rest[:j]
					if !strings.Contains(name, "://") && !strings.HasPrefix(name, "/") {
						line = line[:i+len(key)] + dir + name + q + rest[j:]
					}
				}
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "#EXT-X-PLAYLIST-TYPE:") ||
				strings.HasPrefix(trimmed, "#EXT-X-START:") ||
				trimmed == "#EXT-X-ENDLIST" {
				continue
			}
			if strings.HasPrefix(trimmed, "#EXT-X-TARGETDURATION:") {
				if n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(trimmed, "#EXT-X-TARGETDURATION:")), 64); err == nil && n > 0 {
					targetDur = n
				}
			}
			if strings.HasPrefix(trimmed, "#EXTINF:") || strings.HasPrefix(trimmed, "#EXT-X-MAP:") {
				injectHeaders()
			}
			if strings.HasPrefix(trimmed, "#EXTINF:") {
				pendingINF = true
				pendingDur = extinfSeconds(strings.TrimPrefix(trimmed, "#EXTINF:"))
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		injectHeaders()
		if pendingINF {
			sum += pendingDur
			pendingINF = false
		}
		base := trimmed
		if i := strings.IndexByte(base, '?'); i >= 0 {
			base = base[:i]
		}
		if n := parseSegIndex(base); n > lastSeg {
			lastSeg = n
		}
		if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "/") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(dir)
		out.WriteString(trimmed)
		out.WriteString(q)
		out.WriteByte('\n')
	}
	injectHeaders()
	if durationSec > 0 {
		segDur := targetDur
		if segDur < 1 {
			segDur = 4
		}
		next := lastSeg + 1
		if next < 0 {
			next = 0
		}
		for sum+0.05 < durationSec {
			remain := durationSec - sum
			d := segDur
			if remain < d {
				d = remain
			}
			out.WriteString("#EXTINF:")
			out.WriteString(strconv.FormatFloat(d, 'f', 6, 64))
			out.WriteString(",\n")
			out.WriteString(dir)
			out.WriteString("seg")
			out.WriteString(padSegIndex(next))
			out.WriteString(".ts")
			out.WriteString(q)
			out.WriteByte('\n')
			sum += d
			next++
		}
		out.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(out.String())
}

func parseSegIndex(name string) int {
	name = filepath.Base(name)
	if !strings.HasPrefix(name, "seg") {
		return -1
	}
	rest := strings.TrimPrefix(name, "seg")
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		rest = rest[:i]
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func padSegIndex(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 5 {
		s = "0" + s
	}
	return s
}

func waitSessionFileOrDone(path string, timeout time.Duration, done <-chan error) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(hlsPlaylistPollEvery)
	defer ticker.Stop()
	for {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return nil
		}
		select {
		case err, ok := <-done:
			if st, e := os.Stat(path); e == nil && st.Size() > 0 {
				return nil
			}
			if !ok {
				return os.ErrNotExist
			}
			if err != nil {
				return err
			}
			return os.ErrNotExist
		case <-ticker.C:
			if time.Now().After(deadline) {
				return os.ErrNotExist
			}
		}
	}
}

// ensureSessionSegment waits briefly for linear mux to write the segment; no seek-restart.
func (s *hlsSession) ensureSessionSegment(name string) error {
	path := filepath.Join(s.dir, filepath.Base(name))
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return nil
	}
	return waitSessionFileOrDone(path, hlsSegWaitTimeout, s.done)
}

func safeSessionFile(dir, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", false
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	full := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return full, true
}

func findSession(videoID int64, token, sid string) *hlsSession {
	hlsMu.Lock()
	defer hlsMu.Unlock()
	s, ok := hlsSessions[hlsSessionKey(videoID, token)]
	if !ok || s.id != sid {
		return nil
	}
	s.lastUse = time.Now()
	return s
}

// peekHLSSession returns the live pipe HLS session for video+token, if any.
func peekHLSSession(videoID int64, token string) *hlsSession {
	hlsMu.Lock()
	defer hlsMu.Unlock()
	s, ok := hlsSessions[hlsSessionKey(videoID, token)]
	if !ok {
		return nil
	}
	s.lastUse = time.Now()
	return s
}

func serveFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	stat, err := f.Stat()
	if err == nil {
		http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), f)
		return
	}
	_, _ = io.Copy(w, f)
}
