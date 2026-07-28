package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	apperrors "github.com/xyxxyxxy/Creatorr/internal/errors"
	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

// RemuxContainer is the fixed on-disk container after a video download (Creatorr ffmpeg).
const RemuxContainer = "mkv"

// RemuxAudioContainer is the fixed on-disk container after an audio-only download (Creatorr ffmpeg).
const RemuxAudioContainer = "mka"

// RemuxIfNeeded remuxes media into MKV via ffmpeg when the file extension is not
// already .mkv. Returns the path to use, whether a remux ran, and any error (CodeRemuxFailed).
// Maps only video (excluding attached pics) and audio; drops data streams such as
// QuickTime tmcd that Matroska cannot store (otherwise -c copy fails).
func RemuxIfNeeded(ctx context.Context, mediaPath string) (path string, remuxed bool, err error) {
	if mediaPath == "" {
		return mediaPath, false, nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if ext == RemuxContainer {
		return mediaPath, false, nil
	}
	dir := filepath.Dir(mediaPath)
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	out := filepath.Join(dir, base+"."+RemuxContainer)
	args := []string{
		"-y", "-i", mediaPath,
		"-map", "0:V", "-map", "0:a?",
		"-dn",
		"-c", "copy", out,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	outb, err := cmd.CombinedOutput()
	if err != nil {
		return "", false, apperrors.WithDetail(
			apperrors.New(apperrors.CodeRemuxFailed, "ffmpeg remux failed"),
			fmt.Sprintf("%v: %s", err, truncateBytesTail(outb, 600)),
		)
	}
	_ = os.Remove(mediaPath)
	return out, true, nil
}

// RemuxAudioIfNeeded remuxes audio-only media into MKA via ffmpeg when the file
// extension is not already .mka. Drops video/data streams (audio-only container).
// Returns the path to use, whether a remux ran, and any error (CodeRemuxFailed).
func RemuxAudioIfNeeded(ctx context.Context, mediaPath string) (path string, remuxed bool, err error) {
	if mediaPath == "" {
		return mediaPath, false, nil
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if ext == RemuxAudioContainer {
		return mediaPath, false, nil
	}
	dir := filepath.Dir(mediaPath)
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	out := filepath.Join(dir, base+"."+RemuxAudioContainer)
	args := []string{
		"-y", "-i", mediaPath,
		"-map", "0:a",
		"-dn",
		"-c", "copy", out,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	outb, err := cmd.CombinedOutput()
	if err != nil {
		return "", false, apperrors.WithDetail(
			apperrors.New(apperrors.CodeRemuxFailed, "ffmpeg audio remux failed"),
			fmt.Sprintf("%v: %s", err, truncateBytesTail(outb, 600)),
		)
	}
	_ = os.Remove(mediaPath)
	return out, true, nil
}

func truncateBytes(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// truncateBytesTail keeps the end of ffmpeg logs (banner is at the start; the
// failure line is usually last).
func truncateBytesTail(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
