package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/api/gen"
	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/library"
)

func (s *Server) ListRoots(w http.ResponseWriter, r *http.Request) {
	list, err := s.Library.ListRoots()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list roots failed", err.Error())
		return
	}
	out := make([]gen.RootFolder, 0, len(list))
	for _, root := range list {
		out = append(out, mapRoot(root))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateRoot(w http.ResponseWriter, r *http.Request) {
	var body gen.CreateRootRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	name := ""
	if body.Name != nil {
		name = *body.Name
	}
	epFmt := ""
	if body.EpisodeFormat != nil {
		epFmt = *body.EpisodeFormat
	}
	root, err := s.Library.CreateRoot(name, body.Path, epFmt, body.RetentionTtlSeconds)
	if err != nil {
		writeLibraryErr(w, err, "create root failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapRoot(*root))
}

func (s *Server) UpdateRoot(w http.ResponseWriter, r *http.Request, id gen.RootId) {
	var body gen.UpdateRootRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	clearRetention := false
	var retention *int64
	if body.RetentionTtlSeconds != nil {
		retention = body.RetentionTtlSeconds
	}
	root, err := s.Library.UpdateRoot(int64(id), body.Name, body.Path, body.EpisodeFormat, retention, clearRetention)
	if err != nil {
		writeLibraryErr(w, err, "update root failed")
		return
	}
	writeJSON(w, http.StatusOK, mapRoot(*root))
}

func (s *Server) DeleteRoot(w http.ResponseWriter, r *http.Request, id gen.RootId) {
	if err := s.Library.DeleteRoot(int64(id)); err != nil {
		writeLibraryErr(w, err, "delete root failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListQualityProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := s.Library.ListProfiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list profiles failed", err.Error())
		return
	}
	out := make([]gen.QualityProfile, 0, len(list))
	for _, p := range list {
		out = append(out, mapProfile(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateQualityProfile(w http.ResponseWriter, r *http.Request) {
	var body gen.CreateQualityProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	hours, sidecarHours := 0, 0
	if body.MaturityRedownloadHours != nil {
		hours = *body.MaturityRedownloadHours
	}
	if body.MaturitySidecarHours != nil {
		sidecarHours = *body.MaturitySidecarHours
	}
	var mark, remove []string
	if body.SponsorblockMark != nil {
		mark = *body.SponsorblockMark
	}
	if body.SponsorblockRemove != nil {
		remove = *body.SponsorblockRemove
	}
	infoCards := false
	if body.SponsorblockInfoCards != nil {
		infoCards = *body.SponsorblockInfoCards
	}
	reencode := false
	if body.SponsorblockReencodeCut != nil {
		reencode = *body.SponsorblockReencodeCut
	}
	verifyMedia := false
	if body.VerifyMedia != nil {
		verifyMedia = *body.VerifyMedia
	}
	p, err := s.Library.CreateProfileFull(body.Name, body.FormatSelector, hours, sidecarHours, mark, remove, reencode, infoCards, verifyMedia)
	if err != nil {
		writeLibraryErr(w, err, "create profile failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapProfile(*p))
}

func (s *Server) UpdateQualityProfile(w http.ResponseWriter, r *http.Request, id gen.QualityProfileId) {
	var body gen.UpdateQualityProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p, err := s.Library.UpdateProfileParams(int64(id), library.UpdateProfileParams{
		Name:                    body.Name,
		FormatSelector:          body.FormatSelector,
		MaturityRedownloadHours: body.MaturityRedownloadHours,
		MaturitySidecarHours:    body.MaturitySidecarHours,
		SponsorBlockMark:        body.SponsorblockMark,
		SponsorBlockRemove:      body.SponsorblockRemove,
		SponsorBlockReencodeCut: body.SponsorblockReencodeCut,
		SponsorBlockInfoCards:   body.SponsorblockInfoCards,
		VerifyMedia:             body.VerifyMedia,
	})
	if err != nil {
		writeLibraryErr(w, err, "update profile failed")
		return
	}
	writeJSON(w, http.StatusOK, mapProfile(*p))
}

func (s *Server) DeleteQualityProfile(w http.ResponseWriter, r *http.Request, id gen.QualityProfileId) {
	if err := s.Library.DeleteProfile(int64(id)); err != nil {
		writeLibraryErr(w, err, "delete profile failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListSeries(w http.ResponseWriter, r *http.Request) {
	list, err := s.Library.ListSeries()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, "list series failed", err.Error())
		return
	}
	out := make([]gen.Series, 0, len(list))
	for _, ser := range list {
		out = append(out, mapSeries(s.Library, ser, false, nil))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateSeries(w http.ResponseWriter, r *http.Request) {
	var body gen.CreateSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.CreateSeriesParams{
		RootID:           body.RootId,
		QualityProfileID: body.QualityProfileId,
		Monitored:        true,
	}
	if body.Title != nil {
		p.Title = *body.Title
	}
	if body.SourceUrl != nil {
		p.SourceURL = *body.SourceUrl
	}
	if body.Monitored != nil {
		p.Monitored = *body.Monitored
	}
	if body.FullScanLimit != nil {
		p.FullScanLimit = *body.FullScanLimit
	}
	if body.TitleRegexpInclude != nil {
		p.TitleRegexpInclude = *body.TitleRegexpInclude
	}
	if body.TitleRegexpExclude != nil {
		p.TitleRegexpExclude = *body.TitleRegexpExclude
	}
	ser, err := s.Library.CreateSeries(p)
	if err != nil {
		writeLibraryErr(w, err, "create series failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapSeries(s.Library, *ser, false, nil))
}

func (s *Server) CreateSeriesVideo(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	var body gen.CreateVideoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	draftToken := ""
	if body.DraftToken != nil {
		draftToken = strings.TrimSpace(*body.DraftToken)
	}
	title := ""
	if body.Title != nil {
		title = strings.TrimSpace(*body.Title)
	}
	upload := ""
	if body.UploadDate != nil {
		upload = strings.TrimSpace(*body.UploadDate)
	}
	var (
		v   *library.Video
		err error
	)
	if draftToken != "" {
		v, err = s.Library.CreateIndexedVideoFromAddDraft(int64(id), draftToken, title, upload)
		if err == nil {
			_ = s.Library.ClearAddVideoDraft(draftToken)
		}
	} else {
		remoteID := ""
		if body.RemoteId != nil {
			remoteID = strings.TrimSpace(*body.RemoteId)
		}
		v, err = s.Library.CreateIndexedVideo(library.CreateIndexedVideoParams{
			SeriesID:   int64(id),
			Title:      title,
			RemoteID:   remoteID,
			UploadDate: upload,
		})
	}
	if err != nil {
		writeLibraryErr(w, err, "create video failed")
		return
	}
	sizes, _ := s.Library.VideoSizeBytesMap([]int64{v.ID})
	writeJSON(w, http.StatusCreated, mapVideoWithSize(*v, sizes))
}

func (s *Server) GetSeries(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	ser, err := s.Library.GetSeries(int64(id), true)
	if err != nil {
		writeLibraryErr(w, err, "get series failed")
		return
	}
	ids := make([]int64, 0, len(ser.Videos))
	for _, v := range ser.Videos {
		ids = append(ids, v.ID)
	}
	sizes, _ := s.Library.VideoSizeBytesMap(ids)
	writeJSON(w, http.StatusOK, mapSeries(s.Library, *ser, true, sizes))
}

func (s *Server) DeleteSeries(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	// API keeps library files; UI delete requires delete_files confirm and always purges disk.
	if err := s.Library.DeleteSeries(int64(id), false); err != nil {
		writeLibraryErr(w, err, "delete series failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ScanSeries(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	taskID, err := s.Library.EnqueueScan(int64(id))
	if err != nil {
		writeLibraryErr(w, err, "scan enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: taskID})
}

func (s *Server) MetadataRescanSeries(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	taskID, err := s.Library.EnqueueMetadataRescanSeries(int64(id))
	if err != nil {
		writeLibraryErr(w, err, "metadata rescan enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: taskID})
}

func (s *Server) MetadataRescanVideo(w http.ResponseWriter, r *http.Request, id gen.VideoId) {
	taskID, err := s.Library.EnqueueMetadataRescanVideo(int64(id))
	if err != nil {
		writeLibraryErr(w, err, "metadata rescan enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, gen.EnqueueTaskResponse{Id: taskID})
}

func (s *Server) AddSource(w http.ResponseWriter, r *http.Request, id gen.SeriesId) {
	var body gen.CreateSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.AddSourceParams{URL: body.Url}
	if body.Label != nil {
		p.Label = *body.Label
	}
	if body.Kind != nil {
		p.Kind = string(*body.Kind)
	}
	if body.FullScanLimit != nil {
		p.FullScanLimit = *body.FullScanLimit
	}
	if body.TitleRegexpInclude != nil {
		p.TitleRegexpInclude = *body.TitleRegexpInclude
	}
	if body.TitleRegexpExclude != nil {
		p.TitleRegexpExclude = *body.TitleRegexpExclude
	}
	src, err := s.Library.AddSource(int64(id), p)
	if err != nil {
		writeLibraryErr(w, err, "add source failed")
		return
	}
	writeJSON(w, http.StatusCreated, mapSource(s.Library, *src))
}

func (s *Server) UpdateSource(w http.ResponseWriter, r *http.Request, id gen.SeriesId, sourceId gen.SourceId) {
	var body gen.UpdateSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, "invalid JSON", err.Error())
		return
	}
	p := library.UpdateSourceParams{
		Label:              body.Label,
		FullScanLimit:      body.FullScanLimit,
		TitleRegexpInclude: body.TitleRegexpInclude,
		TitleRegexpExclude: body.TitleRegexpExclude,
	}
	src, err := s.Library.UpdateSource(int64(id), int64(sourceId), p)
	if err != nil {
		writeLibraryErr(w, err, "update source failed")
		return
	}
	writeJSON(w, http.StatusOK, mapSource(s.Library, *src))
}

func (s *Server) DeleteSource(w http.ResponseWriter, r *http.Request, id gen.SeriesId, sourceId gen.SourceId) {
	if err := s.Library.DeleteSource(int64(id), int64(sourceId)); err != nil {
		writeLibraryErr(w, err, "delete source failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetVideo(w http.ResponseWriter, r *http.Request, id gen.VideoId) {
	v, err := s.Library.GetVideo(int64(id))
	if err != nil {
		writeLibraryErr(w, err, "get video failed")
		return
	}
	sizes, _ := s.Library.VideoSizeBytesMap([]int64{v.ID})
	writeJSON(w, http.StatusOK, mapVideoWithSize(*v, sizes))
}

func (s *Server) WantVideo(w http.ResponseWriter, r *http.Request, id gen.VideoId) {
	v, err := s.Library.WantVideo(int64(id))
	if err != nil {
		writeLibraryErr(w, err, "want video failed")
		return
	}
	sizes, _ := s.Library.VideoSizeBytesMap([]int64{v.ID})
	writeJSON(w, http.StatusOK, mapVideoWithSize(*v, sizes))
}

func writeLibraryErr(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, apperrors.CodeNotFound, err.Error(), "")
	case errors.Is(err, library.ErrConflict):
		writeErr(w, http.StatusConflict, apperrors.CodeConflict, err.Error(), "")
	case errors.Is(err, library.ErrInvalid):
		writeErr(w, http.StatusBadRequest, apperrors.CodeInternal, err.Error(), "")
	default:
		writeErr(w, http.StatusInternalServerError, apperrors.CodeInternal, fallback, err.Error())
	}
}

func mapRoot(r library.RootFolder) gen.RootFolder {
	out := gen.RootFolder{Id: r.ID, Name: r.Name, Path: r.Path, EpisodeFormat: r.EpisodeFormat}
	if r.RetentionTTLSeconds.Valid {
		v := r.RetentionTTLSeconds.Int64
		out.RetentionTtlSeconds = &v
	}
	return out
}

func mapProfile(p library.QualityProfile) gen.QualityProfile {
	mark := append([]string(nil), p.SponsorBlockMark...)
	remove := append([]string(nil), p.SponsorBlockRemove...)
	if mark == nil {
		mark = []string{}
	}
	if remove == nil {
		remove = []string{}
	}
	return gen.QualityProfile{
		Id:                      p.ID,
		Name:                    p.Name,
		FormatSelector:          p.FormatSelector,
		MaturityRedownloadHours: p.MaturityRedownloadHours,
		MaturitySidecarHours:    p.MaturitySidecarHours,
		SponsorblockMark:        mark,
		SponsorblockRemove:      remove,
		SponsorblockReencodeCut: p.SponsorBlockReencodeCut,
		SponsorblockInfoCards:   p.SponsorBlockInfoCards,
		VerifyMedia:             p.VerifyMedia,
	}
}

func mapSource(lib *library.Store, src library.Source) gen.Source {
	kind := gen.SourceKind(library.NormalizeSourceKind(src.Kind))
	out := gen.Source{
		Id:        src.ID,
		SeriesId:  src.SeriesID,
		Url:       src.URL,
		Kind:      kind,
		Monitored: true, // deprecated API field; use series.monitored + domain.active
	}
	if src.Label.Valid {
		s := src.Label.String
		out.Label = &s
	}
	if src.FullScanLimit > 0 {
		n := src.FullScanLimit
		out.FullScanLimit = &n
	}
	if src.TitleRegexpInclude != "" {
		s := src.TitleRegexpInclude
		out.TitleRegexpInclude = &s
	}
	if src.TitleRegexpExclude != "" {
		s := src.TitleRegexpExclude
		out.TitleRegexpExclude = &s
	}
	if lib != nil {
		st, _ := lib.LatestSourceScanStatus(src.ID)
		if st.Event == library.SourceHistScanError {
			if st.LastErrorCode != "" {
				c := st.LastErrorCode
				out.LastErrorCode = &c
			}
			if st.LastErrorMessage != "" {
				m := st.LastErrorMessage
				out.LastErrorMessage = &m
			}
		}
		if st.LastScannedAt != "" {
			ts := parseTime(st.LastScannedAt)
			out.LastScannedAt = &ts
		}
	}
	return out
}

func mapVideo(v library.Video) gen.Video {
	out := gen.Video{
		Id:       v.ID,
		SeriesId: v.SeriesID,
		RemoteId: v.RemoteID,
		Title:    v.Title,
		Status:   v.Status,
	}
	via := gen.VideoAcquiredVia(library.NormalizeAcquiredVia(v.AcquiredVia))
	out.AcquiredVia = &via
	if v.Description != "" {
		d := v.Description
		out.Description = &d
	}
	if v.SourceID.Valid {
		id := v.SourceID.Int64
		out.SourceId = &id
	}
	if v.UploadDate.Valid {
		s := v.UploadDate.String
		out.UploadDate = &s
	}
	if v.SourceURL.Valid {
		s := v.SourceURL.String
		out.SourceUrl = &s
	}
	if v.ThumbnailURL.Valid {
		s := v.ThumbnailURL.String
		out.ThumbnailUrl = &s
	}
	if v.MediaType != "" {
		mt := v.MediaType
		out.MediaType = &mt
	}
	if v.Season.Valid {
		n := int(v.Season.Int64)
		out.Season = &n
	}
	if v.Episode.Valid {
		n := int(v.Episode.Int64)
		out.Episode = &n
	}
	if label := v.ResolutionLabel(); label != "" {
		r := gen.VideoResolution(label)
		out.Resolution = &r
	}
	return out
}

func mapVideoWithSize(v library.Video, sizes map[int64]int64) gen.Video {
	out := mapVideo(v)
	if n, ok := sizes[v.ID]; ok {
		out.SizeBytes = &n
	}
	return out
}

func mapSeries(lib *library.Store, ser library.Series, withVideos bool, sizes map[int64]int64) gen.Series {
	out := gen.Series{
		Id:               ser.ID,
		Title:            ser.Title,
		RootId:           ser.RootID,
		QualityProfileId: ser.QualityProfileID,
		Monitored:        ser.Monitored,
		AddedAt:          parseTime(ser.AddedAt),
	}
	if ser.RootName != "" {
		n := ser.RootName
		out.RootName = &n
	}
	if ser.QualityProfileName != "" {
		n := ser.QualityProfileName
		out.QualityProfileName = &n
	}
	vc, dc, wc, sc := ser.VideoCount, ser.DownloadedCount, ser.WantedCount, ser.SourceCount
	out.VideoCount = &vc
	out.DownloadedCount = &dc
	out.WantedCount = &wc
	out.SourceCount = &sc
	srcs := make([]gen.Source, 0, len(ser.Sources))
	for _, src := range ser.Sources {
		srcs = append(srcs, mapSource(lib, src))
	}
	out.Sources = &srcs
	if withVideos {
		vids := make([]gen.Video, 0, len(ser.Videos))
		for _, v := range ser.Videos {
			vids = append(vids, mapVideoWithSize(v, sizes))
		}
		out.Videos = &vids
	}
	return out
}
