package library

// OverviewTotals is live library counts for the Overview page.
type OverviewTotals struct {
	SeriesCount      int
	VideoCount       int
	DownloadedCount  int // status downloaded only (matches series DownloadedCount)
	SizeBytes        int64
}

// OverviewTotals returns series count, video counts, and packed video media bytes.
func (s *Store) OverviewTotals() (OverviewTotals, error) {
	var out OverviewTotals
	err := s.DB.SQL.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM series),
			(SELECT COUNT(*) FROM videos),
			(SELECT COUNT(*) FROM videos WHERE status = ?),
			(SELECT COALESCE(SUM(size_bytes), 0) FROM files WHERE kind = 'video' AND size_bytes IS NOT NULL)
	`, StatusDownloaded).Scan(&out.SeriesCount, &out.VideoCount, &out.DownloadedCount, &out.SizeBytes)
	return out, err
}
