package worker

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

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

		archiveLane := library.IsArchiveDownloadTask(t.Domain, t.Payload)
		downloadURL := dlctx.URL
		if archiveLane {
			downloadURL = library.YtArchiveURL(dlctx.Video.RemoteID)
			if downloadURL == "" {
				return apperrors.New(apperrors.CodeDownloadFailed, "archive download missing remote_id")
			}
			if library.TaskPayloadMaturity(t.Payload) {
				return apperrors.New(apperrors.CodeDownloadFailed, "maturity re-download not used for Web Archive media")
			}
		}

		tmpRoot := d.TmpRoot
		if tmpRoot == "" {
			tmpRoot = os.TempDir()
		}
		work, err := os.MkdirTemp(tmpRoot, "creatorr-dl-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(work) }()

		var jar string
		if archiveLane {
			progress("Web Archive download…", nil)
			// Optional Access cookies only if operator configured archive.org; never use YouTube jar.
			jar, err = domains.TempJarForURL(d.Library.DB, work, "https://archive.org/")
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
			}
		} else {
			progress("Resolving cookies…", nil)
			jar, err = domains.TempJarForURL(d.Library.DB, work, dlctx.URL)
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodeCookieInvalid, "cookie jar failed"), err.Error())
			}
		}

		audioOnly := dlctx.DeliveryMode == library.DeliveryAudio
		formatSelector := dlctx.FormatSelector
		if audioOnly {
			// Audio delivery ignores the quality profile's video format ladder.
			formatSelector = library.AudioFormatSelector
		}

		if !archiveLane {
			progress("Downloading…", nil)
		}
		lim, _ := settings.LimitsForDomain(d.Library.DB, t.Domain)
		subOpts, _ := settings.GetSubtitleOpts(d.Library.DB)
		matchFilter := library.BuildDownloadMatchFilter()
		media, err := downloadMedia(ctx, d, ytdlp.DownloadOpts{
			URL:            downloadURL,
			CookiesPath:    jar,
			FormatSelector: formatSelector,
			OutDir:         work,
			LimitRate:      lim.DownloadRateLimit,
			SleepRequests:  lim.SleepRequests,
			MatchFilter:    matchFilter,
			SubLangs:       subOpts.Langs,
			SubAuto:        subOpts.Auto,
			// StepProgress (in ytdlp) labels video/audio (1/2) and resets the bar
			// 0→100% per format on purpose.
			OnProgress: progress,
		})
		if err != nil {
			if !archiveLane && apperrors.DetectVideoUnavailable(err.Error()) {
				on, _ := settings.ArchiveFallbackEnabled(d.Library.DB)
				src := ""
				if dlctx.Video.SourceURL.Valid {
					src = dlctx.Video.SourceURL.String
				}
				if on && library.IsYouTubeSourceURL(src) {
					progress("Live unavailable; queuing Web Archive retry…", nil)
					return apperrors.WithDetail(
						apperrors.New(apperrors.CodeArchiveFallbackQueued, "live unavailable; Web Archive retry queued"),
						err.Error(),
					)
				}
			}
			return err
		}

		if st, _ := d.Library.Queue.TaskStatus(t.ID); st == queue.StatusCancelled {
			return context.Canceled
		}
		sbCfg := dlctx.Profile.SponsorBlockConfig()
		if audioOnly {
			// Info cards are a burned-in video overlay; audio-only media has no video track.
			sbCfg.InfoCards = false
		}
		infoSrc, thumbSrc, subSrcs := library.FindDownloadSidecars(media)
		subSrcs = library.MarkAutoSubtitleFiles(subSrcs, infoSrc)

		// Remove categories → stage + system-lane cut (no remux / pack here).
		if len(sponsorblock.NormalizeCategoryList(sbCfg.Remove)) > 0 {
			progress("Staging for SponsorBlock cut…", nil)
			staged, err := d.Library.StageSponsorblockCut(t.VideoID.Int64, media, infoSrc, thumbSrc, subSrcs)
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "SponsorBlock staging failed"), err.Error())
			}
			upload := ""
			if dlctx.Video.UploadDate.Valid {
				upload = dlctx.Video.UploadDate.String
			}
			if filled, ferr := d.Library.SoftFillUploadDateFromInfoJSON(t.VideoID.Int64, infoSrc); ferr != nil {
				return ferr
			} else if filled != "" {
				upload = filled
				dlctx.Video.UploadDate = sql.NullString{String: filled, Valid: true}
			}
			aired := upload
			season, episode := 0, 0
			if dlctx.Video.Season.Valid {
				season = int(dlctx.Video.Season.Int64)
			}
			if dlctx.Video.Episode.Valid {
				episode = int(dlctx.Video.Episode.Int64)
			}
			if upload != "" {
				sNum, eNum, aerr := d.Library.AssignSeasonEpisode(dlctx.Video.SeriesID, upload, 0, t.VideoID.Int64)
				if aerr != nil {
					return aerr
				}
				season, episode = sNum, eNum
			}
			staged.PageURL = dlctx.URL
			staged.RemoteID = dlctx.Video.RemoteID
			staged.Maturity = library.TaskPayloadMaturity(t.Payload)
			staged.FormatSelector = formatSelector
			staged.SeriesTitle = dlctx.SeriesTitle
			staged.VideoTitle = dlctx.Video.Title
			staged.Description = dlctx.Video.Description
			staged.Aired = aired
			staged.Season = season
			staged.Episode = episode
			staged.SeriesID = dlctx.Video.SeriesID
			staged.RootPath = dlctx.RootPath
			staged.NamingDomain = library.NamingDomain(dlctx.URL)
			staged.ParentTaskID = t.ID
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
		remuxContainer := library.RemuxContainer
		if audioOnly {
			remuxContainer = library.RemuxAudioContainer
			media, remuxed, err = library.RemuxAudioIfNeeded(ctx, media)
		} else {
			media, remuxed, err = library.RemuxIfNeeded(ctx, media)
		}
		if err != nil {
			return err
		}
		if remuxed {
			_ = d.Library.AddVideoHistory(t.VideoID.Int64, "remuxed", "Remuxed to "+remuxContainer, map[string]any{
				"container": remuxContainer,
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
		if err := finishArchivePack(d, t, dlctx, media, infoSrc, thumbSrc, subSrcs, remuxed, remuxContainer, formatSelector, sbPlanPath, sbWarn, progress); err != nil {
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
	remuxContainer, formatSelector string,
	sbPlanPath, sbWarn string,
	progress func(msg string, pct *float64),
) error {
	// Soft-fill upload_date from download info.json before season assign / pack.
	// Index rows often lack dates (flat playlist); packing then used S0000, and the
	// post-pack rename in CompleteDownload was skipped while this download task ran.
	upload := ""
	if dlctx.Video.UploadDate.Valid {
		upload = dlctx.Video.UploadDate.String
	}
	if filled, ferr := d.Library.SoftFillUploadDateFromInfoJSON(t.VideoID.Int64, infoSrc); ferr != nil {
		return ferr
	} else if filled != "" {
		upload = filled
		dlctx.Video.UploadDate = sql.NullString{String: filled, Valid: true}
	}
	archiveLane := library.IsArchiveDownloadTask(t.Domain, t.Payload)
	if archiveLane {
		if err := d.Library.SoftFillArchiveMetaFromInfoJSON(t.VideoID.Int64, infoSrc); err != nil {
			return err
		}
		if fresh, gerr := d.Library.GetVideo(t.VideoID.Int64); gerr == nil {
			dlctx.Video = *fresh
			if fresh.UploadDate.Valid {
				upload = fresh.UploadDate.String
			}
		}
	}

	season, episode := 0, 0
	if dlctx.Video.Season.Valid {
		season = int(dlctx.Video.Season.Int64)
	}
	if dlctx.Video.Episode.Valid {
		episode = int(dlctx.Video.Episode.Int64)
	}
	if upload != "" {
		sNum, eNum, aerr := d.Library.AssignSeasonEpisode(dlctx.Video.SeriesID, upload, 0, t.VideoID.Int64)
		if aerr != nil {
			return aerr
		}
		season, episode = sNum, eNum
	}
	aired := upload
	if aired == "" && dlctx.Video.UploadDate.Valid {
		aired = dlctx.Video.UploadDate.String
	}
	thumbURL := ""
	if dlctx.Video.ThumbnailURL.Valid {
		thumbURL = dlctx.Video.ThumbnailURL.String
	}
	thumbSrc, cleanupThumb := library.MaterializeThumbSrc(thumbSrc, thumbURL)
	defer cleanupThumb()

	// Soft-fill empty genres from download-time info.json, then ensure domain tag, then build NFO.
	_, _ = d.Library.SoftFillVideoGenresFromInfoJSON(t.VideoID.Int64, infoSrc)
	sourceURL := dlctx.URL
	if dlctx.Video.SourceURL.Valid && strings.TrimSpace(dlctx.Video.SourceURL.String) != "" {
		sourceURL = dlctx.Video.SourceURL.String
	}
	_, _ = d.Library.EnsureVideoDomainTag(t.VideoID.Int64, sourceURL)
	v := &dlctx.Video
	if fresh, gerr := d.Library.GetVideo(t.VideoID.Int64); gerr == nil {
		v = fresh
	}
	runtime := 0
	if v.DurationSeconds.Valid && v.DurationSeconds.Int64 > 0 {
		runtime = int(v.DurationSeconds.Int64)
	}
	epMeta := library.EpisodeMetaFromVideo(v, dlctx.SeriesTitle, season, episode, aired, runtime)
	mediaPath, nfoPath, infoPath, thumbPath, subPaths, pathSuffix, err := library.PackMedia(
		media, dlctx.RootPath, epMeta,
		library.NamingConfig{EpisodeFormat: dlctx.EpisodeFormat}, infoSrc, thumbSrc, subSrcs,
	)
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "pack failed"), err.Error())
	}
	if pathSuffix > 0 {
		chosen := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
		ideal := strings.TrimSuffix(chosen, fmt.Sprintf("_%d", pathSuffix))
		_ = notify.PathCollisionPacked(context.Background(), d.Library.DB, t.ID, dlctx.SeriesTitle, v.Title, ideal, chosen, pathSuffix)
	}
	meta := library.MediaCompleteMeta{
		AcquiredVia:            library.AcquiredViaSource,
		DownloadFormatSelector: formatSelector,
	}
	if archiveLane {
		meta.AcquiredVia = library.AcquiredViaArchive
	}
	if remuxed {
		meta.DownloadRemuxContainer = remuxContainer
	}
	if err := d.Library.CompleteDownload(t.VideoID.Int64, mediaPath, nfoPath, infoPath, thumbPath, subPaths, meta, t.ID); err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodePackFailed, "record install failed"), err.Error())
	}
	if year := library.SeasonYearFromUpload(aired); year > 0 {
		progress("Aligning episode numbers…", nil)
		_, _ = d.Library.AlignSeriesYearEpisodes(dlctx.Video.SeriesID, int64(year), t.ID)
	}
	if archiveLane {
		seriesTitle := dlctx.SeriesTitle
		videoTitle := dlctx.Video.Title
		if fresh, gerr := d.Library.GetVideo(t.VideoID.Int64); gerr == nil {
			videoTitle = fresh.Title
		}
		_ = notify.ArchiveFallback(context.Background(), d.Library.DB, t.ID, seriesTitle, videoTitle, "acquired_via=archive")
		_ = d.Library.AddVideoHistory(t.VideoID.Int64, "archive_fallback_packed", "Packed from Web Archive", map[string]any{
			"path": mediaPath,
		}, t.ID)
		_ = d.Library.Queue.UpdateProgress(t.ID, "Done (Web Archive)", nil)
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
	if _, err := d.Library.MaybeEnqueueMediaVerifyAfterPack(t.VideoID.Int64, maturity, t.ID); err != nil {
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
		audioOnly := dlctx.DeliveryMode == library.DeliveryAudio
		formatSelector := payload.FormatSelector
		if formatSelector == "" {
			formatSelector = dlctx.FormatSelector
		}
		sbCfg := dlctx.Profile.SponsorBlockConfig()
		if audioOnly {
			// Info cards are a burned-in video overlay; audio-only media has no video track.
			sbCfg.InfoCards = false
		}
		media := payload.MediaPath
		infoSrc := payload.InfoPath
		thumbSrc := payload.ThumbPath
		subSrcs := append([]string{}, payload.SubPaths...)

		remuxed := false
		remuxContainer := library.RemuxContainer
		if audioOnly {
			remuxContainer = library.RemuxAudioContainer
		}
		if !sbCfg.ReencodeCut {
			progress("Remuxing…", nil)
			if audioOnly {
				media, remuxed, err = library.RemuxAudioIfNeeded(ctx, media)
			} else {
				media, remuxed, err = library.RemuxIfNeeded(ctx, media)
			}
			if err != nil {
				return err
			}
			if remuxed {
				_ = d.Library.AddVideoHistory(videoID, "remuxed", "Remuxed to "+remuxContainer, map[string]any{
					"container": remuxContainer,
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
		if err := finishArchivePack(d, t, dlctx, media, infoSrc, thumbSrc, subSrcs, remuxed, remuxContainer, formatSelector, sbRes.PlanPath, sbRes.Warning, progress); err != nil {
			return err
		}
		d.Library.RemoveSponsorblockCutStaging(videoID)
		return nil
	}
}

// MediaVerifyHandler runs initial integrity check for packed library media.
