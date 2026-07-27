package library

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// VideoHistCancelled is written when a video-scoped task is cancelled.
const VideoHistCancelled = "cancelled"

// RecordTaskCancelled appends source_history and/or video_history for a cancelled task.
// Idempotent per task_id + event so queue cancel hooks and the worker can both call it.
func (s *Store) RecordTaskCancelled(t *queue.Task) error {
	if s == nil || t == nil || t.ID <= 0 {
		return nil
	}
	msg := strings.TrimSpace(t.Message)
	if msg == "" {
		msg = "Cancelled"
	}

	switch t.Kind {
	case queue.KindScan:
		srcID := queue.SourceIDFromPayload(t.Payload)
		if srcID <= 0 {
			break
		}
		exists, err := s.hasSourceHistoryEvent(t.ID, SourceHistCancelled)
		if err != nil {
			return err
		}
		if exists {
			break
		}
		detail := map[string]any{"mode": scanModeFromPayload(t.Payload)}
		if err := s.AddSourceHistory(srcID, SourceHistCancelled, msg, detail, t.ID); err != nil {
			return err
		}
	}

	if !t.VideoID.Valid || t.VideoID.Int64 <= 0 {
		return nil
	}
	switch t.Kind {
	case queue.KindDownload, queue.KindRescanMetadata, queue.KindRefreshSidecars, queue.KindSponsorblockCut, queue.KindMediaVerify:
		// ok
	default:
		return nil
	}
	exists, err := s.hasVideoHistoryEvent(t.ID, VideoHistCancelled)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if t.Kind == queue.KindSponsorblockCut {
		s.RemoveSponsorblockCutStaging(t.VideoID.Int64)
	}
	return s.AddVideoHistory(t.VideoID.Int64, VideoHistCancelled, msg, map[string]any{
		"kind": t.Kind,
	}, t.ID)
}

func (s *Store) hasSourceHistoryEvent(taskID int64, event string) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM source_history WHERE task_id = ? AND event = ?
	`, taskID, event).Scan(&n)
	return n > 0, err
}

func (s *Store) hasVideoHistoryEvent(taskID int64, event string) (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM video_history WHERE task_id = ? AND event = ?
	`, taskID, event).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return n > 0, err
}

func scanModeFromPayload(payload string) string {
	var p struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	switch strings.TrimSpace(p.Mode) {
	case SourceHistModeFull, SourceHistModeScan, SourceHistModeRescanMetadata:
		return p.Mode
	default:
		return SourceHistModeFull
	}
}
