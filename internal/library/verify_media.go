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
	"github.com/xyxxyxxy/Creatorr/internal/library/integrity"
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
	var v int
	err = s.DB.SQL.QueryRow(`
		SELECT qp.verify_media
		FROM videos v
		JOIN series s ON s.id = v.series_id
		JOIN quality_profiles qp ON qp.id = s.quality_profile_id
		WHERE v.id = ?
	`, videoID).Scan(&v)
	if err == sql.ErrNoRows {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return v != 0, nil
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
		  AND event IN ('file_externally_changed', 'sidecar_externally_changed', 'integrity_check_failed')
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
// When File integrity is off, returns a skipped report and nil error.
func (s *Store) RunIntegrityCheckVideo(ctx context.Context, videoID int64, progress func(msg string, pct *float64), opts IntegrityCheckOpts) (*IntegrityCheckReport, error) {
	report := &IntegrityCheckReport{}
	path, ok, err := s.HasVideoFile(videoID)
	if err != nil {
		return report, err
	}
	if !ok || path == "" {
		return report, context.Canceled // superseded
	}
	profileOn, err := s.seriesProfileVerifyMedia(videoID)
	if err != nil {
		return report, err
	}
	if !profileOn {
		report.setCheck(IntegrityCheckNullDecode, IntegrityResultSkipped, "File integrity off")
		report.setCheck(IntegrityCheckMediaChecksum, IntegrityResultSkipped, "File integrity off")
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultSkipped, "File integrity off")
		report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "File integrity off")
		report.Outcome = IntegrityOutcomeSkipped
		return report, nil
	}
	if err := VerifyDownloadedMedia(ctx, path, progress); err != nil {
		report.setCheck(IntegrityCheckNullDecode, IntegrityResultFailed, integrityFailDetail(err))
		report.setCheck(IntegrityCheckMediaChecksum, IntegrityResultSkipped, "not run")
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultSkipped, "not run")
		report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "not run")
		report.finalizeOutcome()
		return report, err
	}
	report.setCheck(IntegrityCheckNullDecode, IntegrityResultOK, "")
	if progress != nil {
		progress("Checking file integrity…", nil)
	}
	mediaFiles, err := s.ListVideoMediaFiles(videoID)
	if err != nil {
		return report, err
	}
	mediaResult := IntegrityResultSkipped
	mediaDetail := ""
	if len(mediaFiles) == 0 {
		mediaDetail = "no media files"
	}
	for _, f := range mediaFiles {
		result, mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path)
		if herr != nil {
			report.setCheck(IntegrityCheckMediaChecksum, IntegrityResultFailed, integrityFailDetail(herr))
			report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultSkipped, "not run")
			report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "not run")
			report.finalizeOutcome()
			return report, herr
		}
		if result == IntegrityResultFailed {
			report.setCheck(IntegrityCheckMediaChecksum, IntegrityResultFailed, mismatch)
			report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultSkipped, "not run")
			report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "not run")
			report.finalizeOutcome()
			return report, apperrors.WithDetail(
				apperrors.New(apperrors.CodeIntegrityCheckFailed, "integrity check failed"),
				mismatch,
			)
		}
		switch {
		case mediaResult == IntegrityResultSkipped:
			mediaResult = result
		case result == IntegrityResultFilled:
			mediaResult = IntegrityResultFilled
		case mediaResult != IntegrityResultFilled && result == IntegrityResultOK:
			mediaResult = IntegrityResultOK
		}
	}
	if mediaResult == IntegrityResultSkipped && mediaDetail == "" {
		mediaDetail = "no media files"
	}
	report.setCheck(IntegrityCheckMediaChecksum, mediaResult, mediaDetail)

	sidecars, err := s.listRegisteredSidecars(videoID)
	if err != nil {
		return report, err
	}
	sideChecked := 0
	sideFilled := 0
	sideFailed := 0
	var sideDetail strings.Builder
	for _, f := range sidecars {
		if f.Kind == "nfo" {
			continue
		}
		if _, serr := os.Stat(f.Path); serr != nil {
			continue
		}
		sideChecked++
		result, mismatch, herr := s.ensureOrCompareFileHash(f.ID, f.Path)
		if herr != nil {
			report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultFailed, integrityFailDetail(herr))
			report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "not run")
			report.finalizeOutcome()
			return report, herr
		}
		if result == IntegrityResultFailed {
			sideFailed++
			stored, _, _ := s.FileContentHash(f.ID)
			disk, _ := integrity.SHA256File(f.Path)
			_ = s.AddVideoHistory(videoID, "sidecar_externally_changed", "Sidecar integrity check failed", map[string]any{
				"reason":   "integrity_check",
				"kind":     f.Kind,
				"path":     f.Path,
				"file_id":  f.ID,
				"detail":   mismatch,
				"old_hash": stored,
				"new_hash": disk,
			}, opts.TaskID)
			if sideDetail.Len() > 0 {
				sideDetail.WriteString("; ")
			}
			sideDetail.WriteString(f.Kind + ": " + mismatch)
			continue
		}
		if result == IntegrityResultFilled {
			sideFilled++
		}
	}
	switch {
	case sideChecked == 0:
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultSkipped, "no sidecars")
	case sideFailed > 0:
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultPartial, sideDetail.String())
	case sideFilled > 0:
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultFilled, "")
	default:
		report.setCheck(IntegrityCheckSidecarChecksum, IntegrityResultOK, "")
	}

	match, nfoPath, nerr := s.nfoDiskMatchesVideo(videoID)
	if nerr != nil {
		report.setCheck(IntegrityCheckNFO, IntegrityResultFailed, integrityFailDetail(nerr))
		report.finalizeOutcome()
		return report, nerr
	}
	if nfoPath == "" {
		report.setCheck(IntegrityCheckNFO, IntegrityResultSkipped, "no NFO")
	} else if !match {
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
		report.setCheck(IntegrityCheckNFO, IntegrityResultFailed, "NFO does not match expected metadata")
	} else {
		report.setCheck(IntegrityCheckNFO, IntegrityResultOK, "")
	}
	report.finalizeOutcome()
	return report, nil
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
func (s *Store) CancelMediaVerifyForVideo(videoID int64, reason string) error {
	if s.Queue == nil || videoID <= 0 {
		return nil
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
		if _, err := s.Queue.CancelWithReason(id, reason); err != nil {
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
	_ = s.CancelMediaVerifyForVideo(videoID, queue.CancelReasonSupersededImport)
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
	_ = s.CancelMediaVerifyForVideo(videoID, queue.CancelReasonSupersededPack)
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
// report is optional; when set, its checks/outcome are stored in history detail.
func (s *Store) MarkVerifyFailed(videoID, taskID int64, message string, report *IntegrityCheckReport) error {
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
	detail := map[string]any{
		"code": apperrors.CodeIntegrityCheckFailed,
	}
	if report != nil {
		if report.Outcome == "" {
			report.finalizeOutcome()
		}
		for k, v := range report.DetailMap() {
			detail[k] = v
		}
		for _, c := range report.Checks {
			if c.Result == IntegrityResultFailed {
				detail["check"] = c.Key
				if d := strings.TrimSpace(c.Detail); d != "" {
					detail["detail"] = d
				}
				break
			}
		}
	}
	return s.AddVideoHistory(videoID, VideoHistVerifyFailed, msg, detail, taskID)
}

// MarkVerified appends integrity_checked history and restores status to downloaded
// (including videos previously marked integrity_check_failed).
// report is optional; when outcome is partial, message notes sidecar/NFO issues.
func (s *Store) MarkVerified(videoID, taskID int64, report *IntegrityCheckReport) error {
	if _, err := s.DB.SQL.Exec(`
		UPDATE videos SET status = 'downloaded'
		WHERE id = ? AND status IN ('downloaded', 'integrity_check_failed')
	`, videoID); err != nil {
		return err
	}
	msg := "Integrity check ok"
	var detail map[string]any
	if report != nil {
		if report.Outcome == "" {
			report.finalizeOutcome()
		}
		if report.Outcome == IntegrityOutcomePartial {
			msg = "Integrity check ok (sidecar/NFO issues)"
		}
		detail = report.DetailMap()
	}
	return s.AddVideoHistory(videoID, VideoHistIntegrityChecked, msg, detail, taskID)
}
