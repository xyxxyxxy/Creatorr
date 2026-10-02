package queue

// PathTouchingKinds are system tasks that move, rename, or rewrite library paths.
// They never run together: see PathTouchingSystemBusy and series_move in rejectDuplicate.
var PathTouchingKinds = []string{
	KindRenameEpisodes, KindRegenerateNFO, KindResetMetadataFromInfo, KindSyncFiles, KindRetentionDelete, KindSeriesMove,
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
