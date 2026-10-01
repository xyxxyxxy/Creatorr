package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// ErrSeriesMoveBusy is returned when a series_move is pending or running and the
// action would touch episode paths or start a download.
var ErrSeriesMoveBusy = fmt.Errorf("%w: series folder move pending or running - wait or cancel first", ErrConflict)

type seriesMovePayload struct {
	SeriesID  int64  `json:"series_id"`
	OldTitle  string `json:"old_title"`
	OldRootID int64  `json:"old_root_id"`
	NewTitle  string `json:"new_title"`
	NewRootID int64  `json:"new_root_id"`
}

// EnqueueSeriesMove queues an async title/root change. The DB title/root stay unchanged
// until the worker runs (it updates SQLite, moves the folder, then applies naming).
func (s *Store) EnqueueSeriesMove(seriesID int64, newTitle string, newRootID int64) (int64, error) {
	return s.enqueueSeriesMove(seriesID, newTitle, newRootID, queue.OriginManual, 0)
}

func (s *Store) enqueueSeriesMove(seriesID int64, newTitle string, newRootID int64, origin string, parentTaskID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	cur, err := s.GetSeries(seriesID, false)
	if err != nil {
		return 0, err
	}
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return 0, fmt.Errorf("%w: title", ErrInvalid)
	}
	if newRootID <= 0 {
		newRootID = cur.RootID
	}
	if _, err := s.GetRoot(newRootID); err != nil {
		return 0, fmt.Errorf("%w: root_id", ErrInvalid)
	}
	if newTitle == cur.Title && newRootID == cur.RootID {
		return 0, fmt.Errorf("%w: title and root unchanged", ErrInvalid)
	}
	busy, err := s.SeriesHasBlockingTasks(seriesID)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, ErrSeriesBusy
	}
	if taken, err := s.seriesFolderTaken(newRootID, newTitle, seriesID); err != nil {
		return 0, err
	} else if taken {
		return 0, fmt.Errorf("%w: a series with this title already exists under the same root folder", ErrConflict)
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Origin:       origin,
		ParentTaskID: parentTaskID,
		Kind:         queue.KindSeriesMove,
		Domain:       queue.SystemDomain,
		SeriesID:     seriesID,
		Message:      "Move series folder",
		Payload: map[string]any{
			"series_id":   seriesID,
			"old_title":   cur.Title,
			"old_root_id": cur.RootID,
			"new_title":   newTitle,
			"new_root_id": newRootID,
		},
	})
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, fmt.Errorf("%w: a series move, episode rename, NFO rewrite, file sync, or retention purge is already pending or running", ErrConflict)
		}
		return 0, err
	}
	return id, nil
}

// SeriesHasOpenMove reports a pending/running series_move for seriesID.
func (s *Store) SeriesHasOpenMove(seriesID int64) (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	return s.Queue.SeriesMoveOpen(seriesID)
}

// SeriesHasBlockingTasks reports busy media tasks, an open series_move, or a
// series-scoped rename_episodes. Title/root edits and new moves are blocked while true.
func (s *Store) SeriesHasBlockingTasks(seriesID int64) (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	if busy, err := s.SeriesHasBusyMediaTasks(seriesID); err != nil || busy {
		return busy, err
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks t
		WHERE t.status IN ('pending', 'running')
		  AND (
		    (t.kind = ? AND t.series_id = ?)
		    OR (t.kind = ? AND (
		      t.series_id = ?
		      OR json_extract(t.payload, '$.series_id') = ?
		      OR EXISTS (SELECT 1 FROM json_each(t.payload, '$.series_ids') j WHERE j.value = ?)
		    ))
		  )
	`, queue.KindSeriesMove, seriesID, queue.KindRenameEpisodes, seriesID, seriesID, seriesID).Scan(&n)
	return n > 0, err
}

// SeriesMovePass runs a series_move task: update title/root, move the folder, write
// tvshow.nfo, then apply episode naming. All steps run in-process. Episode NFOs are not
// rewritten (they omit showtitle; series name is folder + tvshow.nfo).
// If the folder move fails the DB title/root are reverted; later failures leave disk as-is.
func (s *Store) SeriesMovePass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (renamed, skippedBusy, failed int, err error) {
	step := func(msg string, pct float64) {
		if progress != nil {
			progress(msg, &pct)
		}
	}
	var p seriesMovePayload
	if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
		return 0, 0, 0, fmt.Errorf("%w: payload: %v", ErrInvalid, err)
	}
	p.NewTitle = strings.TrimSpace(p.NewTitle)
	p.OldTitle = strings.TrimSpace(p.OldTitle)
	if p.SeriesID <= 0 || p.NewTitle == "" || p.OldTitle == "" || p.NewRootID <= 0 || p.OldRootID <= 0 {
		return 0, 0, 0, fmt.Errorf("%w: series_move payload incomplete", ErrInvalid)
	}
	cur, err := s.GetSeries(p.SeriesID, false)
	if err != nil {
		return 0, 0, 0, err
	}
	// Resume-safe: DB may already hold the new title/root from an interrupted run.
	if cur.Title != p.NewTitle || cur.RootID != p.NewRootID {
		if taken, err := s.seriesFolderTaken(p.NewRootID, p.NewTitle, p.SeriesID); err != nil {
			return 0, 0, 0, err
		} else if taken {
			return 0, 0, 0, fmt.Errorf("%w: a series with this title already exists under the same root folder", ErrConflict)
		}
		step("Updating series title and root", 0.1)
		if _, err := s.DB.SQL.Exec(`UPDATE series SET title = ?, root_id = ? WHERE id = ?`,
			p.NewTitle, p.NewRootID, p.SeriesID); err != nil {
			return 0, 0, 0, err
		}
	}
	if err := ctx.Err(); err != nil {
		_ = s.revertSeriesTitleRoot(p)
		return 0, 0, 0, err
	}
	updated, err := s.GetSeries(p.SeriesID, false)
	if err != nil {
		return 0, 0, 0, err
	}
	step("Moving series folder", 0.3)
	if err := s.MoveSeriesFolder(updated, p.OldTitle, p.OldRootID); err != nil {
		if rerr := s.revertSeriesTitleRoot(p); rerr != nil {
			return 0, 0, 0, fmt.Errorf("folder move failed: %w (revert title/root also failed: %v)", err, rerr)
		}
		return 0, 0, 0, fmt.Errorf("folder move failed (reverted title/root): %w", err)
	}
	step("Writing series NFO", 0.5)
	if err := s.WriteSeriesNFODisk(p.SeriesID); err != nil {
		return 0, 0, 0, fmt.Errorf("write series NFO after folder move: %w", err)
	}
	// Episode NFOs omit showtitle; folder rename already moved them. No rewrite here.
	step("Applying episode naming", 0.7)
	renamed, skippedBusy, failed, err = s.ApplySeriesEpisodeNaming(ctx, p.SeriesID, task.ID)
	if err != nil {
		return renamed, skippedBusy, failed, fmt.Errorf("apply episode naming after folder move: %w", err)
	}
	return renamed, skippedBusy, failed, nil
}

func (s *Store) revertSeriesTitleRoot(p seriesMovePayload) error {
	_, err := s.DB.SQL.Exec(`UPDATE series SET title = ?, root_id = ? WHERE id = ?`,
		p.OldTitle, p.OldRootID, p.SeriesID)
	return err
}
