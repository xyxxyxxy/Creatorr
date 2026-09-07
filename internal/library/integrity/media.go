package integrity

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
	"github.com/xyxxyxxy/Creatorr/internal/sponsorblock"
)

const (
	VideoHistIntegrityChecked = "integrity_checked"
	VideoHistVerifyFailed     = "integrity_check_failed"
	// VideoHistVerified is the legacy history event; dual-display with integrity_checked.
	VideoHistVerified = "verified"
)

// ShouldVerifyMedia decides automatic post-pack enqueue for integrity_check_initial.
// Mature-only: when maturityHours > 0, skip young first packs; run on maturity
// re-download and when maturity will never run (already past due at acquire).
// parseUpload returns UTC time for uploadDate (ok=false when undated/unparseable).
func ShouldVerifyMedia(verifyOn bool, maturityHours int, maturityPack bool, uploadDate string, acquiredAt time.Time, parseUpload func(string) (time.Time, bool)) bool {
	if !verifyOn {
		return false
	}
	if maturityHours <= 0 {
		return true
	}
	if maturityPack {
		return true
	}
	upload, ok := parseUpload(uploadDate)
	if !ok {
		return true
	}
	due := upload.UTC().Add(time.Duration(maturityHours) * time.Hour)
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
