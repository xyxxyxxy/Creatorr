package library

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/settings"
	"github.com/xyxxyxxy/Creatorr/internal/ytdlp"
)

// ListedVideo is scan input for upserting the index.
type ListedVideo struct {
	RemoteID     string
	Title        string
	WebpageURL   string
	UploadDate   string
	Description  string
	ThumbnailURL string
	MediaType    string // yt-dlp media_type; empty = missing
	SourceID     int64
	// DurationSeconds from list/resolve when known (>0); stored in duration_seconds on create.
	DurationSeconds float64
}

// UpsertResult is one upsert outcome.
type UpsertResult struct {
	VideoID      int64
	Created      bool
	Skipped      bool   // true when title filter rejects create (no row)
	SkipReason   string // title_regexp_include | title_regexp_exclude when Skipped
	Status       string
	IgnoreReason string // "" | index_as_ignored (create only)
}

// NormalizeUploadTime validates and canonicalizes handler/DB upload_date as RFC3339 UTC.
// Empty or non-RFC3339 input yields "". Handlers must send a full timestamp (not date-only / Unix).
func NormalizeUploadTime(raw string) string {
	t, ok := ParseUploadTime(raw)
	if !ok {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// UploadCalendarDate returns YYYY-MM-DD (UTC) for NFO aired / season-day math.
func UploadCalendarDate(raw string) string {
	t, ok := ParseUploadTime(raw)
	if !ok {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// ParseUploadTime parses an RFC3339 / RFC3339Nano upload_date.
func ParseUploadTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

// UpsertListed inserts or updates a video from a scan listing (index-only).
// taskID links episode repack history when season/episode shift; list-pass
// discover/update facts live on source_history, not video_history.
func (s *Store) UpsertListed(seriesID int64, li ListedVideo, taskID int64) (UpsertResult, error) {
	var out UpsertResult
	if li.RemoteID == "" {
		return out, fmt.Errorf("%w: remote_id required", ErrInvalid)
	}
	upload := NormalizeUploadTime(li.UploadDate)

	var existingID int64
	var existingStatus string
	err := s.DB.SQL.QueryRow(`
		SELECT id, status FROM videos
		WHERE series_id = ? AND remote_id = ?
	`, seriesID, li.RemoteID).Scan(&existingID, &existingStatus)
	if err != nil && err != sql.ErrNoRows {
		return out, err
	}

	if err == sql.ErrNoRows {
		status := "wanted"
		ignoreReason := ""
		if li.SourceID > 0 {
			if src, serr := s.GetSourceByID(li.SourceID); serr == nil {
				if ok, reason := TitlePassesFilters(src.TitleRegexpInclude, src.TitleRegexpExclude, li.Title); !ok {
					out.Skipped = true
					out.SkipReason = reason
					return out, nil
				}
				if src.IndexAsIgnored {
					status = "ignored"
					ignoreReason = IgnoreReasonIndexAsIgnored
				}
			}
		}
		var src any
		if li.SourceID > 0 {
			src = li.SourceID
		}
		var uploadVal, thumb, seasonVal, episodeVal any
		if upload != "" {
			uploadVal = upload
		}
		if li.ThumbnailURL != "" {
			thumb = li.ThumbnailURL
		}
		res, err := s.insertListedVideo(seriesID, src, li, uploadVal, status, seasonVal, episodeVal, thumb)
		if err != nil {
			return out, err
		}
		id, _ := res.LastInsertId()
		out.VideoID = id
		out.Created = true
		out.Status = status
		out.IgnoreReason = ignoreReason
		if li.DurationSeconds > 0 {
			_ = s.SetDurationSeconds(id, int(li.DurationSeconds+0.5))
		}
		if mt := NormalizeMediaType(li.MediaType); mt != "" {
			_ = s.SetMediaType(id, mt)
		}
		if upload != "" {
			changed, rerr := s.ReindexSeriesUTCDay(seriesID, UploadCalendarDate(upload))
			if rerr != nil {
				return out, rerr
			}
			_ = s.repackEpisodeNumberChanges(changed, taskID)
		}
		return out, nil
	}

	out.VideoID = existingID
	out.Status = existingStatus
	// Soft-fill title/description/thumbnail_url only when empty (never clobber first-seen / operator).
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = COALESCE(NULLIF(title, ''), ?),
		  source_url = COALESCE(NULLIF(?, ''), source_url),
		  description = COALESCE(NULLIF(description, ''), ?),
		  thumbnail_url = COALESCE(NULLIF(thumbnail_url, ''), ?),
		  media_type = CASE WHEN ? != '' THEN ? ELSE media_type END,
		  source_id = COALESCE(source_id, ?)
		WHERE id = ?
	`, li.Title, li.WebpageURL, li.Description, li.ThumbnailURL,
		NormalizeMediaType(li.MediaType), NormalizeMediaType(li.MediaType),
		nullInt64(li.SourceID), existingID)
	if err != nil {
		return out, err
	}
	if li.DurationSeconds > 0 {
		_ = s.SetDurationSecondsIfEmpty(existingID, int(li.DurationSeconds+0.5))
	}
	return out, nil
}

// insertListedVideo writes a new videos row.
func (s *Store) insertListedVideo(seriesID int64, src any, li ListedVideo, uploadVal any, status string, season, episode, thumb any) (sql.Result, error) {
	mt := NormalizeMediaType(li.MediaType)
	return s.DB.SQL.Exec(`
		INSERT INTO videos (
		  series_id, source_id, remote_id, title, upload_date,
		  source_url, status, season, episode, description, thumbnail_url, media_type, acquired_via
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, seriesID, src, li.RemoteID, li.Title, uploadVal, nullEmpty(li.WebpageURL),
		status, season, episode, li.Description, thumb, mt, AcquiredViaSource)
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// AddVideoHistory appends a timeline event. taskID is required (must reference tasks.id).
func (s *Store) AddVideoHistory(videoID int64, event, message string, detail map[string]any, taskID int64) error {
	if taskID <= 0 {
		return fmt.Errorf("%w: video history requires task_id", ErrInvalid)
	}
	raw := "{}"
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := s.DB.SQL.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, videoID, nowRFC3339(), event, message, raw, taskID)
	return err
}

// ListSources returns all sources for a series (no source-level monitored gate).
func (s *Store) ListSources(seriesID int64) ([]Source, error) {
	return s.listSources(seriesID)
}

// MonitoredSources is an alias for ListSources (legacy name kept for call sites).
func (s *Store) MonitoredSources(seriesID int64) ([]Source, error) {
	return s.listSources(seriesID)
}

// EntryFromYtDlp maps a yt-dlp list/resolve entry into ListedVideo.
func EntryFromYtDlp(e ytdlp.Entry, sourceID int64) ListedVideo {
	return ListedVideo{
		RemoteID:        e.ID,
		Title:           e.Title,
		WebpageURL:      e.WebpageURL,
		UploadDate:      e.UploadDate,
		Description:     e.Description,
		ThumbnailURL:    e.ThumbnailURL,
		MediaType:       e.MediaType,
		SourceID:        sourceID,
		DurationSeconds: e.Duration,
	}
}

// DownloadContext gathers paths and profile for a video download.
type DownloadContext struct {
	Video          Video
	SeriesTitle    string
	RootPath       string
	RootID         int64
	EpisodeFormat  string
	FormatSelector string
	URL            string
	Profile        QualityProfile
	DeliveryMode   string
}

// PrepareDownload loads series/root/profile for a video id.
func (s *Store) PrepareDownload(videoID int64) (*DownloadContext, error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return nil, err
	}
	var title, rootPath, episodeFormat, deliveryMode string
	var profileID, rootID int64
	err = s.DB.SQL.QueryRow(`
		SELECT s.title, r.id, r.path, r.episode_format, s.quality_profile_id, s.delivery_mode
		FROM series s
		JOIN root_folders r ON r.id = s.root_id
		WHERE s.id = ?
	`, v.SeriesID).Scan(&title, &rootID, &rootPath, &episodeFormat, &profileID, &deliveryMode)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	prof, err := s.GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	url := ""
	if v.SourceURL.Valid {
		url = strings.TrimSpace(v.SourceURL.String)
	}
	url = DownloadURL(url, v.RemoteID)
	_ = s.EnsureSeriesDirCapped(rootPath, title)
	return &DownloadContext{
		Video:          *v,
		SeriesTitle:    title,
		RootPath:       rootPath,
		RootID:         rootID,
		EpisodeFormat:  settings.NormalizeEpisodeFormat(episodeFormat),
		FormatSelector: prof.FormatSelector,
		URL:            url,
		Profile:        *prof,
		DeliveryMode:   NormalizeDeliveryMode(deliveryMode),
	}, nil
}

// HasVideoFile returns true if a video file row points at an existing path.
func (s *Store) HasVideoFile(videoID int64) (string, bool, error) {
	rows, err := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ? AND kind = 'video'`, videoID)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return "", false, err
		}
		if fileExists(p) {
			return p, true, nil
		}
	}
	return "", false, rows.Err()
}

// HasPackAnchor returns the on-disk path of packed media (kind=video).
func (s *Store) HasPackAnchor(videoID int64) (string, bool, error) {
	return s.HasVideoFile(videoID)
}

// CompleteDownload records installed files and marks video downloaded.
func (s *Store) CompleteDownload(videoID int64, mediaPath, nfoPath, infoPath, thumbPath string, subPaths []string, meta MediaCompleteMeta, taskID int64) error {
	return s.completeMedia(videoID, mediaPath, nfoPath, infoPath, thumbPath, subPaths, meta, taskID, "packed", "Packed to library")
}

// CompleteImport records files installed from the import folder or bound in place from the library.
func (s *Store) CompleteImport(videoID int64, mediaPath, nfoPath, infoPath, thumbPath string, subPaths []string, meta MediaCompleteMeta, taskID int64) error {
	msg := "Imported from import folder"
	if meta.InPlace || (meta.ImportSrc != "" && s.ImportInPlace(meta.ImportSrc)) {
		msg = "Bound library file in place"
	}
	return s.completeMedia(videoID, mediaPath, nfoPath, infoPath, thumbPath, subPaths, meta, taskID, "imported", msg)
}

func (s *Store) completeMedia(videoID int64, mediaPath, nfoPath, infoPath, thumbPath string, subPaths []string, meta MediaCompleteMeta, taskID int64, event, message string) error {
	infoMeta := MediaMetaFromInfoJSON(infoPath)
	dur := meta.DurationSeconds
	if dur <= 0 {
		dur = infoMeta.DurationSeconds
	}
	width := meta.Width
	if width <= 0 {
		width = infoMeta.Width
	}
	height := meta.Height
	if height <= 0 {
		height = infoMeta.Height
	}
	fps := meta.FPS
	if fps <= 0 {
		fps = infoMeta.FPS
	}
	acquired := nowRFC3339()
	uploadFromInfo := UploadDateFromInfoJSON(infoPath)

	// Disk work outside the write tx so `_txlock=immediate` is not held across Remove/Stat.
	oldRows, err := s.DB.SQL.Query(`SELECT path FROM files WHERE video_id = ?`, videoID)
	if err != nil {
		return err
	}
	var oldPaths []string
	for oldRows.Next() {
		var p string
		if oldRows.Scan(&p) == nil && p != "" {
			oldPaths = append(oldPaths, p)
		}
	}
	_ = oldRows.Close()
	if err := oldRows.Err(); err != nil {
		return err
	}
	for _, p := range oldPaths {
		_ = os.Remove(p)
	}

	type fileRow struct {
		kind string
		path string
		size any
	}
	var files []fileRow
	for kind, path := range map[string]string{"video": mediaPath, "nfo": nfoPath, "json": infoPath, "thumb": thumbPath} {
		if path == "" || !fileExists(path) {
			continue
		}
		var size any
		if kind == "video" {
			if fi, err := os.Stat(path); err == nil {
				size = fi.Size()
			}
		}
		files = append(files, fileRow{kind: kind, path: path, size: size})
	}
	for _, p := range subPaths {
		if p == "" || !fileExists(p) {
			continue
		}
		files = append(files, fileRow{kind: "sub", path: p})
	}

	tx, err := s.DB.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM files WHERE video_id = ? AND kind IN ('video','nfo','json','thumb','sub','sponsorblock')`, videoID); err != nil {
		return err
	}
	for _, f := range files {
		if f.kind == "sub" {
			if _, err := tx.Exec(`
				INSERT INTO files (video_id, path, kind, acquired_at) VALUES (?, ?, 'sub', ?)
			`, videoID, f.path, acquired); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO files (video_id, path, kind, acquired_at, size_bytes) VALUES (?, ?, ?, ?, ?)
		`, videoID, f.path, f.kind, acquired, f.size); err != nil {
			return err
		}
	}
	var curDate sql.NullString
	if err := tx.QueryRow(`SELECT upload_date FROM videos WHERE id = ?`, videoID).Scan(&curDate); err != nil {
		return err
	}
	needDate := !curDate.Valid || strings.TrimSpace(curDate.String) == ""

	var remuxVal any
	if strings.TrimSpace(meta.DownloadRemuxContainer) != "" {
		remuxVal = strings.TrimSpace(meta.DownloadRemuxContainer)
	}
	var formatVal any
	if strings.TrimSpace(meta.DownloadFormatSelector) != "" {
		formatVal = strings.TrimSpace(meta.DownloadFormatSelector)
	}
	var toolVal any
	if strings.TrimSpace(meta.Tool) != "" {
		toolVal = strings.TrimSpace(meta.Tool)
	}
	var importSrcVal any
	if strings.TrimSpace(meta.ImportSrc) != "" {
		importSrcVal = strings.TrimSpace(meta.ImportSrc)
	}
	acquiredVia := NormalizeAcquiredVia(meta.AcquiredVia)
	if strings.TrimSpace(meta.ImportSrc) != "" && acquiredVia == AcquiredViaSource {
		acquiredVia = AcquiredViaImport
	}
	var durVal, widthVal, heightVal, fpsVal any
	if dur > 0 {
		durVal = dur
	}
	if width > 0 {
		widthVal = width
	}
	if height > 0 {
		heightVal = height
	}
	if fps > 0 {
		fpsVal = fps
	}
	mediaType := NormalizeMediaType(infoMeta.MediaType)

	var descVal any
	if infoMeta.Description != "" {
		descVal = infoMeta.Description
	}

	setCols := `
		UPDATE videos SET status = 'downloaded',
		  acquired_at = ?,
		  sidecars_acquired_at = ?,
		  tool = COALESCE(?, tool),
		  acquired_via = ?,
		  download_format_selector = COALESCE(?, download_format_selector),
		  download_remux_container = ?,
		  import_src = COALESCE(?, import_src),
		  duration_seconds = COALESCE(?, duration_seconds),
		  width = COALESCE(?, width),
		  height = COALESCE(?, height),
		  fps = COALESCE(?, fps),
		  media_type = CASE WHEN (media_type IS NULL OR media_type = '') AND ? != '' THEN ? ELSE media_type END,
		  description = COALESCE(NULLIF(description, ''), ?)`
	args := []any{acquired, acquired, toolVal, acquiredVia, formatVal, remuxVal, importSrcVal, durVal, widthVal, heightVal, fpsVal, mediaType, mediaType, descVal}

	if uploadFromInfo != "" && needDate {
		var seriesID int64
		if err := tx.QueryRow(`SELECT series_id FROM videos WHERE id = ?`, videoID).Scan(&seriesID); err != nil {
			return err
		}
		setCols += `, upload_date = ? WHERE id = ?`
		args = append(args, uploadFromInfo, videoID)
		if _, err := tx.Exec(setCols, args...); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"path": mediaPath, "thumb": thumbPath != "", "json": infoPath != ""})
		if taskID <= 0 {
			return fmt.Errorf("%w: task_id required for video history", ErrInvalid)
		}
		if _, err := tx.Exec(`
			INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
			VALUES (?, ?, ?, ?, ?, ?)
		`, videoID, acquired, event, message, string(detail), taskID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		changed, rerr := s.ReindexSeriesUTCDay(seriesID, UploadCalendarDate(uploadFromInfo))
		if rerr != nil {
			return rerr
		}
		_ = s.repackEpisodeNumberChanges(changed, taskID)
		return nil
	}
	setCols += ` WHERE id = ?`
	args = append(args, videoID)
	if _, err := tx.Exec(setCols, args...); err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]any{"path": mediaPath, "thumb": thumbPath != "", "json": infoPath != ""})
	if taskID <= 0 {
		return fmt.Errorf("%w: task_id required for video history", ErrInvalid)
	}
	if _, err := tx.Exec(`
		INSERT INTO video_history (video_id, created_at, event, message, detail, task_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, videoID, acquired, event, message, string(detail), taskID); err != nil {
		return err
	}
	return tx.Commit()
}


// UploadDateFromInfoJSON reads upload_date from a packed info.json (RFC3339 or YYYYMMDD).
func UploadDateFromInfoJSON(path string) string {
	if path == "" || !fileExists(path) {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var data map[string]any
	if json.Unmarshal(b, &data) != nil {
		return ""
	}
	switch v := data["upload_date"].(type) {
	case string:
		return sidecarUploadTime(v)
	}
	return ""
}

// SoftFillUploadDateFromInfoJSON sets videos.upload_date from info.json when empty.
// Does not reindex or rename; callers assign season/episode before pack.
// Returns the effective upload_date (existing or newly filled), or "" if still unset.
func (s *Store) SoftFillUploadDateFromInfoJSON(videoID int64, infoPath string) (string, error) {
	var cur sql.NullString
	if err := s.DB.SQL.QueryRow(`SELECT upload_date FROM videos WHERE id = ?`, videoID).Scan(&cur); err != nil {
		return "", err
	}
	if cur.Valid {
		if u := NormalizeUploadTime(cur.String); u != "" {
			return u, nil
		}
	}
	fromInfo := UploadDateFromInfoJSON(infoPath)
	if fromInfo == "" {
		return "", nil
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET upload_date = ? WHERE id = ? AND (upload_date IS NULL OR trim(upload_date) = '')`, fromInfo, videoID); err != nil {
		return "", err
	}
	return fromInfo, nil
}

// SoftFillArchiveMetaFromInfoJSON fills placeholder title (== remote_id) and empty
// description / upload_date / thumbnail_url from archive download info.json.
func (s *Store) SoftFillArchiveMetaFromInfoJSON(videoID int64, infoPath string) error {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	meta := MediaMetaFromInfoJSON(infoPath)
	title := strings.TrimSpace(v.Title)
	newTitle := ""
	if TitleIsRemoteIDPlaceholder(title, v.RemoteID) || title == "" {
		if t := strings.TrimSpace(meta.Title); t != "" && !TitleIsRemoteIDPlaceholder(t, v.RemoteID) {
			newTitle = t
		}
	}
	desc := ""
	if strings.TrimSpace(v.Description) == "" && strings.TrimSpace(meta.Description) != "" {
		desc = meta.Description
	}
	thumb := ""
	if (!v.ThumbnailURL.Valid || strings.TrimSpace(v.ThumbnailURL.String) == "") && meta.ThumbnailURL != "" {
		thumb = meta.ThumbnailURL
	}
	var uploadVal any
	if (!v.UploadDate.Valid || strings.TrimSpace(v.UploadDate.String) == "") && meta.UploadDate != "" {
		if u := NormalizeUploadTime(meta.UploadDate); u != "" {
			uploadVal = u
		}
	}
	if newTitle == "" && desc == "" && thumb == "" && uploadVal == nil {
		return nil
	}
	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = CASE WHEN ? != '' THEN ? ELSE title END,
		  description = COALESCE(NULLIF(description, ''), ?),
		  thumbnail_url = COALESCE(NULLIF(thumbnail_url, ''), NULLIF(?, '')),
		  upload_date = COALESCE(upload_date, ?)
		WHERE id = ?
	`, newTitle, newTitle, desc, thumb, uploadVal, videoID)
	return err
}

// DurationSecondsFromInfoJSON reads yt-dlp-style duration (seconds) from a packed info.json.
func DurationSecondsFromInfoJSON(path string) int {
	return MediaMetaFromInfoJSON(path).DurationSeconds
}
