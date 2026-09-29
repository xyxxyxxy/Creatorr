package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func ImportPlanHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil || d.Library.Queue == nil {
			return apperrors.New(apperrors.CodeInternal, "import deps missing")
		}
		var payload library.ImportPlanPayload
		if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan payload invalid")
		}
		if len(payload.Jobs) == 0 {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan missing jobs")
		}
		if payload.DraftSeriesIDs == nil {
			payload.DraftSeriesIDs = map[string]int64{}
		}
		// Resolve series_id for bind jobs so units group correctly on resume.
		for i := range payload.Jobs {
			j := &payload.Jobs[i]
			if j.VideoID > 0 && j.SeriesID <= 0 && strings.TrimSpace(j.SeriesDraftKey) == "" {
				if v, err := d.Library.GetVideo(j.VideoID); err == nil && v != nil {
					j.SeriesID = v.SeriesID
				}
			}
		}
		units := library.BuildImportPlanUnits(payload)
		if len(units) == 0 {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan has no series units")
		}
		persist := func() error {
			return d.Library.Queue.UpdatePayload(t.ID, payload.PersistMap())
		}
		totalJobs := len(payload.Jobs)
		doneJobs := 0
		for ui := payload.SeriesUnitIndex; ui < len(units); ui++ {
			payload.SeriesUnitIndex = ui
			unit := units[ui]
			if err := persist(); err != nil {
				return err
			}
			seriesID := unit.SeriesID
			if unit.Draft != nil {
				dk := unit.Draft.DraftKey
				if sid, ok := payload.DraftSeriesIDs[dk]; ok && sid > 0 {
					seriesID = sid
				} else {
					progress(fmt.Sprintf("Creating series %d/%d…", ui+1, len(units)),
						ptrFloat(float64(doneJobs)/float64(importPlanJobDenom(totalJobs+len(units)))*0.95))
					sid, err := d.Library.ApplyImportSeriesDraft(*unit.Draft)
					if err != nil {
						return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "create series failed"), err.Error())
					}
					payload.DraftSeriesIDs[dk] = sid
					seriesID = sid
					if err := persist(); err != nil {
						return err
					}
				}
			} else if seriesID <= 0 && unit.DraftKey != "" {
				if sid, ok := payload.DraftSeriesIDs[unit.DraftKey]; ok {
					seriesID = sid
				}
			}
			startJob := 0
			if ui == payload.SeriesUnitIndex && payload.JobIndex > 0 {
				startJob = payload.JobIndex
			}
			for ji := startJob; ji < len(unit.Jobs); ji++ {
				payload.SeriesUnitIndex = ui
				payload.JobIndex = ji
				if err := persist(); err != nil {
					return err
				}
				jobIdx := unit.Jobs[ji]
				job := payload.Jobs[jobIdx]
				progress(fmt.Sprintf("Importing video %d/%d…", doneJobs+1, totalJobs),
					ptrFloat(0.05+0.9*float64(doneJobs)/float64(importPlanJobDenom(totalJobs))))
				if err := runImportPlanJob(ctx, d, t.ID, &payload, jobIdx, seriesID, progress); err != nil {
					_ = persist()
					pathHint := strings.TrimSpace(job.Path)
					if pathHint == "" && len(job.Paths) > 0 {
						pathHint = job.Paths[0]
					}
					msg := "import failed"
					if pathHint != "" {
						msg = "import failed: " + pathHint
					}
					return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, msg), err.Error())
				}
				doneJobs++
				payload.JobIndex = ji + 1
				if err := persist(); err != nil {
					return err
				}
			}
			payload.JobIndex = 0
			if unit.Draft != nil && normalizeImportDraftKind(unit.Draft.Kind) == library.ImportSeriesDraftNFO {
				_ = d.Library.RemoveImportSeriesFolderIfDrained(unit.Draft.FolderPath)
			}
			payload.SeriesUnitIndex = ui + 1
			if err := persist(); err != nil {
				return err
			}
		}
		progress("Done", ptrFloat(1))
		return nil
	}
}

func normalizeImportDraftKind(kind string) string {
	if strings.TrimSpace(kind) == library.ImportSeriesDraftBare {
		return library.ImportSeriesDraftBare
	}
	return library.ImportSeriesDraftNFO
}

func importPlanJobDenom(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func runImportPlanJob(
	ctx context.Context,
	d Deps,
	planTaskID int64,
	payload *library.ImportPlanPayload,
	jobIdx int,
	seriesID int64,
	progress func(msg string, pct *float64),
) error {
	job := &payload.Jobs[jobIdx]
	if job.VideoID > 0 {
		// Resume or bind: skip create; pack/attach if needed.
		if _, ok, err := d.Library.HasVideoFile(job.VideoID); err != nil {
			return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "import_plan video check failed"), err.Error())
		} else if ok && len(job.Paths) == 0 && job.Path == "" {
			return nil
		}
		paths := job.Paths
		if len(paths) == 0 && job.Path != "" {
			paths = []string{job.Path}
		}
		if len(paths) == 0 {
			return nil
		}
		return runImportMedia(ctx, d, planTaskID, job.VideoID, paths, "", job.Replace, progress)
	}
	sid := seriesID
	if sid <= 0 {
		sid = job.SeriesID
	}
	if sid <= 0 {
		if dk := strings.TrimSpace(job.SeriesDraftKey); dk != "" {
			if mapped, ok := payload.DraftSeriesIDs[dk]; ok {
				sid = mapped
			}
		}
	}
	if sid <= 0 {
		return apperrors.New(apperrors.CodeImportFailed, "import_plan job missing series")
	}
	videoID, abs, meta, err := d.Library.CreateImportVideo(job.Path, library.CreateImportVideoParams{
		SeriesID:    sid,
		Title:       job.Title,
		RemoteID:    job.RemoteID,
		HandlerID:   job.HandlerID,
		WebpageURL:  job.WebpageURL,
		UploadDate:  job.UploadDate,
		Description: job.Description,
	})
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "create video failed"), err.Error())
	}
	job.VideoID = videoID
	payload.Jobs[jobIdx] = *job
	_ = d.Library.AddVideoHistory(videoID, "import_created", "Created from import plan", map[string]any{
		"path":      abs,
		"remote_id": meta.RemoteID,
		"site":      meta.HandlerID,
	}, planTaskID)
	if err := runImportMedia(ctx, d, planTaskID, videoID, []string{abs}, "", false, progress); err != nil {
		// Pack failed after create: delete orphan with no media so rematch stays clean.
		if _, ok, herr := d.Library.HasVideoFile(videoID); herr == nil && !ok {
			_, _ = d.Library.DeleteVideo(videoID)
			job.VideoID = 0
			payload.Jobs[jobIdx] = *job
		}
		return err
	}
	return nil
}
