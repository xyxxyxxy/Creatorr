package library

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
	"github.com/xyxxyxxy/Creatorr/internal/domains"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// Source kinds: feed = channel/playlist (catch-up); single = one video URL (index once).
const (
	SourceKindFeed   = "feed"
	SourceKindSingle = "single"

	DeliveryVideo = "video"
	DeliveryAudio = "audio"

	// AudioFormatSelector is the yt-dlp format ladder for audio-delivery series.
	// Overrides the quality profile's format_selector, which targets video containers.
	AudioFormatSelector = "ba/bestaudio/b"
)

// NormalizeDeliveryMode returns video or audio.
func NormalizeDeliveryMode(m string) string {
	if strings.EqualFold(strings.TrimSpace(m), DeliveryAudio) {
		return DeliveryAudio
	}
	return DeliveryVideo
}

// NormalizeSourceKind returns feed or single.
func NormalizeSourceKind(k string) string {
	if strings.EqualFold(strings.TrimSpace(k), SourceKindSingle) {
		return SourceKindSingle
	}
	return SourceKindFeed
}

// Source is a feed or single URL on a series.
type Source struct {
	ID                 int64
	SeriesID           int64
	URL                string
	Label              sql.NullString
	Kind               string
	ScanCron           string // empty = never (Scan schedule); feed default weekly
	IndexAsIgnored     bool   // new videos → ignored instead of wanted
	TitleRegexpInclude string // empty = no include filter; Go regexp must match to index
	TitleRegexpExclude string // empty = no exclude filter; matching titles are not indexed (wins over include)
	FullScanLimit      int    // 0 = unlimited; yt-dlp --playlist-end on full scan only
	FullScanDone       bool
}

// IsSingle reports kind=single (one-shot index; no tip Scan).
func (src Source) IsSingle() bool {
	return src.Kind == SourceKindSingle
}

// ScanCronNever reports scheduled tip Scan is off.
func (src Source) ScanCronNever() bool {
	return strings.TrimSpace(src.ScanCron) == "" || strings.EqualFold(strings.TrimSpace(src.ScanCron), "never")
}

// Series is one title with root + quality profile.
type Series struct {
	ID                 int64
	Title              string
	RootID             int64
	QualityProfileID   int64
	Monitored          bool
	DeliveryMode       string // video | audio
	AddedAt            string
	Meta               SeriesMeta
	RootName           string
	QualityProfileName string
	VideoCount         int64
	DownloadedCount    int64 // successful: status downloaded only
	WantedCount        int64 // status wanted (API); subset of PendingCount
	PendingCount       int64 // open work: wanted | wanted_download_error | verify_failed
	SourceCount        int64
	Sources            []Source
	Videos             []Video
	AutoIgnoreMediaTypes []string // excluded yt-dlp media_type values; empty = all active
}

// IsAudio reports delivery_mode=audio.
func (ser Series) IsAudio() bool {
	return ser.DeliveryMode == DeliveryAudio
}

// ProgressTotal is downloaded + pending open work (list progress denominator).
func (ser Series) ProgressTotal() int64 {
	return ser.DownloadedCount + ser.PendingCount
}

// ErrorCount is open error statuses in list progress (PendingCount minus wanted).
func (ser Series) ErrorCount() int64 {
	n := ser.PendingCount - ser.WantedCount
	if n < 0 {
		return 0
	}
	return n
}

// CreateSeriesParams creates a series and optional first source.
type CreateSeriesParams struct {
	Title            string
	SourceURL        string
	RootID           int64
	QualityProfileID int64
	Monitored        bool
	DeliveryMode     string
	FullScanLimit    int // first source full-scan playlist cap; 0 = unlimited
	ScanCron         string // feed default weekly when SourceURL set and empty
	IndexAsIgnored   bool
	TitleRegexpInclude string
	TitleRegexpExclude string
	AutoIgnoreMediaTypes []string // series-level exclude; applied on create + download
	SourceLabel      string
}

const (
	SeriesListStatusMonitored   = "monitored"
	SeriesListStatusUnmonitored = "unmonitored"
	SeriesListStatusComplete    = "complete"
	SeriesListStatusIncomplete  = "incomplete"
	SeriesListStatusHasErrors   = "has_errors"
)

// SeriesListFilter scopes the series admin list (title, root, quality, delivery, status).
type SeriesListFilter struct {
	Title            string // case-insensitive substring; empty = any
	RootID           int64  // 0 = any
	QualityProfileID int64  // 0 = any
	DeliveryMode     string // video|audio; empty = any
	Status           string // SeriesListStatus*; empty = any
}

// Active reports whether any series list filter constraint is set.
func (f SeriesListFilter) Active() bool {
	return strings.TrimSpace(f.Title) != "" || f.RootID > 0 || f.QualityProfileID > 0 ||
		f.DeliveryMode == DeliveryVideo || f.DeliveryMode == DeliveryAudio ||
		seriesListStatusActive(f.Status)
}

func seriesListStatusActive(status string) bool {
	switch status {
	case SeriesListStatusMonitored, SeriesListStatusUnmonitored,
		SeriesListStatusComplete, SeriesListStatusIncomplete, SeriesListStatusHasErrors:
		return true
	default:
		return false
	}
}

// seriesProgressOpenStatuses: still-open for list progress + Incomplete (wanted, download error, verify fail).
const seriesProgressOpenStatuses = `'wanted', 'wanted_download_error', 'verify_failed'`

const seriesListSelectCols = `s.id, s.title, s.root_id, s.quality_profile_id, s.monitored, s.delivery_mode, s.added_at,
		       COALESCE(s.auto_ignore_media_types,'[]'),
		       r.name, q.name,
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status = 'downloaded'),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status = 'wanted'),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status IN (` + seriesProgressOpenStatuses + `)),
		       (SELECT COUNT(*) FROM sources f WHERE f.series_id = s.id)`

func appendSeriesListFilterSQL(b *strings.Builder, args *[]any, f SeriesListFilter) {
	if title := strings.TrimSpace(f.Title); title != "" {
		b.WriteString(` AND s.title LIKE ? ESCAPE '\' COLLATE NOCASE`)
		*args = append(*args, likeContainsPattern(title))
	}
	if f.RootID > 0 {
		b.WriteString(` AND s.root_id = ?`)
		*args = append(*args, f.RootID)
	}
	if f.QualityProfileID > 0 {
		b.WriteString(` AND s.quality_profile_id = ?`)
		*args = append(*args, f.QualityProfileID)
	}
	if f.DeliveryMode == DeliveryVideo || f.DeliveryMode == DeliveryAudio {
		b.WriteString(` AND s.delivery_mode = ?`)
		*args = append(*args, f.DeliveryMode)
	}
	switch f.Status {
	case SeriesListStatusMonitored:
		b.WriteString(` AND s.monitored = 1`)
	case SeriesListStatusUnmonitored:
		b.WriteString(` AND s.monitored = 0`)
	case SeriesListStatusComplete:
		// Match list progress: has downloaded or open work, and no open work left.
		b.WriteString(` AND (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status IN ('downloaded', ` + seriesProgressOpenStatuses + `)) > 0
			AND NOT EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN (` + seriesProgressOpenStatuses + `)
			)`)
	case SeriesListStatusIncomplete:
		b.WriteString(` AND EXISTS (
			SELECT 1 FROM videos v
			WHERE v.series_id = s.id AND v.status IN (` + seriesProgressOpenStatuses + `)
		)`)
	case SeriesListStatusHasErrors:
		b.WriteString(` AND (
			EXISTS (
				SELECT 1 FROM videos v
				WHERE v.series_id = s.id AND v.status IN ('wanted_download_error', 'verify_failed')
			)
			OR EXISTS (
				SELECT 1 FROM sources src
				WHERE src.series_id = s.id
				  AND (
				    SELECT sh.event FROM source_history sh
				    WHERE sh.source_id = src.id AND sh.event IN (?, ?)
				    ORDER BY sh.id DESC LIMIT 1
				  ) = ?
			)
		)`)
		*args = append(*args, SourceHistScanned, SourceHistScanError, SourceHistScanError)
	}
}

func scanSeriesListRow(rows *sql.Rows) (Series, error) {
	var ser Series
	var mon int
	var mediaTypeExclude string
	if err := rows.Scan(
		&ser.ID, &ser.Title, &ser.RootID, &ser.QualityProfileID, &mon, &ser.DeliveryMode, &ser.AddedAt,
		&mediaTypeExclude,
		&ser.RootName, &ser.QualityProfileName,
		&ser.VideoCount, &ser.DownloadedCount, &ser.WantedCount, &ser.PendingCount,
		&ser.SourceCount,
	); err != nil {
		return Series{}, err
	}
	ser.Monitored = mon != 0
	ser.DeliveryMode = NormalizeDeliveryMode(ser.DeliveryMode)
	ser.AutoIgnoreMediaTypes = ParseAutoIgnoreMediaTypesJSON(mediaTypeExclude)
	return ser, nil
}

func (s *Store) ListSeries() ([]Series, error) {
	out, err := s.ListSeriesFiltered(SeriesListFilter{}, 0, 0)
	if err != nil {
		return nil, err
	}
	// Load sources after closing the series rows - MaxOpenConns(1) deadlocks on nested queries.
	for i := range out {
		srcs, err := s.listSources(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Sources = srcs
	}
	return out, nil
}

// CountSeriesFiltered returns how many series match the filter.
func (s *Store) CountSeriesFiltered(filter SeriesListFilter) (int, error) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM series s WHERE 1=1`)
	args := []any{}
	appendSeriesListFilterSQL(&b, &args, filter)
	var n int
	err := s.DB.SQL.QueryRow(b.String(), args...).Scan(&n)
	return n, err
}

// ListSeriesFiltered returns series matching filter, newest title order.
// limit <= 0 means no LIMIT (all matches). offset ignored when limit <= 0.
func (s *Store) ListSeriesFiltered(filter SeriesListFilter, limit, offset int) ([]Series, error) {
	var b strings.Builder
	b.WriteString(`
		SELECT ` + seriesListSelectCols + `
		FROM series s
		JOIN root_folders r ON r.id = s.root_id
		JOIN quality_profiles q ON q.id = s.quality_profile_id
		WHERE 1=1`)
	args := []any{}
	appendSeriesListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY s.title COLLATE NOCASE`)
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		b.WriteString(` LIMIT ? OFFSET ?`)
		args = append(args, limit, offset)
	}
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Series
	for rows.Next() {
		ser, err := scanSeriesListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ser)
	}
	return out, rows.Err()
}

func (s *Store) GetSeries(id int64, withVideos bool) (*Series, error) {
	var ser Series
	var genresJSON, tagsJSON, actorsJSON string
	var mediaTypeExclude string
	var mon int
	err := s.DB.SQL.QueryRow(`
		SELECT s.id, s.title, s.root_id, s.quality_profile_id, s.monitored, s.delivery_mode, s.added_at,
		       s.plot, s.sorttitle, s.originaltitle, s.studio, s.genres, s.tags,
		       s.uniqueid_type, s.uniqueid_value, s.actors, s.tagline, s.country, s.mpaa, s.premiered,
		       COALESCE(s.auto_ignore_media_types,'[]'),
		       r.name, q.name,
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status = 'downloaded'),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status = 'wanted'),
		       (SELECT COUNT(*) FROM videos v WHERE v.series_id = s.id AND v.status IN (`+seriesProgressOpenStatuses+`)),
		       (SELECT COUNT(*) FROM sources f WHERE f.series_id = s.id)
		FROM series s
		JOIN root_folders r ON r.id = s.root_id
		JOIN quality_profiles q ON q.id = s.quality_profile_id
		WHERE s.id = ?
	`, id).Scan(
		&ser.ID, &ser.Title, &ser.RootID, &ser.QualityProfileID, &mon, &ser.DeliveryMode, &ser.AddedAt,
		&ser.Meta.Plot, &ser.Meta.SortTitle, &ser.Meta.OriginalTitle, &ser.Meta.Studio, &genresJSON, &tagsJSON,
		&ser.Meta.UniqueIDType, &ser.Meta.UniqueIDValue, &actorsJSON, &ser.Meta.Tagline, &ser.Meta.Country, &ser.Meta.MPAA, &ser.Meta.Premiered,
		&mediaTypeExclude,
		&ser.RootName, &ser.QualityProfileName,
		&ser.VideoCount, &ser.DownloadedCount, &ser.WantedCount, &ser.PendingCount,
		&ser.SourceCount,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ser.Monitored = mon != 0
	ser.DeliveryMode = NormalizeDeliveryMode(ser.DeliveryMode)
	ser.AutoIgnoreMediaTypes = ParseAutoIgnoreMediaTypesJSON(mediaTypeExclude)
	ser.Meta.Genres = decodeStringSlice(genresJSON)
	ser.Meta.Tags = decodeStringSlice(tagsJSON)
	ser.Meta.Actors = decodeActors(actorsJSON)
	srcs, err := s.listSources(id)
	if err != nil {
		return nil, err
	}
	ser.Sources = srcs
	if withVideos {
		vids, err := s.ListVideos(id)
		if err != nil {
			return nil, err
		}
		ser.Videos = vids
	}
	return &ser, nil
}

func (s *Store) CreateSeries(p CreateSeriesParams) (*Series, error) {
	if _, err := s.GetRoot(p.RootID); err != nil {
		return nil, fmt.Errorf("%w: root_id", ErrInvalid)
	}
	if _, err := s.GetProfile(p.QualityProfileID); err != nil {
		return nil, fmt.Errorf("%w: quality_profile_id", ErrInvalid)
	}
	title := strings.TrimSpace(p.Title)
	sourceURL := strings.TrimSpace(p.SourceURL)
	if title == "" {
		if sourceURL != "" {
			title = sourceURL
		} else {
			title = "Untitled series"
		}
	}
	if taken, err := s.seriesFolderTaken(p.RootID, title, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: a series with this title already exists under the same root folder", ErrConflict)
	}
	mon := 0
	if p.Monitored {
		mon = 1
	}
	mode := NormalizeDeliveryMode(p.DeliveryMode)
	res, err := s.DB.SQL.Exec(`
		INSERT INTO series (title, root_id, quality_profile_id, monitored, delivery_mode, added_at, auto_ignore_media_types)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, title, p.RootID, p.QualityProfileID, mon, mode, nowRFC3339(), AutoIgnoreMediaTypesJSON(p.AutoIgnoreMediaTypes))
	if err != nil {
		return nil, err
	}
	sid, _ := res.LastInsertId()
	if sourceURL != "" {
		if _, err := s.AddSource(sid, AddSourceParams{
			URL:                sourceURL,
			Label:              p.SourceLabel,
			FullScanLimit:      p.FullScanLimit,
			ScanCron:           p.ScanCron,
			IndexAsIgnored:     p.IndexAsIgnored,
			TitleRegexpInclude: p.TitleRegexpInclude,
			TitleRegexpExclude: p.TitleRegexpExclude,
		}); err != nil {
			_, _ = s.DB.SQL.Exec(`DELETE FROM series WHERE id = ?`, sid)
			return nil, err
		}
	}
	return s.GetSeries(sid, false)
}

// seriesFolderTaken reports whether another series on rootID would use the same SeriesDir name.
func (s *Store) seriesFolderTaken(rootID int64, title string, excludeSeriesID int64) (bool, error) {
	want := sanitizeName(title, 0)
	rows, err := s.DB.SQL.Query(`SELECT id, title FROM series WHERE root_id = ?`, rootID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return false, err
		}
		if excludeSeriesID > 0 && id == excludeSeriesID {
			continue
		}
		if sanitizeName(t, 0) == want {
			return true, nil
		}
	}
	return false, rows.Err()
}

// UpdateSeriesParams patches series fields; nil pointers mean unchanged.
type UpdateSeriesParams struct {
	Title            *string
	RootID           *int64
	QualityProfileID *int64
	DeliveryMode     *string
	AutoIgnoreMediaTypes *[]string
}

// UpdateSeries updates title, root folder, quality profile, and/or delivery mode.
// Title or root changes move the series folder on disk (blocked while media tasks busy).
// On move failure the DB field is reverted so paths stay correct.
func (s *Store) UpdateSeries(id int64, p UpdateSeriesParams) (*Series, error) {
	cur, err := s.GetSeries(id, false)
	if err != nil {
		return nil, err
	}
	title := cur.Title
	rootID := cur.RootID
	qpID := cur.QualityProfileID
	mode := cur.DeliveryMode
	mediaTypeExclude := cur.AutoIgnoreMediaTypes
	if p.Title != nil {
		title = strings.TrimSpace(*p.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: title", ErrInvalid)
		}
	}
	if p.RootID != nil {
		if _, err := s.GetRoot(*p.RootID); err != nil {
			return nil, fmt.Errorf("%w: root_id", ErrInvalid)
		}
		rootID = *p.RootID
	}
	if p.QualityProfileID != nil {
		if _, err := s.GetProfile(*p.QualityProfileID); err != nil {
			return nil, fmt.Errorf("%w: quality_profile_id", ErrInvalid)
		}
		qpID = *p.QualityProfileID
	}
	if p.DeliveryMode != nil {
		mode = NormalizeDeliveryMode(*p.DeliveryMode)
	}
	if p.AutoIgnoreMediaTypes != nil {
		mediaTypeExclude = NormalizeAutoIgnoreMediaTypes(*p.AutoIgnoreMediaTypes)
	}
	titleChanged := title != cur.Title
	rootChanged := rootID != cur.RootID
	if titleChanged || rootChanged {
		busy, err := s.SeriesHasBusyMediaTasks(id)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, ErrSeriesBusy
		}
	}

	if _, err := s.DB.SQL.Exec(`
		UPDATE series SET title = ?, root_id = ?, quality_profile_id = ?, delivery_mode = ?, auto_ignore_media_types = ? WHERE id = ?
	`, title, rootID, qpID, mode, AutoIgnoreMediaTypesJSON(mediaTypeExclude), id); err != nil {
		return nil, err
	}

	if titleChanged || rootChanged {
		updated, err := s.GetSeries(id, false)
		if err != nil {
			return nil, err
		}
		if err := s.MoveSeriesFolder(updated, cur.Title, cur.RootID); err != nil {
			// Revert DB so Creatorr paths stay correct.
			_, _ = s.DB.SQL.Exec(`
				UPDATE series SET title = ?, root_id = ? WHERE id = ?
			`, cur.Title, cur.RootID, id)
			return nil, fmt.Errorf("folder move failed (reverted title/root): %w", err)
		}
		if err := s.WriteSeriesNFODisk(id); err != nil {
			// Soft: folder moved; NFO rewrite best-effort.
			_ = err
		}
		if s.Queue != nil {
			tid, qerr := s.Queue.InsertRunning(queue.EnqueueParams{
				Kind:     queue.KindRegenerateNFO,
				Domain:   queue.SystemDomain,
				SeriesID: id,
				Message:  "Rewrite episode NFOs after series folder move",
				Payload:  map[string]any{"series_id": id, "scope": "series_move"},
			})
			if qerr == nil {
				_, _, _ = s.RewriteSeriesEpisodeNFOs(id, tid)
				_ = s.Queue.Finish(tid, queue.StatusDone, "Episode NFOs updated after folder move", "", "")
			}
		}
		return s.GetSeries(id, false)
	}

	return s.GetSeries(id, false)
}

// DeleteSeries removes the series and its database index (sources and indexed entries).
// Related scan/download tasks are cancelled. When deleteFiles is true, enqueues delete_files
// (worker owns disk + series DELETE); otherwise deletes the series row immediately and keeps files.
func (s *Store) DeleteSeries(id int64, deleteFiles bool) error {
	if _, err := s.GetSeries(id, false); err != nil {
		return err
	}
	if s.Queue != nil {
		_, _ = s.Queue.CancelPendingScansForSeries(id)
	}

	rows, err := s.DB.SQL.Query(`SELECT id FROM videos WHERE series_id = ?`, id)
	if err != nil {
		return err
	}
	var videoIDs []int64
	for rows.Next() {
		var vid int64
		if err := rows.Scan(&vid); err != nil {
			_ = rows.Close()
			return err
		}
		videoIDs = append(videoIDs, vid)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, vid := range videoIDs {
		if s.Queue != nil {
			_, _ = s.Queue.CancelDownloadsForVideo(vid, "Cancelled (series deleted)")
		}
	}

	if deleteFiles {
		if ok, err := s.SeriesQueuedForDelete(id); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: series already queued for deletion", ErrConflict)
		}
		_, err := s.EnqueueDeleteFiles([]int64{id}, nil)
		return err
	}

	res, err := s.DB.SQL.Exec(`DELETE FROM series WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanSource(scanner interface {
	Scan(dest ...any) error
}) (Source, error) {
	var src Source
	var indexAsIgnored, fullScanDone int
	var titleInclude, titleExclude sql.NullString
	err := scanner.Scan(
		&src.ID, &src.SeriesID, &src.URL, &src.Label, &src.Kind,
		&src.ScanCron, &indexAsIgnored, &titleInclude, &titleExclude, &src.FullScanLimit, &fullScanDone,
	)
	src.Kind = NormalizeSourceKind(src.Kind)
	src.IndexAsIgnored = indexAsIgnored != 0
	if titleInclude.Valid {
		src.TitleRegexpInclude = titleInclude.String
	}
	if titleExclude.Valid {
		src.TitleRegexpExclude = titleExclude.String
	}
	src.FullScanDone = fullScanDone != 0
	if src.IsSingle() {
		src.ScanCron = ""
	}
	return src, err
}

const sourceSelectCols = `id, series_id, url, label, kind, scan_cron, index_as_ignored,
		       title_regexp_include, title_regexp_exclude, full_scan_limit, full_scan_done`

func (s *Store) listSources(seriesID int64) ([]Source, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT `+sourceSelectCols+`
		FROM sources WHERE series_id = ? ORDER BY id
	`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Source
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// SeriesAutoIgnoreMediaTypes returns the series exclude list (empty = all types active).
func (s *Store) SeriesAutoIgnoreMediaTypes(seriesID int64) ([]string, error) {
	var raw string
	err := s.DB.SQL.QueryRow(`SELECT COALESCE(auto_ignore_media_types,'[]') FROM series WHERE id = ?`, seriesID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ParseAutoIgnoreMediaTypesJSON(raw), nil
}

// ListAutoIgnoreMediaTypeSuggestions returns YouTube seed ∪ customs from all series exclude lists.
func (s *Store) ListAutoIgnoreMediaTypeSuggestions() ([]string, error) {
	rows, err := s.DB.SQL.Query(`SELECT COALESCE(auto_ignore_media_types,'[]') FROM series`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var customs []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		customs = append(customs, ParseAutoIgnoreMediaTypesJSON(raw)...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return MergeMediaTypeSuggestions(customs), nil
}

// ListSourceDomains returns sorted unique hostnames from all source URLs.
func (s *Store) ListSourceDomains() ([]string, error) {
	rows, err := s.DB.SQL.Query(`SELECT DISTINCT url FROM sources WHERE url != ''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]struct{}{}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		d := queue.DomainFromURL(raw)
		if d == "" || d == "unknown" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// AddSourceParams adds a source URL to a series.
type AddSourceParams struct {
	URL                string
	Label              string
	Kind               string // feed (default) or single
	ScanCron           string // empty = never; feed default weekly if omitted
	IndexAsIgnored     bool
	TitleRegexpInclude string
	TitleRegexpExclude string
	FullScanLimit      int // 0 = unlimited; ignored for single
}

func (s *Store) AddSource(seriesID int64, p AddSourceParams) (*Source, error) {
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return nil, err
	}
	url := normalizeSourceURL(p.URL)
	if err := ValidateSourceURL(url); err != nil {
		return nil, err
	}
	titleInclude := strings.TrimSpace(p.TitleRegexpInclude)
	if err := ValidateTitleRegexp("title_regexp_include", titleInclude); err != nil {
		return nil, err
	}
	titleExclude := strings.TrimSpace(p.TitleRegexpExclude)
	if err := ValidateTitleRegexp("title_regexp_exclude", titleExclude); err != nil {
		return nil, err
	}
	kind := NormalizeSourceKind(p.Kind)
	limit := p.FullScanLimit
	if limit < 0 {
		return nil, fmt.Errorf("%w: full_scan_limit must be >= 0", ErrInvalid)
	}
	scanCron := strings.TrimSpace(p.ScanCron)
	if kind == SourceKindSingle {
		scanCron = ""
		limit = 0
		titleInclude = ""
		titleExclude = ""
		p.IndexAsIgnored = false
	} else if scanCron == "" {
		scanCron = cronexpr.ScanCronWeekly
	} else if strings.EqualFold(scanCron, "never") {
		scanCron = ""
	}
	var label any
	if strings.TrimSpace(p.Label) != "" {
		label = strings.TrimSpace(p.Label)
	}
	var titleIncludeVal, titleExcludeVal any
	if titleInclude != "" {
		titleIncludeVal = titleInclude
	}
	if titleExclude != "" {
		titleExcludeVal = titleExclude
	}
	idx := 0
	if p.IndexAsIgnored {
		idx = 1
	}
	res, err := s.insertSource(seriesID, url, label, kind, scanCron, idx, titleIncludeVal, titleExcludeVal, limit)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: source URL already on this series", ErrConflict)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id, _ := res.LastInsertId()
	if s.Queue != nil {
		domOK, _ := domains.IsActive(s.DB, queue.DomainFromURL(url))
		if domOK {
			_, _ = s.EnqueueScanSource(id)
		}
	}
	return s.GetSource(seriesID, id)
}

// insertSource writes a sources row.
func (s *Store) insertSource(seriesID int64, url string, label any, kind, scanCron string, indexAsIgnored int, titleInclude, titleExclude any, fullScanLimit int) (sql.Result, error) {
	return s.DB.SQL.Exec(`
		INSERT INTO sources (series_id, url, label, kind, scan_cron, index_as_ignored, title_regexp_include, title_regexp_exclude, full_scan_limit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, seriesID, url, label, kind, scanCron, indexAsIgnored, titleInclude, titleExclude, fullScanLimit)
}

func (s *Store) GetSource(seriesID, sourceID int64) (*Source, error) {
	row := s.DB.SQL.QueryRow(`
		SELECT `+sourceSelectCols+`
		FROM sources WHERE id = ? AND series_id = ?
	`, sourceID, seriesID)
	src, err := scanSource(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &src, nil
}

// UpdateSourceParams patches a source; nil pointers mean unchanged.
// Kind and URL are immutable after create. Single sources force scan_cron empty and clear limit/filters.
type UpdateSourceParams struct {
	Label              *string
	ScanCron           *string
	IndexAsIgnored     *bool
	TitleRegexpInclude *string
	TitleRegexpExclude *string
	FullScanLimit      *int
}

func (s *Store) UpdateSource(seriesID, sourceID int64, p UpdateSourceParams) (*Source, error) {
	cur, err := s.GetSource(seriesID, sourceID)
	if err != nil {
		return nil, err
	}
	label := cur.Label
	scanCron := cur.ScanCron
	indexAsIgnored := cur.IndexAsIgnored
	titleInclude := cur.TitleRegexpInclude
	titleExclude := cur.TitleRegexpExclude
	limit := cur.FullScanLimit
	if p.Label != nil {
		if strings.TrimSpace(*p.Label) == "" {
			label = sql.NullString{}
		} else {
			label = sql.NullString{String: strings.TrimSpace(*p.Label), Valid: true}
		}
	}
	if p.IndexAsIgnored != nil {
		indexAsIgnored = *p.IndexAsIgnored
	}
	if p.TitleRegexpInclude != nil {
		titleInclude = strings.TrimSpace(*p.TitleRegexpInclude)
		if err := ValidateTitleRegexp("title_regexp_include", titleInclude); err != nil {
			return nil, err
		}
	}
	if p.TitleRegexpExclude != nil {
		titleExclude = strings.TrimSpace(*p.TitleRegexpExclude)
		if err := ValidateTitleRegexp("title_regexp_exclude", titleExclude); err != nil {
			return nil, err
		}
	}
	if p.FullScanLimit != nil {
		if *p.FullScanLimit < 0 {
			return nil, fmt.Errorf("%w: full_scan_limit must be >= 0", ErrInvalid)
		}
		limit = *p.FullScanLimit
	}
	if cur.IsSingle() {
		scanCron = ""
		limit = 0
		indexAsIgnored = false
		titleInclude = ""
		titleExclude = ""
	} else {
		if p.ScanCron != nil {
			c := strings.TrimSpace(*p.ScanCron)
			if c == "" || strings.EqualFold(c, "never") {
				scanCron = ""
			} else {
				scanCron = c
			}
		}
	}
	var labelVal, titleIncludeVal, titleExcludeVal any
	if label.Valid {
		labelVal = label.String
	}
	if titleInclude != "" {
		titleIncludeVal = titleInclude
	}
	if titleExclude != "" {
		titleExcludeVal = titleExclude
	}
	idx := 0
	if indexAsIgnored {
		idx = 1
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE sources SET label = ?, scan_cron = ?, index_as_ignored = ?, title_regexp_include = ?, title_regexp_exclude = ?, full_scan_limit = ?
		WHERE id = ? AND series_id = ?
	`, labelVal, scanCron, idx, titleIncludeVal, titleExcludeVal, limit, sourceID, seriesID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	becameNever := !cur.IsSingle() && p.ScanCron != nil && scanCron == "" && !cur.ScanCronNever()
	if becameNever && s.Queue != nil {
		_, _ = s.Queue.CancelPendingScansForSource(sourceID)
	}
	return s.GetSource(seriesID, sourceID)
}

// SetSeriesMonitored updates only series.monitored. Never mutates source/video/domain flags.
// Turning off cancels pending tip Scan tasks only; full scans keep going.
// Turning on does not enqueue scans - unfinished full scan already runs; tip Scan is cron/manual.
func (s *Store) SetSeriesMonitored(seriesID int64, monitored bool) error {
	if _, err := s.GetSeries(seriesID, false); err != nil {
		return err
	}
	mon := 0
	if monitored {
		mon = 1
	}
	_, err := s.DB.SQL.Exec(`UPDATE series SET monitored = ? WHERE id = ?`, mon, seriesID)
	if err != nil {
		return err
	}
	if !monitored && s.Queue != nil {
		_, _ = s.Queue.CancelPendingTipScansForSeries(seriesID)
	}
	_ = s.RewriteSeriesNFOIfPresent(seriesID)
	return nil
}

// DeleteSource removes a source and hard-deletes all videos that belong to it
// (index rows, on-disk artifacts, cancel download tasks). Unmonitor to keep videos.
func (s *Store) DeleteSource(seriesID, sourceID int64) error {
	if _, err := s.GetSource(seriesID, sourceID); err != nil {
		return err
	}
	ser, err := s.GetSeries(seriesID, false)
	if err != nil {
		return err
	}
	if s.Queue != nil {
		_, _ = s.Queue.CancelPendingScansForSource(sourceID)
	}

	rows, err := s.DB.SQL.Query(`SELECT id FROM videos WHERE source_id = ?`, sourceID)
	if err != nil {
		return err
	}
	var videoIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		videoIDs = append(videoIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	var mediaPaths []string
	for _, vid := range videoIDs {
		if s.Queue != nil {
			_, _ = s.Queue.CancelDownloadsForVideo(vid, "Cancelled (source deleted)")
		}
		if path, ok, err := s.HasVideoFile(vid); err != nil {
			return err
		} else if ok {
			mediaPaths = append(mediaPaths, path)
		}
	}
	for _, path := range mediaPaths {
		deleteVideoArtifacts(path)
	}

	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM videos WHERE source_id = ?`, sourceID); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM sources WHERE id = ? AND series_id = ?`, sourceID, seriesID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if root, err := s.GetRoot(ser.RootID); err == nil && root != nil {
		pruneEmptyDirs(root.Path)
	}
	return nil
}
