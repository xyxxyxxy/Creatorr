package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) actionFetchAddSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sourceURL := strings.TrimSpace(r.FormValue("source_url"))
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if sourceURL == "" {
		writeJSON(http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}
	if h.Queue == nil {
		writeJSON(http.StatusServiceUnavailable, map[string]string{"error": "queue is not available"})
		return
	}
	token, err := newAddSeriesDraftToken()
	if err != nil {
		writeJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tid, err := h.Library.EnqueueAddSeriesPrefetch(sourceURL, token)
	if err != nil {
		writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(http.StatusOK, map[string]any{"task_id": tid, "draft_token": token})
}

func newAddSeriesDraftToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) addSeriesPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Kind != queue.KindPrefetchAddSeries {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	token := queue.DraftTokenFromPayload(task.Payload)
	out := map[string]any{
		"status":      task.Status,
		"task_id":     tid,
		"draft_token": token,
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusFailed, queue.StatusCancelled:
		msg := task.ErrorMessage
		if msg == "" {
			msg = "Prefetch failed"
		}
		if token != "" {
			if d, err := h.Library.ReadAddSeriesDraft(token); err == nil && strings.TrimSpace(d.Error) != "" {
				msg = d.Error
			}
		}
		out["error"] = msg
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusDone:
		if token == "" {
			out["error"] = "draft token missing"
			writeJSON(http.StatusOK, out)
			return
		}
		draft, err := h.Library.ReadAddSeriesDraft(token)
		if err != nil {
			out["error"] = "draft not found"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Error) != "" {
			out["error"] = draft.Error
			writeJSON(http.StatusOK, out)
			return
		}
		title := library.SeriesTitleFromDraft(draft)
		if title == "" {
			out["error"] = "could not determine series title from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		out["title"] = title
		writeJSON(http.StatusOK, out)
		return
	default:
		out["error"] = "unexpected task status"
		writeJSON(http.StatusOK, out)
	}
}

func (h *Handler) actionFetchAddVideo(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sourceURL := strings.TrimSpace(r.FormValue("url"))
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(r.FormValue("source_url"))
	}
	seriesID, _ := strconv.ParseInt(r.FormValue("series_id"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if sourceURL == "" {
		writeJSON(http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}
	if h.Queue == nil {
		writeJSON(http.StatusServiceUnavailable, map[string]string{"error": "queue is not available"})
		return
	}
	token, err := newAddSeriesDraftToken()
	if err != nil {
		writeJSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tid, err := h.Library.EnqueueAddVideoPrefetch(sourceURL, token, seriesID)
	if err != nil {
		writeJSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(http.StatusOK, map[string]any{"task_id": tid, "draft_token": token})
}

func (h *Handler) addVideoPrefetchStatus(w http.ResponseWriter, r *http.Request) {
	tid, _ := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	task, err := h.Queue.GetTask(tid)
	if err != nil || task == nil {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Kind != queue.KindPrefetchAddVideo {
		writeJSON(http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	token := queue.DraftTokenFromPayload(task.Payload)
	out := map[string]any{
		"status":      task.Status,
		"task_id":     tid,
		"draft_token": token,
	}
	switch task.Status {
	case queue.StatusPending, queue.StatusRunning:
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusFailed, queue.StatusCancelled:
		msg := task.ErrorMessage
		if msg == "" {
			msg = "Prefetch failed"
		}
		if token != "" {
			if d, err := h.Library.ReadAddVideoDraft(token); err == nil && strings.TrimSpace(d.Error) != "" {
				msg = d.Error
			}
		}
		out["error"] = msg
		writeJSON(http.StatusOK, out)
		return
	case queue.StatusDone:
		if token == "" {
			out["error"] = "draft token missing"
			writeJSON(http.StatusOK, out)
			return
		}
		draft, err := h.Library.ReadAddVideoDraft(token)
		if err != nil {
			out["error"] = "draft not found"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Error) != "" {
			out["error"] = draft.Error
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.Title) == "" {
			out["error"] = "could not determine video title from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		if strings.TrimSpace(draft.UploadDate) == "" {
			out["error"] = "could not determine upload date from URL"
			writeJSON(http.StatusOK, out)
			return
		}
		out["title"] = draft.Title
		out["remote_id"] = draft.RemoteID
		out["upload_date"] = draft.UploadDate
		out["source_url"] = draft.SourceURL
		writeJSON(http.StatusOK, out)
		return
	default:
		out["error"] = "unexpected task status"
		writeJSON(http.StatusOK, out)
	}
}

func (h *Handler) actionAddSeries(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	rootID, _ := strconv.ParseInt(r.PostFormValue("root_id"), 10, 64)
	qpID, _ := strconv.ParseInt(r.PostFormValue("quality_profile_id"), 10, 64)
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	title := strings.TrimSpace(r.PostFormValue("title"))
	delivery := r.PostFormValue("delivery_mode")
	draftToken := strings.TrimSpace(r.PostFormValue("draft_token"))
	// Monitored defaults on at create; toggle only from series list.
	wantJSON := strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.FormValue("response") == "json" ||
		r.URL.Query().Get("response") == "json"

	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	redirErr := func(msg string) {
		if wantJSON {
			writeJSON(http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		http.Redirect(w, r, "/?err="+urlQuery(msg)+"&add=1", http.StatusSeeOther)
	}
	doneOK := func(ser *library.Series, warn string) {
		if wantJSON {
			out := map[string]any{"id": ser.ID, "title": ser.Title}
			if warn != "" {
				out["warning"] = warn
			}
			writeJSON(http.StatusCreated, out)
			return
		}
		if warn != "" {
			http.Redirect(w, r, fmt.Sprintf("/series/%d?err=%s", ser.ID, urlQuery(warn)), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/series/%d", ser.ID), http.StatusSeeOther)
	}

	if sourceURL != "" {
		var draft library.PrefetchDraft
		if draftToken != "" {
			if d, err := h.Library.ReadAddSeriesDraft(draftToken); err == nil {
				draft = d
			}
		}
		if title == "" {
			title = library.SeriesTitleFromDraft(draft)
		}
		if title == "" {
			redirErr("title is required - fetch metadata again or enter a title")
			return
		}

		sched, err := parseFeedScanCron(r, "@weekly")
		if err != nil {
			redirErr(err.Error())
			return
		}
		scanCron := sched
		fullScanLimit, err := parseFullScanLimitForm(r)
		if err != nil {
			redirErr(err.Error())
			return
		}

		ser, err := h.Library.CreateSeries(library.CreateSeriesParams{
			Title:              title,
			SourceURL:          sourceURL,
			RootID:             rootID,
			QualityProfileID:   qpID,
			Monitored:          r.FormValue("monitored") == "1",
			DeliveryMode:       delivery,
			FullScanLimit:      fullScanLimit,
			ScanCron:           scanCron,
			IndexAsIgnored:     r.FormValue("index_as_ignored") == "1",
			TitleRegexpInclude: strings.TrimSpace(r.FormValue("title_regexp_include")),
			TitleRegexpExclude: strings.TrimSpace(r.FormValue("title_regexp_exclude")),
			SourceLabel:        strings.TrimSpace(r.FormValue("source_label")),
		})
		if err != nil {
			redirErr(err.Error())
			return
		}
		warn := ""
		if draft.Plot != "" || draft.Studio != "" || draft.OriginalTitle != "" || len(draft.ArtFiles) > 0 ||
			draft.UniqueIDValue != "" || len(draft.Actors) > 0 {
			if err := h.Library.SaveSeriesMetadata(ser.ID, library.SaveSeriesMetadataParams{
				Plot:          draft.Plot,
				SortTitle:     draft.SortTitle,
				OriginalTitle: draft.OriginalTitle,
				Studio:        draft.Studio,
				UniqueIDType:  draft.UniqueIDType,
				UniqueIDValue: draft.UniqueIDValue,
				Actors:        draft.Actors,
				ArtSrc:        draft.ArtFiles,
			}); err != nil {
				slog.Warn("add series metadata save", "series_id", ser.ID, "err", err)
				warn = "series created but metadata save failed: " + err.Error()
			}
		}
		if draftToken != "" {
			_ = h.Library.ClearAddSeriesDraft(draftToken)
		}
		doneOK(ser, warn)
		return
	}

	if title == "" {
		redirErr("title is required when creating manually")
		return
	}
	ser, err := h.Library.CreateSeries(library.CreateSeriesParams{
		Title:            title,
		RootID:           rootID,
		QualityProfileID: qpID,
		Monitored:        true,
		DeliveryMode:     delivery,
	})
	if err != nil {
		redirErr(err.Error())
		return
	}
	doneOK(ser, "")
}
