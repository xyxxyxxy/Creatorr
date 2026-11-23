package queue

// PathTouchingKinds are system tasks that move, rename, or rewrite library paths.
// They never run together: see PathTouchingSystemBusy and series_move in rejectDuplicate.
// Bulk series root changes run inline inside bulk_edit_series (not listed here); see
// BulkEditSeriesRootBusy / SeriesBulkRootMoveOpen.
var PathTouchingKinds = []string{
	KindRenameEpisodes, KindRegenerateNFO, KindResetMetadataFromInfo, KindSyncFiles, KindRetentionDelete, KindSeriesMove,
	KindRewriteCatalogMeta,
}

// PathTouchingSystemBusy reports a pending/running system task of any given kind.
// With no kinds it checks every PathTouchingKinds entry.
func (s *Store) PathTouchingSystemBusy(kinds ...string) (bool, error) {
	if len(kinds) == 0 {
		kinds = PathTouchingKinds
	}
	args := []any{SystemDomain, StatusPending, StatusRunning}
	ph := ""
	for i, k := range kinds {
		if i > 0 {
			ph += ","
		}
		ph += "?"
		args = append(args, k)
	}
	err := s.rejectIfExists(`
		SELECT 1 FROM tasks WHERE domain = ? AND status IN (?, ?) AND kind IN (`+ph+`) LIMIT 1
	`, args...)
	if err == ErrDuplicate {
		return true, nil
	}
	return false, err
}

// BulkEditSeriesRootBusy reports an open bulk_edit_series whose payload sets root_id.
func (s *Store) BulkEditSeriesRootBusy() (bool, error) {
	err := s.rejectIfExists(`
		SELECT 1 FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
		  AND json_extract(payload, '$.root_id') IS NOT NULL
		LIMIT 1
	`, SystemDomain, KindBulkEditSeries, StatusPending, StatusRunning)
	if err == ErrDuplicate {
		return true, nil
	}
	return false, err
}

// SeriesMoveOpen reports a pending/running series_move for seriesID.
func (s *Store) SeriesMoveOpen(seriesID int64) (bool, error) {
	if seriesID <= 0 {
		return false, nil
	}
	err := s.rejectIfExists(`
		SELECT 1 FROM tasks WHERE kind = ? AND series_id = ? AND status IN (?, ?) LIMIT 1
	`, KindSeriesMove, seriesID, StatusPending, StatusRunning)
	if err == ErrDuplicate {
		return true, nil
	}
	return false, err
}

// SeriesBulkRootMoveOpen reports an open bulk_edit_series with root_id that includes seriesID.
func (s *Store) SeriesBulkRootMoveOpen(seriesID int64) (bool, error) {
	if seriesID <= 0 {
		return false, nil
	}
	err := s.rejectIfExists(`
		SELECT 1 FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
		  AND json_extract(payload, '$.root_id') IS NOT NULL
		  AND EXISTS (SELECT 1 FROM json_each(payload, '$.series_ids') j WHERE j.value = ?)
		LIMIT 1
	`, SystemDomain, KindBulkEditSeries, StatusPending, StatusRunning, seriesID)
	if err == ErrDuplicate {
		return true, nil
	}
	return false, err
}
