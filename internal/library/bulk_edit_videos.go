package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// BulkEditVideosParams patches selected videos' catalog metadata.
// Nil field pointers mean unchanged. Metadata pointers (including empty string / empty slice) mean set.
type BulkEditVideosParams struct {
	VideoIDs []int64
	Studio   *string
	Country  *string
	MPAA     *string
	Genres   *[]string
	Tags     *[]string
	Actors   *[]SeriesActor
}

func (p BulkEditVideosParams) hasMetadata() bool {
	return p.Studio != nil || p.Country != nil || p.MPAA != nil ||
		p.Genres != nil || p.Tags != nil || p.Actors != nil
}

// BulkEditVideosBusy reports whether a bulk_edit_videos task is pending or running.
func (s *Store) BulkEditVideosBusy() (bool, error) {
	if s.Queue == nil {
		return false, nil
	}
	var n int
	err := s.DB.SQL.QueryRow(`
		SELECT COUNT(*) FROM tasks
		WHERE domain = ? AND kind = ? AND status IN (?, ?)
	`, queue.SystemDomain, queue.KindBulkEditVideos, queue.StatusPending, queue.StatusRunning).Scan(&n)
	return n > 0, err
}

// EnqueueBulkEditVideos queues a system-lane bulk video metadata edit.
func (s *Store) EnqueueBulkEditVideos(p BulkEditVideosParams) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	ids := uniqPositive(p.VideoIDs)
	if len(ids) == 0 {
		return 0, fmt.Errorf("%w: video_ids required", ErrInvalid)
	}
	if !p.hasMetadata() {
		return 0, fmt.Errorf("%w: at least one metadata field required", ErrInvalid)
	}
	payload := map[string]any{
		"video_ids": ids,
		"index":     0,
	}
	if p.Studio != nil {
		payload["set_studio"] = true
		payload["studio"] = *p.Studio
	}
	if p.Country != nil {
		payload["set_country"] = true
		payload["country"] = *p.Country
	}
	if p.MPAA != nil {
		payload["set_mpaa"] = true
		payload["mpaa"] = *p.MPAA
	}
	if p.Genres != nil {
		payload["set_genres"] = true
		payload["genres"] = *p.Genres
	}
	if p.Tags != nil {
		payload["set_tags"] = true
		payload["tags"] = *p.Tags
	}
	if p.Actors != nil {
		payload["set_actors"] = true
		payload["actors"] = *p.Actors
	}
	id, err := s.Queue.Enqueue(queue.EnqueueParams{
		Kind:    queue.KindBulkEditVideos,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: "Bulk edit video metadata",
	})
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, fmt.Errorf("%w: bulk edit already queued", ErrConflict)
		}
		return 0, err
	}
	return id, nil
}

// ListVideoIDsFiltered returns video ids for a series matching filter (list order).
func (s *Store) ListVideoIDsFiltered(seriesID int64, filter VideoListFilter) ([]int64, error) {
	var b strings.Builder
	b.WriteString(`SELECT id FROM videos WHERE series_id = ?`)
	args := []any{seriesID}
	appendVideoListFilterSQL(&b, &args, filter)
	b.WriteString(` ORDER BY ` + videoListOrderBy)
	rows, err := s.DB.SQL.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type bulkEditVideosPayload struct {
	VideoIDs  []int64       `json:"video_ids"`
	Index     int           `json:"index"`
	SetStudio bool          `json:"set_studio"`
	Studio    string        `json:"studio"`
	SetCountry bool         `json:"set_country"`
	Country   string        `json:"country"`
	SetMPAA   bool          `json:"set_mpaa"`
	MPAA      string        `json:"mpaa"`
	SetGenres bool          `json:"set_genres"`
	Genres    []string      `json:"genres"`
	SetTags   bool          `json:"set_tags"`
	Tags      []string      `json:"tags"`
	SetActors bool          `json:"set_actors"`
	Actors    []SeriesActor `json:"actors"`
}

// BulkEditVideosPass applies queued bulk video metadata; resumable via payload index.
func (s *Store) BulkEditVideosPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (updated, skipped, failed int, err error) {
	var p bulkEditVideosPayload
	if task.Payload != "" {
		if err := json.Unmarshal([]byte(task.Payload), &p); err != nil {
			return 0, 0, 0, fmt.Errorf("%w: payload: %v", ErrInvalid, err)
		}
	}
	ids := uniqPositive(p.VideoIDs)
	n := len(ids)
	if n == 0 {
		return 0, 0, 0, fmt.Errorf("%w: video_ids required", ErrInvalid)
	}
	if p.Index < 0 {
		p.Index = 0
	}
	for i := p.Index; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return updated, skipped, failed, err
		}
		vid := ids[i]
		if progress != nil {
			pct := float64(i) / float64(n) * 100
			progress(fmt.Sprintf("Updating %d/%d", i+1, n), &pct)
		}
		if err := s.applyBulkEditVideoOne(vid, p); err != nil {
			if errors.Is(err, ErrNotFound) {
				skipped++
			} else {
				failed++
			}
			_ = s.persistBulkEditVideosCursor(task.ID, i+1, p)
			continue
		}
		updated++
		_ = s.persistBulkEditVideosCursor(task.ID, i+1, p)
	}
	if progress != nil {
		pct := 100.0
		progress(BulkEditSeriesMessage(updated, skipped, failed), &pct)
	}
	return updated, skipped, failed, nil
}

func (s *Store) applyBulkEditVideoOne(videoID int64, p bulkEditVideosPayload) error {
	if !p.SetStudio && !p.SetCountry && !p.SetMPAA && !p.SetGenres && !p.SetTags && !p.SetActors {
		return nil
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return err
	}
	meta := SaveVideoMetadataParams{
		Title:         v.Title,
		Plot:          v.Description,
		SortTitle:     v.SortTitle,
		OriginalTitle: v.OriginalTitle,
		Studio:        v.Studio,
		Genres:        append([]string(nil), v.Genres...),
		Tags:          append([]string(nil), v.Tags...),
		UniqueIDType:  v.UniqueIDType,
		UniqueIDValue: v.UniqueIDValue,
		Actors:        append([]SeriesActor(nil), v.Actors...),
		Tagline:       v.Tagline,
		Country:       v.Country,
		MPAA:          v.MPAA,
	}
	if v.UploadDate.Valid {
		meta.UploadDate = v.UploadDate.String
	}
	if p.SetStudio {
		meta.Studio = strings.TrimSpace(p.Studio)
	}
	if p.SetCountry {
		meta.Country = strings.TrimSpace(p.Country)
	}
	if p.SetMPAA {
		meta.MPAA = strings.TrimSpace(p.MPAA)
	}
	if p.SetGenres {
		meta.Genres = ParseStringListFields(p.Genres)
	}
	if p.SetTags {
		meta.Tags = ParseStringListFields(p.Tags)
	}
	if p.SetActors {
		meta.Actors = normalizeActorsList(p.Actors)
	}
	_, err = s.SaveVideoMetadata(v.ID, meta)
	return err
}

func (s *Store) persistBulkEditVideosCursor(taskID int64, index int, p bulkEditVideosPayload) error {
	if s.Queue == nil {
		return nil
	}
	p.Index = index
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	return s.Queue.UpdatePayload(taskID, m)
}

// WantVideosBulk sets wanted on eligible videos. Skips wrong status / missing.
func (s *Store) WantVideosBulk(ids []int64) (updated, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if _, err := s.WantVideo(id); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
				skipped++
				continue
			}
			return updated, skipped, err
		}
		updated++
	}
	return updated, skipped, nil
}

// IgnoreVideosBulk marks eligible videos ignored. Skips downloaded/verify_failed / missing.
func (s *Store) IgnoreVideosBulk(ids []int64) (updated, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if _, err := s.IgnoreVideo(id); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "cannot ignore") {
				skipped++
				continue
			}
			return updated, skipped, err
		}
		updated++
	}
	return updated, skipped, nil
}

// EnqueueDownloadVideosBulk queues Queue-download for each id. Skips conflicts / invalid.
func (s *Store) EnqueueDownloadVideosBulk(ids []int64) (queued, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if _, err := s.EnqueueDownloadNow(id); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
				skipped++
				continue
			}
			return queued, skipped, err
		}
		queued++
	}
	return queued, skipped, nil
}

// EnqueueRefreshSidecarsVideosBulk queues refresh for videos with pack anchors.
func (s *Store) EnqueueRefreshSidecarsVideosBulk(ids []int64) (queued, skipped int, err error) {
	for _, id := range uniqPositive(ids) {
		if _, err := s.EnqueueRefreshSidecarsVideo(id); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) {
				skipped++
				continue
			}
			return queued, skipped, err
		}
		queued++
	}
	return queued, skipped, nil
}

// EnqueueBulkDeleteVideos queues delete_files for downloaded/missing videos among ids.
func (s *Store) EnqueueBulkDeleteVideos(ids []int64) (taskID int64, queued, skipped int, err error) {
	var eligible []int64
	for _, id := range uniqPositive(ids) {
		v, gerr := s.GetVideo(id)
		if gerr != nil {
			if errors.Is(gerr, ErrNotFound) {
				skipped++
				continue
			}
			return 0, 0, skipped, gerr
		}
		switch v.Status {
		case "downloaded", "missing":
			if ok, qerr := s.VideoQueuedForDelete(id); qerr != nil {
				return 0, 0, skipped, qerr
			} else if ok {
				skipped++
				continue
			}
			if s.Queue != nil {
				_, _ = s.Queue.CancelDownloadsForVideo(id, "Cancelled (video deleted)")
			}
			eligible = append(eligible, id)
		default:
			skipped++
		}
	}
	if len(eligible) == 0 {
		return 0, 0, skipped, fmt.Errorf("%w: no deletable videos selected", ErrInvalid)
	}
	taskID, err = s.EnqueueDeleteFiles(nil, eligible)
	if err != nil {
		return 0, 0, skipped, err
	}
	return taskID, len(eligible), skipped, nil
}

// CommonVideoMetadata reports unanimous catalog fields across video ids (ordered lists/actors).
func (s *Store) CommonVideoMetadata(ids []int64) (CommonSeriesMetadata, error) {
	ids = uniqPositive(ids)
	if len(ids) == 0 {
		return CommonSeriesMetadata{}, fmt.Errorf("%w: video_ids required", ErrInvalid)
	}
	var (
		studio, country, mpaa string
		genres, tags          []string
		actors                []SeriesActor
		studioSame            = true
		countrySame           = true
		mpaaSame              = true
		genresSame            = true
		tagsSame              = true
		actorsSame            = true
		n                     int
	)
	for _, id := range ids {
		v, err := s.GetVideo(id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return CommonSeriesMetadata{}, err
		}
		n++
		if n == 1 {
			studio = v.Studio
			country = v.Country
			mpaa = v.MPAA
			genres = cloneStrings(v.Genres)
			tags = cloneStrings(v.Tags)
			actors = cloneActors(v.Actors)
			continue
		}
		if studioSame && studio != v.Studio {
			studioSame = false
		}
		if countrySame && country != v.Country {
			countrySame = false
		}
		if mpaaSame && mpaa != v.MPAA {
			mpaaSame = false
		}
		if genresSame && !stringSliceEqualOrdered(genres, v.Genres) {
			genresSame = false
		}
		if tagsSame && !stringSliceEqualOrdered(tags, v.Tags) {
			tagsSame = false
		}
		if actorsSame && !seriesActorsEqualOrdered(actors, v.Actors) {
			actorsSame = false
		}
	}
	if n == 0 {
		return CommonSeriesMetadata{}, fmt.Errorf("%w: no videos found", ErrNotFound)
	}
	out := CommonSeriesMetadata{
		Studio:  CommonMetaString{Same: studioSame},
		Country: CommonMetaString{Same: countrySame},
		MPAA:    CommonMetaString{Same: mpaaSame},
		Genres:  CommonMetaStrings{Same: genresSame},
		Tags:    CommonMetaStrings{Same: tagsSame},
		Actors:  CommonMetaActors{Same: actorsSame},
	}
	if studioSame {
		out.Studio.Value = studio
	}
	if countrySame {
		out.Country.Value = country
	}
	if mpaaSame {
		out.MPAA.Value = mpaa
	}
	if genresSame {
		out.Genres.Value = genres
		if out.Genres.Value == nil {
			out.Genres.Value = []string{}
		}
	}
	if tagsSame {
		out.Tags.Value = tags
		if out.Tags.Value == nil {
			out.Tags.Value = []string{}
		}
	}
	if actorsSame {
		out.Actors.Value = actors
		if out.Actors.Value == nil {
			out.Actors.Value = []SeriesActor{}
		}
	}
	return out, nil
}
