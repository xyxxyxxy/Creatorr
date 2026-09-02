package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

const (
	VideoHistVerified     = "verified"
	VideoHistVerifyFailed = "verify_failed"
)

// ShouldVerifyMedia decides automatic post-pack enqueue for media_verify.
// Import confirm verify ignores this gate (always enqueue when checked).
// Mature-only: when maturity_redownload_hours > 0, skip young first packs;
// run on maturity re-download and when maturity will never run (already past due at acquire).
func ShouldVerifyMedia(p QualityProfile, maturityPack bool, uploadDate string, acquiredAt time.Time) bool {
	if !p.VerifyMedia {
		return false
	}
	if p.MaturityRedownloadHours <= 0 {
		return true
	}
	if maturityPack {
		return true
	}
	upload, ok := ParseUploadTime(uploadDate)
	if !ok {
		return true
	}
	due := upload.UTC().Add(time.Duration(p.MaturityRedownloadHours) * time.Hour)
	if acquiredAt.IsZero() {
		acquiredAt = time.Now().UTC()
	}
	if acquiredAt.Before(due) {
		return false
	}
	return true
}

// VerifyDownloadedMedia null-decodes path with ffmpeg -xerror. Reports progress
// "Verifying…" with fraction from -progress when duration is known.
func VerifyDownloadedMedia(ctx context.Context, path string, progress func(msg string, pct *float64)) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return apperrors.New(apperrors.CodeMediaVerifyFailed, "media path empty")
	}
	if progress == nil {
		progress = func(string, *float64) {}
	}

	dur := 0.0
	if p, err := sponsorblock.ProbeMedia(ctx, path); err == nil && p.Duration > 0 {
		dur = p.Duration
	}
	progress("Verifying…", nil)

	args := sponsorblock.WithFFmpegProgressArgs([]string{
		"-xerror", "-i", path, "-f", "null", "-",
	})
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeMediaVerifyFailed, "media verify failed"), err.Error())
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeMediaVerifyFailed, "media verify failed"), err.Error())
	}
	if dur > 0 {
		_ = sponsorblock.ScanFFmpegProgressPipe(stdout, dur, func(frac float64) {
			f := frac
			progress("Verifying…", &f)
		})
	} else {
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		} else if len(detail) > 400 {
			detail = detail[:400]
		}
		return apperrors.WithDetail(apperrors.New(apperrors.CodeMediaVerifyFailed, "media verify failed"), detail)
	}
	done := 1.0
	progress("Verified", &done)
	return nil
}

// CancelMediaVerifyForVideo cancels pending/running media_verify for one video.
func (s *Store) CancelMediaVerifyForVideo(videoID int64, message string) error {
	if s.Queue == nil || videoID <= 0 {
		return nil
	}
	if strings.TrimSpace(message) == "" {
		message = "Cancelled"
	}
	rows, err := s.DB.SQL.Query(`
		SELECT id FROM tasks
		WHERE kind = ? AND video_id = ? AND status IN (?, ?)
	`, queue.KindMediaVerify, videoID, queue.StatusPending, queue.StatusRunning)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.Queue.CancelWithMessage(id, message); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueMediaVerify queues system-lane null-decode verify for packed library media.
func (s *Store) EnqueueMediaVerify(videoID int64) (int64, error) {
	if s.Queue == nil {
		return 0, fmt.Errorf("%w: queue not configured", ErrInvalid)
	}
	if videoID <= 0 {
		return 0, fmt.Errorf("%w: video_id required", ErrInvalid)
	}
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	path, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return 0, err
	}
	if !ok || path == "" {
		return 0, fmt.Errorf("%w: video has no media file to verify", ErrInvalid)
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Kind:     queue.KindMediaVerify,
		Domain:   queue.SystemDomain,
		SeriesID: v.SeriesID,
		VideoID:  videoID,
		Priority: queue.PriorityMediaVerify,
		Message:  "Verify media",
		Payload:  map[string]any{"video_id": videoID, "media_path": path},
	})
}

// MaybeEnqueueMediaVerifyAfterPack cancels prior verify tasks, then enqueues when the profile gate says so.
func (s *Store) MaybeEnqueueMediaVerifyAfterPack(videoID int64, maturityPack bool) (int64, error) {
	_ = s.CancelMediaVerifyForVideo(videoID, "Superseded by new pack")
	v, err := s.GetVideo(videoID)
	if err != nil {
		return 0, err
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return 0, err
	}
	prof, err := s.GetProfile(ser.QualityProfileID)
	if err != nil {
		return 0, err
	}
	upload := ""
	if v.UploadDate.Valid {
		upload = v.UploadDate.String
	}
	acquired := time.Now().UTC()
	if v.AcquiredAt.Valid && strings.TrimSpace(v.AcquiredAt.String) != "" {
		if t, err := time.Parse(time.RFC3339Nano, v.AcquiredAt.String); err == nil {
			acquired = t
		} else if t, err := time.Parse(time.RFC3339, v.AcquiredAt.String); err == nil {
			acquired = t
		}
	}
	if !ShouldVerifyMedia(*prof, maturityPack, upload, acquired) {
		return 0, nil
	}
	id, err := s.EnqueueMediaVerify(videoID)
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// MarkVerifyFailed sets status verify_failed and history.
func (s *Store) MarkVerifyFailed(videoID, taskID int64, message string) error {
	if _, err := s.GetVideo(videoID); err != nil {
		return err
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'verify_failed' WHERE id = ?`, videoID); err != nil {
		return err
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "Media verify failed"
	}
	return s.AddVideoHistory(videoID, VideoHistVerifyFailed, msg, map[string]any{
		"code": apperrors.CodeMediaVerifyFailed,
	}, taskID)
}

// MarkVerified appends verified history; status stays downloaded.
func (s *Store) MarkVerified(videoID, taskID int64) error {
	return s.AddVideoHistory(videoID, VideoHistVerified, "Media verified", nil, taskID)
}
