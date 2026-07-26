package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// Deps wires real task handlers.
type Deps struct {
	Library *library.Store
	TmpRoot string        // optional parent for cookie/work temps
	YtDlp   *ytdlp.Client // required
	Events  *events.Hub
}

// DefaultHandlers returns scan/download implementations plus stubs for other kinds.
func DefaultHandlers(d Deps) map[string]TaskHandler {
	stubs := StubHandlers()
	out := make(map[string]TaskHandler, len(stubs))
	for k, v := range stubs {
		out[k] = v
	}
	out[queue.KindScan] = ScanHandler(d)
	out[queue.KindDownload] = DownloadHandler(d)
	out[queue.KindCacheBeginning] = CacheBeginningHandler(d)
	out[queue.KindPackStream] = PackStreamHandler(d)
	out[queue.KindRescanMetadata] = RescanMetadataHandler(d)
	out[queue.KindRefreshSidecars] = RefreshSidecarsHandler(d)
	out[queue.KindImport] = ImportHandler(d)
	out[queue.KindPrefetchSeriesMeta] = PrefetchSeriesMetaHandler(d)
	out[queue.KindPrefetchVideoMeta] = PrefetchVideoMetaHandler(d)
	out[queue.KindPrefetchAddSeries] = PrefetchAddSeriesHandler(d)
	out[queue.KindPrefetchAddVideo] = PrefetchAddVideoHandler(d)
	out[queue.KindSyncFiles] = SyncFilesHandler(d)
	out[queue.KindRetentionDelete] = RetentionDeleteHandler(d)
	out[queue.KindRenameEpisodes] = RenameEpisodesHandler(d)
	out[queue.KindRegenerateNFO] = RegenerateNFOHandler(d)
	out[queue.KindRegenerateStrm] = RegenerateStrmHandler(d)
	out[queue.KindClearBeginningCache] = ClearBeginningCacheHandler(d)
	out[queue.KindClearPlaybackCache] = ClearPlaybackCacheHandler(d)
	out[queue.KindDeleteFiles] = DeleteFilesHandler(d)
	out[queue.KindSponsorblockCut] = SponsorblockCutHandler(d)
	out[queue.KindMediaVerify] = MediaVerifyHandler(d)
	return out
}

// RegenerateNFOHandler rewrites episode/series NFOs (resumable).
func RegenerateNFOHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		rewrote, skipped, failed, err := d.Library.NFORegeneratePass(ctx, t, progress)
		if err != nil {
			return err
		}
		d.Library.RecordNFORegenerateActivity(t.ID, rewrote, skipped, failed)
		return nil
	}
}

// RegenerateStrmHandler rewrites .strm URL lines (resumable).
func RegenerateStrmHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		rewrote, skipped, failed, err := d.Library.StrmRegeneratePass(ctx, t, progress)
		if err != nil {
			return err
		}
		d.Library.RecordStrmRegenerateActivity(t.ID, rewrote, skipped, failed)
		return nil
	}
}

// ClearBeginningCacheHandler wipes download-beginning caches.
func ClearBeginningCacheHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		cleared, err := d.Library.ClearBeginningCachePass(ctx, t, progress)
		if err != nil {
			return err
		}
		d.Library.RecordClearBeginningCacheActivity(t.ID, cleared)
		return nil
	}
}

// ClearPlaybackCacheHandler wipes progressive on-play stream caches.
func ClearPlaybackCacheHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		cleared, err := d.Library.ClearPlaybackCachePass(ctx, t, progress)
		if err != nil {
			return err
		}
		d.Library.RecordClearPlaybackCacheActivity(t.ID, cleared)
		return nil
	}
}

// DeleteFilesHandler removes on-disk library files then MarkDeleted / DELETE series.
func DeleteFilesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if err := d.Library.FileDeletePass(ctx, t, progress); err != nil {
			return err
		}
		d.Library.RecordFileDeleteActivity(t)
		return nil
	}
}

// SyncFilesHandler runs FileSyncPass on the system lane.
func SyncFilesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "sync files deps missing")
		}
		res, err := d.Library.FileSyncPass(t.ID, progress)
		if err != nil {
			return err
		}
		if len(res.MissingIDs) == 0 && len(res.ExternallyChangedIDs) == 0 &&
			len(res.SidecarMissing) == 0 && len(res.SidecarChanged) == 0 {
			return nil
		}
		missing := fileSyncIssueItems(d.Library, res.MissingIDs)
		missing = append(missing, fileSyncSidecarIssueItems(d.Library, res.SidecarMissing)...)
		changed := fileSyncIssueItems(d.Library, res.ExternallyChangedIDs)
		changed = append(changed, fileSyncSidecarIssueItems(d.Library, res.SidecarChanged)...)
		_ = notify.FileSyncIssues(ctx, d.Library.DB, t.ID, missing, changed)
		return nil
	}
}

func fileSyncIssueItems(lib *library.Store, ids []int64) []notify.FileSyncIssueItem {
	out := make([]notify.FileSyncIssueItem, 0, len(ids))
	for _, id := range ids {
		it := notify.FileSyncIssueItem{}
		v, err := lib.GetVideo(id)
		if err == nil && v != nil {
			it.Title = v.Title
			if ser, serr := lib.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
				it.Series = ser.Title
			}
		}
		out = append(out, it)
	}
	return out
}

func fileSyncSidecarIssueItems(lib *library.Store, issues []library.FileSyncSidecarIssue) []notify.FileSyncIssueItem {
	out := make([]notify.FileSyncIssueItem, 0, len(issues))
	for _, si := range issues {
		it := notify.FileSyncIssueItem{Detail: sidecarIssueDetail(si.Kind, si.Path)}
		v, err := lib.GetVideo(si.VideoID)
		if err == nil && v != nil {
			it.Title = v.Title
			if ser, serr := lib.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
				it.Series = ser.Title
			}
		}
		out = append(out, it)
	}
	return out
}

func sidecarIssueDetail(kind, path string) string {
	kind = strings.TrimSpace(kind)
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return kind
	}
	if kind == "" {
		return base
	}
	return kind + ": " + base
}

// RetentionDeleteHandler runs RetentionPurgePass on the system lane.
func RetentionDeleteHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		_ = ctx
		_, err := d.Library.RetentionPurgePass(t.ID, progress)
		return err
	}
}

// RenameEpisodesHandler renames packed episode files to the snapshot formats.
func RenameEpisodesHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		renamed, skipped, failed, err := d.Library.ApplyEpisodeNamingPass(ctx, t, progress)
		if err != nil {
			return err
		}
		msg := library.ApplyNamingMessage(renamed, skipped, failed)
		progress(msg, ptrFloat(1))
		detail, _ := json.Marshal(map[string]any{
			"renamed": renamed, "skipped_busy": skipped, "failed": failed,
		})
		_ = d.Library.Queue.SetDetail(t.ID, string(detail))
		return nil
	}
}

// PackStreamHandler writes .strm + NFO for stream proxy delivery.
func PackStreamHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "pack stream deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodePackFailed, "pack stream task missing video_id")
		}
		videoID := t.VideoID.Int64
		progress("Resolving stream…", nil)
		resolved, err := resolveStreamForPack(ctx, d, videoID)
		runtimeSec := 0
		if err == nil {
			runtimeSec = resolved.RuntimeSec
			if resolved.IsLive {
				return apperrors.New(apperrors.CodeLiveBroadcastSkipped, "currently live")
			}
			ignored, ierr := ApplyPackStreamAutoIgnore(d.Library, resolved.SeriesID, videoID, t.ID, resolved.MediaType)
			if ierr != nil {
				return ierr
			}
			if ignored {
				msg := "Ignored (auto-ignore media type: " + library.NormalizeMediaType(resolved.MediaType) + ")"
				progress(msg, nil)
				if d.Library.Queue != nil {
					_ = d.Library.Queue.UpdateProgress(t.ID, msg, nil)
				}
				// No pack_stream files, no cache_beginning.
				return nil
			}
			_ = d.Library.SetStreamMeta(videoID, resolved.Kind, runtimeSec, 0, 0, 0)
			if runtimeSec > 0 {
				_ = d.Library.SetDurationSeconds(videoID, runtimeSec)
			}
		}
		progress("Writing stream files…", nil)
		var subSrcs []string
		subOpts, _ := settings.GetSubtitleOpts(d.Library.DB)
		if len(subOpts.Langs) > 0 {
			progress("Fetching subtitles…", nil)
			fetched, cleanup, ferr := fetchPackStreamSubs(ctx, d, videoID, t.Domain)
			if cleanup != nil {
				defer cleanup()
			}
			if ferr == nil {
				subSrcs = fetched
			}
		}
		sbWarn := ""
		var streamPlan sponsorblock.AppliedCutPlan
		dlctx, _ := d.Library.PrepareDownload(videoID)
		if dlctx != nil && len(sponsorblock.NormalizeCategoryList(dlctx.Profile.SponsorBlockRemove)) > 0 {
			progress("SponsorBlock…", nil)
			plan, warn, _ := sponsorblock.BuildStreamPlan(ctx, dlctx.URL, dlctx.Video.RemoteID, dlctx.Profile.SponsorBlockConfig(), float64(runtimeSec))
			sbWarn = warn
			streamPlan = plan
			if plan.HasCuts() {
				playDur := sponsorblock.PlaybackDuration(float64(runtimeSec), plan)
				if playDur > 0 {
					runtimeSec = int(playDur + 0.5)
				}
				sponsorblock.RemapSubtitleFiles(subSrcs, plan.Cuts(), plan.CardDurationSec, plan.InfoCards)
			}
		}
		if err := d.Library.PackStreamForVideo(videoID, t.ID, runtimeSec, subSrcs); err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "pack stream failed"), err.Error())
		}
		if strm, ok, _ := d.Library.HasPackAnchor(videoID); ok {
			if streamPlan.HasCuts() {
				if pp, err := sponsorblock.WritePlan(strm, streamPlan); err == nil {
					_ = d.Library.RegisterFileKind(videoID, pp, "sponsorblock")
				}
			} else {
				// Clear stale play-skip sidecar when remove empty / no cuts.
				old := sponsorblock.PlanPath(strm)
				_ = os.Remove(old)
				_, _ = d.Library.DB.SQL.Exec(`DELETE FROM files WHERE video_id = ? AND kind = 'sponsorblock'`, videoID)
			}
		}
		if library.TaskPayloadMaturity(t.Payload) {
			_ = d.Library.AddVideoHistory(videoID, "maturity_repacked", "Maturity stream re-pack", map[string]any{}, t.ID)
		}
		if _, err := d.Library.EnqueueCacheBeginning(videoID); err != nil {
			_ = err
		}
		if sbWarn != "" {
			progress(sbWarn, nil)
			if d.Library.Queue != nil {
				_ = d.Library.Queue.UpdateProgress(t.ID, sbWarn, nil)
			}
		} else {
			progress("Done", nil)
		}
		return nil
	}
}

// ApplyPackStreamAutoIgnore marks the video ignored when media_type is listed on the series.
// Empty media_type never auto-ignores. Returns ignored=true when pack + beginning must not run.
func ApplyPackStreamAutoIgnore(lib *library.Store, seriesID, videoID, taskID int64, mediaType string) (ignored bool, err error) {
	if lib == nil {
		return false, nil
	}
	mt := library.NormalizeMediaType(mediaType)
	if mt == "" {
		return false, nil
	}
	_ = lib.SetMediaType(videoID, mt)
	exclude, xerr := lib.SeriesAutoIgnoreMediaTypes(seriesID)
	if xerr != nil || !library.MediaTypeExcluded(exclude, mt) {
		return false, nil
	}
	if err := lib.MarkIgnoredMediaType(videoID, taskID, mt); err != nil {
		return false, err
	}
	return true, nil
}

// CacheBeginningHandler caches the first N seconds of a streamable video under CacheDir.
func CacheBeginningHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "download beginning deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeDownloadFailed, "download beginning task missing video_id")
		}
		videoID := t.VideoID.Int64
		wantSec, err := settings.CacheBeginningSeconds(d.Library.DB)
		if err != nil {
			return err
		}
		if wantSec <= 0 {
			progress("Disabled", ptrFloat(1))
			return nil
		}
		cur, err := d.Library.GetVideo(videoID)
		if err != nil {
			return err
		}
		if cur.Status != "streamable" {
			return apperrors.New(apperrors.CodeDownloadFailed, "video not streamable")
		}
		dlctx, err := d.Library.PrepareDownload(videoID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(dlctx.URL) == "" {
			return apperrors.New(apperrors.CodeDownloadFailed, "video has no source_url")
		}
		if d.YtDlp == nil {
			return apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
		}
		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-begin-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		progress("Resolving cookies…", ptrFloat(0.05))
		jar, err := cookies.TempJarForURL(d.Library.DB, work, dlctx.URL)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}
		flare, err := domains.FlareSolverrURL(d.Library.DB, t.Domain)
		if err != nil {
			return err
		}
		lim, _ := settings.LimitsForDomain(d.Library.DB, t.Domain)
		progress("Resolving stream URLs…", ptrFloat(0.15))
		urls, err := d.YtDlp.FetchUrls(ctx, ytdlp.UrlsOpts{
			URL:             dlctx.URL,
			FormatSelector:  dlctx.FormatSelector,
			CookiesPath:     jar,
			FlareSolverrURL: flare,
			LimitRate:       lim.DownloadRateLimit,
			SleepRequests:   lim.SleepRequests,
		})
		if err != nil {
			return err
		}
		if urls.Kind != ytdlp.UrlsKindPipe {
			// Soft skip: beginning cache is for pipe mux only (CDN HLS/progressive skip beginning).
			_ = d.Library.SetStreamURLsKind(videoID, urls.Kind)
			progress("Skipped (CDN direct - no beginning needed)", ptrFloat(1))
			return nil
		}
		_ = d.Library.SetStreamURLsKind(videoID, ytdlp.UrlsKindPipe)

		sourceDur := urls.DurationSeconds
		if sourceDur <= 0 && cur.DurationSeconds.Valid {
			sourceDur = float64(cur.DurationSeconds.Int64)
		}
		plan, hasPlan := sponsorblock.AppliedCutPlan{}, false
		if strm, ok, _ := d.Library.HasPackAnchor(videoID); ok {
			if p, found, _ := sponsorblock.ReadPlan(strm); found && p.HasCuts() {
				plan, hasPlan = p, true
			}
		}

		hlsDir := filepath.Join(work, "hls")
		if err := os.MkdirAll(hlsDir, 0o755); err != nil {
			return err
		}
		progress("Muxing beginning…", ptrFloat(0.25))

		var got float64
		handoffSrc := float64(wantSec)
		if hasPlan {
			windows := sponsorblock.TipSourceWindows(float64(wantSec), sourceDur, plan)
			var keepWins []sponsorblock.PlayPiece
			for _, w := range windows {
				if w.Kind == "keep" && w.PlayDur > 0.05 {
					keepWins = append(keepWins, w)
				}
			}
			if len(keepWins) == 0 {
				progress("Skipped (beginning window empty after SponsorBlock)", ptrFloat(1))
				return nil
			}
			segNum := 0
			var playlist strings.Builder
			playlist.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
			for wi, win := range keepWins {
				partDir := filepath.Join(work, fmt.Sprintf("hls-part-%d", wi))
				_ = os.MkdirAll(partDir, 0o755)
				muxCtx, cancel := context.WithCancel(ctx)
				opts := ytdlp.StreamOptsFromUrls(ytdlp.StreamOpts{
					URL: dlctx.URL, FormatSelector: dlctx.FormatSelector,
					CookiesPath: jar, FlareSolverrURL: flare, HLSDir: partDir,
					HLSStartSec: win.Start, HLSMaxSec: win.End - win.Start,
					LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
				}, urls)
				done, err := d.YtDlp.StartHLSStream(muxCtx, opts)
				if err != nil {
					cancel()
					return apperrors.WithDetail(apperrors.New(apperrors.CodeDownloadFailed, "hls start failed"), err.Error())
				}
				deadline := time.Now().Add(3 * time.Minute)
				wantPart := win.PlayDur
				for {
					sum := playlistEXTINFSum(filepath.Join(partDir, "index.m3u8"))
					if sum >= wantPart*0.85 {
						break
					}
					select {
					case <-done:
						sum = playlistEXTINFSum(filepath.Join(partDir, "index.m3u8"))
						if sum >= wantPart*0.4 {
							goto partDone
						}
						cancel()
						return apperrors.New(apperrors.CodeDownloadFailed, "hls beginning part ended early")
					case <-time.After(200 * time.Millisecond):
						if time.Now().After(deadline) {
							cancel()
							return apperrors.New(apperrors.CodeDownloadFailed, "timeout waiting for beginning part")
						}
					case <-ctx.Done():
						cancel()
						return ctx.Err()
					}
				}
			partDone:
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
				}
				n, partSum, err := appendBeginningSegments(partDir, hlsDir, &playlist, &segNum)
				_ = n
				if err != nil {
					return err
				}
				got += partSum
				progress(fmt.Sprintf("Muxing beginning… %.0fs", got), ptrFloat(0.25+0.6*got/float64(wantSec)))
				if got >= float64(wantSec)*0.95 {
					break
				}
			}
			playlist.WriteString("#EXT-X-ENDLIST\n")
			if err := os.WriteFile(filepath.Join(hlsDir, "index.m3u8"), []byte(playlist.String()), 0o644); err != nil {
				return err
			}
			handoffSrc = sponsorblock.SourceOffsetForPlay(got, sourceDur, plan)
		} else {
			muxCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			opts := ytdlp.StreamOptsFromUrls(ytdlp.StreamOpts{
				URL: dlctx.URL, FormatSelector: dlctx.FormatSelector,
				CookiesPath: jar, FlareSolverrURL: flare, HLSDir: hlsDir,
				HLSMaxSec: float64(wantSec) + 2,
				LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
			}, urls)
			done, err := d.YtDlp.StartHLSStream(muxCtx, opts)
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodeDownloadFailed, "hls start failed"), err.Error())
			}
			deadline := time.Now().Add(3 * time.Minute)
			enough := false
			for !enough {
				if err := ctx.Err(); err != nil {
					cancel()
					return err
				}
				got = playlistEXTINFSum(filepath.Join(hlsDir, "index.m3u8"))
				if got >= float64(wantSec) {
					enough = true
					break
				}
				select {
				case err := <-done:
					got = playlistEXTINFSum(filepath.Join(hlsDir, "index.m3u8"))
					if got >= float64(wantSec)*0.5 {
						enough = true
						break
					}
					if err != nil {
						return apperrors.WithDetail(apperrors.New(apperrors.CodeDownloadFailed, "hls ended early"), err.Error())
					}
					return apperrors.New(apperrors.CodeDownloadFailed, "hls ended before beginning duration")
				case <-time.After(200 * time.Millisecond):
					if time.Now().After(deadline) {
						cancel()
						return apperrors.New(apperrors.CodeDownloadFailed, "timeout waiting for beginning segments")
					}
					frac := got / float64(wantSec)
					if frac > 0.95 {
						frac = 0.95
					}
					progress(fmt.Sprintf("Muxing beginning… %.0fs", got), ptrFloat(0.25+0.6*frac))
				}
			}
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			handoffSrc = got
		}
		progress("Persisting beginning…", ptrFloat(0.9))
		dest := d.Library.BeginningDir(videoID)
		_ = os.RemoveAll(dest)
		if err := copyBeginningDir(hlsDir, dest); err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeDownloadFailed, "persist beginning failed"), err.Error())
		}
		if got <= 0 {
			got = float64(wantSec)
		}
		if err := d.Library.WriteBeginningMetaHandoff(videoID, got, handoffSrc); err != nil {
			_ = os.RemoveAll(dest)
			return err
		}
		_ = d.Library.AddVideoHistory(videoID, "beginning_cached", "Beginning cached", map[string]any{
			"duration_seconds": got, "handoff_source_seconds": handoffSrc,
		}, t.ID)
		progress("Done", ptrFloat(1))
		return nil
	}
}

func playlistEXTINFSum(path string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var sum float64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		rest := strings.TrimPrefix(line, "#EXTINF:")
		if i := strings.IndexByte(rest, ','); i >= 0 {
			rest = rest[:i]
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err == nil {
			sum += f
		}
	}
	return sum
}

// appendBeginningSegments copies media segments from partDir into destDir with renumbered names
// and appends EXTINF lines to playlist. segNum is the next segment index.
func appendBeginningSegments(partDir, destDir string, playlist *strings.Builder, segNum *int) (int, float64, error) {
	data, err := os.ReadFile(filepath.Join(partDir, "index.m3u8"))
	if err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, 0, err
	}
	var sum float64
	n := 0
	lines := strings.Split(string(data), "\n")
	var pendingDur string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#EXTINF:") {
			pendingDur = trimmed
			rest := strings.TrimPrefix(trimmed, "#EXTINF:")
			if i := strings.IndexByte(rest, ','); i >= 0 {
				rest = rest[:i]
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil {
				sum += f
			}
			continue
		}
		if pendingDur == "" || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		src := filepath.Join(partDir, trimmed)
		if !fileExistsLocal(src) {
			// URI may be absolute path within dir
			src = filepath.Join(partDir, filepath.Base(trimmed))
		}
		ext := filepath.Ext(trimmed)
		if ext == "" {
			ext = ".ts"
		}
		name := fmt.Sprintf("seg%05d%s", *segNum, ext)
		*segNum++
		b, err := os.ReadFile(src)
		if err != nil {
			return n, sum, err
		}
		if err := os.WriteFile(filepath.Join(destDir, name), b, 0o644); err != nil {
			return n, sum, err
		}
		playlist.WriteString(pendingDur)
		playlist.WriteByte('\n')
		playlist.WriteString(name)
		playlist.WriteByte('\n')
		pendingDur = ""
		n++
	}
	return n, sum, nil
}

func fileExistsLocal(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func copyBeginningDir(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if name == "cookies.txt" {
			continue
		}
		if !strings.HasSuffix(low, ".m3u8") && !strings.HasSuffix(low, ".ts") &&
			!strings.HasSuffix(low, ".m4s") && !strings.HasSuffix(low, ".mp4") {
			continue
		}
		in, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, name), in, 0o644); err != nil {
			return err
		}
	}
	if st, err := os.Stat(filepath.Join(dest, "index.m3u8")); err != nil || st.Size() == 0 {
		return fmt.Errorf("missing index.m3u8 after copy")
	}
	return nil
}

type packStreamResolve struct {
	RuntimeSec int
	MediaType  string
	SeriesID   int64
	Kind       string // urls kind; written only after auto-ignore check
	IsLive     bool
}

func resolveStreamForPack(ctx context.Context, d Deps, videoID int64) (packStreamResolve, error) {
	var out packStreamResolve
	dlctx, err := d.Library.PrepareDownload(videoID)
	if err != nil {
		return out, err
	}
	out.SeriesID = dlctx.Video.SeriesID
	if strings.TrimSpace(dlctx.URL) == "" {
		return out, fmt.Errorf("no url")
	}
	if d.YtDlp == nil {
		return out, apperrors.New(apperrors.CodeInternal, "yt-dlp client missing")
	}
	tmpRoot := d.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	work, err := os.MkdirTemp(tmpRoot, "creatorr-packstream-*")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(work)
	jar, err := cookies.TempJarForURL(d.Library.DB, work, dlctx.URL)
	if err != nil {
		return out, err
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, queue.DomainFromURL(dlctx.URL))
	if err != nil {
		return out, err
	}
	urls, err := d.YtDlp.FetchUrls(ctx, ytdlp.UrlsOpts{
		URL:             dlctx.URL,
		FormatSelector:  dlctx.FormatSelector,
		CookiesPath:     jar,
		FlareSolverrURL: flare,
	})
	if err != nil {
		return out, err
	}
	out.MediaType = library.NormalizeMediaType(urls.MediaType)
	out.IsLive = urls.IsLive
	kind := strings.TrimSpace(strings.ToLower(urls.Kind))
	if kind == "" {
		kind = ytdlp.UrlsKindProgressive
	}
	out.Kind = kind
	if urls.DurationSeconds > 0 {
		out.RuntimeSec = int(urls.DurationSeconds + 0.5)
		return out, nil
	}
	// Fallback: resolve metadata for duration (and media_type / is_live if still unknown).
	e, err := d.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
		URL: dlctx.URL, CookiesPath: jar, FlareSolverrURL: flare,
	})
	if err != nil || e.Duration <= 0 {
		return out, err
	}
	if out.MediaType == "" {
		out.MediaType = library.NormalizeMediaType(e.MediaType)
	}
	if e.IsLive {
		out.IsLive = true
	}
	out.RuntimeSec = int(e.Duration + 0.5)
	return out, nil
}

// ImportHandler installs a file from the import inbox into the series library folder,
// or binds a library orphan in place (no move).
func ImportHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "import deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeImportFailed, "import task missing video_id")
		}
		var payload struct {
			Path    string   `json:"path"`
			Paths   []string `json:"paths"`
			Mode    string   `json:"mode"`
			InPlace bool     `json:"in_place"`
			Verify  bool     `json:"verify"`
			Replace bool     `json:"replace"`
		}
		if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
			return apperrors.New(apperrors.CodeImportFailed, "import task missing path")
		}
		paths := payload.Paths
		if len(paths) == 0 && strings.TrimSpace(payload.Path) != "" {
			paths = []string{strings.TrimSpace(payload.Path)}
		}
		if len(paths) == 0 {
			return apperrors.New(apperrors.CodeImportFailed, "import task missing path")
		}

		firstRole, _ := library.ClassifyImportFile(filepath.Base(paths[0]))
		if payload.Mode == "sidecars" || library.IsImportSidecarRole(firstRole) {
			progress("Attaching sidecars…", ptrFloat(0.5))
			if err := d.Library.AttachSidecarFiles(t.VideoID.Int64, paths, t.ID); err != nil {
				return err
			}
			progress("Done", ptrFloat(1))
			return nil
		}

		srcPath := strings.TrimSpace(paths[0])
		progress("Validating import…", ptrFloat(0.1))
		abs, inPlace, err := d.Library.ValidateImportSourcePath(srcPath)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "invalid import path"), err.Error())
		}
		if payload.InPlace {
			inPlace = true
		}
		if path, ok, err := d.Library.HasVideoFile(t.VideoID.Int64); err != nil {
			return err
		} else if ok && !payload.Replace {
			return apperrors.New(apperrors.CodeConflict, "video already has a file on disk")
		} else if ok && payload.Replace {
			// Drop existing pack so CompleteImport / PackMedia can install the replacement.
			progress("Removing existing media…", ptrFloat(0.15))
			_ = os.Remove(path)
			oldRows, qerr := d.Library.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ?`, t.VideoID.Int64)
			if qerr == nil && oldRows != nil {
				for oldRows.Next() {
					var p string
					if oldRows.Scan(&p) == nil && p != "" && p != path && p != abs {
						_ = os.Remove(p)
					}
				}
				_ = oldRows.Close()
			}
		}

		if inPlace {
			progress("Binding library file…", ptrFloat(0.5))
			nfoBeside, infoPath := library.SidecarPathsBeside(abs)
			meta := library.MediaCompleteMeta{
				Tool:      "import",
				ImportSrc: abs,
				InPlace:   true,
			}
			// Do not register a foreign .nfo as library provenance - apply metadata then regenerate.
			if err := d.Library.CompleteImport(t.VideoID.Int64, abs, "", infoPath, meta, t.ID); err != nil {
				return err
			}
			if nfoBeside != "" {
				if err := d.Library.ApplyImportNFO(t.VideoID.Int64, nfoBeside, t.ID); err != nil {
					return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "apply nfo failed"), err.Error())
				}
			} else {
				_ = d.Library.SoftFillDurationFromMedia(ctx, t.VideoID.Int64, abs)
				if _, err := d.Library.RewriteVideoNFO(t.VideoID.Int64, 0); err != nil {
					return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "write nfo failed"), err.Error())
				}
			}
			if payload.Verify {
				_ = d.Library.CancelMediaVerifyForVideo(t.VideoID.Int64, "Superseded by import")
				if _, err := d.Library.EnqueueMediaVerify(t.VideoID.Int64); err != nil {
					progress("Verify enqueue failed: "+err.Error(), nil)
				}
			}
			progress("Done", ptrFloat(1))
			return nil
		}

		dlctx, err := d.Library.PrepareDownload(t.VideoID.Int64)
		if err != nil {
			return err
		}
		season, episode := 0, 0
		if dlctx.Video.Season.Valid {
			season = int(dlctx.Video.Season.Int64)
		}
		if dlctx.Video.Episode.Valid {
			episode = int(dlctx.Video.Episode.Int64)
		}
		if season == 0 || episode == 0 {
			upload := ""
			if dlctx.Video.UploadDate.Valid {
				upload = dlctx.Video.UploadDate.String
			}
			s, e, aerr := d.Library.AssignSeasonEpisode(dlctx.Video.SeriesID, upload, 0, t.VideoID.Int64)
			if aerr != nil {
				return aerr
			}
			if season == 0 {
				season = s
			}
			if episode == 0 {
				episode = e
			}
		}
		infoSrc := ""
		for _, cand := range []string{
			strings.TrimSuffix(abs, filepath.Ext(abs)) + ".info.json",
			abs + ".info.json",
		} {
			if _, err := os.Stat(cand); err == nil {
				infoSrc = cand
				break
			}
		}
		srcNFO := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".nfo"
		if _, err := os.Stat(srcNFO); err != nil {
			srcNFO = ""
		}
		if srcNFO != "" {
			if err := d.Library.ApplyImportNFOMetadata(t.VideoID.Int64, srcNFO); err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "apply nfo failed"), err.Error())
			}
			if err := d.Library.AddVideoHistory(t.VideoID.Int64, "nfo_applied", "Episode metadata applied from NFO; library NFO regenerated", map[string]any{
				"source": srcNFO,
			}, t.ID); err != nil {
				return err
			}
		}
		progress("Installing to library…", ptrFloat(0.5))
		dlctx, err = d.Library.PrepareDownload(t.VideoID.Int64)
		if err != nil {
			return err
		}
		aired := ""
		if dlctx.Video.UploadDate.Valid {
			aired = dlctx.Video.UploadDate.String
		}
		uidVal := strings.TrimSpace(dlctx.Video.UniqueIDValue)
		if uidVal == "" {
			uidVal = dlctx.Video.RemoteID
		}
		uidType := strings.TrimSpace(dlctx.Video.UniqueIDType)
		if uidType == "" {
			uidType = "yt-dlp"
		}
		mediaPath, nfoPath, infoPath, _, _, err := library.PackMedia(
			abs, dlctx.RootPath,
			library.EpisodeNFO{
				SeriesTitle:   dlctx.SeriesTitle,
				Title:         dlctx.Video.Title,
				SortTitle:     dlctx.Video.SortTitle,
				OriginalTitle: dlctx.Video.OriginalTitle,
				Season:        season,
				Episode:       episode,
				Plot:          dlctx.Video.Description,
				Tagline:       dlctx.Video.Tagline,
				Studio:        dlctx.Video.Studio,
				Genres:        dlctx.Video.Genres,
				Tags:          dlctx.Video.Tags,
				Actors:        dlctx.Video.Actors,
				Country:       dlctx.Video.Country,
				MPAA:          dlctx.Video.MPAA,
				Aired:         aired,
				UniqueID:      uidVal,
				UniqueIDType:  uidType,
				SourceSite:    uidType,
				Domain:        library.NamingDomain(dlctx.URL),
			},
			library.LoadNamingConfig(d.Library.DB), infoSrc, "", nil,
		)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "install failed"), err.Error())
		}
		// Drop leftover sidecars from the inbox after a successful move.
		for _, leftover := range []string{
			strings.TrimSuffix(abs, filepath.Ext(abs)) + ".nfo",
			strings.TrimSuffix(abs, filepath.Ext(abs)) + ".info.json",
			abs + ".info.json",
		} {
			_ = os.Remove(leftover)
		}
		meta := library.MediaCompleteMeta{
			Tool:      "import",
			ImportSrc: abs,
		}
		if err := d.Library.CompleteImport(t.VideoID.Int64, mediaPath, nfoPath, infoPath, meta, t.ID); err != nil {
			return err
		}
		// NFO soft-fill already ran above when present; ffprobe when duration still empty.
		_ = d.Library.SoftFillDurationFromMedia(ctx, t.VideoID.Int64, mediaPath)
		if _, err := d.Library.RewriteVideoNFO(t.VideoID.Int64, 0); err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "write nfo failed"), err.Error())
		}
		if payload.Verify {
			_ = d.Library.CancelMediaVerifyForVideo(t.VideoID.Int64, "Superseded by import")
			if _, err := d.Library.EnqueueMediaVerify(t.VideoID.Int64); err != nil {
				progress("Verify enqueue failed: "+err.Error(), nil)
			}
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

// ScanHandler runs one source full scan or tip Scan.
func ScanHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "scan deps missing")
		}
		var payload struct {
			SourceID int64  `json:"source_id"`
			SeriesID int64  `json:"series_id"`
			Mode     string `json:"mode"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)
		if payload.SourceID == 0 {
			// Legacy series-wide scan: enqueue per-source full scans and exit.
			if !t.SeriesID.Valid {
				return apperrors.New(apperrors.CodeScanFailed, "scan task missing source_id")
			}
			n, _, err := d.Library.EnqueueFullScansForSeries(t.SeriesID.Int64)
			if err != nil {
				return err
			}
			progress(fmt.Sprintf("Queued %d full scans", n), ptrFloat(1))
			return nil
		}
		src, err := d.Library.GetSourceByID(payload.SourceID)
		if err != nil {
			return err
		}
		seriesID := src.SeriesID
		label := src.URL
		if src.Label.Valid && src.Label.String != "" {
			label = src.Label.String
		}
		progress(fmt.Sprintf("Listing %s", label), ptrFloat(0.05))

		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-scan-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)

		jar, err := cookies.TempJarForURL(d.Library.DB, work, src.URL)
		if err != nil {
			mode := library.SourceHistModeScan
			if !src.FullScanDone {
				mode = library.SourceHistModeFull
			}
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, err.Error(), map[string]any{
				"mode": mode,
				"code": apperrors.CodeCookieInvalid,
			}, t.ID)
			_ = d.Library.HoldSourceOnYtDlpError(src.ID, t.ID)
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}

		domain := queue.DomainFromURL(src.URL)
		lim, _ := settings.LimitsForDomain(d.Library.DB, domain)

		cutoff := ""
		if src.ScanCutoff.Valid {
			cutoff = src.ScanCutoff.String
		}

		fullScan := !src.FullScanDone
		mode := library.SourceHistModeScan
		if fullScan {
			mode = library.SourceHistModeFull
		}
		entries, err := listEntries(ctx, d, src.URL, jar, 0, lim)
		if err != nil {
			code, msg := classify(err)
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, msg+": "+err.Error(), map[string]any{
				"mode": mode,
				"code": code,
			}, t.ID)
			_ = d.Library.HoldSourceOnYtDlpError(src.ID, t.ID)
			return err
		}

		var createdIDs, updatedIDs []int64
		var ignoredMediaTypeIDs, ignoredIndexAsIgnoredIDs []int64
		var skippedTitleInclude, skippedTitleExclude []map[string]string
		var created, updated int
		hitCutoff := false
		hitKnown := false

		recordSkipTitle := func(dest *[]map[string]string, remoteID, title string) {
			*dest = append(*dest, map[string]string{
				"remote_id": remoteID,
				"title":     title,
			})
		}

		recordUpsert := func(res library.UpsertResult, li library.ListedVideo) {
			if res.Skipped {
				switch res.SkipReason {
				case library.SkipReasonTitleRegexpInclude:
					recordSkipTitle(&skippedTitleInclude, li.RemoteID, li.Title)
				case library.SkipReasonTitleRegexpExclude:
					recordSkipTitle(&skippedTitleExclude, li.RemoteID, li.Title)
				}
				return
			}
			if res.Created {
				created++
				createdIDs = append(createdIDs, res.VideoID)
				switch res.IgnoreReason {
				case library.IgnoreReasonMediaType:
					ignoredMediaTypeIDs = append(ignoredMediaTypeIDs, res.VideoID)
				case library.IgnoreReasonIndexAsIgnored:
					ignoredIndexAsIgnoredIDs = append(ignoredIndexAsIgnoredIDs, res.VideoID)
				}
			} else {
				updated++
				updatedIDs = append(updatedIDs, res.VideoID)
			}
		}

		if fullScan {
			progress(fmt.Sprintf("Full scan (listed=%d)", len(entries)), ptrFloat(0.2))
			nEntries := len(entries)
			for i, e := range entries {
				upload := library.NormalizeUploadTime(e.UploadDate)
				if library.BeforeCutoff(upload, cutoff) {
					hitCutoff = true
					break // discard this and older; do not index
				}
				li := library.EntryFromYtDlp(e, src.ID)
				res, err := d.Library.UpsertListed(seriesID, li, t.ID)
				if err != nil {
					return err
				}
				recordUpsert(res, li)
				if nEntries > 0 && (i%10 == 0 || i == nEntries-1) {
					progress(fmt.Sprintf("Indexing %d/%d…", i+1, nEntries),
						ptrFloat(0.2+0.75*float64(i+1)/float64(nEntries)))
				}
			}
			_ = d.Library.MarkFullScanDone(src.ID)
			_, _ = d.Library.RecomputeSeriesPremiered(seriesID)
		} else {
			progress(fmt.Sprintf("Scan (listed=%d)", len(entries)), ptrFloat(0.2))
			var news []ytdlp.Entry
			nEntries := len(entries)
			for i, e := range entries {
				upload := library.NormalizeUploadTime(e.UploadDate)
				if library.BeforeCutoff(upload, cutoff) {
					hitCutoff = true
					break // past cutoff; nothing newer left in newest-first list
				}
				if ok, reason := library.TitlePassesFilters(src.TitleRegexpInclude, src.TitleRegexpExclude, e.Title); !ok {
					switch reason {
					case library.SkipReasonTitleRegexpInclude:
						recordSkipTitle(&skippedTitleInclude, e.ID, e.Title)
					case library.SkipReasonTitleRegexpExclude:
						recordSkipTitle(&skippedTitleExclude, e.ID, e.Title)
					}
					continue // walk past; do not treat as known tip stop
				}
				known, err := d.Library.VideoExistsByRemote(seriesID, e.ID)
				if err != nil {
					return err
				}
				if known {
					hitKnown = true
					break
				}
				news = append(news, e)
				if nEntries > 0 && (i%20 == 0 || i == nEntries-1) {
					progress(fmt.Sprintf("Walking tip %d/%d…", i+1, nEntries),
						ptrFloat(0.2+0.3*float64(i+1)/float64(nEntries)))
				}
			}
			nNews := len(news)
			for i, e := range news {
				li := library.EntryFromYtDlp(e, src.ID)
				res, err := d.Library.UpsertListed(seriesID, li, t.ID)
				if err != nil {
					return err
				}
				recordUpsert(res, li)
				if nNews > 0 && (i%5 == 0 || i == nNews-1) {
					progress(fmt.Sprintf("Indexing new %d/%d…", i+1, nNews),
						ptrFloat(0.5+0.45*float64(i+1)/float64(nNews)))
				}
			}
		}

		scanMsg := fmt.Sprintf("Scan: indexed %d videos (%d new)", created+updated, created)
		if n := len(skippedTitleInclude); n > 0 {
			scanMsg += fmt.Sprintf(", %d skipped by title include", n)
		}
		if n := len(skippedTitleExclude); n > 0 {
			scanMsg += fmt.Sprintf(", %d skipped by title exclude", n)
		}
		if n := len(ignoredMediaTypeIDs); n > 0 {
			scanMsg += fmt.Sprintf(", %d ignored by media type", n)
		}
		if n := len(ignoredIndexAsIgnoredIDs); n > 0 {
			scanMsg += fmt.Sprintf(", %d marked as ignored", n)
		}

		histDetail := map[string]any{
			"mode":                         mode,
			"created":                      created,
			"updated":                      updated,
			"created_ids":                  createdIDs,
			"updated_ids":                  updatedIDs,
			"skipped_title_regexp_include": skippedTitleInclude,
			"skipped_title_regexp_exclude": skippedTitleExclude,
			"ignored_media_type_ids":       ignoredMediaTypeIDs,
			"ignored_index_as_ignored_ids": ignoredIndexAsIgnoredIDs,
			"hit_known":                    hitKnown,
			"hit_cutoff":                   hitCutoff,
		}
		_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanned, scanMsg, histDetail, t.ID)

		msg := scanMsg
		if hitCutoff {
			msg += ", cutoff reached"
		}
		detailBytes, _ := json.Marshal(map[string]any{
			"created":                      created,
			"updated":                      updated,
			"created_ids":                  createdIDs,
			"updated_ids":                  updatedIDs,
			"skipped_title_regexp_include": skippedTitleInclude,
			"skipped_title_regexp_exclude": skippedTitleExclude,
			"ignored_media_type_ids":       ignoredMediaTypeIDs,
			"ignored_index_as_ignored_ids": ignoredIndexAsIgnoredIDs,
			"source_id":                    src.ID,
			"full":                         fullScan,
			"hit_known":                    hitKnown,
			"hit_cutoff":                   hitCutoff,
		})
		_ = d.Library.Queue.UpdateProgress(t.ID, msg, ptrFloat(1))
		_ = d.Library.Queue.SetDetail(t.ID, string(detailBytes))
		progress(msg, ptrFloat(1))

		return nil
	}
}

// DownloadHandler downloads one video via its domain handler, then Creatorr remuxes.
func DownloadHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "download deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeDownloadFailed, "download task missing video_id")
		}
		dlctx, err := d.Library.PrepareDownload(t.VideoID.Int64)
		if err != nil {
			return err
		}
		if path, ok, err := d.Library.HasVideoFile(t.VideoID.Int64); err != nil {
			return err
		} else if ok && !library.TaskPayloadMaturity(t.Payload) {
			progress("Already on disk", nil)
			_, _ = d.Library.DB.SQL.Exec(`UPDATE videos SET status = 'downloaded' WHERE id = ?`, t.VideoID.Int64)
			_ = path
			return nil
		} else if ok && library.TaskPayloadMaturity(t.Payload) {
			// Maturity re-download: remove existing pack so download can replace media + info.json.
			_ = os.Remove(path)
			oldRows, _ := d.Library.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ?`, t.VideoID.Int64)
			if oldRows != nil {
				for oldRows.Next() {
					var p string
					if oldRows.Scan(&p) == nil && p != "" && p != path {
						_ = os.Remove(p)
					}
				}
				_ = oldRows.Close()
			}
		}
		if dlctx.URL == "" {
			return apperrors.New(apperrors.CodeDownloadFailed, "video has no source_url")
		}

		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-dl-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)

		progress("Resolving cookies…", nil)
		jar, err := cookies.TempJarForURL(d.Library.DB, work, dlctx.URL)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}

		progress("Downloading…", nil)
		lim, _ := settings.LimitsForDomain(d.Library.DB, t.Domain)
		subOpts, _ := settings.GetSubtitleOpts(d.Library.DB)
		matchFilter := library.BuildDownloadMatchFilter(nil)
		if exclude, err := d.Library.SeriesAutoIgnoreMediaTypes(dlctx.Video.SeriesID); err == nil {
			matchFilter = library.BuildDownloadMatchFilter(exclude)
		}
		dlMap := ytdlp.ProgressMapper{Lo: 0, Hi: 1}
		media, err := downloadMedia(ctx, d, ytdlp.DownloadOpts{
			URL:            dlctx.URL,
			CookiesPath:    jar,
			FormatSelector: dlctx.FormatSelector,
			OutDir:         work,
			LimitRate:      lim.DownloadRateLimit,
			SleepRequests:  lim.SleepRequests,
			MatchFilter:    matchFilter,
			SubLangs:       subOpts.Langs,
			SubAuto:        subOpts.Auto,
			OnProgress: func(msg string, pct *float64) {
				if pct == nil {
					progress(msg, nil)
					return
				}
				mapped := dlMap.Map(*pct)
				progress(msg, &mapped)
			},
		})
		if err != nil {
			return err
		}

		if st, _ := d.Library.Queue.TaskStatus(t.ID); st == queue.StatusCancelled {
			return context.Canceled
		}

		_ = dlMap.Finish()
		sbCfg := dlctx.Profile.SponsorBlockConfig()
		infoSrc, thumbSrc, subSrcs := library.FindDownloadSidecars(media)
		subSrcs = library.MarkAutoSubtitleFiles(subSrcs, infoSrc)

		// Remove categories → stage + system-lane cut (no remux / pack here).
		if len(sponsorblock.NormalizeCategoryList(sbCfg.Remove)) > 0 {
			progress("Staging for SponsorBlock cut…", nil)
			staged, err := d.Library.StageSponsorblockCut(t.VideoID.Int64, media, infoSrc, thumbSrc, subSrcs)
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "SponsorBlock staging failed"), err.Error())
			}
			aired := ""
			if dlctx.Video.UploadDate.Valid {
				aired = dlctx.Video.UploadDate.String
			}
			season, episode := 0, 0
			if dlctx.Video.Season.Valid {
				season = int(dlctx.Video.Season.Int64)
			}
			if dlctx.Video.Episode.Valid {
				episode = int(dlctx.Video.Episode.Int64)
			}
			staged.PageURL = dlctx.URL
			staged.RemoteID = dlctx.Video.RemoteID
			staged.Maturity = library.TaskPayloadMaturity(t.Payload)
			staged.FormatSelector = dlctx.FormatSelector
			staged.SeriesTitle = dlctx.SeriesTitle
			staged.VideoTitle = dlctx.Video.Title
			staged.Description = dlctx.Video.Description
			staged.Aired = aired
			staged.Season = season
			staged.Episode = episode
			staged.SeriesID = dlctx.Video.SeriesID
			staged.RootPath = dlctx.RootPath
			staged.NamingDomain = library.NamingDomain(dlctx.URL)
			if _, err := d.Library.EnqueueSponsorblockCut(staged); err != nil {
				d.Library.RemoveSponsorblockCutStaging(t.VideoID.Int64)
				return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "enqueue SponsorBlock cut failed"), err.Error())
			}
			_ = d.Library.AddVideoHistory(t.VideoID.Int64, "downloaded", "Download finished (staged for cut)", map[string]any{
				"path": media, "staged": true,
			}, t.ID)
			progress("Queued SponsorBlock cut", nil)
			return nil
		}

		_ = d.Library.AddVideoHistory(t.VideoID.Int64, "downloaded", "Download finished", map[string]any{
			"path": media,
		}, t.ID)

		progress("Remuxing…", nil)
		var remuxed bool
		media, remuxed, err = library.RemuxIfNeeded(ctx, media)
		if err != nil {
			return err
		}
		if remuxed {
			_ = d.Library.AddVideoHistory(t.VideoID.Int64, "remuxed", "Remuxed to "+library.RemuxContainer, map[string]any{
				"container": library.RemuxContainer,
				"path":      media,
			}, t.ID)
		}
		infoSrc, thumbSrc, subSrcs = library.FindDownloadSidecars(media)
		subSrcs = library.MarkAutoSubtitleFiles(subSrcs, infoSrc)

		sbWarn := ""
		var sbPlanPath string
		progress("SponsorBlock…", nil)
		remoteID := dlctx.Video.RemoteID
		sbRes, sbErr := sponsorblock.ApplyArchive(ctx, media, infoSrc, dlctx.URL, remoteID, sbCfg, work, nil)
		if sbErr != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "SponsorBlock failed"), sbErr.Error())
		}
		media = sbRes.MediaPath
		sbWarn = sbRes.Warning
		sbPlanPath = sbRes.PlanPath
		if sbRes.DidCut {
			plan, ok, _ := sponsorblock.ReadPlan(media)
			if ok && plan.HasCuts() {
				sponsorblock.RemapSubtitleFiles(subSrcs, plan.Cuts(), plan.CardDurationSec, plan.InfoCards)
			}
			_ = d.Library.AddVideoHistory(t.VideoID.Int64, "sponsorblock_cut", "SponsorBlock cut applied", map[string]any{
				"plan": sbRes.PlanPath, "cards": sbRes.CardsOK,
			}, t.ID)
		}

		progress("Installing to library…", nil)
		if err := finishArchivePack(d, t, dlctx, media, infoSrc, thumbSrc, subSrcs, remuxed, sbPlanPath, sbWarn, progress); err != nil {
			return err
		}
		return nil
	}
}

func finishArchivePack(
	d Deps,
	t *queue.Task,
	dlctx *library.DownloadContext,
	media, infoSrc, thumbSrc string,
	subSrcs []string,
	remuxed bool,
	sbPlanPath, sbWarn string,
	progress func(msg string, pct *float64),
) error {
	season, episode := 0, 0
	if dlctx.Video.Season.Valid {
		season = int(dlctx.Video.Season.Int64)
	}
	if dlctx.Video.Episode.Valid {
		episode = int(dlctx.Video.Episode.Int64)
	}
	if season == 0 || episode == 0 {
		upload := ""
		if dlctx.Video.UploadDate.Valid {
			upload = dlctx.Video.UploadDate.String
		}
		s, e, aerr := d.Library.AssignSeasonEpisode(dlctx.Video.SeriesID, upload, 0, t.VideoID.Int64)
		if aerr != nil {
			return aerr
		}
		if season == 0 {
			season = s
		}
		if episode == 0 {
			episode = e
		}
	}
	aired := ""
	if dlctx.Video.UploadDate.Valid {
		aired = dlctx.Video.UploadDate.String
	}
	thumbURL := ""
	if dlctx.Video.ThumbnailURL.Valid {
		thumbURL = dlctx.Video.ThumbnailURL.String
	}
	thumbSrc, cleanupThumb := library.MaterializeThumbSrc(thumbSrc, thumbURL)
	defer cleanupThumb()

	// Soft-fill empty genres from download-time info.json, then build NFO from full DB row.
	_, _ = d.Library.SoftFillVideoGenresFromInfoJSON(t.VideoID.Int64, infoSrc)
	v := &dlctx.Video
	if fresh, gerr := d.Library.GetVideo(t.VideoID.Int64); gerr == nil {
		v = fresh
	}
	runtime := 0
	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		runtime = int(v.DurationSeconds.Int64)
	}
	epMeta := library.EpisodeMetaFromVideo(v, dlctx.SeriesTitle, season, episode, aired, runtime)
	mediaPath, nfoPath, infoPath, thumbPath, subPaths, err := library.PackMedia(
		media, dlctx.RootPath, epMeta,
		library.LoadNamingConfig(d.Library.DB), infoSrc, thumbSrc, subSrcs,
	)
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "pack failed"), err.Error())
	}
	meta := library.MediaCompleteMeta{
		Tool:                   "yt-dlp",
		DownloadFormatSelector: dlctx.FormatSelector,
	}
	if remuxed {
		meta.DownloadRemuxContainer = library.RemuxContainer
	}
	if err := d.Library.CompleteDownload(t.VideoID.Int64, mediaPath, nfoPath, infoPath, thumbPath, subPaths, meta, t.ID); err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "record install failed"), err.Error())
	}
	if sbPlanPath != "" {
		if b, err := os.ReadFile(sbPlanPath); err == nil {
			dest := sponsorblock.PlanPath(mediaPath)
			_ = os.WriteFile(dest, b, 0o644)
			_ = d.Library.RegisterFileKind(t.VideoID.Int64, dest, "sponsorblock")
		}
	}
	if library.TaskPayloadMaturity(t.Payload) {
		_ = d.Library.AddVideoHistory(t.VideoID.Int64, "maturity_repacked", "Maturity media refresh", map[string]any{
			"path": mediaPath,
		}, t.ID)
	}
	if sbWarn != "" {
		progress(sbWarn, nil)
	} else {
		progress("Done", nil)
	}
	maturity := library.TaskPayloadMaturity(t.Payload)
	if _, err := d.Library.MaybeEnqueueMediaVerifyAfterPack(t.VideoID.Int64, maturity); err != nil {
		// Pack already succeeded; verify enqueue failure should not fail the pack task.
		progress("Verify enqueue failed: "+err.Error(), nil)
	}
	return nil
}

// SponsorblockCutHandler applies SponsorBlock remove/cut on staged media then packs.
func SponsorblockCutHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "sponsorblock cut deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodePackFailed, "sponsorblock_cut missing video_id")
		}
		payload, err := library.ParseSponsorblockCutPayload(t.Payload)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "invalid sponsorblock_cut payload"), err.Error())
		}
		videoID := t.VideoID.Int64
		defer func() {
			// Leave staging on success cleanup below; cancel/fail paths wipe via OnCancelled or here on hard fail after start.
		}()

		if _, err := os.Stat(payload.MediaPath); err != nil {
			d.Library.RemoveSponsorblockCutStaging(videoID)
			return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "SponsorBlock staging missing"), err.Error())
		}

		// Wipe incomplete cut outputs from a prior interrupted run.
		stage := payload.StageDir
		if stage == "" {
			stage = filepath.Dir(payload.MediaPath)
		}
		entries, _ := os.ReadDir(stage)
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "sb-") || strings.HasPrefix(name, "cut-") || strings.HasSuffix(name, ".sponsorblock.json") {
				_ = os.Remove(filepath.Join(stage, name))
			}
		}

		dlctx, err := d.Library.PrepareDownload(videoID)
		if err != nil {
			return err
		}
		sbCfg := dlctx.Profile.SponsorBlockConfig()
		media := payload.MediaPath
		infoSrc := payload.InfoPath
		thumbSrc := payload.ThumbPath
		subSrcs := append([]string{}, payload.SubPaths...)

		remuxed := false
		if !sbCfg.ReencodeCut {
			progress("Remuxing…", nil)
			media, remuxed, err = library.RemuxIfNeeded(ctx, media)
			if err != nil {
				return err
			}
			if remuxed {
				_ = d.Library.AddVideoHistory(videoID, "remuxed", "Remuxed to "+library.RemuxContainer, map[string]any{
					"container": library.RemuxContainer,
					"path":      media,
				}, t.ID)
				infoSrc, thumbSrc, subSrcs = library.FindDownloadSidecars(media)
				subSrcs = library.MarkAutoSubtitleFiles(subSrcs, infoSrc)
			}
		}

		progress("SponsorBlock cut…", nil)
		pageURL := payload.PageURL
		if pageURL == "" {
			pageURL = dlctx.URL
		}
		remoteID := payload.RemoteID
		if remoteID == "" {
			remoteID = dlctx.Video.RemoteID
		}
		var cutProg sponsorblock.EncodeProgress
		if sbCfg.ReencodeCut {
			cutProg = func(frac *float64) {
				progress("SponsorBlock cut…", frac)
			}
		}
		sbRes, sbErr := sponsorblock.ApplyArchive(ctx, media, infoSrc, pageURL, remoteID, sbCfg, stage, cutProg)
		if sbErr != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "SponsorBlock failed"), sbErr.Error())
		}
		media = sbRes.MediaPath
		if sbRes.DidCut {
			plan, ok, _ := sponsorblock.ReadPlan(media)
			if ok && plan.HasCuts() {
				sponsorblock.RemapSubtitleFiles(subSrcs, plan.Cuts(), plan.CardDurationSec, plan.InfoCards)
			}
			_ = d.Library.AddVideoHistory(videoID, "sponsorblock_cut", "SponsorBlock cut applied", map[string]any{
				"plan": sbRes.PlanPath, "cards": sbRes.CardsOK,
			}, t.ID)
		}

		// Prefer live library context; fall back to payload snapshot for titles/paths.
		if payload.RootPath != "" {
			dlctx.RootPath = payload.RootPath
		}
		if payload.SeriesTitle != "" {
			dlctx.SeriesTitle = payload.SeriesTitle
		}
		if payload.FormatSelector != "" {
			dlctx.FormatSelector = payload.FormatSelector
		}
		if payload.Season > 0 && !dlctx.Video.Season.Valid {
			dlctx.Video.Season.Valid = true
			dlctx.Video.Season.Int64 = int64(payload.Season)
		}
		if payload.Episode > 0 && !dlctx.Video.Episode.Valid {
			dlctx.Video.Episode.Valid = true
			dlctx.Video.Episode.Int64 = int64(payload.Episode)
		}
		if payload.VideoTitle != "" {
			dlctx.Video.Title = payload.VideoTitle
		}
		if payload.Description != "" {
			dlctx.Video.Description = payload.Description
		}
		if payload.Aired != "" && !dlctx.Video.UploadDate.Valid {
			dlctx.Video.UploadDate.Valid = true
			dlctx.Video.UploadDate.String = payload.Aired
		}
		if payload.Maturity {
			// Ensure CompleteDownload maturity history via payload on task.
			t.Payload = `{"maturity":true}`
		}

		progress("Installing to library…", nil)
		if err := finishArchivePack(d, t, dlctx, media, infoSrc, thumbSrc, subSrcs, remuxed, sbRes.PlanPath, sbRes.Warning, progress); err != nil {
			return err
		}
		d.Library.RemoveSponsorblockCutStaging(videoID)
		return nil
	}
}

// MediaVerifyHandler null-decodes packed library media. Fail keeps files and sets verify_failed.
func MediaVerifyHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "media verify deps missing")
		}
		if !t.VideoID.Valid {
			return apperrors.New(apperrors.CodeMediaVerifyFailed, "media_verify missing video_id")
		}
		videoID := t.VideoID.Int64
		var payload struct {
			VideoID   int64  `json:"video_id"`
			MediaPath string `json:"media_path"`
		}
		_ = json.Unmarshal([]byte(t.Payload), &payload)

		path, ok, err := d.Library.HasVideoFile(videoID)
		if err != nil {
			return err
		}
		if !ok || path == "" {
			// File replaced/removed while pending: treat as superseded (cancelled), not verify_failed.
			progress("Superseded (no media)", nil)
			return context.Canceled
		}
		if payload.MediaPath != "" && filepath.Clean(payload.MediaPath) != filepath.Clean(path) {
			progress("Superseded (media replaced)", nil)
			return context.Canceled
		}

		if err := library.VerifyDownloadedMedia(ctx, path, progress); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			msg := err.Error()
			_ = d.Library.MarkVerifyFailed(videoID, t.ID, "Media verify failed")
			v, _ := d.Library.GetVideo(videoID)
			seriesTitle := ""
			videoTitle := ""
			if v != nil {
				videoTitle = v.Title
				if ser, serr := d.Library.GetSeries(v.SeriesID, false); serr == nil && ser != nil {
					seriesTitle = ser.Title
				}
			}
			_ = notify.VerifyFailed(ctx, d.Library.DB, t.ID, seriesTitle, videoTitle, msg)
			return err
		}
		_ = d.Library.MarkVerified(videoID, t.ID)
		return nil
	}
}

func ptrFloat(v float64) *float64 { return &v }

// RefreshSidecarsHandler refreshes NFO/thumb/subs for one video without touching media or info.json.
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
		defer os.RemoveAll(work)

		progress("Resolving cookies…", ptrFloat(0.1))
		jar, err := cookies.TempJarForURL(d.Library.DB, work, url)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
		}
		if err := refreshSidecars(ctx, d, work, jar, url, t.Domain, t.VideoID.Int64, t.ID, progress); err != nil {
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
	defer os.RemoveAll(work)

	progress("Fetching metadata…", ptrFloat(0.05))
	jar, err := cookies.TempJarForURL(d.Library.DB, work, url)
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
	}
	flare, err := domains.FlareSolverrURL(d.Library.DB, t.Domain)
	if err != nil {
		return err
	}
	lim, _ := settings.LimitsForDomain(d.Library.DB, t.Domain)
	e, err := d.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
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
	progress("Refreshing sidecars…", ptrFloat(0.7))
	if err := refreshSidecars(ctx, d, work, jar, url, t.Domain, vid, t.ID, progress); err != nil {
		return err
	}
	progress("Done", ptrFloat(1))
	return nil
}

func refreshSidecars(ctx context.Context, d Deps, work, jar, url, domain string, videoID, taskID int64, progress func(msg string, pct *float64)) error {
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
	bundle := library.SidecarBundle{SubSrcs: subPaths, ThumbSrc: thumbPath}
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
	defer os.RemoveAll(work)

	var refreshed, skippedNew, sidecars, okSources int
	var lastErr error
	n := len(sources)
	for i, src := range sources {
		label := src.URL
		if src.Label.Valid && src.Label.String != "" {
			label = src.Label.String
		}
		progress(fmt.Sprintf("Listing source %d/%d: %s", i+1, n, label), ptrFloat(float64(i)/float64(n)))

		jar, err := cookies.TempJarForURL(d.Library.DB, work, src.URL)
		if err != nil {
			_ = d.Library.AddSourceHistory(src.ID, library.SourceHistScanError, err.Error(), map[string]any{
				"mode": library.SourceHistModeRescanMetadata,
				"code": apperrors.CodeCookieInvalid,
			}, t.ID)
			_ = d.Library.HoldSourceOnYtDlpError(src.ID, t.ID)
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
			_ = d.Library.HoldSourceOnYtDlpError(src.ID, t.ID)
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
			if err := refreshSidecars(ctx, d, work, jar, url, domain, vid, t.ID, progress); err != nil {
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
		defer os.RemoveAll(work)

		jar, err := cookies.TempJarForURL(d.Library.DB, work, fetchURL)
		if err != nil {
			return err
		}
		domain := queue.DomainFromURL(fetchURL)
		flare, err := domains.FlareSolverrURL(d.Library.DB, domain)
		if err != nil {
			return err
		}
		// Interactive: no download_rate_limit / sleep_requests (operator-facing form fetch).
		info, err := d.YtDlp.DumpPlaylistInfo(ctx, ytdlp.ListOpts{
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
		defer os.RemoveAll(work)

		jar, err := cookies.TempJarForURL(d.Library.DB, work, fetchURL)
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
		info, err := d.YtDlp.DumpPlaylistInfo(ctx, ytdlp.ListOpts{
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
		defer os.RemoveAll(work)

		jar, err := cookies.TempJarForURL(d.Library.DB, work, fetchURL)
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
		e, err := d.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
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
		defer os.RemoveAll(work)

		jar, err := cookies.TempJarForURL(d.Library.DB, work, fetchURL)
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
		e, err := d.YtDlp.Resolve(ctx, ytdlp.ResolveOpts{
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
	return d.YtDlp.List(ctx, ytdlp.ListOpts{
		URL: url, CookiesPath: jar, PlaylistEnd: playlistEnd, FlareSolverrURL: flare,
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
	return d.YtDlp.FetchSidecars(ctx, opts)
}

// fetchPackStreamSubs downloads subtitle sidecars for stream pack. cleanup removes the temp work dir.
func fetchPackStreamSubs(ctx context.Context, d Deps, videoID int64, domain string) (subPaths []string, cleanup func(), err error) {
	subOpts, err := settings.GetSubtitleOpts(d.Library.DB)
	if err != nil || len(subOpts.Langs) == 0 {
		return nil, nil, err
	}
	dlctx, err := d.Library.PrepareDownload(videoID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(dlctx.URL) == "" {
		return nil, nil, fmt.Errorf("no url")
	}
	tmpRoot := d.TmpRoot
	if tmpRoot == "" {
		tmpRoot = os.TempDir()
	}
	work, err := os.MkdirTemp(tmpRoot, "creatorr-packstream-subs-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(work) }
	jar, err := cookies.TempJarForURL(d.Library.DB, work, dlctx.URL)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if domain == "" {
		domain = queue.DomainFromURL(dlctx.URL)
	}
	lim, _ := settings.LimitsForDomain(d.Library.DB, domain)
	infoPath, _, subPaths, err := fetchSidecars(ctx, d, ytdlp.SidecarsOpts{
		URL: dlctx.URL, CookiesPath: jar, OutDir: work,
		LimitRate: lim.DownloadRateLimit, SleepRequests: lim.SleepRequests,
		SubLangs: subOpts.Langs, SubAuto: subOpts.Auto,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	subPaths = library.MarkAutoSubtitleFiles(subPaths, infoPath)
	return subPaths, cleanup, nil
}
