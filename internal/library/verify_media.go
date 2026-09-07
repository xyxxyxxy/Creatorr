package library

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/queue"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

const (
	VideoHistIntegrityChecked = "integrity_checked"
	VideoHistVerifyFailed     = "integrity_check_failed"
	// VideoHistVerified is the legacy history event; dual-display with integrity_checked.
	VideoHistVerified = "verified"
)

// ShouldVerifyMedia decides automatic post-pack enqueue for integrity_check_initial.
// Import confirm verify still requires File integrity on, but ignores the mature-only timing gate.
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

// seriesProfileVerifyMedia returns whether the video's series profile has File integrity on.
func (s *Store) seriesProfileVerifyMedia(videoID int64) (on bool, err error) {
	v, err := s.GetVideo(videoID)
	if err != nil {
		return false, err
	}
	ser, err := s.GetSeries(v.SeriesID, false)
	if err != nil {
		return false, err
	}
	prof, err := s.GetProfile(ser.QualityProfileID)
	if err != nil {
		return false, err
	}
	return prof.VerifyMedia, nil
}

// SeriesProfileVerifyMedia is the exported form of seriesProfileVerifyMedia (UI).
func (s *Store) SeriesProfileVerifyMedia(videoID int64) (on bool, err error) {
	return s.seriesProfileVerifyMedia(videoID)
}

// NFODiskMatchesVideo is the exported form of nfoDiskMatchesVideo (UI).
func (s *Store) NFODiskMatchesVideo(videoID int64) (match bool, path string, err error) {
	return s.nfoDiskMatchesVideo(videoID)
}

// LastFileIntegrityIssue returns the newest size/hash/NFO mismatch message for fileID
// and optional task id. Does not compute hashes. kind filters video-level events
// (no file_id in detail) to media rows only.
func (s *Store) LastFileIntegrityIssue(videoID, fileID int64, kind string) (message string, taskID int64) {
	if videoID <= 0 || fileID <= 0 {
		return "", 0
	}
	rows, err := s.DB.SQL.Query(`
		SELECT message, detail, task_id FROM video_history
		WHERE video_id = ?
		  AND event IN ('file_externally_changed', 'sidecar_externally_changed', 'integrity_check_failed', 'verify_failed')
		ORDER BY id DESC
		LIMIT 40
	`, videoID)
	if err != nil {
		return "", 0
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var msg, detail string
		var tid sql.NullInt64
		if err := rows.Scan(&msg, &detail, &tid); err != nil {
			return "", 0
		}
		if !fileIssueDetailMatches(detail, fileID, kind) {
			continue
		}
		if tid.Valid {
			taskID = tid.Int64
		}
		return strings.TrimSpace(msg), taskID
	}
	return "", 0
}

func fileIssueDetailMatches(detail string, fileID int64, kind string) bool {
	detail = strings.TrimSpace(detail)
	if detail == "" || isEmptyJSONObject(detail) {
		return kind == "video"
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(detail), &raw); err != nil {
		return false
	}
	v, ok := raw["file_id"]
	if !ok || v == nil {
		return kind == "video"
	}
	switch n := v.(type) {
	case float64:
		return int64(n) == fileID
	case int64:
		return n == fileID
	default:
		return false
	}
}

func isEmptyJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	return s == "{}" || s == "null"
}

// IntegrityCheckOpts controls RunIntegrityCheckVideo.
type IntegrityCheckOpts struct {
	TaskID int64
}

// RunIntegrityCheckVideo null-decodes media and fills/compares hashes for video + non-NFO
// sidecars and structural/value-compares episode NFO when File integrity is on.
// No-op (nil) when File integrity is off.
func (s *Store) RunIntegrityCheckVideo(ctx context.Context, videoID int64, progress func(msg string, pct *float64), opts IntegrityCheckOpts) error {
	path, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return err
	}
	if !ok || path == "" {
		return context.Canceled // superseded
	}
	profileOn, err := s.seriesProfileVerifyMedia(videoID)
	if err != nil {
		return err
	}
	if !profileOn {
		return nil
	}
	if err := VerifyDownloadedMedia(ctx, path, progress); err != nil {
		return err
	}
	if progress != nil {
		progress("Checking file integrity…", nil)
	}
	mediaFiles, err := s.ListVideoMediaFiles(videoID)
	if err != nil {
		return err
	}
	for _, f := range mediaFiles {
		if mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path); herr != nil {
			return herr
		} else if mismatch != "" {
			return apperrors.WithDetail(
				apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"),
				mismatch,
			)
		}
	}
	sidecars, err := s.listRegisteredSidecars(videoID)
	if err != nil {
		return err
	}
	for _, f := range sidecars {
		if f.Kind == "nfo" {
			continue
		}
		if _, serr := os.Stat(f.Path); serr != nil {
			continue
		}
		if mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path); herr != nil {
			return herr
		} else if mismatch != "" {
			stored, _, _ := s.FileContentHash(f.ID)
			disk, _ := sha256File(f.Path)
			_ = s.AddVideoHistory(videoID, "sidecar_externally_changed", "Sidecar integrity check failed", map[string]any{
				"reason":   "integrity_check",
				"kind":     f.Kind,
				"path":     f.Path,
				"file_id":  f.ID,
				"detail":   mismatch,
				"old_hash": stored,
				"new_hash": disk,
			}, opts.TaskID)
			continue
		}
	}
	match, nfoPath, nerr := s.nfoDiskMatchesVideo(videoID)
	if nerr != nil {
		return nerr
	}
	if nfoPath != "" && !match {
		var fileID int64
		for _, f := range sidecars {
			if f.Kind == "nfo" {
				fileID = f.ID
				break
			}
		}
		_ = s.AddVideoHistory(videoID, "sidecar_externally_changed", "NFO does not match expected metadata", map[string]any{
			"reason":  "integrity_check",
			"kind":    "nfo",
			"path":    nfoPath,
			"file_id": fileID,
		}, opts.TaskID)
	}
	return nil
}

// VerifyDownloadedMedia null-decodes path with ffmpeg -xerror. Reports progress
// "Verifying…" with fraction from -progress when duration is known.
func VerifyDownloadedMedia(ctx context.Context, path string, progress func(msg string, pct *float64)) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return apperrors.New(apperrors.CodeIntegrityCheckFailed, "media path empty")
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
		return apperrors.WithDetail(apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"), err.Error())
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return apperrors.WithDetail(apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"), err.Error())
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
		return apperrors.WithDetail(apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"), detail)
	}
	done := 1.0
	progress("Verified", &done)
	return nil
}

// listRegisteredSidecars returns files rows for non-video kinds.
func (s *Store) listRegisteredSidecars(videoID int64) ([]VideoFile, error) {
	rows, err := s.DB.SQL.Query(`
		SELECT id, path, kind, acquired_at, size_bytes, content_hash
		FROM files
		WHERE video_id = ? AND kind != 'video'
		ORDER BY kind, path
	`, videoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []VideoFile
	for rows.Next() {
		var f VideoFile
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &f.AcquiredAt, &f.SizeBytes, &f.ContentHash); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CancelMediaVerifyForVideo cancels pending/running integrity_check_initial for one video.
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
	`, queue.KindIntegrityCheckInitial, videoID, queue.StatusPending, queue.StatusRunning)
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

// EnqueueMediaVerify queues system-lane initial integrity check for packed library media.
// parentTaskID links to the spawning pack/import task (origin=task). Pass 0 only from tests.
func (s *Store) EnqueueMediaVerify(videoID, parentTaskID int64) (int64, error) {
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
	origin := queue.OriginManual
	if parentTaskID > 0 {
		origin = queue.OriginTask
	}
	return s.Queue.Enqueue(queue.EnqueueParams{
		Origin:       origin,
		ParentTaskID: parentTaskID,
		Kind:         queue.KindIntegrityCheckInitial,
		Domain:       queue.SystemDomain,
		SeriesID:     v.SeriesID,
		VideoID:      videoID,
		Message:      "Initial integrity check",
		Payload:      map[string]any{"video_id": videoID, "media_path": path},
	})
}

// MaybeEnqueueMediaVerifyForImport cancels prior verify tasks, then enqueues when the
// series quality profile has File integrity on. Ignores the mature-only timing gate
// (import verify is an explicit operator opt-in). Returns 0 when File integrity is off.
func (s *Store) MaybeEnqueueMediaVerifyForImport(videoID, parentTaskID int64) (int64, error) {
	_ = s.CancelMediaVerifyForVideo(videoID, "Superseded by import")
	on, err := s.seriesProfileVerifyMedia(videoID)
	if err != nil {
		return 0, err
	}
	if !on {
		return 0, nil
	}
	id, err := s.EnqueueMediaVerify(videoID, parentTaskID)
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// MaybeEnqueueMediaVerifyAfterPack cancels prior verify tasks, then enqueues when the profile gate says so.
func (s *Store) MaybeEnqueueMediaVerifyAfterPack(videoID int64, maturityPack bool, parentTaskID int64) (int64, error) {
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
	id, err := s.EnqueueMediaVerify(videoID, parentTaskID)
	if err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// MarkVerifyFailed sets status integrity_check_failed and history.
func (s *Store) MarkVerifyFailed(videoID, taskID int64, message string) error {
	if _, err := s.GetVideo(videoID); err != nil {
		return err
	}
	if _, err := s.DB.SQL.Exec(`UPDATE videos SET status = 'integrity_check_failed' WHERE id = ?`, videoID); err != nil {
		return err
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "Integrity check failed"
	}
	return s.AddVideoHistory(videoID, VideoHistVerifyFailed, msg, map[string]any{
		"code": apperrors.CodeIntegrityCheckFailed,
	}, taskID)
}

// MarkVerified appends integrity_checked history and restores status to downloaded
// (including videos previously marked integrity_check_failed).
func (s *Store) MarkVerified(videoID, taskID int64) error {
	if _, err := s.DB.SQL.Exec(`
		UPDATE videos SET status = 'downloaded'
		WHERE id = ? AND status IN ('downloaded', 'integrity_check_failed')
	`, videoID); err != nil {
		return err
	}
	return s.AddVideoHistory(videoID, VideoHistIntegrityChecked, "Integrity check ok", nil, taskID)
}
