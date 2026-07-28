package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

const defaultYtdlpBin = "yt-dlp"

func appendCookiesAndAuth(args []string, cookiesPath string, o options) []string {
	if cookiesPath != "" {
		args = append(args, "--cookies", cookiesPath)
	}
	if u := strings.TrimSpace(o.username); u != "" {
		args = append(args, "--username", u)
		if o.password != "" {
			args = append(args, "--password", o.password)
		}
	}
	return args
}

func (o options) resolveYtdlpBin() string {
	if v := strings.TrimSpace(o.ytdlpPath); v != "" {
		return v
	}
	return ytdlpBin()
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

// normalizeFormat returns the profile format selector as-is, or the default
// merge-with-progressive-fallback selector when empty. Does not append further
// /best - height-capped profiles keep their own soft tails.
func normalizeFormat(sel string) string {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return "bv*+ba/b"
	}
	return sel
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
	args = appendCookiesAndAuth(args, cookiesPath, o)
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
	args = appendCookiesAndAuth(args, cookiesPath, o)
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

	stderrTail, err := runStream(ctx, o, args, outdir, onProgress, nil)
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
	args = appendCookiesAndAuth(args, cookiesPath, o)
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
	".mka": true, ".opus": true, ".ogg": true, ".flac": true,
}

// findMedia picks the downloaded media file, preferring mkv (yt-dlp's default
// merge container) so a stray leftover of a different extension doesn't win.
// Skips HLS playlists misnamed as .mp4/.mkv (content starts with #EXTM3U).
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
		if looksLikeHLSPlaylist(path) {
			continue
		}
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

func looksLikeHLSPlaylist(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var buf [8]byte
	n, err := f.Read(buf[:])
	if err != nil && n == 0 {
		return false
	}
	return strings.HasPrefix(string(buf[:n]), "#EXTM3U")
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
// steps may be nil (a fresh StepProgress is used) or pre-seeded with total.
func runStream(ctx context.Context, o options, args []string, cwd string, onProgress func(message string, fraction *float64), steps *StepProgress) ([]byte, error) {
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

	if steps == nil {
		steps = &StepProgress{}
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
		if msg, frac, ok := steps.Feed(line); ok && onProgress != nil {
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
