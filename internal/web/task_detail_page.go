package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func (h *Handler) taskDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	t, err := h.Queue.GetTask(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if t == nil {
		http.NotFound(w, r)
		return
	}
	now := time.Now().UTC()
	view := taskToHistoryView(*t, now)

	var series *seriesLink
	if view.SeriesID > 0 {
		if s, err := h.Library.GetSeries(view.SeriesID, false); err == nil {
			series = &seriesLink{ID: s.ID, Title: s.Title}
		} else {
			series = &seriesLink{ID: view.SeriesID, Title: fmt.Sprintf("#%d", view.SeriesID)}
		}
	}
	var videoLib *library.Video
	var video *videoLink
	if view.VideoID > 0 {
		if vv, err := h.Library.GetVideo(view.VideoID); err == nil {
			videoLib = vv
			video = &videoLink{ID: vv.ID, SeriesID: vv.SeriesID, Title: vv.Title}
			if series == nil {
				if s, err := h.Library.GetSeries(vv.SeriesID, false); err == nil {
					series = &seriesLink{ID: s.ID, Title: s.Title}
				}
			}
		} else {
			video = &videoLink{ID: view.VideoID, Title: fmt.Sprintf("#%d", view.VideoID)}
		}
	}

	var videoRow *seriesVideoRow
	if videoLib != nil {
		if rows := h.buildSeriesVideoRows([]library.Video{*videoLib}, nil, nil); len(rows) > 0 {
			videoRow = &rows[0]
		}
	}

	var source *sourceLink
	if sid := historySourceID(t, videoLib); sid > 0 {
		if src, err := h.Library.GetSourceByID(sid); err == nil {
			source = &sourceLink{ID: src.ID, Title: sourceBreadcrumbLabel(src)}
			if series == nil {
				if s, err := h.Library.GetSeries(src.SeriesID, false); err == nil {
					series = &seriesLink{ID: s.ID, Title: s.Title}
				} else {
					series = &seriesLink{ID: src.SeriesID, Title: fmt.Sprintf("#%d", src.SeriesID)}
				}
			}
		}
	}

	events, err := h.Library.ListVideoHistoryByTaskID(t.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	taskStarted, taskFinished := "", ""
	if t.StartedAt.Valid {
		taskStarted = t.StartedAt.String
	}
	if t.FinishedAt.Valid {
		taskFinished = t.FinishedAt.String
	}
	var parentTaskID int64
	parentKind := ""
	if t.ParentTaskID.Valid && t.ParentTaskID.Int64 > 0 {
		parentTaskID = t.ParentTaskID.Int64
		if pt, err := h.Queue.GetTask(parentTaskID); err == nil && pt != nil {
			parentKind = pt.Kind
		}
	}
	children, err := h.Queue.ListByParentTaskID(t.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	view.Origin = t.Origin
	if view.Origin == "" {
		view.Origin = queue.OriginManual
	}
	view.ParentTaskID = parentTaskID
	view.ParentKind = parentKind
	cookieAttach := parseCookieAttachDetail(t.Detail)
	stages := taskStages(taskStagesInput{
		Events:       events,
		Now:          now,
		Created:      t.CreatedAt,
		Started:      taskStarted,
		Finished:     taskFinished,
		Status:       t.Status,
		Origin:       t.Origin,
		ParentTaskID: parentTaskID,
		ParentKind:   parentKind,
		Children:     children,
		CookieAttach: cookieAttach,
	})
	failError := taskFailErrorText(t.ErrorMessage, t.Detail)
	detailFields := h.taskDetailFieldsOpts(t.Detail, failError != "")
	integrityChecks := h.buildIntegrityChecks(t, events)
	if integrityChecks != nil {
		detailFields = filterSuppressedDetailFields(detailFields, integrityDetailSuppressKeys)
	}
	if !singleVideoHistory(events) {
		histRows := make([]taskDetailHistRow, 0, len(events))
		for _, e := range events {
			row := taskDetailHistRow{Event: e.Event, Detail: e.Detail, VideoID: e.VideoID}
			if vv, err := h.Library.GetVideo(e.VideoID); err == nil {
				row.VideoTitle = vv.Title
				row.SeriesID = vv.SeriesID
			} else {
				row.VideoTitle = fmt.Sprintf("#%d", e.VideoID)
			}
			histRows = append(histRows, row)
		}
		if integrityChecks != nil {
			histRows = filterIntegrityHistoryRows(histRows)
		}
		detailFields = mergeVideoHistoryDetailFields(detailFields, histRows)
	}

	payload := t.Payload
	payloadMuted := isEmptyJSONPayload(payload)
	pot := parsePOTDetail(t.Detail)
	domainAccess := parseDomainAccessDetail(t.Detail)
	cookieAttachUI := cookieAttachView(cookieAttach)

	var progress *float64
	if t.Progress.Valid {
		p := t.Progress.Float64
		progress = &p
	}

	live := !isHistoryStatus(t.Status)
	nav := "history"
	if live {
		nav = "tasks"
	}
	logLines := h.Queue.Logs.Snapshot(id)
	if len(logLines) == 0 {
		logLines = t.Logs
	}
	logText := strings.Join(logLines, "\n")
	commands := t.Commands
	renameList := h.buildTaskRenameList(t, events, live)

	render(w, "task_detail", struct {
		pageBase
		Item            historyView
		FailError       string
		Payload         string
		PayloadMuted    bool
		DetailFields    []detailField
		Stages          []taskStageView
		POT             *potDetailView
		CookieAttach    *cookieAttachDetailView
		DomainAccess    *domains.DomainAccessSnapshot
		Commands        []string
		RenameList      *taskRenameListView
		IntegrityChecks *integrityChecksView
		Progress        *float64
		Live            bool
		LogText         string
		LogLines        []string
		Series          *seriesLink
		Source          *sourceLink
		Video           *videoLink
		VideoRow        *seriesVideoRow
		Crumbs          []breadcrumb
	}{
		pageBase:        newPage(fmt.Sprintf("Task #%d", id), nav, nil),
		Item:            view,
		FailError:       failError,
		Payload:         payload,
		PayloadMuted:    payloadMuted,
		DetailFields:    detailFields,
		Stages:          stages,
		POT:             pot,
		CookieAttach:    cookieAttachUI,
		DomainAccess:    domainAccess,
		Commands:        commands,
		RenameList:      renameList,
		IntegrityChecks: integrityChecks,
		Progress:        progress,
		Live:            live,
		LogText:         logText,
		LogLines:        logLines,
		Series:          series,
		Source:          source,
		Video:           video,
		VideoRow:        videoRow,
		Crumbs:          taskBreadcrumbs(series, source, video, view.Kind, live),
	})
}

func (h *Handler) taskLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	t, err := h.Queue.GetTask(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if t == nil {
		http.NotFound(w, r)
		return
	}
	if isHistoryStatus(t.Status) {
		http.NotFound(w, r)
		return
	}
	lines := h.Queue.Logs.Snapshot(id)
	render(w, "task_logs", struct {
		ID            int64
		Live          bool
		Lines         []string
		LogText       string
		RefreshOnLoad bool
	}{
		ID:      id,
		Live:    true,
		Lines:   lines,
		LogText: strings.Join(lines, "\n"),
	})
}
