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

func ytdlpAuth(database *db.DB, rawURL string) (username, password string) {
	creds, err := settings.CredentialsForURL(database, rawURL)
	if err != nil {
		return "", ""
	}
	return creds.Username, creds.Password
}
