package worker

import (
	"context"
	"encoding/json"
	"fmt"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func ImportPlanHandler(d Deps) TaskHandler {
	return func(ctx context.Context, t *queue.Task, progress func(msg string, pct *float64)) error {
		if d.Library == nil {
			return apperrors.New(apperrors.CodeInternal, "import deps missing")
		}
		var payload library.ImportPlanPayload
		if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan payload invalid")
		}
		if len(payload.Series) == 0 {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan missing series")
		}
		draftToID := map[string]int64{}
		nSeries := len(payload.Series)
		for i, draft := range payload.Series {
			progress(fmt.Sprintf("Creating series %d/%d…", i+1, nSeries), ptrFloat(float64(i)/float64(nSeries+len(payload.Jobs)+1)*0.4))
			sid, err := d.Library.ApplyImportSeriesFolder(draft)
			if err != nil {
				return apperrors.WithDetail(apperrors.New(apperrors.CodeImportFailed, "create series failed"), err.Error())
			}
			draftToID[draft.DraftKey] = sid
		}
		nJobs := len(payload.Jobs)
		for i, job := range payload.Jobs {
			base := 0.4 + 0.6*float64(i)/float64(importPlanJobDenom(nJobs))
			progress(fmt.Sprintf("Importing video %d/%d…", i+1, nJobs), ptrFloat(base))
			if err := runImportPlanJob(ctx, d, t.ID, job, draftToID, progress); err != nil {
				return err
			}
		}
		for _, draft := range payload.Series {
			_ = d.Library.RemoveImportSeriesFolderIfDrained(draft.FolderPath)
		}
		progress("Done", ptrFloat(1))
		return nil
	}
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
	job library.ImportPlanJob,
	draftToID map[string]int64,
	progress func(msg string, pct *float64),
) error {
	if job.VideoID > 0 {
		paths := job.Paths
		if len(paths) == 0 && job.Path != "" {
			paths = []string{job.Path}
		}
		return runImportMedia(ctx, d, planTaskID, job.VideoID, paths, "", job.Replace, progress)
	}
	seriesID := job.SeriesID
	if seriesID <= 0 {
		sid, ok := draftToID[job.SeriesDraftKey]
		if !ok || sid <= 0 {
			return apperrors.New(apperrors.CodeImportFailed, "import_plan job missing series")
		}
		seriesID = sid
	}
	videoID, abs, meta, err := d.Library.CreateImportVideo(job.Path, library.CreateImportVideoParams{
		SeriesID:    seriesID,
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
	_ = d.Library.AddVideoHistory(videoID, "import_created", "Created from import plan", map[string]any{
		"path":      abs,
		"remote_id": meta.RemoteID,
		"site":      meta.HandlerID,
	}, planTaskID)
	return runImportMedia(ctx, d, planTaskID, videoID, []string{abs}, "", false, progress)
}
