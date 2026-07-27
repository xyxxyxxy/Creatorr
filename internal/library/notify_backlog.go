package library

import (
	"github.com/xyxxyxxy/Creatorr/internal/domains"
)

// HasEligibleWantedMedia reports whether any monitored wanted video could still
// be enqueued for download (same gates as EnqueueDownloadWanted, without enqueueing).
func (s *Store) HasEligibleWantedMedia() (bool, error) {
	return s.hasEligibleDownloadWanted()
}

func (s *Store) hasEligibleDownloadWanted() (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, COALESCE(v.source_url,''), COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1 AND v.status = 'wanted'
	`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var sourceURL, feedURL string
		if err := rows.Scan(&id, &sourceURL, &feedURL); err != nil {
			return false, err
		}
		if _, ok, err := s.HasVideoFile(id); err != nil {
			return false, err
		} else if ok {
			continue
		}
		busy, err := s.hasPendingDownload(id)
		if err != nil {
			return false, err
		}
		if busy {
			continue
		}
		domain := queueDomain(sourceURL)
		if domain == "unknown" || domain == "" {
			domain = queueDomain(feedURL)
		}
		active, err := domains.IsActive(s.DB, domain)
		if err != nil {
			return false, err
		}
		if !active {
			continue
		}
		return true, nil
	}
	return false, rows.Err()
}
