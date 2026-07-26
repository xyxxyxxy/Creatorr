package ytdlp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// Client invokes yt-dlp (and ffmpeg for pipe/HLS mux) in-process for Creatorr.
type Client struct {
	Bin              string // path to yt-dlp binary; empty uses YTDLP_BIN / "yt-dlp"
	PluginsDir       string // operator plugin mounts; always passed as --plugin-dirs when non-empty
	SystemPluginsDir string // baked POT plugin path; always passed in addition to PluginsDir
	FFmpegBin        string // path to ffmpeg; empty uses FFMPEG_BIN / "ffmpeg"
	PotProviderURL   string // CREATORR_POT_PROVIDER_URL; empty forces fetch_pot=never
	// PotFetch returns Settings pot_fetch (or never when URL unset). Optional; defaults apply when nil.
	PotFetch func() string
}

// ListOpts controls List (flat playlist / channel index).
type ListOpts struct {
	URL             string
	CookiesPath     string
	PlaylistEnd     int // 0 = no cap
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
}

// ResolveOpts controls Resolve (single-video metadata).
type ResolveOpts struct {
	URL             string
	CookiesPath     string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
}

// UrlsOpts controls FetchUrls (stream URL resolution for proxy).
type UrlsOpts struct {
	URL             string
	FormatSelector  string
	CookiesPath     string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
}

// StreamOpts controls StartHLSStream / PipeStream.
type StreamOpts struct {
	URL             string
	FormatSelector  string
	CookiesPath     string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
	HLSDir          string
	HLSStartSec     float64
	HLSStartNumber  int
	// HLSMaxSec when >0 stops mux after this many seconds of output (after -ss).
	HLSMaxSec        float64
	VideoURL         string
	AudioURL         string
	VideoHeadersJSON string
	AudioHeadersJSON string
}

// DownloadOpts controls Download.
type DownloadOpts struct {
	URL             string
	OutDir          string
	FormatSelector  string
	CookiesPath     string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
	MatchFilter     string // yt-dlp --match-filters; empty = omit
	SubLangs        []string
	SubAuto         bool
	OnProgress      func(msg string, frac *float64)
}

// SidecarsOpts controls FetchSidecars.
type SidecarsOpts struct {
	URL             string
	OutDir          string
	CookiesPath     string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
	SubLangs        []string
	SubAuto         bool
}

// UrlsResult.Kind values.
const (
	UrlsKindProgressive = "progressive"
	UrlsKindPipe        = "pipe"
	UrlsKindHLS         = "hls"
)

// UrlsResult is stream resolution: progressive | pipe | hls.
type UrlsResult struct {
	Kind            string
	URL             string
	Headers         map[string]string
	VideoURL        string
	AudioURL        string
	VideoHeaders    map[string]string
	AudioHeaders    map[string]string
	DurationSeconds float64
	MediaType       string // yt-dlp media_type from the same -J extract; empty when missing
	IsLive          bool   // true only when yt-dlp is_live is explicitly true
}

func (c *Client) fill(o *options) {
	if c == nil {
		return
	}
	o.ytdlpPath = strings.TrimSpace(c.Bin)
	o.ffmpegPath = strings.TrimSpace(c.FFmpegBin)
	o.pluginDirs = strings.TrimSpace(c.PluginsDir)
	o.systemPluginDirs = strings.TrimSpace(c.SystemPluginsDir)
	o.potProviderURL = strings.TrimSpace(c.PotProviderURL)
	if o.potProviderURL == "" {
		o.potFetch = "never"
		return
	}
	if c.PotFetch != nil {
		o.potFetch = strings.TrimSpace(c.PotFetch())
	}
	if o.potFetch == "" {
		o.potFetch = "auto"
	}
}

func sleepSeconds(v float64) string {
	if v <= 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// List lists channel/playlist entries (yt-dlp --flat-playlist).
func (c *Client) List(ctx context.Context, opts ListOpts) ([]Entry, error) {
	o := options{
		url:           opts.URL,
		cookies:       opts.CookiesPath,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
		playlistEnd:   opts.PlaylistEnd,
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return nil, appErr(apperrors.CodeInternal, "url required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	info, err := dumpJSON(ctx, o.url, jarPath, true, o.playlistEnd, ua, o)
	if err != nil {
		return nil, err
	}
	entries := entriesFromInfo(info)
	if entries == nil {
		entries = []Entry{}
	}
	return entries, nil
}

// DumpPlaylistInfo returns the raw yt-dlp -J --flat-playlist object (wrapper + entries).
func (c *Client) DumpPlaylistInfo(ctx context.Context, opts ListOpts) (map[string]any, error) {
	o := options{
		url:           opts.URL,
		cookies:       opts.CookiesPath,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
		playlistEnd:   opts.PlaylistEnd,
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return nil, appErr(apperrors.CodeInternal, "url required", "")
	}
	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return dumpJSON(ctx, o.url, jarPath, true, o.playlistEnd, ua, o)
}

// Resolve returns full metadata for a single video URL.
func (c *Client) Resolve(ctx context.Context, opts ResolveOpts) (Entry, error) {
	o := options{
		url:           opts.URL,
		cookies:       opts.CookiesPath,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return Entry{}, appErr(apperrors.CodeInternal, "url required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return Entry{}, err
	}
	defer cleanup()

	info, err := dumpJSON(ctx, o.url, jarPath, false, 0, ua, o)
	if err != nil {
		return Entry{}, err
	}
	entries := entriesFromInfo(info)
	if len(entries) == 0 {
		return Entry{}, appErr(apperrors.CodeResolveFailed, "yt-dlp returned no resolvable entry", "")
	}
	return entries[0], nil
}

// FetchUrls returns stream resolution: hls (ABR), pipe, or progressive.
func (c *Client) FetchUrls(ctx context.Context, opts UrlsOpts) (UrlsResult, error) {
	if fake, ok := fakeUrlsFromEnv(); ok {
		return fake, nil
	}
	o := options{
		url:           opts.URL,
		cookies:       opts.CookiesPath,
		format:        opts.FormatSelector,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return UrlsResult{}, appErr(apperrors.CodeInternal, "url required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return UrlsResult{}, err
	}
	defer cleanup()

	result, err := fetchStreamURLs(ctx, o.url, o.format, jarPath, ua, o)
	if err != nil {
		return UrlsResult{}, err
	}
	return urlsResultFromMap(result), nil
}

// Download fetches media into OutDir (no remux) and returns the media path.
func (c *Client) Download(ctx context.Context, opts DownloadOpts) (string, error) {
	o := options{
		url:           opts.URL,
		outdir:        opts.OutDir,
		cookies:       opts.CookiesPath,
		format:        opts.FormatSelector,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
		matchFilter:   strings.TrimSpace(opts.MatchFilter),
		subLangs:      append([]string(nil), opts.SubLangs...),
		subAuto:       opts.SubAuto,
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return "", appErr(apperrors.CodeInternal, "url required", "")
	}
	if strings.TrimSpace(o.outdir) == "" {
		return "", appErr(apperrors.CodeDownloadFailed, "outdir required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return "", err
	}
	defer cleanup()

	return downloadMedia(ctx, o.url, o.outdir, o.format, jarPath, ua, o, opts.OnProgress)
}

// FetchSidecars writes info.json / thumbnail / subs via --skip-download.
func (c *Client) FetchSidecars(ctx context.Context, opts SidecarsOpts) (info, thumb string, subs []string, err error) {
	o := options{
		url:           opts.URL,
		outdir:        opts.OutDir,
		cookies:       opts.CookiesPath,
		flaresolverr:  opts.FlareSolverrURL,
		limitRate:     opts.LimitRate,
		sleepRequests: sleepSeconds(opts.SleepRequests),
		subLangs:      append([]string(nil), opts.SubLangs...),
		subAuto:       opts.SubAuto,
	}
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" {
		return "", "", nil, appErr(apperrors.CodeInternal, "url required", "")
	}
	if strings.TrimSpace(o.outdir) == "" {
		return "", "", nil, appErr(apperrors.CodeResolveFailed, "outdir required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return "", "", nil, err
	}
	defer cleanup()

	info, thumb, subs, err = fetchSidecars(ctx, o.url, o.outdir, jarPath, ua, o)
	if err != nil {
		return "", "", nil, err
	}
	if subs == nil {
		subs = []string{}
	}
	return info, thumb, subs, nil
}

// StartHLSStream muxes A+V to MPEG-TS HLS under opts.HLSDir in the background.
// Caller must cancel ctx to stop ffmpeg. Returns after the process has started.
// done receives the wait error (nil on clean exit) once; then closes.
func (c *Client) StartHLSStream(ctx context.Context, opts StreamOpts) (done <-chan error, err error) {
	if strings.TrimSpace(opts.HLSDir) == "" {
		return nil, appErr(apperrors.CodeInternal, "hls dir empty", "")
	}
	if _, ok := fakeUrlsFromEnv(); ok {
		return fakeStartHLS(ctx, opts.HLSDir)
	}
	o := streamOptions(opts)
	c.fill(&o)

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return nil, err
	}

	videoURL, audioURL, videoHdr, audioHdr, err := streamAVInputs(ctx, o, jarPath, ua)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.MkdirAll(opts.HLSDir, 0o755); err != nil {
		cleanup()
		return nil, err
	}

	cmd, stderr, err := startMuxToHLS(ctx, o, opts.HLSDir, videoURL, audioURL, videoHdr, audioHdr, opts.HLSStartSec, opts.HLSStartNumber, opts.HLSMaxSec, nil)
	if err != nil {
		cleanup()
		return nil, err
	}

	ch := make(chan error, 1)
	go func() {
		defer cleanup()
		waitErr := cmd.Wait()
		if waitErr != nil && ctx.Err() == nil {
			mapped := wrapFfmpegFail(stderr.Bytes(), waitErr)
			slog.Warn("ytdlp hls stream", "err", mapped)
			ch <- mapped
		} else {
			ch <- waitErr
		}
		close(ch)
	}()
	return ch, nil
}

// PipeStream muxes A+V to Matroska and copies stdout to dst until EOF or cancel.
func (c *Client) PipeStream(ctx context.Context, opts StreamOpts, dst io.Writer) error {
	if _, ok := fakeUrlsFromEnv(); ok {
		return fakePipeStream(dst)
	}
	o := streamOptions(opts)
	c.fill(&o)
	if strings.TrimSpace(o.url) == "" && strings.TrimSpace(o.videoURL) == "" {
		return appErr(apperrors.CodeInternal, "url required", "")
	}

	jarPath, ua, cleanup, err := resolveCookies(ctx, o.flaresolverr, o.url, o.cookies)
	if err != nil {
		return err
	}
	defer cleanup()

	videoURL, audioURL, videoHdr, audioHdr, err := streamAVInputs(ctx, o, jarPath, ua)
	if err != nil {
		return err
	}
	return muxToMatroska(ctx, o, videoURL, audioURL, videoHdr, audioHdr, dst)
}

// StreamOptsFromUrls fills reuse fields from a pipe urls result.
func StreamOptsFromUrls(base StreamOpts, urls UrlsResult) StreamOpts {
	base.VideoURL = strings.TrimSpace(urls.VideoURL)
	base.AudioURL = strings.TrimSpace(urls.AudioURL)
	if len(urls.VideoHeaders) > 0 {
		if b, err := json.Marshal(urls.VideoHeaders); err == nil {
			base.VideoHeadersJSON = string(b)
		}
	} else if len(urls.Headers) > 0 && base.VideoURL != "" {
		if b, err := json.Marshal(urls.Headers); err == nil {
			base.VideoHeadersJSON = string(b)
		}
	}
	if len(urls.AudioHeaders) > 0 {
		if b, err := json.Marshal(urls.AudioHeaders); err == nil {
			base.AudioHeadersJSON = string(b)
		}
	} else if len(urls.Headers) > 0 && base.AudioURL != "" {
		if b, err := json.Marshal(urls.Headers); err == nil {
			base.AudioHeadersJSON = string(b)
		}
	}
	return base
}

func streamOptions(opts StreamOpts) options {
	return options{
		url:              opts.URL,
		cookies:          opts.CookiesPath,
		format:           opts.FormatSelector,
		flaresolverr:     opts.FlareSolverrURL,
		limitRate:        opts.LimitRate,
		sleepRequests:    sleepSeconds(opts.SleepRequests),
		hlsDir:           opts.HLSDir,
		hlsStartSec:      opts.HLSStartSec,
		hlsStartNumber:   opts.HLSStartNumber,
		videoURL:         opts.VideoURL,
		audioURL:         opts.AudioURL,
		videoHeadersJSON: opts.VideoHeadersJSON,
		audioHeadersJSON: opts.AudioHeadersJSON,
	}
}

// streamAVInputs prefers VideoURL/AudioURL reuse; otherwise resolves via yt-dlp.
func streamAVInputs(ctx context.Context, o options, jarPath, ua string) (videoURL, audioURL string, videoHdr, audioHdr map[string]string, err error) {
	if strings.TrimSpace(o.videoURL) != "" {
		videoURL = strings.TrimSpace(o.videoURL)
		audioURL = strings.TrimSpace(o.audioURL)
		videoHdr = parseHeadersJSON(o.videoHeadersJSON)
		audioHdr = parseHeadersJSON(o.audioHeadersJSON)
		return videoURL, audioURL, videoHdr, audioHdr, nil
	}
	if strings.TrimSpace(o.url) == "" {
		return "", "", nil, nil, appErr(apperrors.CodeInternal, "url required", "")
	}
	return resolveAVURLs(ctx, o.url, o.format, jarPath, ua, o)
}

func parseHeadersJSON(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

func urlsResultFromMap(m map[string]any) UrlsResult {
	if m == nil {
		return UrlsResult{}
	}
	out := UrlsResult{
		Kind:            strMap(m, "kind"),
		URL:             strMap(m, "url"),
		VideoURL:        strMap(m, "video_url"),
		AudioURL:        strMap(m, "audio_url"),
		DurationSeconds: durationSeconds(m),
		MediaType:       strings.TrimSpace(strMap(m, "media_type")),
		IsLive:          boolField(m, "is_live"),
	}
	out.Headers = stringMapField(m, "headers")
	out.VideoHeaders = stringMapField(m, "video_headers")
	out.AudioHeaders = stringMapField(m, "audio_headers")
	return out
}

func strMap(m map[string]any, key string) string {
	s, _ := stringField(m, key)
	return s
}

func stringMapField(m map[string]any, key string) map[string]string {
	raw, ok := m[key].(map[string]string)
	if ok {
		return raw
	}
	anyMap, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, v := range anyMap {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
