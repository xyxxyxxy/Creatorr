package library

import (
	"encoding/json"
	"fmt"
)

// historyDetailJSON marshals detail, defaulting nil to "{}".
func historyDetailJSON(detail map[string]any) (string, error) {
	if detail == nil {
		return "{}", nil
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// insertHistory appends a row to video_history or source_history.
// table and idCol are package-internal constants, never user input.
func (s *Store) insertHistory(table, idCol, label string, ownerID int64, event, message string, detail map[string]any, taskID int64) error {
	if taskID <= 0 {
		return fmt.Errorf("%w: %s history requires task_id", ErrInvalid, label)
	}
	if label == "source" && ownerID <= 0 {
		return fmt.Errorf("%w: source history requires source_id", ErrInvalid)
	}
	raw, err := historyDetailJSON(detail)
	if err != nil {
		return err
	}
	_, err = s.DB.SQL.Exec(`
		INSERT INTO `+table+` (`+idCol+`, created_at, event, message, detail, task_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, ownerID, nowRFC3339(), event, message, raw, taskID)
	return err
}
