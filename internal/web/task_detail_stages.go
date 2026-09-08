package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func originLucide(origin string) string {
	switch origin {
	case queue.OriginManual:
		return "mouse-pointer-click"
	case queue.OriginScheduled:
		return "calendar-clock"
	case queue.OriginBoot:
		return "power"
	case queue.OriginTask:
		return "git-branch"
	default:
		return ""
	}
}

func childStageStyle(status string) (hasError, neutral bool, icon string) {
	switch status {
	case queue.StatusPending, queue.StatusRunning:
		return false, true, "git-branch"
	case queue.StatusFailed:
		return true, false, ""
	case queue.StatusCancelled:
		return false, true, ""
	default:
		return false, false, ""
	}
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
// Always includes origin + enqueued from created_at when set. Single-video video_history
// appends those events; otherwise appends started. Direct children (parent_task_id) appear
// as kind nodes. Terminal done/failed/cancelled is always included when the task has finished.
func taskStages(in taskStagesInput) []taskStageView {
	items := make([]stagedItem, 0, len(in.Events)+len(in.Children)+6)

	appendItem := func(event, message, rawAt string, err, neutral bool, historyID int64, originIcon string, rank stageRank) {
		if event == "" {
			return
		}
		tAt, ok := parseActivityTime(rawAt)
		if !ok {
			tAt = time.Time{}
		}
		abs, ago := "", ""
		if rawAt != "" {
			abs, ago = createdAgoPairShort(rawAt, in.Now)
		}
		items = append(items, stagedItem{
			at:   tAt,
			rank: rank,
			view: taskStageView{
				Event:      event,
				Message:    message,
				CreatedAt:  abs,
				CreatedAgo: ago,
				HasError:   err,
				Neutral:    neutral && !err,
				HistoryID:  historyID,
				OriginIcon: originIcon,
			},
		})
	}

	createdAt := strings.TrimSpace(in.Created)
	if createdAt == "" {
		createdAt = in.Now.UTC().Format(time.RFC3339Nano)
	}

	origin := strings.TrimSpace(in.Origin)
	if origin == "" {
		origin = queue.OriginManual
	}
	originMsg := ""
	originHID := int64(0)
	if origin == queue.OriginTask && in.ParentTaskID > 0 {
		originHID = in.ParentTaskID
		if k := strings.TrimSpace(in.ParentKind); k != "" {
			originMsg = fmt.Sprintf("%s #%d", k, in.ParentTaskID)
		} else {
			originMsg = fmt.Sprintf("#%d", in.ParentTaskID)
		}
	}
	appendItem(origin, originMsg, createdAt, false, false, originHID, originLucide(origin), stageRankOrigin)
	appendItem("enqueued", "", createdAt, false, false, 0, "", stageRankEnqueued)

	for _, c := range in.Children {
		err, neutral, icon := childStageStyle(c.Status)
		appendItem(c.Kind, c.Status, c.CreatedAt, err, neutral, c.ID, icon, stageRankChild)
	}

	if singleVideoHistory(in.Events) {
		for _, e := range in.Events {
			label := historyEventLabel(e.Event, e.Detail)
			if label == "" {
				continue
			}
			appendItem(label, e.Message, e.CreatedAt, historyEventError(e.Event), historyEventNeutral(e.Event), 0, "", stageRankHistory)
		}
	} else if strings.TrimSpace(in.Started) != "" {
		appendItem("started", "", in.Started, false, false, 0, "", stageRankStarted)
	}

	if in.CookieAttach != nil && in.CookieAttach.ShowStage() {
		at := strings.TrimSpace(in.Started)
		if at == "" {
			at = createdAt
		}
		appendItem("cookies", in.CookieAttach.StageMessage(), at, false, false, 0, "cookie", stageRankCookies)
	}

	if term, termErr, termNeutral, ok := taskTerminalStage(in.Status); ok {
		at := in.Finished
		if strings.TrimSpace(at) == "" {
			at = in.Started
		}
		if strings.TrimSpace(at) == "" {
			at = createdAt
		}
		appendItem(term, "", at, termErr, termNeutral, 0, "", stageRankTerminal)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].at.Equal(items[j].at) {
			return items[i].at.Before(items[j].at)
		}
		return items[i].rank < items[j].rank
	})

	out := make([]taskStageView, len(items))
	rawTimes := make([]time.Time, len(items))
	for i := range items {
		out[i] = items[i].view
		rawTimes[i] = items[i].at
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
	blankDuplicateStageAgos(out)
	if len(out) > 0 {
		out[0].IsFirst = true
		out[len(out)-1].IsLast = true
	}
	return out
}

// blankDuplicateStageAgos clears CreatedAt/CreatedAgo when the relative label
// matches the previous non-empty stage (newest-first timelines: Stages + video History).
func blankDuplicateStageAgos(out []taskStageView) {
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
}

// taskTerminalStage maps finished task status to a Stages event label.
func taskTerminalStage(status string) (label string, hasError, neutral, ok bool) {
	switch status {
	case "done":
		return "done", false, false, true
	case "failed":
		return "failed", true, false, true
	case "cancelled":
		return "cancelled", false, true, true
	default:
		return "", false, false, false
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

// taskRenameListView is From→To paths for rename_episodes task detail.
type taskRenameListView struct {
	Items     []library.ApplyRenamePreviewItem
	Total     int
	Truncated bool
	Planned   bool // dry-run for pending; false = recorded renames from history
}

func (h *Handler) buildTaskRenameList(t *queue.Task, events []library.VideoHistoryEvent, live bool) *taskRenameListView {
	if h.Library == nil || t == nil || t.Kind != queue.KindRenameEpisodes {
		return nil
	}
	if items, total, trunc := renameItemsFromHistory(h, events); total > 0 {
		return &taskRenameListView{Items: items, Total: total, Truncated: trunc, Planned: false}
	}
	if !live {
		return &taskRenameListView{Planned: false}
	}
	seriesIDs, videoIDs := renameScopeFromPayload(t.Payload)
	prev, err := h.Library.PreviewApplyEpisodeNaming(seriesIDs, videoIDs)
	if err != nil || prev == nil {
		return &taskRenameListView{Planned: true}
	}
	return &taskRenameListView{
		Items:     prev.Items,
		Total:     prev.TotalChanges,
		Truncated: prev.Truncated,
		Planned:   true,
	}
}

func renameScopeFromPayload(payload string) (seriesIDs, videoIDs []int64) {
	var p struct {
		SeriesID  int64   `json:"series_id"`
		SeriesIDs []int64 `json:"series_ids"`
		VideoIDs  []int64 `json:"video_ids"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	if len(p.VideoIDs) > 0 {
		return nil, p.VideoIDs
	}
	if p.SeriesID > 0 {
		return []int64{p.SeriesID}, nil
	}
	return p.SeriesIDs, nil
}

func renameItemsFromHistory(h *Handler, events []library.VideoHistoryEvent) (items []library.ApplyRenamePreviewItem, total int, truncated bool) {
	for _, e := range events {
		if e.Event != "renamed" {
			continue
		}
		total++
		if len(items) >= library.PreviewApplyRenameCap {
			truncated = true
			continue
		}
		from, to := renamePathsFromDetail(e.Detail)
		title := fmt.Sprintf("#%d", e.VideoID)
		seriesTitle := ""
		if vv, err := h.Library.GetVideo(e.VideoID); err == nil && vv != nil {
			title = vv.Title
			if ser, serr := h.Library.GetSeries(vv.SeriesID, false); serr == nil && ser != nil {
				seriesTitle = ser.Title
			}
		}
		items = append(items, library.ApplyRenamePreviewItem{
			VideoID:     e.VideoID,
			Title:       title,
			SeriesTitle: seriesTitle,
			From:        from,
			To:          to,
		})
	}
	return items, total, truncated
}

func renamePathsFromDetail(detail string) (from, to string) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return "", ""
	}
	from = jsonStringField(raw, "previous_path")
	to = jsonStringField(raw, "new_path")
	if from == "" {
		from = jsonStringField(raw, "previous")
	}
	if to == "" {
		to = jsonStringField(raw, "new")
	}
	return from, to
}

func jsonStringField(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}
