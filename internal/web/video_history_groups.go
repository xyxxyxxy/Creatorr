package web

import (
	"fmt"
	"strings"
)

// videoHistoryGroup is one video-detail History row. Multi-stage download/remux/pack
// (and similar) share a task_id and collapse into one row with Stages.
type videoHistoryGroup struct {
	CreatedAt  string
	CreatedAgo string
	Event      string // single-stage event label, or task kind (e.g. download)
	Message    string
	TaskID     int64
	HasTask    bool
	HistoryID  int64 // task id for /task/{id} navigation (0 = no link)
	Grouped    bool  // true when Stages has 2+ entries
	Stages     []videoHistoryView
	HasError   bool
}

// groupVideoHistoryByTask collapses consecutive timeline rows that share the same
// task_id (>0) into one group. Timeline input and output are newest-first (latest
// at top). Stages inside a multi-row group stay newest-first too.
// Rows without a task_id stay single (including projected discover/update).
func groupVideoHistoryByTask(rows []videoHistoryView) []videoHistoryGroup {
	if len(rows) == 0 {
		return nil
	}
	out := make([]videoHistoryGroup, 0, len(rows))
	i := 0
	for i < len(rows) {
		r := rows[i]
		if !r.HasTask || r.TaskID <= 0 {
			out = append(out, singleHistoryGroup(r))
			i++
			continue
		}
		j := i + 1
		for j < len(rows) && rows[j].HasTask && rows[j].TaskID == r.TaskID {
			j++
		}
		chunk := rows[i:j]
		if len(chunk) == 1 {
			out = append(out, singleHistoryGroup(chunk[0]))
		} else {
			out = append(out, multiHistoryGroup(chunk))
		}
		i = j
	}
	return out
}

func singleHistoryGroup(r videoHistoryView) videoHistoryGroup {
	return videoHistoryGroup{
		CreatedAt:  r.CreatedAt,
		CreatedAgo: r.CreatedAgo,
		Event:      r.Event,
		Message:    r.Message,
		TaskID:     r.TaskID,
		HasTask:    r.HasTask,
		HistoryID:  r.HistoryID,
		Grouped:    false,
		Stages:     []videoHistoryView{r},
		HasError:   historyEventError(r.Event),
	}
}

func multiHistoryGroup(newestFirst []videoHistoryView) videoHistoryGroup {
	// newestFirst[0] is the latest stage (When + top of substages).
	head := newestFirst[0]
	stages := make([]videoHistoryView, len(newestFirst))
	copy(stages, newestFirst)
	hasErr := false
	for _, s := range stages {
		if historyEventError(s.Event) {
			hasErr = true
		}
	}
	event := strings.TrimSpace(head.TaskKind)
	if event == "" {
		event = fmt.Sprintf("Task #%d", head.TaskID)
	}
	return videoHistoryGroup{
		CreatedAt:  head.CreatedAt,
		CreatedAgo: head.CreatedAgo,
		Event:      event,
		Message:    "",
		TaskID:     head.TaskID,
		HasTask:    true,
		HistoryID:  head.HistoryID,
		Grouped:    true,
		Stages:     stages,
		HasError:   hasErr,
	}
}

// videoHistoryGroupsToTimeline maps History groups onto the shared Stages timeline shape.
func videoHistoryGroupsToTimeline(groups []videoHistoryGroup) []taskStageView {
	if len(groups) == 0 {
		return nil
	}
	out := make([]taskStageView, 0, len(groups))
	for _, g := range groups {
		item := taskStageView{
			Event:      g.Event,
			Message:    g.Message,
			CreatedAt:  g.CreatedAt,
			CreatedAgo: g.CreatedAgo,
			HasError:   g.HasError,
			HistoryID:  g.HistoryID,
		}
		if g.Grouped {
			subs := make([]taskStageSubview, 0, len(g.Stages))
			for _, s := range g.Stages {
				subs = append(subs, taskStageSubview{
					Event:    s.Event,
					Message:  s.Message,
					HasError: historyEventError(s.Event),
				})
			}
			item.Substages = subs
			item.Message = ""
		}
		out = append(out, item)
	}
	out[0].IsFirst = true
	out[len(out)-1].IsLast = true
	return out
}
