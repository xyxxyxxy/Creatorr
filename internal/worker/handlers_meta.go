package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

func RefreshSidecarsHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "sidecar refresh deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeScanFailed, "sidecar refresh missing video_id")
		}
		v, err := d.Library.GetVideo(t.VideoID.Int64)
		if err != nil {
			return err
		}
		url := ""
		if v.SourceURL.Valid {
			url = strings.TrimSpace(v.SourceURL.String)
		}
		if url == "" {
			return apperrors.New(apperrors.CodeResolveFailed, "video has no source_url")
		}
		url = library.DownloadURL(url, v.RemoteID)
		if d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
		}
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-side-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		progress("Resolving cookies…", ptrFloat(0.1))
		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, url)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}
		if err := refreshSidecars(ctx, d, work, jar, url, t.Domain, t.VideoID.Int64, t.ID, progress, false); err != nil {
			return err
		}
		if library.TaskPayloadMaturity(t.Payload) {
			if err := d.Library.MarkSidecarsAcquired(t.VideoID.Int64); err != nil {
				return err
			}
			_ = d.Library.AddVideoHistory(t.VideoID.Int64, "maturity_sidecars_refreshed", "Maturity sidecar refresh", map[string]any{}, t.ID)
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

// RescanMetadataHandler refreshes metadata for existing videos only (no discovery).
func RescanMetadataHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "metadata rescan deps missing")
		}
		if t.VideoID.Valid {
			return metadataRescanOne(ctx, d, t, progress)
		}
		if !t.SeriesID.Valid {
			return apperrors.New(apperrors.CodeScanFailed, "metadata rescan missing series_id or video_id")
		}
		return metadataRescanSeries(ctx, d, t, progress)
	}
}

func metadataRescanOne(ctx context.Context, d Deps, t *queue.Task, progress func(msg string, pct *float64)) error {
	v, err := d.Library.GetVideo(t.VideoID.Int64)
	if err != nil {
		return err
	}
	url := ""
	if v.SourceURL.Valid {
		url = strings.TrimSpace(v.SourceURL.String)
	}
	if url == "" {
		return apperrors.New(apperrors.CodeResolveFailed, "video has no source_url")
	}
	url = library.DownloadURL(url, v.RemoteID)
	if d.YtDlp == nil {
		return apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	tmpRoot := d.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	work, err := os.MkdirTemp(tmpRoot, "creatorr-meta-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	progress("Fetching metadata…", ptrFloat(0.05))
	jar, err := domains.TempJarForNonDownload(d.Library.DB, work, url)
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, t.Domain)
	if err != nil {
		return err
	}
	lim, _ := settings.LimitsForDomain(d.Library.DB, t.Domain)
	e, err := resolveEntry(ctx, d, ytdlp.ResolveOpts{
		URL: url, CookiesPath: jar, FlareSolverrURL: flare,
		LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
	})
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = v.RemoteID
	}
	srcID := int64(0)
	if v.SourceID.Valid {
		srcID = v.SourceID.Int64
	}
	vid, ok, err := d.Library.RefreshListed(v.SeriesID, library.EntryFromYtDlp(e, srcID), t.ID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(apperrors.CodeNotFound, "video not in index")
	}
	if library.TaskPayloadGapFill(t.Payload) {
		_ = d.Library.SoftFillVideoFromEntry(vid, e, t.ID)
	}
	progress("Refreshing sidecars…", ptrFloat(0.7))
	if err := refreshSidecars(ctx, d, work, jar, url, t.Domain, vid, t.ID, progress, library.TaskPayloadGapFill(t.Payload)); err != nil {
		return err
	}
	progress("Done", ptrFloat(1))
	return nil
}

func refreshSidecars(ctx context.Context, d Deps, work, jar, url, domain string, videoID, taskID int64, progress func(msg string, pct *float64), gapFill bool) error {
	_, hasFile, err := d.Library.HasPackAnchor(videoID)
	if err != nil {
		return err
	}
	if !hasFile {
		return nil
	}
	sideWork := filepath.Join(work, fmt.Sprintf("side-%d", videoID))
	if err := os.MkdirAll(sideWork, 0o755); err != nil {
		return err
	}
	lim, _ := settings.LimitsForDomain(d.Library.DB, domain)
	subOpts, _ := settings.GetSubtitleOpts(d.Library.DB)
	if progress != nil {
		progress("Fetching sidecars…", ptrFloat(0.4))
	}
	infoPath, thumbPath, subPaths, err := fetchSidecars(ctx, d, ytdlp.SidecarsOpts{
		URL: url, CookiesPath: jar, OutDir: sideWork,
		LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
		SubLangs: subOpts.Langs, SubAuto: subOpts.Auto,
	})
	if err != nil {
		return err
	}
	subPaths = library.MarkAutoSubtitleFiles(subPaths, infoPath)
	_ = infoPath // info.json is download-time provenance; never rewrite on sidecar refresh
	if progress != nil {
		progress("Writing sidecars…", ptrFloat(0.85))
	}
	bundle := library.SidecarBundle{SubSrcs: subPaths, ThumbSrc: thumbPath, GapFill: gapFill}
	return d.Library.RefreshDiskSidecars(videoID, bundle, taskID)
}

func metadataRescanSeries(ctx context.Context, d Deps, t *queue.Task, progress func(msg string, pct *float64)) error {
	seriesID := t.SeriesID.Int64
	sources, err := d.Library.MonitoredSources(seriesID)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return apperrors.New(apperrors.CodeScanFailed, "no sources")
	}

	tmpRoot := d.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	work, err := os.MkdirTemp(tmpRoot, "creatorr-meta-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	var refreshed, skippedNew, sidecars, okSources int
	var lastErr error
	n := len(sources)
	for i, src := range sources {
		label := src.URL
		if src.Label.Valid && src.Label.String != "" {
			label = src.Label.String
		}
		progress(fmt.Sprintf("Listing source %d/%d: %s", i+1, n, label), ptrFloat(float64(i)/float64(n)))

		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, src.URL)
		if err != nil {
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, err.Error(), map[string]any{
				"mode": library.SourceHistModeRescanMetadata,
				"code": apperrors.CodeCookieInvalid,
			}, t.ID)
			lastErr = apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
			continue
		}
		domain := queue.DomainFromURL(src.URL)
		lim, _ := settings.LimitsForDomain(d.Library.DB, domain)
		entries, err := listEntries(ctx, d, src.URL, jar, 0, lim)
		if err != nil {
			code, msg := classify(err)
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, msg+": "+err.Error(), map[string]any{
				"mode": library.SourceHistModeRescanMetadata,
				"code": code,
			}, t.ID)
			lastErr = err
			continue
		}
		okSources++
		var updatedIDs []int64
		for _, e := range entries {
			vid, ok, err := d.Library.RefreshListed(seriesID, library.EntryFromYtDlp(e, src.ID), t.ID)
			if err != nil {
				lastErr = err
				continue
			}
			if !ok {
				skippedNew++
				continue
			}
			refreshed++
			updatedIDs = append(updatedIDs, vid)
			url := e.WebpageURL
			if url == "" {
				if v, err := d.Library.GetVideo(vid); err == nil && v.SourceURL.Valid {
					url = v.SourceURL.String
				}
			}
			if url == "" {
				continue
			}
			url = library.DownloadURL(url, e.ID)
			if err := refreshSidecars(ctx, d, work, jar, url, domain, vid, t.ID, progress, false); err != nil {
				lastErr = err
				continue
			}
			if path, has, _ := d.Library.HasVideoFile(vid); has && path != "" {
				sidecars++
			}
		}
		_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanned,
			fmt.Sprintf("Metadata rescan: refreshed %d videos", len(updatedIDs)),
			map[string]any{
				"mode":        library.SourceHistModeRescanMetadata,
				"created":     0,
				"updated":     len(updatedIDs),
				"created_ids": []int64{},
				"updated_ids": updatedIDs,
			}, t.ID)
		progress(fmt.Sprintf("Refreshed from %s", label), ptrFloat(float64(i+1)/float64(n)))
	}
	progress(fmt.Sprintf("Metadata rescan complete: refreshed=%d sidecars=%d skipped_new=%d", refreshed, sidecars, skippedNew), ptrFloat(1))
	if okSources == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

// PrefetchSeriesMetaHandler dumps channel/playlist metadata into a cache draft for the form.
func PrefetchSeriesMetaHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil || d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "series meta prefetch deps missing")
		}
		if !t.SeriesID.Valid {
			return apperrors.New(apperrors.CodeInternal, "series_id required")
		}
		var payload struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		fetchURL := strings.TrimSpace(payload.URL)
		if fetchURL == "" {
			return apperrors.New(apperrors.CodeInternal, "url required")
		}
		progress("Fetching metadata…", ptrFloat(0.1))
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-series-meta-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, fetchURL)
		if err != nil {
			return err
		}
		domain := queue.DomainFromURL(fetchURL)
		flare, err := domains.FlareSolverrURL(d.Library.DB, domain)
		if err != nil {
			return err
		}
		// Interactive: no download_rate_limit / sleep_requests (operator-facing form fetch).
		info, err := dumpPlaylistInfo(ctx, d, ytdlp.ListOpts{
			URL: fetchURL, CookiesPath: jar, PlaylistEnd: 1, FlareSolverrURL: flare,
		})
		if err != nil {
			draft := library.PrefetchDraft{Error: err.Error(), ArtFiles: map[string]string{}}
			_ = d.Library.WritePrefetchDraft(t.SeriesID.Int64, t.ID, draft)
			return err
		}
		progress("Downloading artwork…", ptrFloat(0.6))
		artDir := filepath.Join(work, "art")
		_ = os.MkdirAll(artDir, 0o755)
		draft := library.BuildPrefetchDraftFromInfo(info, artDir)
		// Persist art into series cache so it survives work dir cleanup.
		cacheRoot := strings.TrimSpace(d.Library.CacheDir)
		if cacheRoot == "" {
			cacheRoot = filepath.Join("data", "cache")
		}
		cacheArt := filepath.Join(cacheRoot, "series-meta",
			strconv.FormatInt(t.SeriesID.Int64, 10), fmt.Sprintf("art-%d", t.ID))
		_ = os.MkdirAll(cacheArt, 0o755)
		persisted := map[string]string{}
		for role, src := range draft.ArtFiles {
			ext := filepath.Ext(src)
			if ext == "" {
				ext = ".jpg"
			}
			dest := filepath.Join(cacheArt, role+ext)
			if err := copyFileWorker(src, dest); err == nil {
				persisted[role] = dest
			}
		}
		draft.ArtFiles = persisted
		if err := d.Library.WritePrefetchDraft(t.SeriesID.Int64, t.ID, draft); err != nil {
			return err
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

// ProbeSourceTitleHandler resolves a URL title for the Add-series probe (interactive).
func ProbeSourceTitleHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.YtDlp == nil || d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "probe deps missing")
		}
		var payload struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		fetchURL := strings.TrimSpace(payload.URL)
		if fetchURL == "" {
			return apperrors.New(apperrors.CodeInternal, "url required")
		}
		progress("Probing title…", ptrFloat(0.1))
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-probe-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()
		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, fetchURL)
		if err != nil {
			jar = ""
		}
		flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(fetchURL))
		if err != nil {
			return err
		}
		authUser, authPass := ytdlpAuth(d.Library.DB, fetchURL)
		e, err := d.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
			URL: fetchURL, CookiesPath: jar, Username: authUser, Password: authPass,
			FlareSolverrURL: flare,
		})
		if err != nil {
			return err
		}
		title := strings.TrimSpace(e.Title)
		if title == "" {
			return apperrors.New(apperrors.CodeResolveFailed, "empty title")
		}
		_ = d.Library.Queue.SetDetail(t.ID, title)
		progress(title, ptrFloat(1))
		return nil
	}
}

// PrefetchAddSeriesHandler dumps channel/playlist metadata into cache/add-series/{token}/.
func PrefetchAddSeriesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil || d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "add series prefetch deps missing")
		}
		var payload struct {
			URL        string `json:"url"`
			DraftToken string `json:"draft_token"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		fetchURL := strings.TrimSpace(payload.URL)
		token := strings.TrimSpace(payload.DraftToken)
		if fetchURL == "" {
			return apperrors.New(apperrors.CodeInternal, "url required")
		}
		if token == "" {
			return apperrors.New(apperrors.CodeInternal, "draft_token required")
		}
		progress("Fetching metadata…", ptrFloat(0.1))
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-add-series-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, fetchURL)
		if err != nil {
			jar = ""
		}
		domain := queue.DomainFromURL(fetchURL)
		flare, err := domains.FlareSolverrURL(d.Library.DB, domain)
		if err != nil {
			draft := library.PrefetchDraft{Error: err.Error(), ArtFiles: map[string]string{}}
			_ = d.Library.WriteAddSeriesDraft(token, draft)
			return err
		}
		// Interactive: no download_rate_limit / sleep_requests (operator-facing form fetch).
		info, err := dumpPlaylistInfo(ctx, d, ytdlp.ListOpts{
			URL: fetchURL, CookiesPath: jar, PlaylistEnd: 1, FlareSolverrURL: flare,
		})
		if err != nil {
			draft := library.PrefetchDraft{Error: err.Error(), ArtFiles: map[string]string{}}
			_ = d.Library.WriteAddSeriesDraft(token, draft)
			return err
		}
		progress("Downloading artwork…", ptrFloat(0.6))
		artDir := filepath.Join(work, "art")
		_ = os.MkdirAll(artDir, 0o755)
		draft := library.BuildPrefetchDraftFromInfo(info, artDir)
		if err := d.Library.WriteAddSeriesDraft(token, draft); err != nil {
			return err
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

// PrefetchAddVideoHandler resolves a video URL into cache/add-video/{token}/ for Add video.
func PrefetchAddVideoHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil || d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "add video prefetch deps missing")
		}
		var payload struct {
			URL        string `json:"url"`
			DraftToken string `json:"draft_token"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		fetchURL := strings.TrimSpace(payload.URL)
		token := strings.TrimSpace(payload.DraftToken)
		if fetchURL == "" {
			return apperrors.New(apperrors.CodeInternal, "url required")
		}
		if token == "" {
			return apperrors.New(apperrors.CodeInternal, "draft_token required")
		}
		progress("Fetching metadata…", ptrFloat(0.1))
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-add-video-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, fetchURL)
		if err != nil {
			draft := library.AddVideoDraft{Error: err.Error()}
			_ = d.Library.WriteAddVideoDraft(token, draft)
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}
		domain := queue.DomainFromURL(fetchURL)
		flare, err := domains.FlareSolverrURL(d.Library.DB, domain)
		if err != nil {
			draft := library.AddVideoDraft{Error: err.Error()}
			_ = d.Library.WriteAddVideoDraft(token, draft)
			return err
		}
		// Interactive: no download_rate_limit / sleep_requests (operator-facing form fetch).
		e, err := resolveEntry(ctx, d, ytdlp.ResolveOpts{
			URL: fetchURL, CookiesPath: jar, FlareSolverrURL: flare,
		})
		if err != nil {
			draft := library.AddVideoDraft{Error: err.Error()}
			_ = d.Library.WriteAddVideoDraft(token, draft)
			return err
		}
		draft := library.BuildAddVideoDraftFromEntry(e, fetchURL)
		library.EnsureAddVideoDraftUploadDate(&draft)
		if err := d.Library.WriteAddVideoDraft(token, draft); err != nil {
			return err
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

// PrefetchVideoMetaHandler resolves a video URL into a cache draft for the metadata form.
func PrefetchVideoMetaHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil || d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "video meta prefetch deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeInternal, "video_id required")
		}
		videoID := t.VideoID.Int64
		var payload struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		fetchURL := strings.TrimSpace(payload.URL)
		if fetchURL == "" {
			return apperrors.New(apperrors.CodeInternal, "url required")
		}
		progress("Fetching metadata…", ptrFloat(0.1))
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-video-meta-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		jar, err := domains.TempJarForNonDownload(d.Library.DB, work, fetchURL)
		if err != nil {
			draft := library.VideoPrefetchDraft{Error: err.Error()}
			_ = d.Library.WriteVideoPrefetchDraft(videoID, t.ID, draft)
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}
		domain := queue.DomainFromURL(fetchURL)
		flare, err := domains.FlareSolverrURL(d.Library.DB, domain)
		if err != nil {
			return err
		}
		// Interactive: no download_rate_limit / sleep_requests (operator-facing form fetch).
		e, err := resolveEntry(ctx, d, ytdlp.ResolveOpts{
			URL: fetchURL, CookiesPath: jar, FlareSolverrURL: flare,
		})
		if err != nil {
			draft := library.VideoPrefetchDraft{Error: err.Error()}
			_ = d.Library.WriteVideoPrefetchDraft(videoID, t.ID, draft)
			return err
		}
		draft := library.BuildVideoPrefetchDraftFromEntry(e)
		progress("Downloading thumbnail…", ptrFloat(0.7))
		d.Library.PersistVideoPrefetchThumb(videoID, t.ID, &draft)

		if err := d.Library.WriteVideoPrefetchDraft(videoID, t.ID, draft); err != nil {
			return err
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}
