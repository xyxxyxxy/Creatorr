package library

// OverviewTotals is live library counts for the Overview page.
type OverviewTotals struct {
	SeriesCount int
	VideoCount  int
	SizeBytes   int64
}

// OverviewTotals returns series count, video count, and packed video media bytes.
func (s *Store) OverviewTotals() (OverviewTotals, error) {
	var out OverviewTotals
	err := s.DB.SQL.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM series),
			(SELECT COUNT(*) FROM videos),
			(SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE kind = 'video' AND size_bytes IS NOT NULL)
	`).Scan(&out.SeriesCount, &out.VideoCount, &out.SizeBytes)
	return out, err
}
