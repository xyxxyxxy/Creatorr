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

func (s *Server) ScanImport(w http.ResponseWriter, r *http.Request, params gen.ScanImportParams) {
	if busy, err := s.Queue.HasPendingOrRunningKind(queue.KindImport, queue.SystemDomain); err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "import scan failed", err.Error())
		return
	} else if busy {
		writeLibraryErr(w, fmt.Errorf("%w: import already queued or running", library.ErrConflict), "import scan failed")
		return
	}
	var rootID int64
	if params.RootId != nil {
		rootID = *params.RootId
	}
	res, err := s.Library.ScanImport(rootID)
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

	verify := body.Verify != nil && *body.Verify
	replace := body.Replace != nil && *body.Replace

	var taskID int64
	var err error
	if hasVideo {
		if len(paths) > 0 {
			taskID, err = s.Library.EnqueueAttachSidecars(*body.VideoId, paths)
		} else if path != "" {
			taskID, err = s.Library.EnqueueImport(path, *body.VideoId, verify, replace)
		} else {
			writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "path or paths required", "")
			return
		}
	} else {
		if path == "" {
			writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "path required when creating", "")
			return
		}
		p := library.CreateImportVideoParams{SeriesID: *body.SeriesId, Verify: verify}
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

func mapImportScan(res *library.ImportScanResult) gen.ImportScanResponse {
	out := gen.ImportScanResponse{
		ImportPath: res.ImportPath,
		Candidates: make([]gen.ImportCandidate, 0, len(res.Candidates)),
	}
	for _, c := range res.Candidates {
		gc := gen.ImportCandidate{
			Path:              c.Path,
			Filename:          c.Filename,
			Source:            gen.ImportCandidateSource(c.Source),
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
	return out
}
