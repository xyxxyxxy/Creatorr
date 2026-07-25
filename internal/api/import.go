package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (s *Server) ScanImport(w http.ResponseWriter, r *http.Request) {
	res, err := s.Library.ScanImportInbox()
	if err != nil {
		writeLibraryErr(w, err, "import scan failed")
		return
	}
	writeJSON(w, http.StatusOK, mapImportScan(res))
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

	var taskID int64
	var err error
	if hasVideo {
		if len(paths) > 0 {
			taskID, err = s.Library.EnqueueAttachSidecars(*body.VideoId, paths)
		} else if path != "" {
			taskID, err = s.Library.EnqueueImport(path, *body.VideoId, verify)
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
