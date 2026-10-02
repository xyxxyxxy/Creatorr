package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/queue"
)

// EnqueueResetMetadataFromInfoScoped queues a resumable reset of video catalog
// metadata from packed info.json. Empty scope = whole library. seriesIDs and
// videoIDs are mutually exclusive.
func (s *Store) EnqueueResetMetadataFromInfoScoped(seriesIDs, videoIDs []int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue unavailable", ErrInvalid)
	}
	seriesIDs = uniqInt64(seriesIDs)
	videoIDs = uniqInt64(videoIDs)
	if len(seriesIDs) > 0 && len(videoIDs) > 0 {
		return 0, fmt.Errorf("%w: series_ids and video_ids are mutually exclusive", ErrInvalid)
	}
	payload := map[string]any{
		"video_cursor": 0,
		"reset":        0,
		"skipped":      0,
		"failed":       0,
	}
	msg := "Reset metadata from info.json"
	if len(seriesIDs) > 0 {
		payload["series_ids"] = seriesIDs
		msg = "Reset series metadata from info.json"
	} else if len(videoIDs) > 0 {
		payload["video_ids"] = videoIDs
		msg = "Reset selected metadata from info.json"
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:  queue.OriginManual,
		Kind:    queue.KindResetMetadataFromInfo,
		Domain:  queue.SystemDomain,
		Payload: payload,
		Message: msg,
	})
}

type resetMetaInfoPayload struct {
	VideoCursor int64   `json:"video_cursor"`
	Reset       int     `json:"reset"`
	Skipped     int     `json:"skipped"`
	Failed      int     `json:"failed"`
	SeriesIDs   []int64 `json:"series_ids"`
	VideoIDs    []int64 `json:"video_ids"`
}

func (p resetMetaInfoPayload) persistMap(reset, skipped, failed int) map[string]any {
	m := map[string]any{
		"video_cursor": p.VideoCursor,
		"reset":        reset,
		"skipped":      skipped,
		"failed":       failed,
	}
	if len(p.SeriesIDs) > 0 {
		m["series_ids"] = p.SeriesIDs
	}
	if len(p.VideoIDs) > 0 {
		m["video_ids"] = p.VideoIDs
	}
	return m
}

// ResetFromInfoOutcome reports side effects of ResetVideoMetadataFromInfoJSON.
type ResetFromInfoOutcome struct {
	Skipped      bool // no packed info.json (or path missing on disk)
	RenameQueued bool
	RenameTaskID int64
}

// ResetMetadataFromInfoPass force-resets scoped videos from packed info.json.
func (s *Store) ResetMetadataFromInfoPass(ctx context.Context, task *queue.Task, progress func(msg string, pct *float64)) (reset, skipped, failed int, err error) {
	var p resetMetaInfoPayload
	_ = json.Unmarshal([]byte(task.Payload), &p)
	reset, skipped, failed = p.Reset, p.Skipped, p.Failed

	persist := func() error {
		return s.Queue.UpdatePayload(task.ID, p.persistMap(reset, skipped, failed))
	}

	q := `SELECT DISTINCT f.video_id FROM files f`
	args := []any{}
	where := ` WHERE f.kind = 'video' AND f.video_id > ?`
	args = append(args, p.VideoCursor)
	if len(p.VideoIDs) > 0 {
		where += ` AND f.video_id IN (` + sqlIntPlaceholders(len(p.VideoIDs)) + `)`
		for _, id := range p.VideoIDs {
			args = append(args, id)
		}
	} else if len(p.SeriesIDs) > 0 {
		q += ` JOIN videos v ON v.id = f.video_id`
		where += ` AND v.series_id IN (` + sqlIntPlaceholders(len(p.SeriesIDs)) + `)`
		for _, id := range p.SeriesIDs {
			args = append(args, id)
		}
	}
	q += where + ` ORDER BY f.video_id`
	rows, qerr := s.DB.SQL.Query(q, args...)
	if qerr != nil {
		return reset, skipped, failed, qerr
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return reset, skipped, failed, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()

	for i, id := range ids {
		select {
		case <-ctx.Done():
			_ = persist()
			return reset, skipped, failed, ctx.Err()
		default:
		}
		out, rerr := s.ResetVideoMetadataFromInfoJSON(id, task.ID)
		if rerr != nil {
			failed++
		} else if out.Skipped {
			skipped++
		} else {
			reset++
		}
		p.VideoCursor = id
		_ = persist()
		if progress != nil {
			pct := float64(i+1) / float64(len(ids)+1)
			progress(fmt.Sprintf("Reset from info.json: reset=%d skipped=%d failed=%d", reset, skipped, failed), &pct)
		}
	}
	return reset, skipped, failed, nil
}

// ResetVideoMetadataFromInfoJSON force-overwrites catalog fields from packed info.json.
// Wipes Metadata fields not present in info.json; never touches notes or info.json itself.
// taskID is passed to RewriteVideoNFO history (0 = no history row).
func (s *Store) ResetVideoMetadataFromInfoJSON(videoID int64, taskID int64) (ResetFromInfoOutcome, error) {
	var out ResetFromInfoOutcome
	infoPath, err := s.packedInfoJSONPath(videoID)
	if err != nil {
		return out, err
	}
	if infoPath == "" || !fileExists(infoPath) {
		out.Skipped = true
		return out, nil
	}

	v, err := s.GetVideo(videoID)
	if err != nil {
		return out, err
	}
	meta := MediaMetaFromInfoJSON(infoPath)
	genres := CategoriesFromInfoJSON(infoPath)
	tags := TagsFromInfoJSON(infoPath)

	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = v.Title
	}
	prevTitle := v.Title
	prevRole := NormalizePackRole(v.PackRole)
	packRole := PackRoleRegular

	oldDay := ""
	if v.UploadDate.Valid {
		oldDay = UploadCalendarDate(v.UploadDate.String)
	}
	var uploadVal any
	newDay := ""
	normalized := strings.TrimSpace(meta.UploadDate)
	if normalized != "" {
		normalized = NormalizeUploadTime(normalized)
		if normalized == "" {
			normalized = strings.TrimSpace(meta.UploadDate)
		}
		newDay = UploadCalendarDate(normalized)
		uploadVal = normalized
	}
	dayChanged := newDay != oldDay
	roleChanged := prevRole != packRole
	titleChanged := title != prevTitle

	if titleChanged || roleChanged || dayChanged {
		if open, err := s.SeriesHasOpenMove(v.SeriesID); err != nil {
			return out, err
		} else if open {
			return out, ErrSeriesMoveBusy
		}
	}

	var thumb any
	if meta.ThumbnailURL != "" {
		thumb = meta.ThumbnailURL
	}

	_, err = s.DB.SQL.Exec(`
		UPDATE videos SET
		  title = ?, description = ?,
		  sorttitle = '', originaltitle = '', studio = '',
		  genres = ?, tags = ?, uniqueid_type = '', uniqueid_value = '',
		  actors = ?, tagline = '', country = '', mpaa = '',
		  upload_date = ?, special_feature = ?,
		  thumbnail_url = ?,
		  duration_seconds = CASE WHEN ? > 0 THEN ? ELSE duration_seconds END,
		  width = CASE WHEN ? > 0 THEN ? ELSE width END,
		  height = CASE WHEN ? > 0 THEN ? ELSE height END,
		  fps = CASE WHEN ? > 0 THEN ? ELSE fps END,
		  media_type = CASE WHEN ? != '' THEN ? ELSE media_type END
		WHERE id = ?
	`, title, strings.TrimSpace(meta.Description),
		encodeStringSlice(genres), encodeStringSlice(tags),
		encodeActors(nil),
		uploadVal, PackRoleDBValue(packRole),
		thumb,
		meta.DurationSeconds, meta.DurationSeconds,
		meta.Width, meta.Width,
		meta.Height, meta.Height,
		meta.FPS, meta.FPS,
		meta.MediaType, meta.MediaType,
		videoID)
	if err != nil {
		return out, err
	}

	var renameIDs []int64
	if roleChanged {
		if _, err := s.ReindexPackRoleBucket(v.SeriesID, prevRole); err != nil {
			return out, err
		}
		if _, err := s.ReindexPackRoleBucket(v.SeriesID, packRole); err != nil {
			return out, err
		}
		renameIDs = append(renameIDs, videoID)
	}
	if dayChanged {
		if newDay == "" {
			if _, err := s.DB.SQL.Exec(`UPDATE videos SET season = NULL, episode = NULL WHERE id = ?`, videoID); err != nil {
				return out, err
			}
			renameIDs = append(renameIDs, videoID)
		} else {
			years := map[int]bool{}
			if y := SeasonYearFromCalendarDay(newDay); y > 0 {
				years[y] = true
			}
			if oldDay != "" {
				if y := SeasonYearFromCalendarDay(oldDay); y > 0 {
					years[y] = true
				}
			}
			for y := range years {
				c, rerr := s.ReindexSeriesUTCYear(v.SeriesID, y)
				if rerr != nil {
					return out, rerr
				}
				renameIDs = append(renameIDs, c...)
			}
		}
	} else if roleChanged && (prevRole == PackRoleRegular || packRole == PackRoleRegular) {
		upload := ""
		if v.UploadDate.Valid {
			upload = v.UploadDate.String
		}
		if normalized != "" {
			upload = normalized
		}
		if y := SeasonYearFromUpload(upload); y > 0 {
			c, rerr := s.ReindexSeriesUTCYear(v.SeriesID, y)
			if rerr != nil {
				return out, rerr
			}
			renameIDs = append(renameIDs, c...)
		}
	}
	if titleChanged {
		renameIDs = append(renameIDs, videoID)
	}
	renameIDs = uniqInt64(renameIDs)
	if len(renameIDs) > 0 {
		tid, qerr := s.EnqueueRenameEpisodesVideos(renameIDs)
		if qerr != nil {
			return out, qerr
		}
		out.RenameQueued = true
		out.RenameTaskID = tid
	}

	if _, err := s.RewriteVideoNFO(videoID, taskID); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) packedInfoJSONPath(videoID int64) (string, error) {
	var path string
	err := s.DB.SQL.QueryRow(`
		SELECT path FROM files WHERE video_id = ? AND kind = 'json' LIMIT 1
	`, videoID).Scan(&path)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(path), nil
}

// ResetMetadataFromInfoMessage formats the finish message.
func ResetMetadataFromInfoMessage(reset, skipped, failed int) string {
	msg := fmt.Sprintf("Reset metadata from info.json: reset %d, skipped %d", reset, skipped)
	if failed > 0 {
		msg += fmt.Sprintf(", %d failed", failed)
	}
	return msg
}

// RecordResetMetadataFromInfoActivity stores the outcome message and detail on the task.
func (s *Store) RecordResetMetadataFromInfoActivity(taskID int64, reset, skipped, failed int) {
	msg := ResetMetadataFromInfoMessage(reset, skipped, failed)
	if s.Queue != nil {
		p := 1.0
		_ = s.Queue.UpdateProgress(taskID, msg, &p)
	}
	detail, _ := json.Marshal(map[string]any{
		"reset": reset, "skipped": skipped, "failed": failed,
	})
	if s.Queue != nil {
		_ = s.Queue.SetDetail(taskID, string(detail))
	}
}
