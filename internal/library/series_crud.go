package library

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// CreateSeriesParams creates a series and optional first source.
type CreateSeriesParams struct {
	Title              string
	SourceURL          string
	RootID             int64
	QualityProfileID   int64
	Monitored          bool
	DeliveryMode       string
	FullScanLimit      int // first source full-scan playlist cap; 0 = unlimited
	ScanCron           string // feed default weekly when SourceURL set and empty
	IndexAsIgnored     bool
	TitleRegexpInclude string
	TitleRegexpExclude string
	SourceLabel        string
}

func (s *Store) GetSeries(id int64, withVideos bool) (*Series, error) {
	var ser Series
	var genresJSON, tagsJSON, actorsJSON string
	var mon int
	err := s.DB.SQL.QueryRow(`
		SELECT s.id, s.title, s.root_id, s.quality_profile_id, s.monitored, s.delivery_mode, s.added_at,
		       COALESCE(s.notes,''),
		       s.plot, s.sorttitle, s.originaltitle, s.studio, s.genres, s.tags,
		       s.uniqueid_type, s.uniqueid_value, s.actors, s.tagline, s.country, s.mpaa, s.premiered,
		       r.name, q.name,
		       COALESCE(vc.video_count, 0),
		       COALESCE(vc.downloaded_count, 0),
		       COALESCE(vc.wanted_count, 0),
		       COALESCE(vc.pending_count, 0),
		       COALESCE(sc.source_count, 0)
		`+seriesListFromJoins+`
		WHERE s.id = ?
	`, id).Scan(
		&ser.ID, &ser.Title, &ser.RootID, &ser.QualityProfileID, &mon, &ser.DeliveryMode, &ser.AddedAt,
		&ser.Notes,
		&ser.Meta.Plot, &ser.Meta.SortTitle, &ser.Meta.OriginalTitle, &ser.Meta.Studio, &genresJSON, &tagsJSON,
		&ser.Meta.UniqueIDType, &ser.Meta.UniqueIDValue, &actorsJSON, &ser.Meta.Tagline, &ser.Meta.Country, &ser.Meta.MPAA, &ser.Meta.Premiered,
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
		INSERT INTO series (title, root_id, quality_profile_id, monitored, delivery_mode, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, title, p.RootID, p.QualityProfileID, mon, mode, nowRFC3339())
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
	want := sanitizeName(title, SeriesDirMaxRunes)
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
		if sanitizeName(t, SeriesDirMaxRunes) == want {
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
}

// UpdateSeriesOutcome reports side effects of UpdateSeries.
type UpdateSeriesOutcome struct {
	Series *Series
	// MoveQueued is true when a series_move task owns the title/root change.
	MoveQueued bool
	MoveTaskID int64
}

// UpdateSeries updates quality profile and delivery mode now. Title or root changes
// enqueue series_move (blocked while the series has blocking tasks); the worker updates
// title/root in SQLite, then moves the folder and applies naming.
func (s *Store) UpdateSeries(id int64, p UpdateSeriesParams) (*Series, error) {
	out, err := s.UpdateSeriesDetailed(id, p)
	if err != nil {
		return nil, err
	}
	return out.Series, nil
}

// UpdateSeriesDetailed is UpdateSeries with move-queue outcome.
func (s *Store) UpdateSeriesDetailed(id int64, p UpdateSeriesParams) (UpdateSeriesOutcome, error) {
	var out UpdateSeriesOutcome
	cur, err := s.GetSeries(id, false)
	if err != nil {
		return out, err
	}
	title := cur.Title
	rootID := cur.RootID
	qpID := cur.QualityProfileID
	mode := cur.DeliveryMode
	if p.Title != nil {
		title = strings.TrimSpace(*p.Title)
		if title == "" {
			return out, fmt.Errorf("%w: title", ErrInvalid)
		}
	}
	if p.RootID != nil {
		if _, err := s.GetRoot(*p.RootID); err != nil {
			return out, fmt.Errorf("%w: root_id", ErrInvalid)
		}
		rootID = *p.RootID
	}
	if p.QualityProfileID != nil {
		if _, err := s.GetProfile(*p.QualityProfileID); err != nil {
			return out, fmt.Errorf("%w: quality_profile_id", ErrInvalid)
		}
		qpID = *p.QualityProfileID
	}
	if p.DeliveryMode != nil {
		mode = NormalizeDeliveryMode(*p.DeliveryMode)
	}

	// Enqueue first so busy/conflict rejections leave every field unchanged.
	if title != cur.Title || rootID != cur.RootID {
		tid, err := s.EnqueueSeriesMove(id, title, rootID)
		if err != nil {
			return out, err
		}
		out.MoveQueued = true
		out.MoveTaskID = tid
	}
	if _, err := s.DB.SQL.Exec(`
		UPDATE series SET quality_profile_id = ?, delivery_mode = ? WHERE id = ?
	`, qpID, mode, id); err != nil {
		return out, err
	}
	out.Series, err = s.GetSeries(id, false)
	return out, err
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
			_, _ = s.Queue.CancelDownloadsForVideo(vid, queue.CancelReasonSeriesDeleted)
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
