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

// RemuxContainer is the fixed on-disk container after download (Creatorr ffmpeg).
const RemuxContainer = "mkv"

// RemuxIfNeeded remuxes media into MKV via ffmpeg when the file extension is not
// already .mkv. Returns the path to use, whether a remux ran, and any error (CodeRemuxFailed).
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
	args := []string{"-y", "-i", mediaPath, "-c", "copy", out}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	outb, err := cmd.CombinedOutput()
	if err != nil {
		return "", false, apperrors.WithDetail(
			apperrors.New(apperrors.CodeRemuxFailed, "ffmpeg remux failed"),
			fmt.Sprintf("%v: %s", err, truncateBytes(outb, 400)),
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
