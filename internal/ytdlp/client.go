package ytdlp

import (
	"context"
	"strconv"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
)

// Client invokes yt-dlp in-process for Creatorr.
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
	Username        string
	Password        string
	PlaylistEnd     int // 0 = no cap
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
}

// ResolveOpts controls Resolve (single-video metadata).
type ResolveOpts struct {
	URL             string
	CookiesPath     string
	Username        string
	Password        string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
}

// DownloadOpts controls Download.
type DownloadOpts struct {
	URL             string
	OutDir          string
	FormatSelector  string
	CookiesPath     string
	Username        string
	Password        string
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
	Username        string
	Password        string
	FlareSolverrURL string
	LimitRate       string
	SleepRequests   float64
	SubLangs        []string
	SubAuto         bool
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
		username:      opts.Username,
		password:      opts.Password,
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
		username:      opts.Username,
		password:      opts.Password,
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
		username:      opts.Username,
		password:      opts.Password,
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

// Download fetches media into OutDir (no remux) and returns the media path.
func (c *Client) Download(ctx context.Context, opts DownloadOpts) (string, error) {
	o := options{
		url:           opts.URL,
		outdir:        opts.OutDir,
		cookies:       opts.CookiesPath,
		username:      opts.Username,
		password:      opts.Password,
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
		username:      opts.Username,
		password:      opts.Password,
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
