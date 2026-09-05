package api

import (
	"encoding/json"
	"net/http"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (s *Server) ListSeriesIds(w http.ResponseWriter, r *http.Request, params gen.ListSeriesIdsParams) {
	filter := library.SeriesListFilter{}
	if params.Q != nil {
		filter.Title = *params.Q
	}
	if params.Root != nil {
		filter.RootID = *params.Root
	}
	if params.Quality != nil {
		filter.QualityProfileID = *params.Quality
	}
	if params.Delivery != nil {
		filter.DeliveryMode = string(*params.Delivery)
	}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	ids, err := s.Library.ListSeriesIDsFiltered(filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list series ids failed", err.Error())
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	writeJSON(w, http.StatusOK, gen.SeriesIdsResponse{Ids: ids})
}

func (s *Server) BulkEditSeries(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkEditSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.BulkEditSeriesParams{SeriesIDs: body.SeriesIds}
	if body.RootId != nil {
		p.RootID = body.RootId
	}
	if body.QualityProfileId != nil {
		p.QualityProfileID = body.QualityProfileId
	}
	if body.DeliveryMode != nil {
		m := string(*body.DeliveryMode)
		p.DeliveryMode = &m
	}
	if body.Monitored != nil {
		p.Monitored = body.Monitored
	}
	id, err := s.Library.EnqueueBulkEditSeries(p)
	if err != nil {
		writeLibraryErr(w, err, "bulk edit series failed")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.EnqueueTaskResponse{Id: id})
}

func (s *Server) BulkEditSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkEditSeriesMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.BulkEditSeriesParams{SeriesIDs: body.SeriesIds}
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
	id, err := s.Library.EnqueueBulkEditSeries(p)
	if err != nil {
		writeLibraryErr(w, err, "bulk edit series metadata failed")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.EnqueueTaskResponse{Id: id})
}

func (s *Server) BulkSetSeriesMonitored(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkSetSeriesMonitoredRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	if len(body.SeriesIds) == 0 {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "series_ids required", "")
		return
	}
	updated, skipped, err := s.Library.SetSeriesMonitoredBulk(body.SeriesIds, body.Monitored)
	if err != nil {
		writeLibraryErr(w, err, "bulk set monitored failed")
		return
	}
	writeJSON(w, http.StatusOK, gen.BulkSetSeriesMonitoredResponse{Updated: updated, Skipped: skipped})
}

func (s *Server) BulkDeleteSeries(w http.ResponseWriter, r *http.Request) {
	var body gen.BulkDeleteSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	id, err := s.Library.EnqueueDeleteFiles(body.SeriesIds, nil)
	if err != nil {
		writeLibraryErr(w, err, "bulk delete series failed")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.EnqueueTaskResponse{Id: id})
}
