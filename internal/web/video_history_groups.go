package web

import (
	"fmt"
)

// videoHistoryGroup is one video-detail History row. Multi-stage download/remux/pack
// (and similar) share a task_id and collapse into one row with Stages.
type videoHistoryGroup struct {
	CreatedAt  string
	CreatedAgo string
	Event      string // single-stage event label, or "Task #N"
	Message    string
	TaskID     int64
	HasTask    bool
	HistoryID  int64 // task id for /task/{id} navigation (0 = no link)
	Grouped    bool  // true when Stages has 2+ entries
	Stages     []videoHistoryView
	HasError   bool
}

// groupVideoHistoryByTask collapses consecutive timeline rows that share the same
// task_id (>0) into one group. Timeline input is newest-first; Stages inside a
// multi-row group are oldest-first so download / remux / pack reads top-to-bottom.
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
	// newestFirst[0] is the latest stage (When column).
	head := newestFirst[0]
	stages := make([]videoHistoryView, len(newestFirst))
	copy(stages, newestFirst)
	// Oldest first for display.
	for a, b := 0, len(stages)-1; a < b; a, b = a+1, b-1 {
		stages[a], stages[b] = stages[b], stages[a]
	}
	hasErr := false
	for _, s := range stages {
		if historyEventError(s.Event) {
			hasErr = true
		}
	}
	return videoHistoryGroup{
		CreatedAt:  head.CreatedAt,
		CreatedAgo: head.CreatedAgo,
		Event:      fmt.Sprintf("Task #%d", head.TaskID),
		Message:    fmt.Sprintf("%d stages", len(stages)),
		TaskID:     head.TaskID,
		HasTask:    true,
		HistoryID:  head.HistoryID,
		Grouped:    true,
		Stages:     stages,
		HasError:   hasErr,
	}
}
