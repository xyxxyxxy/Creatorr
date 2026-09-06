package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// detailVideoRef is one video id from task detail JSON.
type detailVideoRef struct {
	ID            int64
	Title         string
	SeriesID      int64
	Missing       bool
	State         string // wanted|ignored; set for created_ids rows
	IgnoredReason string // media_type|index_as_ignored; only when State=ignored
	StatusTip     string // badge tooltip when HasState (includes ignore reason)
	HasState      bool   // true when State is meaningful for this list
}

// detailSkippedTitle is a listing skipped by title filter (no video row).
type detailSkippedTitle struct {
	RemoteID string
	Title    string
}

// potDetailView is PO token state from task detail JSON for the Details table.
type potDetailView struct {
	State  string
	Label  string
	Detail string
	Fetch  string
}

func parsePOTDetail(detail string) *potDetailView {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}
	potRaw, ok := raw[ytdlp.DetailKeyPOToken]
	if !ok || potRaw == nil {
		return nil
	}
	b, err := json.Marshal(potRaw)
	if err != nil {
		return nil
	}
	var pot struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
		Fetch  string `json:"fetch"`
	}
	if err := json.Unmarshal(b, &pot); err != nil || pot.State == "" {
		return nil
	}
	label := pot.State
	switch pot.State {
	case "issued":
		label = "Issued"
	case "failed":
		label = "Failed"
	case "skipped":
		label = "Skipped"
	case "off":
		label = "Off"
	}
	return &potDetailView{State: pot.State, Label: label, Detail: pot.Detail, Fetch: pot.Fetch}
}

func parseDomainAccessDetail(detail string) *domains.DomainAccessSnapshot {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return nil
	}
	accRaw, ok := raw[domains.DetailKeyDomainAccess]
	if !ok || accRaw == nil {
		return nil
	}
	b, err := json.Marshal(accRaw)
	if err != nil {
		return nil
	}
	var snap domains.DomainAccessSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil
	}
	return &snap
}

// detailField is one top-level key from task detail JSON.
type detailField struct {
	Key                string
	Text               string // scalar / non-video value (pretty)
	Videos             []detailVideoRef
	IsVideoList        bool
	SkippedTitles      []detailSkippedTitle
	SkippedTitlesMore  int // titles beyond first 20
	IsSkippedTitleList bool
}

var detailVideoIDKeys = map[string]struct{}{
	"video_ids":              {},
	"missing_ids":            {},
	"restored_ids":           {},
	"beginning_missing_ids":  {},
	"beginning_restored_ids": {},
	"retention_ids":          {},
	"created_ids":            {},
	"updated_ids":            {},
}

func (h *Handler) resolveDetailVideos(ids []int64) []detailVideoRef {
	out := make([]detailVideoRef, 0, len(ids))
	for _, vid := range ids {
		ref := detailVideoRef{ID: vid, Title: fmt.Sprintf("#%d", vid), Missing: true}
		if h.Library != nil {
			if vv, err := h.Library.GetVideo(vid); err == nil && vv != nil {
				ref.Title = vv.Title
				ref.SeriesID = vv.SeriesID
				ref.Missing = false
			}
		}
		out = append(out, ref)
	}
	return out
}

func idSetFromJSON(v any) map[int64]struct{} {
	ids, ok := jsonNumberIDs(v)
	if !ok {
		return nil
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func (h *Handler) resolveCreatedDetailVideos(ids []int64, mediaTypeIDs, indexAsIgnoredIDs map[int64]struct{}) []detailVideoRef {
	out := h.resolveDetailVideos(ids)
	for i := range out {
		out[i].HasState = true
		out[i].State = "wanted"
		out[i].StatusTip = "wanted"
		if _, ok := mediaTypeIDs[out[i].ID]; ok {
			out[i].State = "ignored"
			out[i].IgnoredReason = library.IgnoreReasonMediaType
			out[i].StatusTip = "ignored (media type)"
			continue
		}
		if _, ok := indexAsIgnoredIDs[out[i].ID]; ok {
			out[i].State = "ignored"
			out[i].IgnoredReason = library.IgnoreReasonIndexAsIgnored
			out[i].StatusTip = "ignored (indexed as ignored)"
		}
	}
	return out
}

func parseSkippedTitleRegexp(v any) ([]detailSkippedTitle, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]detailSkippedTitle, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		title, _ := m["title"].(string)
		remoteID, _ := m["remote_id"].(string)
		if title == "" && remoteID == "" {
			continue
		}
		out = append(out, detailSkippedTitle{RemoteID: remoteID, Title: title})
	}
	return out, true
}

func jsonNumberIDs(v any) ([]int64, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	ids := make([]int64, 0, len(arr))
	for _, el := range arr {
		switch n := el.(type) {
		case float64:
			ids = append(ids, int64(n))
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, false
			}
			ids = append(ids, i)
		default:
			return nil, false
		}
	}
	return ids, true
}

func formatDetailScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

func detailJSONInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func (h *Handler) taskDetailFields(detail string) []detailField {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	var raw map[string]any
	dec := json.NewDecoder(strings.NewReader(detail))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil || len(raw) == 0 {
		return []detailField{{Key: "", Text: detail}}
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	mediaTypeIDs := idSetFromJSON(raw["ignored_media_type_ids"])
	indexAsIgnoredIDs := idSetFromJSON(raw["ignored_index_as_ignored_ids"])
	out := make([]detailField, 0, len(keys))
	indexedShown := false
	for _, k := range keys {
		v := raw[k]
		switch k {
		case ytdlp.DetailKeyPOToken:
			// Shown as dedicated Details row (PO token).
			continue
		case domains.DetailKeyDomainAccess:
			// Shown as dedicated Details row (Domain access chips).
			continue
		case "created", "updated":
			// Scan counts: show one "Videos indexed" = created+updated (new + touched).
			if indexedShown {
				continue
			}
			indexedShown = true
			n := detailJSONInt(raw["created"]) + detailJSONInt(raw["updated"])
			out = append(out, detailField{Key: "Videos indexed", Text: strconv.Itoa(n)})
			continue
		case "ignored_media_type_ids", "ignored_index_as_ignored_ids":
			if ids, ok := jsonNumberIDs(v); ok {
				out = append(out, detailField{Key: k, Text: strconv.Itoa(len(ids))})
				continue
			}
		case "skipped_title_regexp_include", "skipped_title_regexp_exclude", "skipped_title_regexp":
			if titles, ok := parseSkippedTitleRegexp(v); ok {
				const maxShow = 20
				more := 0
				show := titles
				if len(titles) > maxShow {
					more = len(titles) - maxShow
					show = titles[:maxShow]
				}
				out = append(out, detailField{
					Key:                k,
					IsSkippedTitleList: true,
					SkippedTitles:      show,
					SkippedTitlesMore:  more,
					Text:               strconv.Itoa(len(titles)),
				})
				continue
			}
		case "ignored_title_regexp_ids":
			// Legacy scan detail: count only (title filter no longer creates rows).
			if ids, ok := jsonNumberIDs(v); ok {
				out = append(out, detailField{Key: k, Text: strconv.Itoa(len(ids))})
				continue
			}
		}
		if _, isVideoKey := detailVideoIDKeys[k]; isVideoKey {
			if ids, ok := jsonNumberIDs(v); ok {
				var videos []detailVideoRef
				if k == "created_ids" {
					videos = h.resolveCreatedDetailVideos(ids, mediaTypeIDs, indexAsIgnoredIDs)
				} else {
					videos = h.resolveDetailVideos(ids)
				}
				out = append(out, detailField{
					Key:         k,
					IsVideoList: true,
					Videos:      videos,
				})
				continue
			}
		}
		out = append(out, detailField{Key: k, Text: formatDetailScalar(v)})
	}
	return out
}

// taskDetailHistRow is one video_history row used to build Detail video lists.
type taskDetailHistRow struct {
	Event      string
	Detail     string
	VideoID    int64
	VideoTitle string
	SeriesID   int64
}

// taskStageView is one daisyUI vertical-timeline node (task Stages + video History).
type taskStageView struct {
	Event      string
	Message    string
	CreatedAt  string
	CreatedAgo string
	Duration   string // how long this stage lasted until the next (omit ≤1s); kept after reverse for display
	HasError   bool
	IsFirst    bool
	IsLast     bool
	HistoryID  int64             // optional /task/{id} link on Event (0 = none)
	Substages  []taskStageSubview // nested stages under a grouped video-history node
}

// taskStageSubview is one line inside a grouped timeline box (e.g. downloaded / remuxed / packed).
type taskStageSubview struct {
	Event    string
	Message  string
	HasError bool
}

// singleVideoHistory is true when every video_history row shares one video_id > 0.
func singleVideoHistory(events []library.VideoHistoryEvent) bool {
	if len(events) == 0 || events[0].VideoID <= 0 {
		return false
	}
	vid := events[0].VideoID
	for _, e := range events[1:] {
		if e.VideoID != vid {
			return false
		}
	}
	return true
}

// taskStages builds the Stages timeline for every task view (latest at top, oldest at bottom).
// Always includes enqueued from created_at when set. Single-video video_history
// appends those events; otherwise appends started. Terminal done/failed/cancelled
// is always included when the task has finished.
func taskStages(events []library.VideoHistoryEvent, now time.Time, taskCreated, taskStarted, taskFinished, status string) []taskStageView {
	out := make([]taskStageView, 0, len(events)+4)
	rawTimes := make([]time.Time, 0, len(events)+4)

	appendStage := func(event, message, rawAt string, err bool) {
		if event == "" {
			return
		}
		tAt, ok := parseActivityTime(rawAt)
		if !ok {
			tAt = time.Time{}
		}
		abs, ago := "", ""
		if rawAt != "" {
			abs, ago = createdAgoPairShort(rawAt, now)
		}
		rawTimes = append(rawTimes, tAt)
		out = append(out, taskStageView{
			Event:      event,
			Message:    message,
			CreatedAt:  abs,
			CreatedAgo: ago,
			HasError:   err,
		})
	}

	createdAt := strings.TrimSpace(taskCreated)
	if createdAt == "" {
		createdAt = now.UTC().Format(time.RFC3339Nano)
	}
	appendStage("enqueued", "", createdAt, false)

	if singleVideoHistory(events) {
		for _, e := range events {
			label := historyEventLabel(e.Event, e.Detail)
			if label == "" {
				continue
			}
			appendStage(label, e.Message, e.CreatedAt, historyEventError(e.Event))
		}
	} else if strings.TrimSpace(taskStarted) != "" {
		appendStage("started", "", taskStarted, false)
	}

	if term, termErr, ok := taskTerminalStage(status); ok {
		at := taskFinished
		if strings.TrimSpace(at) == "" {
			at = taskStarted
		}
		if strings.TrimSpace(at) == "" {
			at = createdAt
		}
		appendStage(term, "", at, termErr)
	}

	// Durations are how long each stage lasted (older → next) before reversing for display.
	for i := 1; i < len(out); i++ {
		start, end := rawTimes[i-1], rawTimes[i]
		if start.IsZero() || end.IsZero() {
			continue
		}
		d := end.Sub(start)
		if d < 0 {
			continue
		}
		out[i-1].Duration = stageDurationLabel(d)
	}

	// Latest at top, oldest at bottom.
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	var prevAgo string
	for i := range out {
		ago := out[i].CreatedAgo
		if ago != "" && ago == prevAgo {
			out[i].CreatedAt = ""
			out[i].CreatedAgo = ""
		} else if ago != "" {
			prevAgo = ago
		}
	}
	out[0].IsFirst = true
	out[len(out)-1].IsLast = true
	return out
}

// taskTerminalStage maps finished task status to a Stages event label.
func taskTerminalStage(status string) (label string, hasError bool, ok bool) {
	switch status {
	case "done":
		return "done", false, true
	case "failed":
		return "failed", true, true
	case "cancelled":
		return "cancelled", true, true
	default:
		return "", false, false
	}
}

// mergeVideoHistoryDetailFields appends per-event video lists from video_history
// (same shape as discover created_ids). Skips event keys already present in fields.
// Cancelled rows use detail.kind as the list key when set (e.g. download).
// Call for multi-video history; single-video stages use taskStages.
func mergeVideoHistoryDetailFields(fields []detailField, rows []taskDetailHistRow) []detailField {
	if len(rows) == 0 {
		return fields
	}
	have := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		have[f.Key] = struct{}{}
	}
	order := make([]string, 0)
	byEvent := map[string][]detailVideoRef{}
	seenVid := map[string]map[int64]struct{}{}
	for _, e := range rows {
		ev := historyEventLabel(e.Event, e.Detail)
		if ev == "" || e.VideoID <= 0 {
			continue
		}
		if _, ok := have[ev]; ok {
			continue
		}
		if _, ok := byEvent[ev]; !ok {
			order = append(order, ev)
			seenVid[ev] = map[int64]struct{}{}
		}
		if _, ok := seenVid[ev][e.VideoID]; ok {
			continue
		}
		seenVid[ev][e.VideoID] = struct{}{}
		ref := detailVideoRef{
			ID:       e.VideoID,
			Title:    e.VideoTitle,
			SeriesID: e.SeriesID,
			Missing:  e.SeriesID <= 0,
		}
		if ref.Title == "" {
			ref.Title = fmt.Sprintf("#%d", e.VideoID)
		}
		byEvent[ev] = append(byEvent[ev], ref)
	}
	for _, ev := range order {
		fields = append(fields, detailField{
			Key:         ev,
			IsVideoList: true,
			Videos:      byEvent[ev],
		})
	}
	return fields
}

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
	stages := taskStages(events, now, t.CreatedAt, taskStarted, taskFinished, t.Status)
	detailFields := h.taskDetailFields(t.Detail)
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
		detailFields = mergeVideoHistoryDetailFields(detailFields, histRows)
	}

	payload := t.Payload
	payloadMuted := isEmptyJSONPayload(payload)
	pot := parsePOTDetail(t.Detail)
	domainAccess := parseDomainAccessDetail(t.Detail)

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
	logText := strings.Join(logLines, "\n")
	commands := t.Commands

	render(w, "task_detail", struct {
		pageBase
		Item         historyView
		Payload      string
		PayloadMuted bool
		DetailFields []detailField
		Stages       []taskStageView
		POT          *potDetailView
		DomainAccess *domains.DomainAccessSnapshot
		Commands     []string
		Progress     *float64
		Live         bool
		LogText      string
		LogLines     []string
		Series       *seriesLink
		Source       *sourceLink
		Video        *videoLink
		VideoRow     *seriesVideoRow
		Crumbs       []breadcrumb
	}{
		pageBase:     newPage(fmt.Sprintf("Task #%d", id), nav, nil),
		Item:         view,
		Payload:      payload,
		PayloadMuted: payloadMuted,
		DetailFields: detailFields,
		Stages:       stages,
		POT:          pot,
		DomainAccess: domainAccess,
		Commands:     commands,
		Progress:     progress,
		Live:         live,
		LogText:      logText,
		LogLines:     logLines,
		Series:       series,
		Source:       source,
		Video:        video,
		VideoRow:     videoRow,
		Crumbs:       taskBreadcrumbs(series, source, video, view.Kind, live),
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
