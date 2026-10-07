package library

// CountSeriesByRootIDs returns series counts keyed by root_id for the given roots.
func (s *Store) CountSeriesByRootIDs(rootIDs []int64) (map[int64]int, error) {
	rootIDs = uniqPositiveInt64s(rootIDs)
	out := map[int64]int{}
	if len(rootIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(rootIDs))
	for i, id := range rootIDs {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT root_id, COUNT(*) FROM series
		WHERE root_id IN (`+sqlIntPlaceholders(len(rootIDs))+`)
		GROUP BY root_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rid int64
		var n int
		if err := rows.Scan(&rid, &n); err != nil {
			return nil, err
		}
		out[rid] = n
	}
	return out, rows.Err()
}

// CountFilesByVideoIDs returns file counts keyed by video_id.
func (s *Store) CountFilesByVideoIDs(videoIDs []int64) (map[int64]int, error) {
	videoIDs = uniqPositiveInt64s(videoIDs)
	out := map[int64]int{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(videoIDs))
	for i, id := range videoIDs {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT video_id, COUNT(*) FROM files
		WHERE video_id IN (`+sqlIntPlaceholders(len(videoIDs))+`)
		GROUP BY video_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vid int64
		var n int
		if err := rows.Scan(&vid, &n); err != nil {
			return nil, err
		}
		out[vid] = n
	}
	return out, rows.Err()
}
