package sponsorblock

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

// EmbedChapters writes ffmetadata chapters into an MKV (copy codecs).
func EmbedChapters(ctx context.Context, mediaPath string, chapters []Chapter) error {
	if len(chapters) == 0 {
		return nil
	}
	dir := filepath.Dir(mediaPath)
	metaPath := filepath.Join(dir, "sb-chapters.ffmeta")
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")
	for _, ch := range chapters {
		startMS := int64(ch.Start * 1000)
		endMS := int64(ch.End * 1000)
		if endMS <= startMS {
			endMS = startMS + 1
		}
		fmt.Fprintf(&b, "[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n",
			startMS, endMS, escapeMeta(ch.Title))
	}
	if err := os.WriteFile(metaPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	defer os.Remove(metaPath)

	ext := filepath.Ext(mediaPath)
	tmp := filepath.Join(dir, fmt.Sprintf("sb-embed-%d%s", time.Now().UnixNano(), ext))
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", mediaPath,
		"-i", metaPath,
		"-map_metadata", "1",
		"-map", "0",
		"-c", "copy",
		tmp,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("embed chapters: %w: %s", err, truncate(string(out), 300))
	}
	if err := os.Rename(tmp, mediaPath); err != nil {
		_ = os.Remove(mediaPath)
		if err2 := os.Rename(tmp, mediaPath); err2 != nil {
			return err2
		}
	}
	return nil
}

func escapeMeta(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `=`, `\=`)
	s = strings.ReplaceAll(s, `;`, `\;`)
	s = strings.ReplaceAll(s, `#`, `\#`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
