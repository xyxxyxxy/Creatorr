package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (s *Server) ListSeriesVideoIds(w http.ResponseWriter, r *http.Request, id gen.SeriesId, params gen.ListSeriesVideoIdsParams) {
	if _, err := s.Library.GetSeries(int64(id), false); err != nil {
		writeLibraryErr(w, err, "series not found")
		return
	}
	filter := library.VideoListFilter{}
	if params.Q != nil {
		filter.Title = *params.Q
	}
	if params.Source != nil {
		filter.SourceID = *params.Source
	}
	if params.Status != nil {
		for _, part := range strings.Split(*params.Status, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				filter.Statuses = append(filter.Statuses, part)
			}
		}
	}
	if params.Year != nil {
		y := strings.TrimSpace(*params.Year)
		if y == "unknown" {
			filter.Year = library.VideoYearUnknown
		} else if y != "" {
			n, err := strconv.Atoi(y)
			if err == nil && n > 0 {
				filter.Year = n
			}
		}
	}
	ids, err := s.Library.ListVideoIDsFiltered(int64(id), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list video ids failed", err.Error())
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	writeJSON(w, http.StatusOK, gen.VideoIdsResponse{Ids: ids})
}

func (s *Server) BulkEditVideosMetadata(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkEditVideosMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.BulkEditVideosParams{VideoIDs: body.VideoIds}
	if body.Studio != nil {
		p.Studio = body.Studio
	}
	if body.Country != nil {
		p.Country = body.Country
	}
	if body.Mpaa != nil {
		p.MPAA = body.Mpaa
	}
	if body.Genres != nil {
		g := *body.Genres
		p.Genres = &g
	}
	if body.Tags != nil {
		t := *body.Tags
		p.Tags = &t
	}
	if body.Actors != nil {
		actors := make([]library.SeriesActor, 0, len(*body.Actors))
		for i, a := range *body.Actors {
			role := ""
			if a.Role != nil {
				role = *a.Role
			}
			order := i
			if a.Order != nil {
				order = *a.Order
			}
			actors = append(actors, library.SeriesActor{Name: a.Name, Role: role, Order: order})
		}
		p.Actors = &actors
	}
	tid, err := s.Library.EnqueueBulkEditVideos(p)
	if err != nil {
		writeLibraryErr(w, err, "bulk edit videos metadata failed")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.EnqueueTaskResponse{Id: tid})
}

func (s *Server) BulkWantVideos(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkVideoIdsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	updated, skipped, err := s.Library.WantVideosBulk(body.VideoIds)
	if err != nil {
		writeLibraryErr(w, err, "bulk want videos failed")
		return
	}
	writeJSON(w, http.StatusOK, gen.BulkVideoActionResponse{Updated: updated, Skipped: skipped})
}

func (s *Server) BulkIgnoreVideos(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkVideoIdsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	updated, skipped, err := s.Library.IgnoreVideosBulk(body.VideoIds)
	if err != nil {
		writeLibraryErr(w, err, "bulk ignore videos failed")
		return
	}
	writeJSON(w, http.StatusOK, gen.BulkVideoActionResponse{Updated: updated, Skipped: skipped})
}

func (s *Server) BulkDownloadVideos(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkVideoIdsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	queued, skipped, err := s.Library.EnqueueDownloadVideosBulk(body.VideoIds)
	if err != nil {
		writeLibraryErr(w, err, "bulk download videos failed")
		return
	}
	writeJSON(w, http.StatusOK, gen.BulkVideoActionResponse{Updated: queued, Skipped: skipped})
}

func (s *Server) BulkRefreshSidecarsVideos(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkVideoIdsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	queued, skipped, err := s.Library.EnqueueRefreshSidecarsVideosBulk(body.VideoIds)
	if err != nil {
		writeLibraryErr(w, err, "bulk refresh sidecars failed")
		return
	}
	writeJSON(w, http.StatusOK, gen.BulkVideoActionResponse{Updated: queued, Skipped: skipped})
}

func (s *Server) BulkDeleteVideos(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkVideoIdsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	tid, _, _, err := s.Library.EnqueueBulkDeleteVideos(body.VideoIds)
	if err != nil {
		writeLibraryErr(w, err, "bulk delete videos failed")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.EnqueueTaskResponse{Id: tid})
}
