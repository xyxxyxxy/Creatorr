package library

import (
	"context"
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
	// SyncDisk runs folder move + scoped Apply inline (bulk worker). When false,
	// title/root changes enqueue rename_episodes with series_id after DB update.
	SyncDisk bool
}

// UpdateSeriesOutcome reports side effects of UpdateSeries.
type UpdateSeriesOutcome struct {
	Series       *Series
	RenameQueued bool
	RenameTaskID int64
}

// UpdateSeries updates title, root folder, quality profile, and/or delivery mode.
// Title or root changes move the series folder on disk (blocked while media tasks busy).
// On move failure the DB field is reverted so paths stay correct.
func (s *Store) UpdateSeries(id int64, p UpdateSeriesParams) (*Series, error) {
	out, err := s.UpdateSeriesDetailed(id, p)
	if err != nil {
		return nil, err
	}
	return out.Series, nil
}

// UpdateSeriesDetailed is UpdateSeries with rename-queue outcome.
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
	titleChanged := title != cur.Title
	rootChanged := rootID != cur.RootID
	if titleChanged || rootChanged {
		busy, err := s.SeriesHasBusyMediaTasks(id)
		if err != nil {
			return out, err
		}
		if busy {
			return out, ErrSeriesBusy
		}
		if taken, err := s.seriesFolderTaken(rootID, title, id); err != nil {
			return out, err
		} else if taken {
			return out, fmt.Errorf("%w: a series with this title already exists under the same root folder", ErrConflict)
		}
	}

	if _, err := s.DB.SQL.Exec(`
		UPDATE series SET title = ?, root_id = ?, quality_profile_id = ?, delivery_mode = ? WHERE id = ?
	`, title, rootID, qpID, mode, id); err != nil {
		return out, err
	}

	if titleChanged || rootChanged {
		updated, err := s.GetSeries(id, false)
		if err != nil {
			return out, err
		}
		if p.SyncDisk {
			if err := s.MoveSeriesFolder(updated, cur.Title, cur.RootID); err != nil {
				_, _ = s.DB.SQL.Exec(`
					UPDATE series SET title = ?, root_id = ? WHERE id = ?
				`, cur.Title, cur.RootID, id)
				return out, fmt.Errorf("folder move failed (reverted title/root): %w", err)
			}
			if err := s.WriteSeriesNFODisk(id); err != nil {
				return out, fmt.Errorf("write series NFO after folder move: %w", err)
			}
			tid, qerr := s.Queue.InsertRunning(queue.EnqueueParams{
				Origin:   queue.OriginManual,
				Kind:     queue.KindRegenerateNFO,
				Domain:   queue.SystemDomain,
				SeriesID: id,
				Message:  "Rewrite episode NFOs after series folder move",
				Payload:  map[string]any{"series_id": id, "scope": "series_move"},
			})
			if qerr != nil {
				if _, _, _, err := s.ApplySeriesEpisodeNaming(context.Background(), id, 0); err != nil {
					return out, fmt.Errorf("apply episode naming after folder move: %w", err)
				}
			} else {
				if _, _, err := s.RewriteSeriesEpisodeNFOs(id, tid); err != nil {
					_ = s.Queue.Finish(tid, queue.StatusFailed, "Episode NFO rewrite failed after folder move", "", err.Error())
					return out, fmt.Errorf("rewrite episode NFOs after folder move: %w", err)
				}
				if _, _, _, err := s.ApplySeriesEpisodeNaming(context.Background(), id, tid); err != nil {
					_ = s.Queue.Finish(tid, queue.StatusFailed, "Episode naming failed after folder move", "", err.Error())
					return out, fmt.Errorf("apply episode naming after folder move: %w", err)
				}
				if err := s.Queue.Finish(tid, queue.StatusDone, "Episode paths updated after folder move", "", ""); err != nil {
					return out, fmt.Errorf("finish folder-move task: %w", err)
				}
			}
			out.Series, err = s.GetSeries(id, false)
			return out, err
		}
		tid, qerr := s.EnqueueRenameEpisodesSeries(id, cur.Title, cur.RootID)
		if qerr != nil {
			_, _ = s.DB.SQL.Exec(`
				UPDATE series SET title = ?, root_id = ? WHERE id = ?
			`, cur.Title, cur.RootID, id)
			return out, qerr
		}
		out.RenameQueued = true
		out.RenameTaskID = tid
		out.Series = updated
		return out, nil
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
