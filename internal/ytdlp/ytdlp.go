package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

const defaultYtdlpBin = "yt-dlp"
const defaultFfmpegBin = "ffmpeg"

var errSeparateStreams = errors.New("format yields separate audio/video streams")

func (o options) resolveYtdlpBin() string {
	if v := strings.TrimSpace(o.ytdlpPath); v != "" {
		return v
	}
	return ytdlpBin()
}

func (o options) resolveFfmpegBin() string {
	if v := strings.TrimSpace(o.ffmpegPath); v != "" {
		return v
	}
	return ffmpegBin()
}

// withPluginDirs prepends --plugin-dirs for each resolved plugin search parent
// (system POT plugin + operator mounts). yt-dlp accepts the flag multiple times
// - not a PATH-style joined string.
func withPluginDirs(args []string, pluginRoots ...string) []string {
	var dirs []string
	seen := map[string]bool{}
	for _, root := range pluginRoots {
		for _, d := range expandPluginDirs(root) {
			if d == "" || seen[d] {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		return args
	}
	out := make([]string, 0, len(dirs)*2+len(args))
	for _, d := range dirs {
		out = append(out, "--plugin-dirs", d)
	}
	return append(out, args...)
}

// appendPOTArgs adds youtube:fetch_pot and optional youtubepot-bgutilhttp:base_url.
// When a provider URL is set and fetch is not never, also enables pot_trace so
// mint / provider lines appear in captured yt-dlp output (task logs + detect).
func appendPOTArgs(args []string, o options) []string {
	fetch := strings.TrimSpace(o.potFetch)
	if fetch == "" {
		fetch = "never"
	}
	ytArgs := "youtube:fetch_pot=" + fetch
	if u := strings.TrimSpace(o.potProviderURL); u != "" && fetch != "never" {
		ytArgs += ",pot_trace=true"
		args = append(args, "--extractor-args", ytArgs)
		args = append(args, "--extractor-args", "youtubepot-bgutilhttp:base_url="+u)
		return args
	}
	return append(args, "--extractor-args", ytArgs)
}

// expandPluginDirs returns paths for yt-dlp --plugin-dirs.
// yt-dlp expects a parent that contains named package folders
// (each package/<name>/yt_dlp_plugins/...). Passing the package folder itself
// (…/bgutil) yields "Plugin directories: none" and no POT providers.
func expandPluginDirs(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	// Root is already a single package (contains yt_dlp_plugins): pass its parent.
	if hasYtDlpPluginsPkg(root) {
		parent := filepath.Dir(root)
		if parent == "" || parent == "." {
			return []string{root}
		}
		return []string{parent}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		// Root missing/empty: still pass it so mkdir + mount later works.
		return []string{root}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if hasYtDlpPluginsPkg(filepath.Join(root, e.Name())) {
			return []string{root}
		}
	}
	return []string{root}
}

func hasYtDlpPluginsPkg(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "yt_dlp_plugins"))
	return err == nil && st.IsDir()
}

// ytdlpBin resolves the yt-dlp binary: env YTDLP_BIN, else PATH lookup of "yt-dlp".
func ytdlpBin() string {
	if v := strings.TrimSpace(os.Getenv("YTDLP_BIN")); v != "" {
		return v
	}
	return defaultYtdlpBin
}

// ffmpegBin resolves ffmpeg: env FFMPEG_BIN, else PATH lookup of "ffmpeg".
func ffmpegBin() string {
	if v := strings.TrimSpace(os.Getenv("FFMPEG_BIN")); v != "" {
		return v
	}
	return defaultFfmpegBin
}

// appendPaceFlags forwards --limit-rate and Creatorr sleep_requests to yt-dlp.
// When sleep is on, the same value is applied to --sleep-requests, --sleep-subtitles,
// and --sleep-interval (fixed pause; no --max-sleep-interval).
func appendPaceFlags(args []string, limitRate, sleepRequests string) []string {
	if !rateOff(limitRate) {
		args = append(args, "--limit-rate", strings.TrimSpace(limitRate))
	}
	if !secondsOff(sleepRequests) {
		s := strings.TrimSpace(sleepRequests)
		args = append(args,
			"--sleep-requests", s,
			"--sleep-subtitles", s,
			"--sleep-interval", s,
		)
	}
	return args
}

// appendSubtitleFlags adds sidecar subtitle write flags. Empty langs = omit (off).
// Always converts to SRT via --convert-subs. Never passes --embed-subs.
func appendSubtitleFlags(args []string, langs []string, auto bool) []string {
	if len(langs) == 0 {
		return args
	}
	args = append(args, "--write-subs", "--sub-langs", strings.Join(langs, ","), "--convert-subs", "srt")
	if auto {
		args = append(args, "--write-auto-subs")
	}
	return args
}

func rateOff(rate string) bool {
	s := strings.ToLower(strings.TrimSpace(rate))
	return s == "" || s == "0" || s == "off" || s == "none" || s == "unlimited"
}

func secondsOff(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" {
		return true
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f <= 0
}

// normalizeFormat returns the profile format selector as-is, or the strict
// default merge selector when empty. Does not append /best - height-capped
// profiles keep their own soft tails; the best profile is strict bv*+ba.
func normalizeFormat(sel string) string {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return "bv*+ba"
	}
	return sel
}

// normalizeHDFormat is the stream/download-equivalent selector for muxable A+V.
// Bare "best" (Creatorr default profile name / yt-dlp token) must not stay as
// yt-dlp "best" - that picks a single progressive file (often a soft ~360p).
func normalizeHDFormat(sel string) string {
	sel = strings.TrimSpace(sel)
	if sel == "" || sel == "best" {
		return "bv*+ba"
	}
	return normalizeFormat(sel)
}

// normalizeStreamPlayFormat prefers H.264+AAC when available so fMP4 HLS Direct Plays
// in Emby/Jellyfin (AV1/VP9+Opus in HLS often yields "No compatible streams").
func normalizeStreamPlayFormat(sel string) string {
	base := normalizeHDFormat(sel)
	pref := "bv*[vcodec~='^(avc1|avc|h264)']+ba[acodec~='(mp4a|aac)']"
	if base == pref || strings.HasPrefix(base, pref+"/") {
		return base
	}
	return pref + "/" + base
}

// normalizeStreamFormat prefers a single progressive file over separate A/V streams.
// Only used as a last-resort fallback after HD/pipe selection fails.
func normalizeStreamFormat(sel string) string {
	sel = strings.TrimSpace(sel)
	progressive := "best[protocol^=http][protocol!*=m3u8]/best[protocol^=http]/b"
	if sel == "" || sel == "best" {
		return progressive
	}
	// Profiles that already prefer separate A+V cannot be progressive - use progressive ladder.
	if strings.Contains(sel, "+") {
		return progressive
	}
	for _, part := range strings.Split(sel, "/") {
		if strings.TrimSpace(part) == "best" {
			return sel
		}
	}
	return sel + "/best"
}

// dumpJSON runs `yt-dlp --skip-download -J [--flat-playlist] URL` and parses the result.
func dumpJSON(ctx context.Context, url, cookiesPath string, flat bool, playlistEnd int, userAgent string, o options) (map[string]any, error) {
	args := []string{"--no-mtime", "--skip-download", "-J"}
	if flat {
		args = append(args, "--flat-playlist")
	}
	if playlistEnd > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(playlistEnd))
	}
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if userAgent != "" {
		args = append(args, "--user-agent", userAgent)
	}
	args = appendPaceFlags(args, o.limitRate, o.sleepRequests)
	args = appendPOTArgs(args, o)
	args = append(args, url)
	args = withPluginDirs(args, o.systemPluginDirs, o.pluginDirs)

	stdout, stderr, err := runCapture(ctx, o, args, "")
	if err != nil {
		return nil, wrapYtdlpFail(apperrors.CodeResolveFailed, "yt-dlp metadata failed", stderr, stdout, err)
	}
	var info map[string]any
	if err := json.Unmarshal(stdout, &info); err != nil {
		return nil, appErr(apperrors.CodeResolveFailed, "yt-dlp JSON parse failed", err.Error())
	}
	return info, nil
}

// dumpJSONWithFormat runs `yt-dlp --skip-download -f FORMAT -J URL` and parses the result.
func dumpJSONWithFormat(ctx context.Context, url, format, cookiesPath, userAgent string, o options) (map[string]any, error) {
	args := []string{"--no-mtime", "--skip-download", "-f", format, "-J"}
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if userAgent != "" {
		args = append(args, "--user-agent", userAgent)
	}
	args = appendPaceFlags(args, o.limitRate, o.sleepRequests)
	args = appendPOTArgs(args, o)
	args = append(args, url)
	args = withPluginDirs(args, o.systemPluginDirs, o.pluginDirs)

	stdout, stderr, err := runCapture(ctx, o, args, "")
	if err != nil {
		return nil, wrapYtdlpFail(apperrors.CodeResolveFailed, "yt-dlp metadata failed", stderr, stdout, err)
	}
	var info map[string]any
	if err := json.Unmarshal(stdout, &info); err != nil {
		return nil, appErr(apperrors.CodeResolveFailed, "yt-dlp JSON parse failed", err.Error())
	}
	return info, nil
}

// fetchStreamURLs picks hls > pipe > progressive for Creatorr stream proxy.
func fetchStreamURLs(ctx context.Context, url, format, cookiesPath, userAgent string, o options) (map[string]any, error) {
	info, err := dumpJSONWithFormat(ctx, url, normalizeStreamPlayFormat(format), cookiesPath, userAgent, o)
	if err != nil {
		return nil, err
	}
	if result, ok := streamKindFromInfo(info); ok {
		return withDuration(result, info), nil
	}
	// Progressive fallback: retry with a progressive-preferring format ladder.
	info, err = dumpJSONWithFormat(ctx, url, normalizeStreamFormat(format), cookiesPath, userAgent, o)
	if err != nil {
		return nil, err
	}
	if result, ok := streamKindFromInfo(info); ok {
		return withDuration(result, info), nil
	}
	mediaURL, headers, err := progressiveURLFromInfo(info)
	if err != nil {
		if errors.Is(err, errSeparateStreams) {
			return withDuration(pipeResultFromInfo(info), info), nil
		}
		return nil, err
	}
	return withDuration(map[string]any{
		"kind":    "progressive",
		"url":     mediaURL,
		"headers": headers,
	}, info), nil
}

func withDuration(result map[string]any, info map[string]any) map[string]any {
	if result == nil {
		return nil
	}
	if d := durationSeconds(info); d > 0 {
		result["duration"] = d
	}
	if mt := strings.TrimSpace(strField(info, "media_type")); mt != "" {
		result["media_type"] = mt
	}
	if boolField(info, "is_live") {
		result["is_live"] = true
	}
	return result
}

// streamKindFromInfo classifies yt-dlp -J output. ok=false when progressive retry may help.
func streamKindFromInfo(info map[string]any) (map[string]any, bool) {
	if u, headers, ok := hlsMasterFromInfo(info); ok {
		return map[string]any{"kind": "hls", "url": u, "headers": headers}, true
	}
	if needsPipeStream(info) {
		return pipeResultFromInfo(info), true
	}
	mediaURL, headers, err := progressiveURLFromInfo(info)
	if err != nil {
		return nil, false
	}
	return map[string]any{
		"kind":    "progressive",
		"url":     mediaURL,
		"headers": headers,
	}, true
}

func needsPipeStream(info map[string]any) bool {
	if req, ok := info["requested_formats"].([]any); ok && len(req) >= 2 {
		return true
	}
	// yt-dlp reports merged selection as "399+251" even when requested_formats is present.
	if id, ok := stringField(info, "format_id"); ok && strings.Contains(id, "+") {
		return true
	}
	return false
}

// pipeResultFromInfo builds kind:pipe with optional CDN URLs for single-resolve stream.
func pipeResultFromInfo(info map[string]any) map[string]any {
	out := map[string]any{"kind": "pipe"}
	defaultHdr := httpHeadersFromInfo(info)
	if len(defaultHdr) > 0 {
		out["headers"] = defaultHdr
	}
	req, ok := info["requested_formats"].([]any)
	if !ok || len(req) < 2 {
		return out
	}
	v0, _ := req[0].(map[string]any)
	v1, _ := req[1].(map[string]any)
	if vu, ok := stringField(v0, "url"); ok && vu != "" {
		out["video_url"] = vu
		vh := httpHeadersFromInfo(v0)
		if len(vh) == 0 {
			vh = defaultHdr
		}
		if len(vh) > 0 {
			out["video_headers"] = vh
		}
	}
	if au, ok := stringField(v1, "url"); ok && au != "" {
		out["audio_url"] = au
		ah := httpHeadersFromInfo(v1)
		if len(ah) == 0 {
			ah = defaultHdr
		}
		if len(ah) > 0 {
			out["audio_headers"] = ah
		}
	}
	return out
}

func hlsMasterFromInfo(info map[string]any) (string, map[string]string, bool) {
	defaultHeaders := httpHeadersFromInfo(info)
	if u, ok := stringField(info, "manifest_url"); ok && isHLSURL(u) {
		return u, defaultHeaders, true
	}
	if u, ok := stringField(info, "url"); ok && isHLSURL(u) {
		return u, defaultHeaders, true
	}
	if req, ok := info["requested_formats"].([]any); ok {
		for _, r := range req {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := stringField(m, "url"); ok && isHLSURL(u) {
				h := httpHeadersFromInfo(m)
				if len(h) == 0 {
					h = defaultHeaders
				}
				return u, h, true
			}
			if proto, ok := stringField(m, "protocol"); ok && isHLSProtocol(proto) {
				if u, ok := stringField(m, "url"); ok && u != "" {
					h := httpHeadersFromInfo(m)
					if len(h) == 0 {
						h = defaultHeaders
					}
					return u, h, true
				}
			}
		}
	}
	if formats, ok := info["formats"].([]any); ok {
		for _, f := range formats {
			m, ok := f.(map[string]any)
			if !ok {
				continue
			}
			proto, _ := stringField(m, "protocol")
			if !isHLSProtocol(proto) {
				continue
			}
			u, ok := stringField(m, "url")
			if !ok || u == "" || !isHLSURL(u) {
				continue
			}
			h := httpHeadersFromInfo(m)
			if len(h) == 0 {
				h = defaultHeaders
			}
			return u, h, true
		}
	}
	return "", nil, false
}

func isHLSURL(u string) bool {
	low := strings.ToLower(u)
	return strings.Contains(low, ".m3u8") || strings.Contains(low, "m3u8")
}

func isHLSProtocol(proto string) bool {
	low := strings.ToLower(proto)
	return strings.Contains(low, "m3u8")
}

func progressiveURLFromInfo(info map[string]any) (string, map[string]string, error) {
	headers := httpHeadersFromInfo(info)
	if u, ok := stringField(info, "url"); ok && u != "" {
		if !isHLSURL(u) {
			return u, headers, nil
		}
	}
	if req, ok := info["requested_formats"].([]any); ok && len(req) == 1 {
		if m, ok := req[0].(map[string]any); ok {
			if u, ok := stringField(m, "url"); ok && u != "" && !isHLSURL(u) {
				if h := httpHeadersFromInfo(m); len(h) > 0 {
					headers = h
				}
				return u, headers, nil
			}
		}
	}
	if req, ok := info["requested_formats"].([]any); ok && len(req) > 1 {
		return "", nil, errSeparateStreams
	}
	return "", nil, appErr(apperrors.CodeResolveFailed, "yt-dlp returned no progressive media URL", "")
}

// resolveAVURLs returns direct video+audio URLs (+ headers) for pipe streaming.
func resolveAVURLs(ctx context.Context, url, format, cookiesPath, userAgent string, o options) (videoURL, audioURL string, videoHdr, audioHdr map[string]string, err error) {
	info, err := dumpJSONWithFormat(ctx, url, normalizeStreamPlayFormat(format), cookiesPath, userAgent, o)
	if err != nil {
		return "", "", nil, nil, err
	}
	if req, ok := info["requested_formats"].([]any); ok && len(req) >= 2 {
		v0, _ := req[0].(map[string]any)
		v1, _ := req[1].(map[string]any)
		videoURL, _ = stringField(v0, "url")
		audioURL, _ = stringField(v1, "url")
		videoHdr = httpHeadersFromInfo(v0)
		audioHdr = httpHeadersFromInfo(v1)
		if videoURL != "" && audioURL != "" {
			return videoURL, audioURL, videoHdr, audioHdr, nil
		}
	}
	urls, err := directURLsViaG(ctx, url, format, cookiesPath, userAgent, o)
	if err != nil {
		return "", "", nil, nil, err
	}
	switch len(urls) {
	case 0:
		return "", "", nil, nil, appErr(apperrors.CodeResolveFailed, "yt-dlp returned no stream URLs", "")
	case 1:
		return urls[0], "", httpHeadersFromInfo(info), nil, nil
	default:
		return urls[0], urls[1], httpHeadersFromInfo(info), httpHeadersFromInfo(info), nil
	}
}

func directURLsViaG(ctx context.Context, url, format, cookiesPath, userAgent string, o options) ([]string, error) {
	args := []string{"--no-mtime", "--skip-download", "-f", normalizeStreamPlayFormat(format), "-g"}
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if userAgent != "" {
		args = append(args, "--user-agent", userAgent)
	}
	args = appendPaceFlags(args, o.limitRate, o.sleepRequests)
	args = appendPOTArgs(args, o)
	args = append(args, url)
	args = withPluginDirs(args, o.systemPluginDirs, o.pluginDirs)

	stdout, stderr, err := runCapture(ctx, o, args, "")
	if err != nil {
		return nil, wrapYtdlpFail(apperrors.CodeResolveFailed, "yt-dlp stream URL resolve failed", stderr, stdout, err)
	}
	var urls []string
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			urls = append(urls, line)
		}
	}
	return urls, nil
}

func formatFFmpegHeaders(h map[string]string, mediaURL string) string {
	h = enrichCDNHeaders(h, mediaURL)
	if len(h) == 0 {
		return ""
	}
	var b strings.Builder
	for k, v := range h {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return b.String()
}

// enrichCDNHeaders fills User-Agent; for googlevideo also Referer when missing.
func enrichCDNHeaders(h map[string]string, mediaURL string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	if out["User-Agent"] == "" && out["user-agent"] == "" {
		out["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	}
	if out["Referer"] == "" && out["referer"] == "" {
		low := strings.ToLower(mediaURL)
		if strings.Contains(low, "googlevideo.com") || strings.Contains(low, "youtube.com") {
			out["Referer"] = "https://www.youtube.com/"
		}
	}
	return out
}

// muxToMatroska copies A+V inputs to Matroska on dst via ffmpeg (Emby-friendly Direct Play).
func muxToMatroska(ctx context.Context, o options, videoURL, audioURL string, videoHdr, audioHdr map[string]string, dst io.Writer) error {
	args := appendAVInputs(nil, videoURL, audioURL, videoHdr, audioHdr, 0)
	// Low cluster limits so clients see bytes sooner on a live pipe (seek still weak - no Range).
	args = append(args, "-c", "copy", "-f", "matroska",
		"-cluster_size_limit", "2M", "-cluster_time_limit", "1000",
		"pipe:1")
	return runFFmpeg(ctx, o, args, dst)
}

// muxToHLS writes MPEG-TS HLS under dir for stream proxy play.
// Use EVENT (not VOD): ffmpeg only writes the m3u8 at encode-end for VOD, which
// breaks Creatorr's "wait for first segment" cold start. Scrubber length comes
// from Creatorr NFO; playlist grows linearly as segments appear (no mid-mux seek).
// startSec > 0 seeks into the source (download-beginning live handoff only).
// Seek is applied after inputs (see appendAVInputs) so googlevideo does not 403.
func muxToHLS(ctx context.Context, o options, dir, videoURL, audioURL string, videoHdr, audioHdr map[string]string, startSec float64, startNumber int) error {
	cmd, stderr, err := startMuxToHLS(ctx, o, dir, videoURL, audioURL, videoHdr, audioHdr, startSec, startNumber, 0, nil)
	if err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return wrapFfmpegFail(stderr.Bytes(), err)
	}
	return nil
}

func startMuxToHLS(ctx context.Context, o options, dir, videoURL, audioURL string, videoHdr, audioHdr map[string]string, startSec float64, startNumber int, maxSec float64, _ [][2]float64) (*exec.Cmd, *bytes.Buffer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, appErr(apperrors.CodeDownloadFailed, "hls dir create failed", err.Error())
	}
	args := appendAVInputs(nil, videoURL, audioURL, videoHdr, audioHdr, startSec)
	if maxSec > 0 {
		args = append(args, "-t", strconv.FormatFloat(maxSec, 'f', -1, 64))
	}
	index := filepath.Join(dir, "index.m3u8")
	segPattern := filepath.Join(dir, "seg%05d.ts")
	args = append(args,
		"-c", "copy",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-start_at_zero",
		"-hls_segment_filename", segPattern,
	)
	if startNumber > 0 {
		args = append(args, "-start_number", strconv.Itoa(startNumber))
	}
	args = append(args, index)

	cmd := exec.CommandContext(ctx, o.resolveFfmpegBin(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	exectrace.Record(ctx, o.resolveFfmpegBin(), args...)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, &stderr, nil
}

func appendAVInputs(args []string, videoURL, audioURL string, videoHdr, audioHdr map[string]string, startSec float64) []string {
	args = append(args, "-hide_banner", "-loglevel", "error", "-nostdin")
	videoHdr = enrichCDNHeaders(videoHdr, videoURL)
	audioHdr = enrichCDNHeaders(audioHdr, audioURL)
	ua := videoHdr["User-Agent"]
	if ua == "" {
		ua = videoHdr["user-agent"]
	}
	if ua != "" {
		args = append(args, "-user_agent", ua)
	}
	if hdr := formatFFmpegHeaders(videoHdr, videoURL); hdr != "" {
		args = append(args, "-headers", hdr)
	}
	args = append(args, "-i", videoURL)
	if audioURL != "" {
		if hdr := formatFFmpegHeaders(audioHdr, audioURL); hdr != "" {
			args = append(args, "-headers", hdr)
		}
		args = append(args, "-i", audioURL)
	}
	// Output seek after all -i. Input -ss (before -i) makes googlevideo return 403
	// on range/seek opens - live handoff after download-beginning would die instantly.
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', -1, 64))
	}
	return args
}

func runFFmpeg(ctx context.Context, o options, args []string, dst io.Writer) error {
	cmd := exec.CommandContext(ctx, o.resolveFfmpegBin(), args...)
	if dst != nil {
		cmd.Stdout = dst
	} else {
		cmd.Stdout = io.Discard
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	exectrace.Record(ctx, o.resolveFfmpegBin(), args...)
	if err := cmd.Run(); err != nil {
		return wrapFfmpegFail(stderr.Bytes(), err)
	}
	return nil
}

func wrapFfmpegFail(stderr []byte, err error) *apperrors.AppError {
	detail := strings.TrimSpace(string(stderr))
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if len(detail) > 1500 {
		detail = detail[len(detail)-1500:]
	}
	return appErr(apperrors.CodeDownloadFailed, "ffmpeg stream mux failed", detail)
}

func httpHeadersFromInfo(info map[string]any) map[string]string {
	out := map[string]string{}
	raw, ok := info["http_headers"].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}
	return out
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// downloadMedia runs a plain yt-dlp download (no merge/remux flag) into outdir,
// streaming progress to onProgress, and returns the resulting media file path.
// onProgress fraction may be nil for message-only lines (PO token trace).
func downloadMedia(ctx context.Context, url, outdir, format, cookiesPath, userAgent string, o options, onProgress func(message string, fraction *float64)) (string, error) {
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return "", appErr(apperrors.CodeDownloadFailed, "could not create output directory", err.Error())
	}
	args := []string{
		"--no-mtime", "--newline",
		"-f", normalizeFormat(format),
		"-o", "%(title).200B [%(id)s].%(ext)s",
		"--write-info-json", "--write-thumbnail",
	}
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if userAgent != "" {
		args = append(args, "--user-agent", userAgent)
	}
	if mf := strings.TrimSpace(o.matchFilter); mf != "" {
		args = append(args, "--match-filters", mf)
	}
	args = appendSubtitleFlags(args, o.subLangs, o.subAuto)
	args = appendPaceFlags(args, o.limitRate, o.sleepRequests)
	args = appendPOTArgs(args, o)
	args = append(args, url)
	args = withPluginDirs(args, o.systemPluginDirs, o.pluginDirs)

	stderrTail, err := runStream(ctx, o, args, outdir, onProgress)
	notePOTOutput(ctx, o, stderrTail)
	if err != nil {
		if code, msg := classifyMatchFilterReject(string(stderrTail), o.matchFilter); code != "" {
			return "", wrapYtdlpFail(code, msg, stderrTail, nil, err)
		}
		return "", wrapYtdlpFail(apperrors.CodeDownloadFailed, "yt-dlp download failed", stderrTail, nil, err)
	}
	media, err := findMedia(outdir)
	if err != nil {
		if code, msg := classifyMatchFilterReject(string(stderrTail), o.matchFilter); code != "" {
			return "", appErr(code, msg, strings.TrimSpace(string(stderrTail)))
		}
		return "", appErr(apperrors.CodeDownloadFailed, "no media file found after download", err.Error())
	}
	abs, err := filepath.Abs(media)
	if err != nil {
		return media, nil
	}
	return abs, nil
}

// fetchSidecars runs yt-dlp --skip-download to write info.json / thumbnail / subs
// into outdir, then returns whichever of those files it produced.
func fetchSidecars(ctx context.Context, url, outdir, cookiesPath, userAgent string, o options) (infoPath, thumbPath string, subPaths []string, err error) {
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return "", "", nil, appErr(apperrors.CodeResolveFailed, "could not create output directory", err.Error())
	}
	args := []string{
		"--no-mtime", "--skip-download", "-o", "meta.%(ext)s",
		"--write-info-json", "--write-thumbnail",
	}
	args = appendSubtitleFlags(args, o.subLangs, o.subAuto)
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if userAgent != "" {
		args = append(args, "--user-agent", userAgent)
	}
	args = appendPaceFlags(args, o.limitRate, o.sleepRequests)
	args = appendPOTArgs(args, o)
	args = append(args, url)
	args = withPluginDirs(args, o.systemPluginDirs, o.pluginDirs)

	stdout, stderr, runErr := runCapture(ctx, o, args, outdir)
	if runErr != nil {
		return "", "", nil, wrapYtdlpFail(apperrors.CodeResolveFailed, "yt-dlp sidecar fetch failed", stderr, stdout, runErr)
	}
	entries, err := os.ReadDir(outdir)
	if err != nil {
		return "", "", nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path, absErr := filepath.Abs(filepath.Join(outdir, e.Name()))
		if absErr != nil {
			path = filepath.Join(outdir, e.Name())
		}
		lower := strings.ToLower(e.Name())
		switch {
		case strings.HasSuffix(lower, ".info.json"):
			infoPath = path
		case strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") ||
			strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".webp"):
			if thumbPath == "" {
				thumbPath = path
			}
		case strings.HasSuffix(lower, ".vtt") || strings.HasSuffix(lower, ".srt") ||
			strings.HasSuffix(lower, ".ass") || strings.HasSuffix(lower, ".ssa") ||
			strings.HasSuffix(lower, ".sub"):
			subPaths = append(subPaths, path)
		}
	}
	return infoPath, thumbPath, subPaths, nil
}

var mediaExt = map[string]bool{
	".mkv": true, ".mp4": true, ".webm": true, ".mov": true, ".m4a": true, ".mp3": true,
}

// findMedia picks the downloaded media file, preferring mkv (yt-dlp's default
// merge container) so a stray leftover of a different extension doesn't win.
func findMedia(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var best string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !mediaExt[ext] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if ext == ".mkv" {
			return path, nil
		}
		if best == "" {
			best = path
		}
	}
	if best == "" {
		return "", os.ErrNotExist
	}
	return best, nil
}

func runCapture(ctx context.Context, o options, args []string, cwd string) (stdout, stderr []byte, err error) {
	bin := o.resolveYtdlpBin()
	cmd := exec.CommandContext(ctx, bin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	exectrace.Record(ctx, bin, args...)
	err = cmd.Run()
	notePOTOutput(ctx, o, outBuf.Bytes(), errBuf.Bytes())
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// runStream runs yt-dlp with combined stdout+stderr piped line by line so
// progress can be forwarded as it happens; it returns the last portion of
// output (for error detail) alongside cmd.Wait's error, if any.
// onProgress fraction may be nil for message-only lines (PO token trace).
func runStream(ctx context.Context, o options, args []string, cwd string, onProgress func(message string, fraction *float64)) ([]byte, error) {
	bin := o.resolveYtdlpBin()
	cmd := exec.CommandContext(ctx, bin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // combine streams into a single ordered pipe
	exectrace.Record(ctx, bin, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	tracePOT := strings.TrimSpace(o.potProviderURL) != "" && o.potFetch != "never"
	var tail bytes.Buffer
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		tail.WriteString(line)
		tail.WriteByte('\n')
		if tail.Len() > 4096 {
			tail.Next(tail.Len() - 4096)
		}
		if tracePOT {
			observePOTLine(ctx, o, line)
			if onProgress != nil && isPOTTraceLine(line) {
				onProgress(trimPOTLine(line), nil)
			}
		}
		if msg, frac := parseProgress(line); msg != "" && frac != nil && onProgress != nil {
			onProgress(msg, frac)
		}
	}
	waitErr := cmd.Wait()
	return tail.Bytes(), waitErr
}

func wrapYtdlpFail(code, message string, stderr, stdout []byte, err error) *apperrors.AppError {
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(stdout))
	}
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if len(detail) > 1500 {
		detail = detail[len(detail)-1500:]
	}
	code = upgradeCode(code, detail)
	msg := message
	if pm := pauseMessage(code); pm != "" {
		msg = pm
	}
	return appErr(code, msg, detail)
}

// classifyMatchFilterReject maps a yt-dlp --match-filters skip to a stable AppError code.
// Empty code means the stderr is not a match-filter reject.
func classifyMatchFilterReject(stderr, matchFilter string) (code, message string) {
	mf := strings.TrimSpace(matchFilter)
	if mf == "" {
		return "", ""
	}
	low := strings.ToLower(stderr)
	looksLikeSkip := strings.Contains(low, "media_type") ||
		strings.Contains(low, "is_live") ||
		(strings.Contains(low, "[download]") && (strings.Contains(low, "does not pass filter") ||
			strings.Contains(low, "did not match") || strings.Contains(low, "skipping"))) ||
		strings.Contains(low, "does not pass filter") ||
		strings.Contains(low, "did not match")
	if !looksLikeSkip {
		return "", ""
	}
	hasLive := strings.Contains(mf, "is_live")
	hasMedia := strings.Contains(mf, "media_type")
	stderrMedia := strings.Contains(low, "media_type")
	stderrLive := strings.Contains(low, "is_live")
	switch {
	case stderrMedia && !stderrLive:
		return apperrors.CodeMediaTypeExcluded, "media type excluded"
	case stderrLive && !stderrMedia:
		return apperrors.CodeLiveBroadcastSkipped, "currently live"
	case hasLive && hasMedia:
		// Ambiguous: prefer soft-skip over permanent ignore.
		return apperrors.CodeLiveBroadcastSkipped, "currently live"
	case hasMedia:
		return apperrors.CodeMediaTypeExcluded, "media type excluded"
	case hasLive:
		return apperrors.CodeLiveBroadcastSkipped, "currently live"
	default:
		return apperrors.CodeMediaTypeExcluded, "media type excluded"
	}
}

// isMediaTypeFilterReject reports whether yt-dlp skipped due to --match-filters on media_type.
// Deprecated path kept for older call sites; prefer classifyMatchFilterReject.
func isMediaTypeFilterReject(stderr, matchFilter string) bool {
	code, _ := classifyMatchFilterReject(stderr, matchFilter)
	return code == apperrors.CodeMediaTypeExcluded
}

// upgradeCode reclassifies a generic failure as CookieInvalid / RateLimited
// when yt-dlp's own error text matches well-known site responses.
func upgradeCode(code, text string) string {
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "sign in") ||
		strings.Contains(low, "login required") ||
		strings.Contains(low, "private video") ||
		strings.Contains(low, "cookies") && (strings.Contains(low, "expired") || strings.Contains(low, "invalid")) ||
		strings.Contains(low, "http error 401") ||
		strings.Contains(low, "http error 403"):
		return apperrors.CodeCookieInvalid
	case strings.Contains(low, "429") ||
		strings.Contains(low, "too many requests") ||
		strings.Contains(low, "rate-limit") ||
		strings.Contains(low, "rate limit"):
		return apperrors.CodeRateLimited
	default:
		return code
	}
}

func pauseMessage(code string) string {
	switch code {
	case apperrors.CodeCookieInvalid:
		return "site rejected cookies or requires sign-in"
	case apperrors.CodeRateLimited:
		return "site rate limited the request"
	default:
		return ""
	}
}
