package worker

import (
	"context"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func copyFileWorker(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// resolveCookieFallback prepares jar paths for smart cookie usage and runs invoke
// through domains.InvokeWithCookieFallback (anon-first then retry, or cookies-first).
func resolveCookieFallback[T any](
	database *db.DB,
	work, rawURL string,
	sourceID int64,
	invoke func(cookiesPath string) (T, error),
) (T, domains.CookieAttachStatus, error) {
	var zero T
	smart, err := domains.SmartCookiesForURL(database, rawURL)
	if err != nil {
		return zero, domains.CookieAttachStatus{}, apperrors.WithDetail(
			apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
	}
	cookiesFirst, probe, prefer, err := domains.ClaimCookieSmart(database, sourceID, smart)
	if err != nil {
		return zero, domains.CookieAttachStatus{}, apperrors.WithDetail(
			apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
	}
	anonFirst := smart && !cookiesFirst
	jar, hadStored, err := domains.StoredJarForURL(database, work, rawURL, domains.AllowStoredJar(anonFirst, false))
	if err != nil {
		return zero, domains.CookieAttachStatus{}, apperrors.WithDetail(
			apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
	}
	out, attach, err := domains.InvokeWithCookieFallback(anonFirst, hadStored, jar, func() (string, error) {
		path, _, jerr := domains.StoredJarForURL(database, work, rawURL, true)
		return path, jerr
	}, invoke, nil)
	attach.Smart = smart
	attach.PreferCookies = prefer
	attach.Probe = probe
	if err == nil && smart && hadStored && sourceID > 0 {
		if outcome := domains.OutcomeFromAttach(attach, probe); outcome != "" {
			_ = domains.RecordCookieSmartOutcome(database, sourceID, outcome)
		}
	}
	if err != nil && smart && hadStored && sourceID > 0 && attach.State == domains.CookieAttachRetried {
		// Retried means anon failed then cookie pass ran; if cookie pass also failed,
		// still count fallback need (anon required cookies).
		_ = domains.RecordCookieSmartOutcome(database, sourceID, domains.CookieOutcomeFallback)
	}
	return out, attach, err
}

func persistCookieAttach(d Deps, taskID int64, st domains.CookieAttachStatus) {
	if d.Library == nil || d.Library.Queue == nil {
		return
	}
	_ = d.Library.Queue.MergeDetailJSON(taskID, map[string]any{domains.DetailKeyCookieAttach: st})
}

func listEntries(ctx context.Context, d Deps, url, jar string, playlistEnd int, lim settings.DomainLimits) ([]ytdlp.Entry, error) {
	if d.YtDlp == nil {
		return nil, apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(url))
	if err != nil {
		return nil, err
	}
	user, pass := ytdlpAuth(d.Library.DB, url)
	return d.YtDlp.List(ctx, ytdlp.ListOpts{
		URL: url, CookiesPath: jar, Username: user, Password: pass,
		PlaylistEnd: playlistEnd, FlareSolverrURL: flare,
		LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
	})
}

// listEntriesWithCookieFallback lists with smart cookie anon→retry / cookies-first policy.
func listEntriesWithCookieFallback(ctx context.Context, d Deps, work, url string, sourceID int64, playlistEnd int, lim settings.DomainLimits) ([]ytdlp.Entry, domains.CookieAttachStatus, error) {
	return resolveCookieFallback(d.Library.DB, work, url, sourceID, func(jar string) ([]ytdlp.Entry, error) {
		return listEntries(ctx, d, url, jar, playlistEnd, lim)
	})
}

func downloadMedia(ctx context.Context, d Deps, opts ytdlp.DownloadOpts) (string, error) {
	if d.YtDlp == nil {
		return "", apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(opts.URL))
	if err != nil {
		return "", err
	}
	opts.FlareSolverrURL = flare
	opts.Username, opts.Password = ytdlpAuth(d.Library.DB, opts.URL)
	return d.YtDlp.Download(ctx, opts)
}

func fetchSidecars(ctx context.Context, d Deps, opts ytdlp.SidecarsOpts) (infoPath, thumbPath string, subPaths []string, err error) {
	if d.YtDlp == nil {
		return "", "", nil, nil
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(opts.URL))
	if err != nil {
		return "", "", nil, err
	}
	opts.FlareSolverrURL = flare
	opts.Username, opts.Password = ytdlpAuth(d.Library.DB, opts.URL)
	return d.YtDlp.FetchSidecars(ctx, opts)
}

func fetchSidecarsWithCookieFallback(ctx context.Context, d Deps, work string, sourceID int64, opts ytdlp.SidecarsOpts) (infoPath, thumbPath string, subPaths []string, attach domains.CookieAttachStatus, err error) {
	type side struct {
		info, thumb string
		subs        []string
	}
	out, attach, err := resolveCookieFallback(d.Library.DB, work, opts.URL, sourceID, func(jar string) (side, error) {
		o := opts
		o.CookiesPath = jar
		info, thumb, subs, ferr := fetchSidecars(ctx, d, o)
		return side{info: info, thumb: thumb, subs: subs}, ferr
	})
	return out.info, out.thumb, out.subs, attach, err
}

func resolveEntry(ctx context.Context, d Deps, opts ytdlp.ResolveOpts) (ytdlp.Entry, error) {
	if d.YtDlp == nil {
		return ytdlp.Entry{}, apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	if strings.TrimSpace(opts.FlareSolverrURL) == "" {
		flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(opts.URL))
		if err != nil {
			return ytdlp.Entry{}, err
		}
		opts.FlareSolverrURL = flare
	}
	opts.Username, opts.Password = ytdlpAuth(d.Library.DB, opts.URL)
	return d.YtDlp.Resolve(ctx, opts)
}

func resolveEntryWithCookieFallback(ctx context.Context, d Deps, work string, sourceID int64, opts ytdlp.ResolveOpts) (ytdlp.Entry, domains.CookieAttachStatus, error) {
	return resolveCookieFallback(d.Library.DB, work, opts.URL, sourceID, func(jar string) (ytdlp.Entry, error) {
		o := opts
		o.CookiesPath = jar
		return resolveEntry(ctx, d, o)
	})
}

func dumpPlaylistInfo(ctx context.Context, d Deps, opts ytdlp.ListOpts) (map[string]any, error) {
	if d.YtDlp == nil {
		return nil, apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	if strings.TrimSpace(opts.FlareSolverrURL) == "" {
		flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(opts.URL))
		if err != nil {
			return nil, err
		}
		opts.FlareSolverrURL = flare
	}
	opts.Username, opts.Password = ytdlpAuth(d.Library.DB, opts.URL)
	return d.YtDlp.DumpPlaylistInfo(ctx, opts)
}

func dumpPlaylistInfoWithCookieFallback(ctx context.Context, d Deps, work string, sourceID int64, opts ytdlp.ListOpts) (map[string]any, domains.CookieAttachStatus, error) {
	return resolveCookieFallback(d.Library.DB, work, opts.URL, sourceID, func(jar string) (map[string]any, error) {
		o := opts
		o.CookiesPath = jar
		return dumpPlaylistInfo(ctx, d, o)
	})
}

func ytdlpAuth(database *db.DB, rawURL string) (username, password string) {
	creds, err := settings.CredentialsForURL(database, rawURL)
	if err != nil {
		return "", ""
	}
	return creds.Username, creds.Password
}
