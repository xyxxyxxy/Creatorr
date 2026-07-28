package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Source history events (list-pass timeline).
const (
	SourceHistScanned   = "scanned"
	SourceHistScanError = "scan_error"
	SourceHistCancelled = "cancelled"
)

// Source history detail.mode values.
const (
	SourceHistModeFull           = "full"
	SourceHistModeScan           = "scan"
	SourceHistModeRescanMetadata = "rescan_metadata"
)

// SourceHistoryEvent is one row on a source's timeline.
type SourceHistoryEvent struct {
	ID        int64
	SourceID  int64
	CreatedAt string
	Event     string
	Message   string
	Detail    string
	TaskID    int64
}

// SourceScanStatus is derived sticky status from the latest scan-related history row.
type SourceScanStatus struct {
	LastScannedAt    string // empty = never
	LastErrorCode    string
	LastErrorMessage string
	CreatedCount     int64 // from latest scanned detail.created when present
	HasCreatedCount  bool
	TaskID           int64 // latest scan-related event's task
	Event            string
}

// AddSourceHistory appends a timeline event. taskID is required.
func (s *Store) AddSourceHistory(sourceID int64, event, message string, detail map[string]any, taskID int64) error {
	if taskID <= 0 {
		return fmt.Errorf("%w: source history requires task_id", ErrInvalid)
	}
	if sourceID <= 0 {
		return fmt.Errorf("%w: source history requires source_id", ErrInvalid)
	}
	raw := "{}"
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := s.DB.SQL.Exec(`
		INSERT INTO source_history (source_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sourceID, nowRFC3339(), event, message, raw, taskID)
	return err
}

// ListSourceHistoryPage returns newest-first history. limit<=0 returns all rows.
func (s *Store) ListSourceHistoryPage(sourceID int64, limit, offset int) ([]SourceHistoryEvent, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if limit <= 0 {
		rows, err = s.DB.SQL.Query(`
			SELECT id, source_id, created_at, event, message, COALESCE(detail,'{}'), task_id
			FROM source_history WHERE source_id = ?
			ORDER BY id DESC
		`, sourceID)
	} else {
		if offset < 0 {
			offset = 0
		}
		rows, err = s.DB.SQL.Query(`
			SELECT id, source_id, created_at, event, message, COALESCE(detail,'{}'), task_id
			FROM source_history WHERE source_id = ?
			ORDER BY id DESC
			LIMIT ? OFFSET ?
		`, sourceID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSourceHistoryRows(rows)
}

// CountSourceHistory returns history row count for a source.
func (s *Store) CountSourceHistory(sourceID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM source_history WHERE source_id = ?`, sourceID).Scan(&n)
	return n, err
}

// LatestSourceScanStatus derives sticky scan status from the newest scanned/scan_error row.
func (s *Store) LatestSourceScanStatus(sourceID int64) (SourceScanStatus, error) {
	var st SourceScanStatus
	var detail string
	err := s.DB.SQL.QueryRow(`
		SELECT created_at, event, message, COALESCE(detail,'{}'), task_id
		FROM source_history
		WHERE source_id = ? AND event IN (?, ?)
		ORDER BY id DESC
		LIMIT 1
	`, sourceID, SourceHistScanned, SourceHistScanError).Scan(
		&st.LastScannedAt, &st.Event, &st.LastErrorMessage, &detail, &st.TaskID,
	)
	if err == sql.ErrNoRows {
		return SourceScanStatus{}, nil
	}
	if err != nil {
		return st, err
	}
	if st.Event == SourceHistScanError {
		var d struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal([]byte(detail), &d)
		st.LastErrorCode = d.Code
		// Keep LastErrorMessage from row message.
	} else {
		st.LastErrorMessage = ""
		var d struct {
			Created *int64 `json:"created"`
		}
		if json.Unmarshal([]byte(detail), &d) == nil && d.Created != nil {
			st.CreatedCount = *d.Created
			st.HasCreatedCount = true
		}
	}
	return st, nil
}

// LatestTipScannedAt returns created_at of the newest tip scan (mode=scan) success, or zero time.
func (s *Store) LatestTipScannedAt(sourceID int64) (time.Time, error) {
	var raw sql.NullString
	err := s.DB.SQL.QueryRow(`
		SELECT created_at FROM source_history
		WHERE source_id = ? AND event = ?
		  AND json_valid(detail)
		  AND json_extract(detail, '$.mode') = ?
		ORDER BY id DESC
		LIMIT 1
	`, sourceID, SourceHistScanned, SourceHistModeScan).Scan(&raw)
	if err == sql.ErrNoRows || !raw.Valid || raw.String == "" {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if t, err := time.Parse(time.RFC3339Nano, raw.String); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw.String); err == nil {
		return t, nil
	}
	return time.Time{}, nil
}

// ListVideoTimelinePage merges video_history with projected source_history list-pass events.
// limit<=0 returns all. Newest first.
func (s *Store) ListVideoTimelinePage(videoID int64, limit, offset int) ([]VideoHistoryEvent, error) {
	q := `
		SELECT created_at, event, message, detail, task_id, id FROM (
			SELECT created_at, event, message, COALESCE(detail,'{}') AS detail, task_id, id,
			       created_at AS sort_at, id AS sort_id
			FROM video_history WHERE video_id = ?
			UNION ALL
			SELECT sh.created_at,
			       CASE
			         WHEN EXISTS (
			           SELECT 1 FROM json_each(json_extract(sh.detail, '$.created_ids'))
			           WHERE CAST(value AS INTEGER) = ?
			         ) THEN 'discovered'
			         WHEN json_extract(sh.detail, '$.mode') IN ('rescan_metadata', 'metadata_rescan') THEN 'rescan_metadata'
			         ELSE 'updated'
			       END AS event,
			       CASE
			         WHEN EXISTS (
			           SELECT 1 FROM json_each(json_extract(sh.detail, '$.created_ids'))
			           WHERE CAST(value AS INTEGER) = ?
			         ) THEN 'Indexed from scan'
			         WHEN json_extract(sh.detail, '$.mode') IN ('rescan_metadata', 'metadata_rescan') THEN 'Metadata refreshed'
			         ELSE 'Metadata refreshed by scan'
			       END AS message,
			       COALESCE(sh.detail, '{}') AS detail,
			       sh.task_id,
			       sh.id,
			       sh.created_at AS sort_at,
			       sh.id AS sort_id
			FROM source_history sh
			WHERE sh.event = ?
			  AND json_valid(sh.detail)
			  AND (
			    EXISTS (
			      SELECT 1 FROM json_each(json_extract(sh.detail, '$.created_ids'))
			      WHERE CAST(value AS INTEGER) = ?
			    )
			    OR EXISTS (
			      SELECT 1 FROM json_each(json_extract(sh.detail, '$.updated_ids'))
			      WHERE CAST(value AS INTEGER) = ?
			    )
			  )
		)
		ORDER BY sort_at DESC, sort_id DESC
	`
	args := []any{videoID, videoID, videoID, SourceHistScanned, videoID, videoID}
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		q += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := s.DB.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []VideoHistoryEvent
	for rows.Next() {
		var e VideoHistoryEvent
		var taskID sql.NullInt64
		if err := rows.Scan(&e.CreatedAt, &e.Event, &e.Message, &e.Detail, &taskID, &e.ID); err != nil {
			return nil, err
		}
		e.VideoID = videoID
		e.TaskID = taskID
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountVideoTimeline returns merged timeline row count for a video.
func (s *Store) CountVideoTimeline(videoID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT id FROM video_history WHERE video_id = ?
			UNION ALL
			SELECT sh.id FROM source_history sh
			WHERE sh.event = ?
			  AND json_valid(sh.detail)
			  AND (
			    EXISTS (
			      SELECT 1 FROM json_each(json_extract(sh.detail, '$.created_ids'))
			      WHERE CAST(value AS INTEGER) = ?
			    )
			    OR EXISTS (
			      SELECT 1 FROM json_each(json_extract(sh.detail, '$.updated_ids'))
			      WHERE CAST(value AS INTEGER) = ?
			    )
			  )
		)
	`, videoID, SourceHistScanned, videoID, videoID).Scan(&n)
	return n, err
}

// SeriesIDsWithSourceScanError returns series that have a source whose latest
// scan-related history event is scan_error.
func (s *Store) SeriesIDsWithSourceScanError(seriesIDs []int64) (map[int64]struct{}, error) {
	out := make(map[int64]struct{})
	if len(seriesIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(seriesIDs))
	for _, id := range seriesIDs {
		if id > 0 {
			args = append(args, id)
		}
	}
	if len(args) == 0 {
		return out, nil
	}
	ph := sqlIntPlaceholders(len(args))
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT src.series_id
		FROM sources src
		WHERE src.series_id IN (`+ph+`)
		  AND (
		    SELECT sh.event FROM source_history sh
		    WHERE sh.source_id = src.id AND sh.event IN (?, ?)
		    ORDER BY sh.id DESC LIMIT 1
		  ) = ?
	`, append(append([]any{}, args...), SourceHistScanned, SourceHistScanError, SourceHistScanError)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var seriesID int64
		if err := rows.Scan(&seriesID); err != nil {
			return nil, err
		}
		out[seriesID] = struct{}{}
	}
	return out, rows.Err()
}

func scanSourceHistoryRows(rows *sql.Rows) ([]SourceHistoryEvent, error) {
	var out []SourceHistoryEvent
	for rows.Next() {
		var e SourceHistoryEvent
		if err := rows.Scan(&e.ID, &e.SourceID, &e.CreatedAt, &e.Event, &e.Message, &e.Detail, &e.TaskID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
