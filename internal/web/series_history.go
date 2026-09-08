package web

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/library"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

func videoHistoryToView(e library.VideoHistoryEvent, now time.Time) videoHistoryView {
	abs, ago := createdAgoPairShort(e.CreatedAt, now)
	v := videoHistoryView{
		CreatedAt:  abs,
		CreatedAgo: ago,
		Event:      historyEventLabel(e.Event, e.Detail),
		Message:    historyMessageWithDetail(e.Message, e.Detail),
		Detail:     e.Detail,
		HasError:   historyEventError(e.Event),
		Neutral:    historyEventNeutral(e.Event),
		VideoID:    e.VideoID,
	}
	if e.TaskID.Valid {
		v.HasTask = true
		v.TaskID = e.TaskID.Int64
		v.HistoryID = e.TaskID.Int64
	}
	return v
}

// fillVideoHistoryTaskKinds sets TaskKind from tasks.kind for each task_id on the page.
func fillVideoHistoryTaskKinds(q *queue.Store, views []videoHistoryView) {
	if q == nil || len(views) == 0 {
		return
	}
	cache := map[int64]string{}
	for i := range views {
		id := views[i].TaskID
		if !views[i].HasTask || id <= 0 {
			continue
		}
		if kind, ok := cache[id]; ok {
			views[i].TaskKind = kind
			continue
		}
		kind := ""
		if t, err := q.GetTask(id); err == nil && t != nil {
			kind = t.Kind
		}
		cache[id] = kind
		views[i].TaskKind = kind
	}
	preferIntegrityHistoryTaskKind(views)
}

// preferIntegrityHistoryTaskKind uses tasks.kind as the Event link for integrity
// outcome rows (Message keeps the result text). Matches cancelled/download grouping.
func preferIntegrityHistoryTaskKind(views []videoHistoryView) {
	for i := range views {
		if !isIntegrityHistoryOutcomeEvent(views[i].Event) {
			continue
		}
		kind := strings.TrimSpace(views[i].TaskKind)
		if kind == "" {
			continue
		}
		views[i].Event = historyKindDisplay(kind)
	}
}

func isIntegrityHistoryOutcomeEvent(event string) bool {
	switch strings.TrimSpace(event) {
	case library.VideoHistIntegrityChecked, library.VideoHistVerifyFailed:
		return true
	default:
		return false
	}
}

// enrichVideoHistoryRenameMessages clarifies peer-move renames triggered by another video's
// pack/download (task.video_id or detail.trigger_video_id differs from this row's video).
func enrichVideoHistoryRenameMessages(lib *library.Store, q *queue.Store, views []videoHistoryView) {
	if len(views) == 0 {
		return
	}
	for i := range views {
		if views[i].Event != "renamed" {
			continue
		}
		if strings.Contains(views[i].Message, "peer move") {
			continue
		}
		triggerID := int64(0)
		if id, ok := historyDetailInt64FromJSON(views[i].Detail, "trigger_video_id"); ok {
			triggerID = id
		}
		if triggerID <= 0 && views[i].HasTask && q != nil && views[i].TaskID > 0 {
			if t, err := q.GetTask(views[i].TaskID); err == nil && t != nil && t.VideoID.Valid {
				triggerID = t.VideoID.Int64
			}
		}
		if triggerID <= 0 || triggerID == views[i].VideoID {
			continue
		}
		title := ""
		if lib != nil {
			if v, err := lib.GetVideo(triggerID); err == nil && v != nil {
				title = v.Title
			}
		}
		views[i].Message = library.RenamedHistoryMessage(views[i].VideoID, triggerID, title)
	}
}

func historyDetailInt64FromJSON(detail, key string) (int64, bool) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return 0, false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return 0, false
	}
	return historyDetailInt64(raw, key)
}
