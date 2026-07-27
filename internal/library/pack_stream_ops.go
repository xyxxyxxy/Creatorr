package library

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// CompleteStreamable records .strm + NFO (+ thumb + subtitle sidecars) and marks video streamable.
func (s *Store) CompleteStreamable(videoID int64, strmPath, nfoPath, thumbPath string, subPaths []string, taskID int64) error {
	acquired := nowRFC3339()
	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	keep := map[string]struct{}{}
	for _, p := range []string{strmPath, nfoPath, thumbPath} {
		if p != "" {
			keep[p] = struct{}{}
		}
	}
	for _, p := range subPaths {
		if p != "" {
			keep[p] = struct{}{}
		}
	}

	oldRows, _ := tx.Query(`SELECT path FROM files WHERE video_id = ?`, videoID)
	if oldRows != nil {
		var oldPaths []string
		for oldRows.Next() {
			var p string
			if oldRows.Scan(&p) == nil && p != "" {
				oldPaths = append(oldPaths, p)
			}
		}
		_ = oldRows.Close()
		for _, p := range oldPaths {
			if _, ok := keep[p]; !ok {
				_ = os.Remove(p)
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ?`, videoID); err != nil {
		return err
	}
	for kind, path := range map[string]string{"strm": strmPath, "nfo": nfoPath, "thumb": thumbPath} {
		if path == "" || !fileExists(path) {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, ?, ?, NULL)
		`, videoID, path, kind, acquired); err != nil {
			return err
		}
	}
	for _, p := range subPaths {
		if p == "" || !fileExists(p) {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, 'sub', ?, NULL)
		`, videoID, p, acquired); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE videos SET status = 'streamable', acquired_at = ?, sidecars_acquired_at = ? WHERE id = ?`, acquired, acquired, videoID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.AddVideoHistory(videoID, "stream_packed", "Stream files ready", map[string]any{
		"strm": strmPath, "nfo": nfoPath, "thumb": thumbPath, "subs": len(subPaths),
	}, taskID)
	return nil
}

// EnqueuePackStream queues pack_stream for one video (Prepare stream / refresh).
func (s *Store) EnqueuePackStream(videoID int64, highPriority bool) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	ser, err := s.GetSeries(cur.SeriesID, false)
	if err != nil {
		return 0, err
	}
	if !ser.IsStream() {
		return 0, fmt.Errorf("%w: series is not stream delivery", ErrInvalid)
	}
	if strings.TrimSpace(s.EffectivePublicBaseURL()) == "" {
		return 0, fmt.Errorf("%w: external Creatorr URL required", ErrInvalid)
	}
	domain := "unknown"
	if cur.SourceURL.Valid && strings.TrimSpace(cur.SourceURL.String) != "" {
		domain = queueDomain(cur.SourceURL.String)
	} else if cur.SourceID.Valid {
		var u string
		_ = s.DB.SQL.QueryRow(`SELECT url FROM sources WHERE id = ?`, cur.SourceID.Int64).Scan(&u)
		domain = queueDomain(u)
	}
	okActive, err := domains.IsActive(s.DB, domain)
	if err != nil {
		return 0, err
	}
	if !okActive {
		return 0, fmt.Errorf("%w: domain inactive - activate under Settings → Domains", ErrInvalid)
	}
	prio := 0
	if highPriority {
		prio = queue.PriorityDownloadNow
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindPackStream,
		Domain:   domain,
		SeriesID: cur.SeriesID,
		VideoID:  videoID,
		Priority: prio,
		Message:  "Prepare stream",
	})
}

// EnqueuePackStreamWanted packs .strm for wanted videos on stream-mode series.
func (s *Store) EnqueuePackStreamWanted() (int, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if strings.TrimSpace(s.EffectivePublicBaseURL()) == "" {
		return 0, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT v.id, v.series_id, COALESCE(v.source_url,''), v.source_id, COALESCE(src.url,'')
		FROM videos v
		JOIN series ser ON ser.id = v.series_id
		JOIN sources src ON src.id = v.source_id
		WHERE ser.monitored = 1 AND ser.delivery_mode = 'stream' AND v.status = 'wanted'
		ORDER BY v.series_id, v.id
	`)
	if err != nil {
		return 0, err
	}
	type packRow struct {
		id, seriesID        int64
		sourceURL, feedURL  string
		sourceID            sql.NullInt64
	}
	var pending []packRow
	for rows.Next() {
		var r packRow
		if err := rows.Scan(&r.id, &r.seriesID, &r.sourceURL, &r.sourceID, &r.feedURL); err != nil {
			_ = rows.Close()
			return 0, err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range pending {
		domain := queueDomain(r.sourceURL)
		if domain == "unknown" || domain == "" {
			domain = queueDomain(r.feedURL)
		}
		okActive, err := domains.IsActive(s.DB, domain)
		if err != nil || !okActive {
			continue
		}
		if _, err := s.Queue.Enqueue(queue.EnqueueParams{
			Kind:     queue.KindPackStream,
			Domain:   domain,
			SeriesID: r.seriesID,
			VideoID:  r.id,
			Message:  "Prepare stream",
		}); err != nil {
			if err == queue.ErrDuplicate {
				continue
			}
			continue
		}
		n++
	}
	return n, nil
}

// PackStreamForVideo builds strm+nfo (+ optional thumb + subtitle sidecars) for one video (used by worker).
// runtimeSeconds may be 0; when >0 it is written into NFO (Emby strm duration) and duration_seconds.
// thumbSrc is an optional on-disk image from yt-dlp --write-thumbnail; when empty, soft-fetches thumbnail_url.
// subSrcs are optional yt-dlp subtitle files to copy beside the .strm (soft-ok if empty/missing).
func (s *Store) PackStreamForVideo(videoID, taskID int64, runtimeSeconds int, thumbSrc string, subSrcs []string) error {
	if strings.TrimSpace(s.EffectivePublicBaseURL()) == "" {
		return fmt.Errorf("%w: external Creatorr URL required", ErrInvalid)
	}
	_ = s.ClearBeginning(videoID)
	_ = s.ClearPlaybackCache(videoID)
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	s.softFillGenresOntoVideo(v)
	sourceURL := ""
	if v.SourceURL.Valid {
		sourceURL = v.SourceURL.String
	}
	_, _ = s.EnsureVideoDomainTag(videoID, sourceURL)
	if fresh, gerr := s.GetVideo(videoID); gerr == nil {
		v = fresh
	}
	if runtimeSeconds <= 0 && v.DurationSeconds.Valid {
		runtimeSeconds = int(v.DurationSeconds.Int64)
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return err
	}
	root, err := s.GetRoot(ser.RootID)
	if err != nil {
		return err
	}
	tok, err := EnsureStreamToken(s.DB)
	if err != nil {
		return err
	}
	streamURL, err := StreamURLForKind(s.EffectivePublicBaseURL(), videoID, tok, v.StreamKind())
	if err != nil {
		return err
	}
	// Undated index rows leave season/episode NULL → year-season 0 (S0000), not TV default S1.
	season, episode := 0, 0
	if v.Season.Valid {
		season = int(v.Season.Int64)
	}
	if v.Episode.Valid {
		episode = int(v.Episode.Int64)
	}
	aired := ""
	if v.UploadDate.Valid {
		aired = v.UploadDate.String
	}
	meta := episodeMetaFromVideo(v, ser.Title, season, episode, aired, runtimeSeconds)
	thumbURL := ""
	if v.ThumbnailURL.Valid {
		thumbURL = v.ThumbnailURL.String
	}
	resolvedThumb, cleanupThumb := MaterializeThumbSrc(thumbSrc, thumbURL)
	defer cleanupThumb()
	allowPaths, _ := s.filePathsForVideo(videoID)
	strmPath, nfoPath, thumbPath, subPaths, err := PackStream(streamURL, root.Path, meta, LoadNamingConfig(s.DB), resolvedThumb, subSrcs, allowPaths)
	if err != nil {
		return err
	}
	if runtimeSeconds > 0 {
		_ = s.SetDurationSeconds(videoID, runtimeSeconds)
	}
	return s.CompleteStreamable(videoID, strmPath, nfoPath, thumbPath, subPaths, taskID)
}

func (s *Store) filePathsForVideo(videoID int64) ([]string, error) {
	rows, err := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ?`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && p != "" {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func parsePositiveInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid")
	}
	return n, nil
}

// EnsureStreamNFODuration rewrites the streamable episode NFO when runtime is known and missing/outdated.
func (s *Store) EnsureStreamNFODuration(videoID int64, runtimeSeconds int) error {
	if runtimeSeconds <= 0 {
		return nil
	}
	_ = s.SetDurationSeconds(videoID, runtimeSeconds)
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return err
	}
	var nfoPath string
	err = s.DB.SQL.QueryRow(`SELECT path FROM files WHERE video_id = ? AND kind = 'nfo' LIMIT 1`, videoID).Scan(&nfoPath)
	if err != nil || strings.TrimSpace(nfoPath) == "" {
		return nil
	}
	// Undated index rows leave season/episode NULL → year-season 0 (S0000), not TV default S1.
	season, episode := 0, 0
	if v.Season.Valid {
		season = int(v.Season.Int64)
	}
	if v.Episode.Valid {
		episode = int(v.Episode.Int64)
	}
	aired := ""
	if v.UploadDate.Valid {
		aired = v.UploadDate.String
	}
	return WriteEpisodeNFO(nfoPath, episodeMetaFromVideo(v, ser.Title, season, episode, aired, runtimeSeconds))
}
