package library

import (
	"context"
	"fmt"
	"os"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// RunSeriesMetaIntegrityCheck hashes series art files once (skips tvshow.nfo).
// Returns failed file paths/kinds for notify. Does not touch video statuses.
func (s *Store) RunSeriesMetaIntegrityCheck(ctx context.Context, seriesID int64, progress func(msg string, pct *float64)) (failed []SeriesMetaIntegrityFail, err error) {
	if seriesID <= 0 {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, path, kind FROM files
		WHERE series_id = ? AND video_id IS NULL AND kind != ?
		ORDER BY kind, id
	`, seriesID, SeriesMetaFileRoleNFO)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type hit struct {
		id   int64
		path string
		kind string
	}
	var list []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.path, &h.kind); err != nil {
			return nil, err
		}
		list = append(list, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	serTitle := ""
	if ser, gerr := s.GetSeries(seriesID, false); gerr == nil && ser != nil {
		serTitle = ser.Title
	}
	n := len(list)
	for i, h := range list {
		select {
		case <-ctx.Done():
			return failed, ctx.Err()
		default:
		}
		if progress != nil && n > 0 {
			pct := float64(i) / float64(n)
			progress(fmt.Sprintf("Series art integrity %d/%d…", i+1, n), &pct)
		}
		if _, serr := os.Stat(h.path); serr != nil {
			_ = s.MarkFileHashAttempted(h.id)
			failed = append(failed, SeriesMetaIntegrityFail{
				SeriesID: seriesID, SeriesTitle: serTitle, FileID: h.id, Kind: h.kind, Path: h.path,
				Detail: "file missing on disk",
			})
			continue
		}
		result, mismatch, herr := s.ensureOrCompareFileHash(h.id, h.path)
		if herr != nil {
			_ = s.MarkFileHashAttempted(h.id)
			failed = append(failed, SeriesMetaIntegrityFail{
				SeriesID: seriesID, SeriesTitle: serTitle, FileID: h.id, Kind: h.kind, Path: h.path,
				Detail: integrityFailDetail(herr),
			})
			continue
		}
		if result == IntegrityResultFailed {
			failed = append(failed, SeriesMetaIntegrityFail{
				SeriesID: seriesID, SeriesTitle: serTitle, FileID: h.id, Kind: h.kind, Path: h.path,
				Detail: mismatch,
			})
		}
	}
	return failed, nil
}

// SeriesMetaIntegrityFail is one failed series-meta art check.
type SeriesMetaIntegrityFail struct {
	SeriesID    int64
	SeriesTitle string
	FileID      int64
	Kind        string
	Path        string
	Detail      string
}

// RunSingleSeriesMetaFileIntegrityCheck hashes one series-meta art file (not NFO).
func (s *Store) RunSingleSeriesMetaFileIntegrityCheck(ctx context.Context, fileID int64, progress func(msg string, pct *float64)) (ok bool, detail string, err error) {
	_ = ctx
	f, err := s.GetFile(fileID)
	if err != nil {
		return false, "", err
	}
	if !f.IsSeriesMeta() {
		return false, "", fmt.Errorf("%w: not a series-meta file", ErrInvalid)
	}
	if f.Kind == SeriesMetaFileRoleNFO {
		return false, "", fmt.Errorf("%w: series NFO is not hashed - regenerate from Metadata", ErrInvalid)
	}
	if _, serr := os.Stat(f.Path); serr != nil {
		return false, "", fmt.Errorf("%w: file missing on disk", ErrInvalid)
	}
	if progress != nil {
		progress("Checking series art hash…", nil)
	}
	result, mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path)
	if herr != nil {
		return false, integrityFailDetail(herr), herr
	}
	if result == IntegrityResultFailed {
		return false, mismatch, nil
	}
	return true, "", nil
}

// EnqueueSeriesMetaFileHashCheck queues file_hash_check for a series-meta art file.
func (s *Store) EnqueueSeriesMetaFileHashCheck(fileID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	f, err := s.GetFile(fileID)
	if err != nil {
		return 0, err
	}
	if !f.IsSeriesMeta() {
		return 0, fmt.Errorf("%w: not a series-meta file", ErrInvalid)
	}
	if f.Kind == SeriesMetaFileRoleNFO {
		return 0, fmt.Errorf("%w: series NFO is generated from metadata - regenerate instead of Check hash", ErrInvalid)
	}
	busy, taskID, err := s.FileHashCheckBusy(0, fileID)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: integrity check already queued (task %d)", ErrConflict, taskID)
	}
	if _, serr := os.Stat(f.Path); serr != nil {
		return 0, fmt.Errorf("%w: file missing on disk", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:   queue.OriginManual,
		Kind:     queue.KindFileHashCheck,
		Domain:   queue.SystemDomain,
		SeriesID: f.SeriesID,
		Message:  "Check file hash",
		Payload: map[string]any{
			"series_id": f.SeriesID,
			"file_id":   fileID,
			"path":      f.Path,
			"kind":      f.Kind,
		},
	})
}
