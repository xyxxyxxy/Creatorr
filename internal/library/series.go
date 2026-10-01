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
	ID                   int64
	SeriesID             int64
	URL                  string
	Label                sql.NullString
	Kind                 string
	ScanCron             string // empty = never (Scan schedule); feed default weekly
	IndexAsIgnored       bool   // new videos → ignored instead of wanted
	TitleRegexpInclude   string // empty = no include filter; Go regexp must match to index
	TitleRegexpExclude   string // empty = no exclude filter; matching titles are not indexed (wins over include)
	FullScanLimit        int    // 0 = unlimited; yt-dlp --playlist-end on full scan only
	FullScanDone         bool
	// Default catalog SoftFilled onto videos from this source.
	Studio         string
	Country        string
	MPAA           string
	Genres         []string
	Tags           []string
	Actors         []SeriesActor
	SpecialFeature string // empty/NULL = no kind override; else pack role
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
	Notes              string // operator-only; not NFO
	Meta               SeriesMeta
	RootName           string
	QualityProfileName string
	VideoCount         int64
	DownloadedCount    int64 // successful: status downloaded only
	WantedCount        int64 // status wanted | wanted_archive (API); subset of PendingCount
	PendingCount       int64 // open work: wanted | wanted_archive | wanted_download_error | downloaded_integrity_failed
	SourceCount        int64
	Sources            []Source
	Videos             []Video
}

// IsAudio reports delivery_mode=audio.
func (ser Series) IsAudio() bool {
	return ser.DeliveryMode == DeliveryAudio
}

// ProgressTotal is downloaded + pending open work (list progress denominator).
func (ser Series) ProgressTotal() int64 {
	return ser.DownloadedCount + ser.PendingCount
}

// ErrorCount is open error statuses in list progress (PendingCount minus wanted/archive wait).
func (ser Series) ErrorCount() int64 {
	n := ser.PendingCount - ser.WantedCount
	if n < 0 {
		return 0
	}
	return n
}

func scanSource(scanner interface {
	Scan(dest ...any) error
}) (Source, error) {
	var src Source
	var indexAsIgnored, fullScanDone int
	var titleInclude, titleExclude sql.NullString
	var genresRaw, tagsRaw, actorsRaw string
	var specialFeature sql.NullString
	err := scanner.Scan(
		&src.ID, &src.SeriesID, &src.URL, &src.Label, &src.Kind,
		&src.ScanCron, &indexAsIgnored, &titleInclude, &titleExclude, &src.FullScanLimit, &fullScanDone,
		&src.Studio, &src.Country, &src.MPAA, &genresRaw, &tagsRaw, &actorsRaw, &specialFeature,
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
	src.Studio = strings.TrimSpace(src.Studio)
	src.Country = strings.TrimSpace(src.Country)
	src.MPAA = strings.TrimSpace(src.MPAA)
	src.Genres = decodeStringSlice(genresRaw)
	src.Tags = decodeStringSlice(tagsRaw)
	src.Actors = decodeActors(actorsRaw)
	src.SpecialFeature = scanNullPackRole(specialFeature)
	return src, err
}

const sourceSelectCols = `id, series_id, url, label, kind, scan_cron, index_as_ignored,
		       title_regexp_include, title_regexp_exclude, full_scan_limit, full_scan_done,
		       COALESCE(studio,''), COALESCE(country,''), COALESCE(mpaa,''),
		       COALESCE(genres,'[]'), COALESCE(tags,'[]'), COALESCE(actors,'[]'), special_feature`

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
	URL                  string
	Label                string
	Kind                 string // feed (default) or single
	ScanCron             string // empty = never; feed default weekly if omitted
	IndexAsIgnored       bool
	TitleRegexpInclude   string
	TitleRegexpExclude   string
	FullScanLimit        int // 0 = unlimited; ignored for single
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
	res, err := s.insertSource(seriesID, url, label, kind, scanCron, idx, titleIncludeVal, titleExcludeVal, limit, SeedDomainIntoTags(nil, url))
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
			_, _ = s.EnqueueScanSource(id, queue.OriginManual)
		}
	}
	return s.GetSource(seriesID, id)
}

// insertSource writes a sources row (tags typically seeded with domain).
func (s *Store) insertSource(seriesID int64, url string, label any, kind, scanCron string, indexAsIgnored int, titleInclude, titleExclude any, fullScanLimit int, tags []string) (sql.Result, error) {
	return s.DB.SQL.Exec(`
		INSERT INTO sources (series_id, url, label, kind, scan_cron, index_as_ignored, title_regexp_include, title_regexp_exclude, full_scan_limit, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, seriesID, url, label, kind, scanCron, indexAsIgnored, titleInclude, titleExclude, fullScanLimit, encodeStringSlice(tags))
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
	Label                *string
	ScanCron             *string
	IndexAsIgnored       *bool
	TitleRegexpInclude   *string
	TitleRegexpExclude   *string
	FullScanLimit        *int
	Studio               *string
	Country              *string
	MPAA                 *string
	Genres               *[]string
	Tags                 *[]string
	Actors               *[]SeriesActor
	SpecialFeature       *string // empty clears to NULL (no kind override)
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
	studio := cur.Studio
	country := cur.Country
	mpaa := cur.MPAA
	genres := append([]string(nil), cur.Genres...)
	tags := append([]string(nil), cur.Tags...)
	actors := append([]SeriesActor(nil), cur.Actors...)
	specialFeature := cur.SpecialFeature
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
	if p.Studio != nil {
		studio = strings.TrimSpace(*p.Studio)
	}
	if p.Country != nil {
		country = strings.TrimSpace(*p.Country)
	}
	if p.MPAA != nil {
		mpaa = strings.TrimSpace(*p.MPAA)
	}
	if p.Genres != nil {
		genres = ParseStringListFields(*p.Genres)
	}
	if p.Tags != nil {
		tags = ParseStringListFields(*p.Tags)
	}
	if p.Actors != nil {
		actors = append([]SeriesActor(nil), *p.Actors...)
	}
	if p.SpecialFeature != nil {
		sf := strings.TrimSpace(*p.SpecialFeature)
		if sf == "" || NormalizePackRole(sf) == PackRoleRegular {
			specialFeature = ""
		} else {
			if err := ValidatePackRole(sf); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			specialFeature = NormalizePackRole(sf)
		}
	}
	if cur.IsSingle() {
		scanCron = ""
		limit = 0
		indexAsIgnored = false
		titleInclude = ""
		titleExclude = ""
		tags = ensureSourceDomainTagLocked(cur.Kind, cur.URL, tags)
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
		UPDATE sources SET label = ?, scan_cron = ?, index_as_ignored = ?, title_regexp_include = ?, title_regexp_exclude = ?, full_scan_limit = ?,
		  studio = ?, country = ?, mpaa = ?, genres = ?, tags = ?, actors = ?, special_feature = ?
		WHERE id = ? AND series_id = ?
	`, labelVal, scanCron, idx, titleIncludeVal, titleExcludeVal, limit,
		studio, country, mpaa, encodeStringSlice(genres), encodeStringSlice(tags), encodeActors(actors), PackRoleDBValue(specialFeature),
		sourceID, seriesID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.GetSource(seriesID, sourceID)
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
		_, _ = s.Queue.CancelPendingScansForSource(sourceID, queue.CancelReasonSourceDeleted)
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
			_, _ = s.Queue.CancelDownloadsForVideo(vid, queue.CancelReasonSourceDeleted)
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
