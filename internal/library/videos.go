package library

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// videoListOrderBy: dated rows first (newest upload), then undated by id.
const videoListOrderBy = `(upload_date IS NULL OR upload_date = ''), upload_date DESC, id DESC`

// videoDownloadOrderOldest: oldest upload_date first; undated by lowest id.
const videoDownloadOrderOldest = `(v.upload_date IS NULL OR v.upload_date = '') DESC, v.upload_date ASC, v.id ASC`

// videoSelectCols is the shared SELECT list for scanVideo (order must match Scan).
const videoSelectCols = `id, series_id, source_id, remote_id, title, upload_date, source_url,
		       status, season, episode, COALESCE(description,''), thumbnail_url,
		       COALESCE(media_type,''), duration_seconds, width, height, fps,
		       download_format_selector, download_remux_container, tool, import_src,
		       COALESCE(acquired_via,'source'), acquired_at, sidecars_acquired_at,
		       COALESCE(sorttitle,''), COALESCE(originaltitle,''), COALESCE(studio,''),
		       COALESCE(genres,'[]'), COALESCE(tags,'[]'),
		       COALESCE(uniqueid_type,''), COALESCE(uniqueid_value,''), COALESCE(actors,'[]'),
		       COALESCE(tagline,''), COALESCE(country,''), COALESCE(mpaa,'')`

// Video is an indexed instance within a series.
type Video struct {
	ID                            int64
	SeriesID                      int64
	SourceID                      sql.NullInt64
	RemoteID                      string
	Title                         string
	UploadDate                    sql.NullString
	SourceURL                     sql.NullString
	Status                        string
	Season                        sql.NullInt64
	Episode                       sql.NullInt64
	Description                   string // plot in episode NFO / metadata UI
	ThumbnailURL                  sql.NullString
	MediaType                     string // yt-dlp media_type; empty = missing
	DurationSeconds               sql.NullInt64
	Width                         sql.NullInt64
	Height                        sql.NullInt64
	FPS                           sql.NullFloat64
	DownloadFormatSelector        sql.NullString
	DownloadRemuxContainer        sql.NullString
	Tool                          sql.NullString
	ImportSrc                     sql.NullString
	AcquiredVia                   string
	AcquiredAt                    sql.NullString
	SidecarsAcquiredAt            sql.NullString
	SortTitle                     string
	OriginalTitle                 string
	Studio                        string
	Genres                        []string
	Tags                          []string
	UniqueIDType                  string
	UniqueIDValue                 string
	Actors                        []SeriesActor
	Tagline                       string
	Country                       string
	MPAA                          string
}

// VideoListFilter scopes series video lists by title, status, source, media type,
// upload calendar year, and upload calendar day (UTC).
type VideoListFilter struct {
	Title     string   // case-insensitive substring; empty = any title
	Statuses  []string // empty = all statuses
	SourceID  int64    // 0 = all sources
	MediaType string   // non-empty exact match; empty query = all
	Year      int      // UTC calendar year of upload_date; 0 = any; VideoYearUnknown = undated
	FromDay   string   // YYYY-MM-DD inclusive; empty = no lower bound
	ToDay     string   // YYYY-MM-DD inclusive; empty = no upper bound
}

// VideoYearUnknown selects videos with missing/empty upload_date (?year=unknown).
const VideoYearUnknown = -1

// Active reports whether any filter constraint is set.
func (f VideoListFilter) Active() bool {
	return strings.TrimSpace(f.Title) != "" || len(f.Statuses) > 0 || f.SourceID > 0 || strings.TrimSpace(f.MediaType) != "" || f.Year != 0 || f.FromDay != "" || f.ToDay != ""
}

func appendVideoListFilterSQL(b *strings.Builder, args *[]any, f VideoListFilter) {
	if title := strings.TrimSpace(f.Title); title != "" {
		b.WriteString(` AND title LIKE ? ESCAPE '\' COLLATE NOCASE`)
		*args = append(*args, likeContainsPattern(title))
	}
	if len(f.Statuses) > 0 {
		b.WriteString(` AND status IN (` + sqlIntPlaceholders(len(f.Statuses)) + `)`)
		for _, st := range f.Statuses {
			*args = append(*args, st)
		}
	}
	if f.SourceID > 0 {
		b.WriteString(` AND source_id = ?`)
		*args = append(*args, f.SourceID)
	}
	if mt := strings.TrimSpace(f.MediaType); mt != "" {
		b.WriteString(` AND media_type = ? AND media_type != ''`)
		*args = append(*args, mt)
	}
	switch {
	case f.Year == VideoYearUnknown:
		b.WriteString(` AND (upload_date IS NULL OR trim(upload_date) = '')`)
	case f.Year > 0:
		// UTC calendar year of upload_date (same as year-season / {year}).
		b.WriteString(` AND upload_date IS NOT NULL AND trim(upload_date) != ''`)
		b.WriteString(` AND CAST(strftime('%Y', upload_date) AS INTEGER) = ?`)
		*args = append(*args, f.Year)
	}
	if f.FromDay == "" && f.ToDay == "" {
		return
	}
	// Date range applies to videos with a real upload_date; undated rows are excluded.
	b.WriteString(` AND upload_date IS NOT NULL AND upload_date != ''`)
	if f.FromDay != "" {
		b.WriteString(` AND date(upload_date) >= date(?)`)
		*args = append(*args, f.FromDay)
	}
	if f.ToDay != "" {
		b.WriteString(` AND date(upload_date) <= date(?)`)
		*args = append(*args, f.ToDay)
	}
}

// likeContainsPattern wraps s for SQL LIKE … ESCAPE '\' (substring match).
func likeContainsPattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return `%` + replacer.Replace(s) + `%`
}

// ListRecentVideos returns newest packed library videos (downloaded)
// across all series, ordered by acquired_at then id (highest first).
func (s *Store) ListRecentVideos(limit int) ([]Video, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.DB.SQL.Query(`
		SELECT `+videoSelectCols+`
		FROM videos
		WHERE status = 'downloaded'
		ORDER BY (acquired_at IS NULL OR acquired_at = ''), acquired_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SeriesTitles returns id → title for the given series ids (missing ids omitted).
func (s *Store) SeriesTitles(ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, title FROM series WHERE id IN (`+sqlIntPlaceholders(len(ids))+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}

func (s *Store) ListVideos(seriesID int64) ([]Video, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT `+videoSelectCols+`
		FROM videos WHERE series_id = ?
		ORDER BY `+videoListOrderBy+`
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListVideosPage returns one page of videos for a series (release order).
func (s *Store) ListVideosPage(seriesID int64, limit, offset int) ([]Video, error) {
	return s.ListVideosPageFiltered(seriesID, VideoListFilter{}, limit, offset)
}

// ListVideosPageFiltered returns one page of videos for a series with optional title/status/source/date filters.
func (s *Store) ListVideosPageFiltered(seriesID int64, filter VideoListFilter, limit, offset int) ([]Video, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var b strings.Builder
	b.WriteString(`
		SELECT ` + videoSelectCols + `
		FROM videos WHERE series_id = ?`)
	args := []any{seriesID}
	appendVideoListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY ` + videoListOrderBy + ` LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListVideosBySourcePage returns one page of videos for a source (release order).
func (s *Store) ListVideosBySourcePage(sourceID int64, limit, offset int) ([]Video, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.DB.SQL.Query(`
		SELECT `+videoSelectCols+`
		FROM videos WHERE source_id = ?
		ORDER BY `+videoListOrderBy+`
		LIMIT ? OFFSET ?
	`, sourceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CountVideosForSource returns how many videos belong to a source.
func (s *Store) CountVideosForSource(sourceID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM videos WHERE source_id = ?`, sourceID).Scan(&n)
	return n, err
}

// ListVideosBySourceIDsPage returns one page of videos for any of the given sources.
func (s *Store) ListVideosBySourceIDsPage(sourceIDs []int64, limit, offset int) ([]Video, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	if len(sourceIDs) == 1 {
		return s.ListVideosBySourcePage(sourceIDs[0], limit, offset)
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args := make([]any, 0, len(sourceIDs)+2)
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	args = append(args, limit, offset)
	rows, err := s.DB.SQL.Query(`
		SELECT `+videoSelectCols+`
		FROM videos WHERE source_id IN (`+sqlIntPlaceholders(len(sourceIDs))+`)
		ORDER BY `+videoListOrderBy+`
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CountVideosForSourceIDs returns how many videos belong to any of the given sources.
func (s *Store) CountVideosForSourceIDs(sourceIDs []int64) (int, error) {
	if len(sourceIDs) == 0 {
		return 0, nil
	}
	if len(sourceIDs) == 1 {
		return s.CountVideosForSource(sourceIDs[0])
	}
	args := make([]any, len(sourceIDs))
	for i, id := range sourceIDs {
		args[i] = id
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos WHERE source_id IN (`+sqlIntPlaceholders(len(sourceIDs))+`)
	`, args...).Scan(&n)
	return n, err
}

func sqlIntPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// CountVideos returns how many videos belong to a series.
func (s *Store) CountVideos(seriesID int64) (int, error) {
	return s.CountVideosFiltered(seriesID, VideoListFilter{})
}

// anyVideo reports whether the library has at least one video row.
func (s *Store) anyVideo() (bool, error) {
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM videos`).Scan(&n)
	return n > 0, err
}

// CountVideosFiltered returns how many videos match the series list filter.
func (s *Store) CountVideosFiltered(seriesID int64, filter VideoListFilter) (int, error) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM videos WHERE series_id = ?`)
	args := []any{seriesID}
	appendVideoListFilterSQL(&b, &args, filter)
	var n int
	err := s.DB.SQL.QueryRow(b.String(), args...).Scan(&n)
	return n, err
}

// DistinctVideoStatuses returns statuses present on a series (sorted), for filter chips.
func (s *Store) DistinctVideoStatuses(seriesID int64) ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT status FROM videos
		WHERE series_id = ? AND status IS NOT NULL AND status != ''
		ORDER BY status
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// DistinctVideoYears returns UTC calendar years present on a series upload_date
// (newest first) and whether any video has a missing/empty upload_date.
func (s *Store) DistinctVideoYears(seriesID int64) (years []int, unknown bool, err error) {
	var nUnknown int
	err = s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos
		WHERE series_id = ?
		  AND (upload_date IS NULL OR trim(upload_date) = '')
	`, seriesID).Scan(&nUnknown)
	if err != nil {
		return nil, false, err
	}
	unknown = nUnknown > 0
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT CAST(strftime('%Y', upload_date) AS INTEGER) AS y
		FROM videos
		WHERE series_id = ?
		  AND upload_date IS NOT NULL AND trim(upload_date) != ''
		ORDER BY y DESC
	`, seriesID)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, false, err
		}
		if y > 0 {
			years = append(years, y)
		}
	}
	return years, unknown, rows.Err()
}

// CountVideosBySource returns video counts keyed by source_id for a series.
func (s *Store) CountVideosBySource(seriesID int64) (map[int64]int, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT source_id, COUNT(*) FROM videos
		WHERE series_id = ? AND source_id IS NOT NULL
		GROUP BY source_id
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]int{}
	for rows.Next() {
		var sid int64
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			return nil, err
		}
		out[sid] = n
	}
	return out, rows.Err()
}

func (s *Store) GetVideo(id int64) (*Video, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT `+videoSelectCols+`
		FROM videos WHERE id = ?
	`, id)
	v, err := scanVideo(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func scanVideo(scanner interface {
	Scan(dest ...any) error
}) (Video, error) {
	var v Video
	var genresRaw, tagsRaw, actorsRaw string
	err := scanner.Scan(
		&v.ID, &v.SeriesID, &v.SourceID, &v.RemoteID, &v.Title,
		&v.UploadDate, &v.SourceURL, &v.Status, &v.Season, &v.Episode,
		&v.Description, &v.ThumbnailURL, &v.MediaType,
		&v.DurationSeconds, &v.Width, &v.Height, &v.FPS,
		&v.DownloadFormatSelector, &v.DownloadRemuxContainer, &v.Tool, &v.ImportSrc, &v.AcquiredVia, &v.AcquiredAt, &v.SidecarsAcquiredAt,
		&v.SortTitle, &v.OriginalTitle, &v.Studio,
		&genresRaw, &tagsRaw, &v.UniqueIDType, &v.UniqueIDValue, &actorsRaw,
		&v.Tagline, &v.Country, &v.MPAA,
	)
	v.Genres = decodeStringSlice(genresRaw)
	v.Tags = decodeStringSlice(tagsRaw)
	v.Actors = decodeActors(actorsRaw)
	return v, err
}

// EnqueueDownload queues a download for one video if not already pending/running.
// Rejects when the series is unmonitored (already-queued tasks are kept).
func (s *Store) EnqueueDownload(videoID int64) (int64, error) {
	return s.enqueueDownload(videoID, false)
}

// EnqueueDownloadNow (Queue download UI action) queues at the front of the domain lane, bypasses max_download_queue,
// and allows enqueue when the series is unmonitored. Domain must still be active.
func (s *Store) EnqueueDownloadNow(videoID int64) (int64, error) {
	return s.enqueueDownload(videoID, true)
}

func (s *Store) enqueueDownload(videoID int64, downloadNow bool) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	if !downloadNow {
		ok, err := s.SeriesIsMonitored(cur.SeriesID)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("%w: series unmonitored - turn on series monitored first", ErrInvalid)
		}
	}
	busy, err := s.hasPendingDownload(videoID)
	if err != nil {
		return 0, err
	}
	if busy {
		return 0, fmt.Errorf("%w: download already queued", ErrConflict)
	}
	domain := "unknown"
	if cur.SourceURL.Valid && strings.TrimSpace(cur.SourceURL.String) != "" {
		domain = queueDomain(cur.SourceURL.String)
	} else if cur.SourceID.Valid {
		var url string
		_ = s.DB.SQL.QueryRow(`SELECT url FROM sources WHERE id = ?`, cur.SourceID.Int64).Scan(&url)
		domain = queueDomain(url)
	}
	if !cur.SourceURL.Valid || strings.TrimSpace(cur.SourceURL.String) == "" {
		return 0, fmt.Errorf("%w: video has no source_url (metadata rescan or re-index)", ErrInvalid)
	}
	if downloadNow {
		ok, err := domains.IsActive(s.DB, domain)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("%w: domain inactive - activate under Settings → Domains", ErrInvalid)
		}
	}
	switch cur.Status {
	case "ignored", "deleted", "missing", "wanted_download_error", "verify_failed", StatusWantedArchive:
		_ = s.CancelArchiveDownloadsForVideo(videoID)
		_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, videoID)
	}
	params := enqueueDownloadParams(videoID, cur.SeriesID, domain)
	if downloadNow {
		params = enqueueDownloadNowParams(videoID, cur.SeriesID, domain)
	}
	id, err := s.Queue.Enqueue(params)
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, fmt.Errorf("%w: download already queued", ErrConflict)
		}
		if errors.Is(err, queue.ErrQueueFull) {
			return 0, fmt.Errorf("%w: %v", ErrConflict, err)
		}
		return 0, err
	}
	return id, nil
}

// IgnoreVideo marks a video ignored (will not auto-download).
// Pending and running download tasks for the video are cancelled.
// Returns cancelled download tasks so the caller can write Activity rows.
// Downloaded videos cannot be ignored - use DeleteVideo.
func (s *Store) IgnoreVideo(videoID int64) ([]queue.Task, error) {
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return nil, err
	}
	switch cur.Status {
	case "downloaded", "verify_failed":
		return nil, fmt.Errorf("cannot ignore %s video; delete library files instead", cur.Status)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = 'ignored' WHERE id = ?`, videoID)
	if err != nil {
		return nil, err
	}
	if s.Queue == nil {
		return nil, nil
	}
	cancelled, err := s.Queue.CancelDownloadsForVideo(videoID, "Cancelled (video ignored)")
	if err != nil {
		return cancelled, err
	}
	return cancelled, nil
}

// RecordLiveBroadcastSkipped appends video_history when download soft-skips
// a currently live broadcast. Status is unchanged (stays wanted for later retry).
func (s *Store) RecordLiveBroadcastSkipped(videoID, taskID int64) error {
	if s == nil || videoID <= 0 || taskID <= 0 {
		return nil
	}
	return s.AddVideoHistory(videoID, "live_skipped", "Skipped (currently live)", map[string]any{
		"reason": "is_live",
	}, taskID)
}

// ListSeriesMediaTypes returns distinct non-empty media_type values for a series (alpha).
func (s *Store) ListSeriesMediaTypes(seriesID int64) ([]string, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT DISTINCT media_type FROM videos
		WHERE series_id = ? AND media_type IS NOT NULL AND media_type != ''
		ORDER BY media_type COLLATE NOCASE
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var mt string
		if err := rows.Scan(&mt); err != nil {
			return nil, err
		}
		out = append(out, mt)
	}
	return out, rows.Err()
}

// DeleteVideo cancels download tasks and enqueues a delete_files for worker-owned removal.
// Allowed for downloaded, missing. Returns cancelled pending download tasks.
func (s *Store) DeleteVideo(videoID int64) ([]queue.Task, error) {
	cur, err := s.GetVideo(videoID)
	if err != nil {
		return nil, err
	}
	switch cur.Status {
	case "downloaded", "missing":
	default:
		return nil, fmt.Errorf("cannot delete video with status %s", cur.Status)
	}
	if ok, err := s.VideoQueuedForDelete(videoID); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("%w: video already queued for deletion", ErrConflict)
	}
	var cancelled []queue.Task
	if s.Queue != nil {
		cancelled, err = s.Queue.CancelDownloadsForVideo(videoID, "Cancelled (video deleted)")
		if err != nil {
			return cancelled, err
		}
	}
	_, err = s.EnqueueDeleteFiles(nil, []int64{videoID})
	return cancelled, err
}

// VideoHistoryEvent is one row on a video's timeline.
type VideoHistoryEvent struct {
	ID        int64
	VideoID   int64
	CreatedAt string
	Event     string
	Message   string
	Detail    string
	TaskID    sql.NullInt64
}

// ListVideoHistory returns newest-first history for a video.
func (s *Store) ListVideoHistory(videoID int64) ([]VideoHistoryEvent, error) {
	return s.ListVideoHistoryPage(videoID, 0, 0)
}

// ListVideoHistoryPage returns newest-first history. limit<=0 returns all rows.
func (s *Store) ListVideoHistoryPage(videoID int64, limit, offset int) ([]VideoHistoryEvent, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if limit <= 0 {
		rows, err = s.DB.SQL.Query(`
			SELECT id, video_id, created_at, event, message, COALESCE(detail,'{}'), task_id
			FROM video_history WHERE video_id = ?
			ORDER BY id DESC
		`, videoID)
	} else {
		if offset < 0 {
			offset = 0
		}
		rows, err = s.DB.SQL.Query(`
			SELECT id, video_id, created_at, event, message, COALESCE(detail,'{}'), task_id
			FROM video_history WHERE video_id = ?
			ORDER BY id DESC
			LIMIT ? OFFSET ?
		`, videoID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []VideoHistoryEvent
	for rows.Next() {
		var e VideoHistoryEvent
		if err := rows.Scan(&e.ID, &e.VideoID, &e.CreatedAt, &e.Event, &e.Message, &e.Detail, &e.TaskID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountVideoHistory returns history row count for a video.
func (s *Store) CountVideoHistory(videoID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`SELECT COUNT(*) FROM video_history WHERE video_id = ?`, videoID).Scan(&n)
	return n, err
}

// ListVideoHistoryByTaskID returns history rows written by a specific task (oldest first).
func (s *Store) ListVideoHistoryByTaskID(taskID int64) ([]VideoHistoryEvent, error) {
	if taskID <= 0 {
		return nil, nil
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id, video_id, created_at, event, message, COALESCE(detail,'{}'), task_id
		FROM video_history WHERE task_id = ?
		ORDER BY id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []VideoHistoryEvent
	for rows.Next() {
		var e VideoHistoryEvent
		if err := rows.Scan(&e.ID, &e.VideoID, &e.CreatedAt, &e.Event, &e.Message, &e.Detail, &e.TaskID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WantVideo sets status to wanted from ignored, deleted, missing, or verify_failed.
// Does not enqueue a download - download_wanted_cron or Queue download picks it up.
func (s *Store) WantVideo(id int64) (*Video, error) {
	cur, err := s.GetVideo(id)
	if err != nil {
		return nil, err
	}
	switch cur.Status {
	case "ignored", "deleted", "missing", "verify_failed":
		// ok
	default:
		return nil, fmt.Errorf("%w: want only from ignored, deleted, missing, or verify_failed (got %s)", ErrInvalid, cur.Status)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = 'wanted' WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return s.GetVideo(id)
}
