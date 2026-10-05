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
		       download_format_selector, download_remux_container,
		       acquired_via, acquired_at, sidecars_acquired_at,
		       COALESCE(sorttitle,''), COALESCE(originaltitle,''), COALESCE(studio,''),
		       COALESCE(genres,'[]'), COALESCE(tags,'[]'),
		       COALESCE(uniqueid_type,''), COALESCE(uniqueid_value,''), COALESCE(actors,'[]'),
		       COALESCE(tagline,''), COALESCE(country,''), COALESCE(mpaa,''),
		       COALESCE(special_feature,''), COALESCE(notes,'')`

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
	AcquiredVia                   sql.NullString
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
	PackRole                      string // episode | special_episode | feature kind folder name
	Notes                         string // operator-only; not NFO
}

// VideoListFilter scopes video lists by text, catalog metadata, status, source,
// dates, presence (empty/not_empty), and optional series (library-wide lists).
type VideoListFilter struct {
	Title      string   // case-insensitive substring against QField
	QField     string   // title|sorttitle|originaltitle|plot|tagline|notes
	Statuses   []string // empty = all statuses
	SourceIDs  []int64  // VideoSourceImport (-1) = source_id IS NULL; OR with positive ids
	SeriesIDs  []int64  // library-wide lists when seriesID==0
	MediaTypes []string // non-empty values; OR match; empty query = all
	Years      []int    // UTC calendar years of upload_date; 0 entries ignored
	PackRoles  []string // OR: regular | special | exact special_feature
	FromDay    string   // YYYY-MM-DD inclusive; empty = no lower bound
	ToDay      string   // YYYY-MM-DD inclusive; empty = no upper bound
	Studios    []string
	Countries  []string
	MPAAs      []string
	Genres     []string
	Tags       []string
	Actors     []string
	Empty      []string // presence field ids
	NotEmpty   []string
	Sort       string // upload|added|acquired|title; empty = upload
	SortDir    string // asc|desc; empty = DefaultSortDir(Sort)
}

// VideoSourceImport filters videos with source_id IS NULL (?source=import).
// Covers Import-created rows and Add-video indexed rows until they gain a feed source_id.
const VideoSourceImport int64 = -1

// VideoSourceImportQuery is the HTTP/query sentinel for VideoSourceImport.
const VideoSourceImportQuery = "import"

// VideoPackRoleAnySpecial is the list-filter value for every non-regular special_feature.
const VideoPackRoleAnySpecial = "special"

// MenuActive reports whether any Filter-menu constraint is set (not search, not sort).
func (f VideoListFilter) MenuActive() bool {
	return len(f.Statuses) > 0 || len(f.SourceIDs) > 0 || len(f.SeriesIDs) > 0 ||
		len(trimNonEmptyStrings(f.MediaTypes)) > 0 || len(uniqNonZeroInts(f.Years)) > 0 ||
		len(trimNonEmptyStrings(f.PackRoles)) > 0 ||
		f.FromDay != "" || f.ToDay != "" ||
		len(trimNonEmptyStrings(f.Studios)) > 0 || len(trimNonEmptyStrings(f.Countries)) > 0 ||
		len(trimNonEmptyStrings(f.MPAAs)) > 0 ||
		len(f.Genres) > 0 || len(f.Tags) > 0 || len(f.Actors) > 0 ||
		len(f.Empty) > 0 || len(f.NotEmpty) > 0
}

// Active reports whether search or any Filter-menu constraint is set (not sort).
func (f VideoListFilter) Active() bool {
	return strings.TrimSpace(f.Title) != "" || f.MenuActive()
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
		limit = 10
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

// ListVideosPageFiltered returns one page of videos.
// seriesID > 0 scopes to that series; seriesID == 0 lists the whole library
// (optional filter.SeriesIDs still applies).
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
		FROM videos WHERE 1=1`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND series_id = ?`)
		args = append(args, seriesID)
	}
	appendVideoListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY ` + videoOrderByClause(filter.Sort, filter.SortDir) + ` LIMIT ? OFFSET ?`)
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

// CountVideosFiltered returns how many videos match the list filter.
// seriesID > 0 scopes to that series; seriesID == 0 is library-wide.
func (s *Store) CountVideosFiltered(seriesID int64, filter VideoListFilter) (int, error) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM videos WHERE 1=1`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND series_id = ?`)
		args = append(args, seriesID)
	}
	appendVideoListFilterSQL(&b, &args, filter)
	var n int
	err := s.DB.SQL.QueryRow(b.String(), args...).Scan(&n)
	return n, err
}

// DistinctVideoStatuses returns statuses present (sorted). seriesID 0 = library-wide.
func (s *Store) DistinctVideoStatuses(seriesID int64) ([]string, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT DISTINCT status FROM videos
		WHERE status IS NOT NULL AND status != ''`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND series_id = ?`)
		args = append(args, seriesID)
	}
	b.WriteString(` ORDER BY status`)
	rows, err := s.DB.SQL.Query(b.String(), args...)
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

// DistinctVideoYears returns UTC calendar years present on upload_date (newest first).
// seriesID 0 = library-wide. Undated videos use presence empty=upload_date, not a year slot.
func (s *Store) DistinctVideoYears(seriesID int64) (years []int, err error) {
	var b strings.Builder
	b.WriteString(`
		SELECT DISTINCT CAST(strftime('%Y', upload_date) AS INTEGER) AS y
		FROM videos
		WHERE upload_date IS NOT NULL AND trim(upload_date) != ''`)
	args := []any{}
	if seriesID > 0 {
		b.WriteString(` AND series_id = ?`)
		args = append(args, seriesID)
	}
	b.WriteString(` ORDER BY y DESC`)
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		if y > 0 {
			years = append(years, y)
		}
	}
	return years, rows.Err()
}

// CountVideosWithNullSource returns how many videos on the series have source_id IS NULL.
func (s *Store) CountVideosWithNullSource(seriesID int64) (int, error) {
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM videos WHERE series_id = ? AND source_id IS NULL
	`, seriesID).Scan(&n)
	return n, err
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

// CountVideosForSources returns indexed (all statuses) and downloaded (packed media
// statuses) video counts across the given source ids. Used by bulk delete confirm.
func (s *Store) CountVideosForSources(sourceIDs []int64) (indexed, downloaded int, err error) {
	sourceIDs = uniqInt64(sourceIDs)
	if len(sourceIDs) == 0 {
		return 0, 0, nil
	}
	placeholders := make([]string, len(sourceIDs))
	args := make([]any, len(sourceIDs))
	for i, id := range sourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END), 0)
		FROM videos
		WHERE source_id IN (` + strings.Join(placeholders, ",") + `)`
	args = append([]any{StatusDownloaded, StatusDownloadedIntegrityFailed}, args...)
	err = s.DB.SQL.QueryRow(q, args...).Scan(&indexed, &downloaded)
	return indexed, downloaded, err
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
		&v.DownloadFormatSelector, &v.DownloadRemuxContainer, &v.AcquiredVia, &v.AcquiredAt, &v.SidecarsAcquiredAt,
		&v.SortTitle, &v.OriginalTitle, &v.Studio,
		&genresRaw, &tagsRaw, &v.UniqueIDType, &v.UniqueIDValue, &actorsRaw,
		&v.Tagline, &v.Country, &v.MPAA, &v.PackRole, &v.Notes,
	)
	v.Genres = decodeStringSlice(genresRaw)
	v.Tags = decodeStringSlice(tagsRaw)
	v.Actors = decodeActors(actorsRaw)
	v.PackRole = NormalizePackRole(v.PackRole)
	return v, err
}

// EnqueueDownload queues a download for one video if not already pending/running.
// Rejects when the series is unmonitored (already-queued tasks are kept).
func (s *Store) EnqueueDownload(videoID int64) (int64, error) {
	return s.enqueueDownload(videoID, false)
}

// EnqueueDownloadNow (Download now UI action on the video page) enqueues an immediate download
// (ClaimImmediate: skip queue / parallel / cooldown / soft pause), bypasses max_download_queue,
// and allows enqueue when the series is unmonitored. Domain must still be active.
// If a pending download already exists, marks it immediate instead of erroring.
// Refuses status downloaded: media is already present (import, source, or archive).
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
	if cur.Status == "downloaded" {
		return 0, fmt.Errorf("%w: video already present", ErrInvalid)
	}
	if open, err := s.SeriesHasOpenMove(cur.SeriesID); err != nil {
		return 0, err
	} else if open {
		return 0, ErrSeriesMoveBusy
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
		var pendingID int64
		var st string
		err = s.DB.SQL.QueryRow(`
			SELECT id, status FROM tasks
			WHERE kind = ? AND video_id = ? AND status IN (?, ?)
			ORDER BY CASE status WHEN ? THEN 0 ELSE 1 END, id ASC
			LIMIT 1
		`, queue.KindDownload, videoID, queue.StatusPending, queue.StatusRunning, queue.StatusRunning).Scan(&pendingID, &st)
		if err == nil {
			if st == queue.StatusRunning {
				return 0, fmt.Errorf("%w: download already running", ErrConflict)
			}
			if err := s.Queue.MarkDownloadNowImmediate(pendingID); err != nil {
				return 0, err
			}
			return pendingID, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	} else {
		busy, err := s.hasPendingDownload(videoID)
		if err != nil {
			return 0, err
		}
		if busy {
			return 0, fmt.Errorf("%w: download already queued", ErrConflict)
		}
	}
	switch cur.Status {
	case StatusIgnored, StatusDeleted, StatusMissing, StatusWantedDownloadError, StatusDownloadedIntegrityFailed, StatusWantedArchive:
		_ = s.CancelArchiveDownloadsForVideo(videoID)
		_, _ = s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusWanted, videoID)
	}
	params := enqueueDownloadParams(videoID, cur.SeriesID, domain, queue.OriginManual)
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
	case StatusDownloaded, StatusDownloadedIntegrityFailed:
		return nil, fmt.Errorf("cannot ignore %s video; delete library files instead", cur.Status)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusIgnored, videoID)
	if err != nil {
		return nil, err
	}
	if s.Queue == nil {
		return nil, nil
	}
	cancelled, err := s.Queue.CancelDownloadsForVideo(videoID, queue.CancelReasonVideoIgnored)
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
		cancelled, err = s.Queue.CancelDownloadsForVideo(videoID, queue.CancelReasonVideoDeleted)
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

// WantVideo sets status to wanted from ignored, deleted, missing, or downloaded_integrity_failed.
// Does not enqueue a download - download_wanted_cron or Download now picks it up.
func (s *Store) WantVideo(id int64) (*Video, error) {
	cur, err := s.GetVideo(id)
	if err != nil {
		return nil, err
	}
	switch cur.Status {
	case StatusIgnored, StatusDeleted, StatusMissing, StatusDownloadedIntegrityFailed:
		// ok
	default:
		return nil, fmt.Errorf("%w: want only from ignored, deleted, missing, or downloaded_integrity_failed (got %s)", ErrInvalid, cur.Status)
	}
	_, err = s.DB.SQL.Exec(`UPDATE videos SET status = ? WHERE id = ?`, StatusWanted, id)
	if err != nil {
		return nil, err
	}
	return s.GetVideo(id)
}
