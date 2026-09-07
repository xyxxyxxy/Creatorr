package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/notify"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

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
			nfoBeside, _ := library.SidecarPathsBeside(abs)
			infoBeside, thumbBeside, subBeside := library.FindDownloadSidecars(abs)
			meta := library.MediaCompleteMeta{
				AcquiredVia: library.AcquiredViaImport,
				ImportSrc:   abs,
				InPlace:     true,
			}
			// Do not register a foreign .nfo as library provenance - apply metadata then regenerate.
			if err := d.Library.CompleteImport(t.VideoID.Int64, abs, "", infoBeside, thumbBeside, subBeside, meta, t.ID); err != nil {
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
				if id, err := d.Library.MaybeEnqueueMediaVerifyForImport(t.VideoID.Int64, t.ID); err != nil {
					progress("Verify enqueue failed: "+err.Error(), nil)
				} else if id == 0 {
					progress("Integrity check skipped (File integrity off)", nil)
				}
			}
			softEnqueueImportSidecarGapFill(d, t.VideoID.Int64, progress)
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
		upload := ""
		if dlctx.Video.UploadDate.Valid {
			upload = dlctx.Video.UploadDate.String
		}
		if upload != "" {
			sNum, eNum, aerr := d.Library.AssignSeasonEpisode(dlctx.Video.SeriesID, upload, 0, t.VideoID.Int64)
			if aerr != nil {
				return aerr
			}
			season, episode = sNum, eNum
		}
		infoSrc, thumbCompanion, subSrcs := library.FindDownloadSidecars(abs)
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
		thumbURL := ""
		if dlctx.Video.ThumbnailURL.Valid {
			thumbURL = dlctx.Video.ThumbnailURL.String
		}
		thumbSrc, cleanupThumb := library.MaterializeThumbSrc(thumbCompanion, thumbURL)
		defer cleanupThumb()
		mediaPath, nfoPath, infoPath, thumbPath, subPaths, pathSuffix, err := library.PackMedia(
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
			library.NamingConfig{EpisodeFormat: dlctx.EpisodeFormat}, infoSrc, thumbSrc, subSrcs,
		)
		if err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "install failed"), err.Error())
		}
		if pathSuffix > 0 {
			chosen := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
			ideal := strings.TrimSuffix(chosen, fmt.Sprintf("_%d", pathSuffix))
			_ = notify.PathCollisionPacked(ctx, d.Library.DB, t.ID, dlctx.SeriesTitle, dlctx.Video.Title, ideal, chosen, pathSuffix)
		}
		// Drop leftover inbox sidecars after a successful pack (media was moved).
		leftovers := []string{srcNFO, infoSrc}
		if thumbPath != "" {
			leftovers = append(leftovers, thumbCompanion)
		}
		if len(subPaths) > 0 {
			leftovers = append(leftovers, subSrcs...)
		}
		for _, leftover := range leftovers {
			if leftover == "" {
				continue
			}
			_ = os.Remove(leftover)
		}
		meta := library.MediaCompleteMeta{
			AcquiredVia: library.AcquiredViaImport,
			ImportSrc:   abs,
		}
		if err := d.Library.CompleteImport(t.VideoID.Int64, mediaPath, nfoPath, infoPath, thumbPath, subPaths, meta, t.ID); err != nil {
			return err
		}
		if year := library.SeasonYearFromUpload(aired); year > 0 {
			progress("Aligning episode numbers…", ptrFloat(0.7))
			_, _ = d.Library.AlignSeriesYearEpisodes(dlctx.Video.SeriesID, int64(year), t.ID)
		}
		// NFO soft-fill already ran above when present; ffprobe when duration still empty.
		_ = d.Library.SoftFillDurationFromMedia(ctx, t.VideoID.Int64, mediaPath)
		if _, err := d.Library.RewriteVideoNFO(t.VideoID.Int64, 0); err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "write nfo failed"), err.Error())
		}
		if payload.Verify {
			if id, err := d.Library.MaybeEnqueueMediaVerifyForImport(t.VideoID.Int64, t.ID); err != nil {
				progress("Verify enqueue failed: "+err.Error(), nil)
			} else if id == 0 {
				progress("Integrity check skipped (File integrity off)", nil)
			}
		}
		softEnqueueImportSidecarGapFill(d, t.VideoID.Int64, progress)
		progress("Done", ptrFloat(1))
		return nil
	}
}

func softEnqueueImportSidecarGapFill(d Deps, videoID int64, progress func(msg string, pct *float64)) {
	if d.Library == nil {
		return
	}
	id, enqueued, err := d.Library.MaybeEnqueueImportSidecarGapFill(videoID)
	if err != nil {
		if progress != nil {
			progress("Sidecar gap-fill enqueue skipped: "+err.Error(), nil)
		}
		return
	}
	if enqueued && progress != nil {
		progress(fmt.Sprintf("Queued metadata gap-fill from source (task %d)", id), nil)
	}
}
