package library

import (
	"encoding/json"
	"fmt"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

const (
	msgBulkEditQueued  = "Bulk edit queued"
	msgBulkEditOverlap = "One or more items already in a bulk edit"
)

// SeriesIDsLockedByBulkEdit returns series ids in any pending/running bulk_edit_series payload.
func (s *Store) SeriesIDsLockedByBulkEdit() (map[int64]struct{}, error) {
	return s.idsLockedByBulkEdit(queue.KindBulkEditSeries, "series_ids")
}

// VideoIDsLockedByBulkEdit returns video ids in any pending/running bulk_edit_videos payload.
func (s *Store) VideoIDsLockedByBulkEdit() (map[int64]struct{}, error) {
	return s.idsLockedByBulkEdit(queue.KindBulkEditVideos, "video_ids")
}

// SeriesBulkEditLocked reports whether seriesID is in an open bulk_edit_series payload.
func (s *Store) SeriesBulkEditLocked(seriesID int64) (bool, error) {
	ids, err := s.SeriesIDsLockedByBulkEdit()
	if err != nil {
		return false, err
	}
	_, ok := ids[seriesID]
	return ok, nil
}

// VideoBulkEditLocked reports whether videoID is in an open bulk_edit_videos payload.
func (s *Store) VideoBulkEditLocked(videoID int64) (bool, error) {
	ids, err := s.VideoIDsLockedByBulkEdit()
	if err != nil {
		return false, err
	}
	_, ok := ids[videoID]
	return ok, nil
}

func (s *Store) idsLockedByBulkEdit(kind, field string) (map[int64]struct{}, error) {
	out := map[int64]struct{}{}
	if s.Queue == nil {
		return out, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT payload FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, queue.SystemDomain, kind, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if raw == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			continue
		}
		rawIDs, ok := m[field]
		if !ok {
			continue
		}
		var ids []int64
		if err := json.Unmarshal(rawIDs, &ids); err != nil {
			continue
		}
		for _, id := range ids {
			if id > 0 {
				out[id] = struct{}{}
			}
		}
	}
	return out, rows.Err()
}

func (s *Store) errIfSeriesBulkEditLocked(seriesID int64) error {
	ok, err := s.SeriesBulkEditLocked(seriesID)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: %s", ErrConflict, msgBulkEditQueued)
	}
	return nil
}

func (s *Store) errIfVideoBulkEditLocked(videoID int64) error {
	ok, err := s.VideoBulkEditLocked(videoID)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: %s", ErrConflict, msgBulkEditQueued)
	}
	return nil
}

func (s *Store) errIfBulkEditIDsOverlap(kind, field string, ids []int64) error {
	locked, err := s.idsLockedByBulkEdit(kind, field)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := locked[id]; ok {
			return fmt.Errorf("%w: %s", ErrConflict, msgBulkEditOverlap)
		}
	}
	return nil
}
