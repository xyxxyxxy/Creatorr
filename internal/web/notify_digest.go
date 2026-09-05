package web

import (
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/notify"
)

// digestRelatedSectionsFromBody builds Related to video rows from a download_digest body.
// Prefers [#id] markers; falls back to series/title lookup for older digests.
func digestRelatedSectionsFromBody(body string, resolveID func(videoID int64) notifyFileSyncIssueRef, resolveTitles func(series, title string) notifyFileSyncIssueRef) []notifyFileSyncIssueSection {
	lines := notify.ParseDigestBodyLines(body)
	if len(lines) == 0 {
		return nil
	}
	items := make([]notifyFileSyncIssueRef, 0, len(lines))
	seen := map[int64]struct{}{}
	for _, line := range lines {
		var ref notifyFileSyncIssueRef
		if line.VideoID > 0 && resolveID != nil {
			ref = resolveID(line.VideoID)
			ref.ID = line.VideoID
		} else if resolveTitles != nil {
			ref = resolveTitles(line.Series, line.Title)
		}
		if ref.ID <= 0 && line.VideoID <= 0 {
			ref.Series = line.Series
			ref.Title = line.Title
			ref.Missing = true
		}
		if ref.ID > 0 {
			if _, ok := seen[ref.ID]; ok {
				continue
			}
			seen[ref.ID] = struct{}{}
		}
		if line.Suffix != "" && ref.Detail == "" {
			ref.Detail = line.Suffix
		}
		if ref.Series == "" && line.Series != "" {
			ref.Series = line.Series
		}
		if ref.Title == "" && line.Title != "" {
			ref.Title = line.Title
		}
		items = append(items, ref)
	}
	if sec := capFileSyncNotifySection("Downloaded", items); sec != nil {
		return []notifyFileSyncIssueSection{*sec}
	}
	return nil
}

func (h *Handler) resolveDigestNotifyByTitles(series, title string) notifyFileSyncIssueRef {
	ref := notifyFileSyncIssueRef{Series: series, Title: title, Missing: true}
	if h.Library == nil || h.Library.DB == nil {
		return ref
	}
	series = strings.TrimSpace(series)
	title = strings.TrimSpace(title)
	if title == "" {
		return ref
	}
	var id, seriesID int64
	var serTitle, vidTitle string
	var err error
	if series != "" {
		err = h.Library.DB.SQL.QueryRow(`
			SELECT v.id, v.series_id, s.title, v.title
			FROM videos v
			JOIN series s ON s.id = v.series_id
			WHERE s.title = ? AND v.title = ?
			ORDER BY v.id DESC LIMIT 1
		`, series, title).Scan(&id, &seriesID, &serTitle, &vidTitle)
	} else {
		err = h.Library.DB.SQL.QueryRow(`
			SELECT v.id, v.series_id, s.title, v.title
			FROM videos v
			JOIN series s ON s.id = v.series_id
			WHERE v.title = ?
			ORDER BY v.id DESC LIMIT 1
		`, title).Scan(&id, &seriesID, &serTitle, &vidTitle)
	}
	if err != nil || id <= 0 {
		return ref
	}
	ref.ID = id
	ref.SeriesID = seriesID
	ref.Series = serTitle
	ref.Title = vidTitle
	ref.Missing = false
	return ref
}
