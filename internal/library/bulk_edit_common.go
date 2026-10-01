package library

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BulkEditMessage summarizes a finished bulk edit pass (series or videos).
func BulkEditMessage(updated, skipped, failed int) string {
	parts := []string{fmt.Sprintf("Updated %d", updated)}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d", skipped))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("failed %d", failed))
	}
	return strings.Join(parts, ", ")
}

// bulkEditProgressStep reports "Updating i+1/n" for the item at index i.
func bulkEditProgressStep(progress func(msg string, pct *float64), i, n int) {
	if progress == nil {
		return
	}
	pct := float64(i) / float64(n) * 100
	progress(fmt.Sprintf("Updating %d/%d", i+1, n), &pct)
}

// bulkEditProgressDone reports the final summary at 100%.
func bulkEditProgressDone(progress func(msg string, pct *float64), updated, skipped, failed int) {
	if progress == nil {
		return
	}
	pct := 100.0
	progress(BulkEditMessage(updated, skipped, failed), &pct)
}

// persistBulkEditPayload stores payload (already carrying the next index) on the task.
func (s *Store) persistBulkEditPayload(taskID int64, payload any) error {
	if s.Queue == nil {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	return s.Queue.UpdatePayload(taskID, m)
}
