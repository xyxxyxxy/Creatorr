package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (s *Server) importBusy() (bool, error) {
	busy, err := s.Queue.HasPendingOrRunningKind(queue.KindImport, queue.SystemDomain)
	if err != nil || busy {
		return busy, err
	}
	return s.Queue.HasPendingOrRunningKind(queue.KindImportPlan, queue.SystemDomain)
}

func (s *Server) ScanImport(w http.ResponseWriter, r *http.Request) {
	if busy, err := s.importBusy(); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "import scan failed", err.Error())
		return
	} else if busy {
		writeLibraryErr(w, fmt.Errorf("%w: import already queued or running", library.ErrConflict), "import scan failed")
		return
	}
	res, err := s.Library.ScanImport()
	if err != nil {
		writeLibraryErr(w, err, "import scan failed")
		return
	}
	writeJSON(w, http.StatusOK, mapImportScan(res))
}

func (s *Server) GetImportPicker(w http.ResponseWriter, r *http.Request) {
	series, err := s.Library.ListImportPickerSeries()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "import picker series failed", err.Error())
		return
	}
	videos, err := s.Library.ListImportPickerVideos()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "import picker videos failed", err.Error())
		return
	}
	out := gen.ImportPickerResponse{
		Series: make([]gen.ImportPickerSeries, 0, len(series)),
		Videos: make([]gen.ImportPickerVideo, 0, len(videos)),
	}
	for _, ser := range series {
		poster := fmt.Sprintf("/series/%d/art/poster", ser.ID)
		out.Series = append(out.Series, gen.ImportPickerSeries{
			Id:        ser.ID,
			Title:     ser.Title,
			PosterUrl: &poster,
		})
	}
	for _, v := range videos {
		out.Videos = append(out.Videos, gen.ImportPickerVideo{
			Id:          v.ID,
			SeriesId:    v.SeriesID,
			Title:       v.Title,
			SeriesTitle: v.SeriesTitle,
			Status:      v.Status,
			HasMedia:    v.HasMedia,
			HasThumb:    v.HasThumb,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ImportManual(w http.ResponseWriter, r *http.Request) {
	var body gen.ImportManualRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	hasVideo := body.VideoId != nil && *body.VideoId > 0
	hasSeries := body.SeriesId != nil && *body.SeriesId > 0
	if hasVideo == hasSeries {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal,
			"provide exactly one of video_id or series_id", "")
		return
	}

	path := ""
	if body.Path != nil {
		path = strings.TrimSpace(*body.Path)
	}
	var paths []string
	if body.Paths != nil {
		for _, p := range *body.Paths {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
	}

	replace := body.Replace != nil && *body.Replace

	var taskID int64
	var err error
	if hasVideo {
		if len(paths) > 0 {
			taskID, err = s.Library.EnqueueAttachSidecars(*body.VideoId, paths)
		} else if path != "" {
			taskID, err = s.Library.EnqueueImport(path, *body.VideoId, replace)
		} else {
			writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "path or paths required", "")
			return
		}
	} else {
		if path == "" {
			writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "path required when creating", "")
			return
		}
		p := library.CreateImportVideoParams{SeriesID: *body.SeriesId}
		if body.Title != nil {
			p.Title = *body.Title
		}
		if body.RemoteId != nil {
			p.RemoteID = *body.RemoteId
		}
		if body.HandlerId != nil {
			p.HandlerID = *body.HandlerId
		}
		if body.SourceUrl != nil {
			p.WebpageURL = *body.SourceUrl
		}
		if body.UploadDate != nil {
			p.UploadDate = *body.UploadDate
		}
		if body.Description != nil {
			p.Description = *body.Description
		}
		taskID, _, err = s.Library.EnqueueImportCreate(path, p)
	}
	if err != nil {
		writeLibraryErr(w, err, "import enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: taskID})
}

func (s *Server) ImportConfirm(w http.ResponseWriter, r *http.Request) {
	if busy, err := s.importBusy(); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "import confirm failed", err.Error())
		return
	} else if busy {
		writeLibraryErr(w, fmt.Errorf("%w: import already queued or running", library.ErrConflict), "import confirm failed")
		return
	}
	var body gen.ImportConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	plan := library.ImportPlanPayload{
		Series: make([]library.ImportPlanSeriesDraft, 0, len(body.Series)),
		Jobs:   make([]library.ImportPlanJob, 0, len(body.Jobs)),
	}
	for _, ser := range body.Series {
		d := library.ImportPlanSeriesDraft{
			DraftKey:         ser.DraftKey,
			FolderPath:       ser.FolderPath,
			RootID:           ser.RootId,
			QualityProfileID: ser.QualityProfileId,
			Monitored:        ser.Monitored == nil || *ser.Monitored,
		}
		if ser.Title != nil {
			d.Title = *ser.Title // ignored for naming; EnqueueImportPlan re-resolves from disk
		}
		if ser.DeliveryMode != nil {
			d.DeliveryMode = string(*ser.DeliveryMode)
		}
		plan.Series = append(plan.Series, d)
	}
	for _, j := range body.Jobs {
		job := library.ImportPlanJob{}
		if j.Path != nil {
			job.Path = *j.Path
		}
		if j.Paths != nil {
			job.Paths = *j.Paths
		}
		if j.VideoId != nil {
			job.VideoID = *j.VideoId
		}
		if j.SeriesId != nil {
			job.SeriesID = *j.SeriesId
		}
		if j.SeriesDraftKey != nil {
			job.SeriesDraftKey = *j.SeriesDraftKey
		}
		if j.Title != nil {
			job.Title = *j.Title
		}
		if j.RemoteId != nil {
			job.RemoteID = *j.RemoteId
		}
		if j.HandlerId != nil {
			job.HandlerID = *j.HandlerId
		}
		if j.SourceUrl != nil {
			job.WebpageURL = *j.SourceUrl
		}
		if j.UploadDate != nil {
			job.UploadDate = *j.UploadDate
		}
		if j.Description != nil {
			job.Description = *j.Description
		}
		if j.Replace != nil {
			job.Replace = *j.Replace
		}
		plan.Jobs = append(plan.Jobs, job)
	}
	taskID, err := s.Library.EnqueueImportPlan(plan)
	if err != nil {
		writeLibraryErr(w, err, "import confirm failed")
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: taskID})
}

func mapImportScan(res *library.ImportScanResult) gen.ImportScanResponse {
	out := gen.ImportScanResponse{
		ImportPath:    res.ImportPath,
		Candidates:    make([]gen.ImportCandidate, 0, len(res.Candidates)),
		SeriesFolders: make([]gen.ImportSeriesFolder, 0, len(res.SeriesFolders)),
	}
	for _, c := range res.Candidates {
		gc := gen.ImportCandidate{
			Path:              c.Path,
			Filename:          c.Filename,
			Role:              gen.ImportCandidateRole(c.Role),
			SuggestedVideoId:  c.SuggestedVideoID,
			SuggestedSeriesId: c.SuggestedSeriesID,
			Ids:               make([]gen.ImportIDHint, 0, len(c.IDs)),
			VideoSuggestions:  make([]gen.ImportVideoSuggestion, 0, len(c.VideoSuggestions)),
			SeriesSuggestions: make([]gen.ImportSeriesSuggestion, 0, len(c.SeriesSuggestions)),
		}
		if c.SuggestedTitle != "" {
			t := c.SuggestedTitle
			gc.SuggestedTitle = &t
		}
		if c.SuggestedRemoteID != "" {
			rid := c.SuggestedRemoteID
			gc.SuggestedRemoteId = &rid
		}
		if c.SuggestedRemoteIDGenerated {
			g := true
			gc.SuggestedRemoteIdGenerated = &g
		}
		if c.SuggestedUploadDate != "" {
			ud := c.SuggestedUploadDate
			gc.SuggestedUploadDate = &ud
		}
		if c.SuggestedUploadDateFromMtime {
			m := true
			gc.SuggestedUploadDateFromMtime = &m
		}
		if c.SuggestedHandler != "" {
			h := c.SuggestedHandler
			gc.SuggestedHandlerId = &h
		}
		if c.MatchType != "" {
			mt := c.MatchType
			gc.MatchType = &mt
		}
		if c.MatchLabel != "" {
			ml := c.MatchLabel
			gc.MatchLabel = &ml
		}
		if c.SeriesFolderDraftKey != "" {
			dk := c.SeriesFolderDraftKey
			gc.SeriesFolderDraftKey = &dk
		}
		if c.SeriesFolderLocked {
			locked := true
			gc.SeriesFolderLocked = &locked
		}
		for _, id := range c.IDs {
			gc.Ids = append(gc.Ids, gen.ImportIDHint{HandlerId: id.HandlerID, RemoteId: id.RemoteID})
		}
		for _, v := range c.VideoSuggestions {
			sug := gen.ImportVideoSuggestion{
				VideoId: v.VideoID, SeriesId: v.SeriesID, Title: v.Title,
				SeriesTitle: v.SeriesTitle, Score: v.Score,
			}
			if v.RemoteID != "" {
				rid := v.RemoteID
				sug.RemoteId = &rid
			}
			gc.VideoSuggestions = append(gc.VideoSuggestions, sug)
		}
		for _, ser := range c.SeriesSuggestions {
			gc.SeriesSuggestions = append(gc.SeriesSuggestions, gen.ImportSeriesSuggestion{
				SeriesId: ser.SeriesID, Title: ser.Title, Score: ser.Score,
			})
		}
		out.Candidates = append(out.Candidates, gc)
	}
	for _, f := range res.SeriesFolders {
		gf := gen.ImportSeriesFolder{
			DraftKey:   f.DraftKey,
			FolderPath: f.FolderPath,
			SeriesId:   f.SeriesID,
			Unknown:    f.Unknown,
			Title:      f.Title,
			Monitored:  f.Monitored,
			ArtRoles:   f.ArtRoles,
			ArtPaths:   f.ArtPaths,
			MediaCount: f.MediaCount,
		}
		if gf.ArtRoles == nil {
			gf.ArtRoles = []string{}
		}
		if gf.ArtPaths == nil {
			gf.ArtPaths = []string{}
		}
		parsed := gen.ImportSeriesFolderParsed{
			Title:         &f.Parsed.Title,
			Sorttitle:     strPtrOrNil(f.Parsed.SortTitle),
			Originaltitle: strPtrOrNil(f.Parsed.OriginalTitle),
			Plot:          strPtrOrNil(f.Parsed.Plot),
			Tagline:       strPtrOrNil(f.Parsed.Tagline),
			Studio:        strPtrOrNil(f.Parsed.Studio),
			Country:       strPtrOrNil(f.Parsed.Country),
			Mpaa:          strPtrOrNil(f.Parsed.MPAA),
			Premiered:     strPtrOrNil(f.Parsed.Premiered),
			Monitored:     &f.Parsed.Monitored,
			UniqueidType:  strPtrOrNil(f.Parsed.UniqueIDType),
			UniqueidValue: strPtrOrNil(f.Parsed.UniqueIDValue),
		}
		if len(f.Parsed.Genres) > 0 {
			g := f.Parsed.Genres
			parsed.Genres = &g
		}
		if len(f.Parsed.Tags) > 0 {
			tg := f.Parsed.Tags
			parsed.Tags = &tg
		}
		gf.Parsed = &parsed
		out.SeriesFolders = append(out.SeriesFolders, gf)
	}
	return out
}

func strPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
