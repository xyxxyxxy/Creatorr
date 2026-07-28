package sponsorblock

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

// EncodeProgress reports re-encode progress in [0,1] (keeps + filter-stitch when used).
// Pass a non-nil *float64 to report; pass nil to clear (spinner). Nil callback is safe.
type EncodeProgress func(fraction *float64)

func reportProg(on EncodeProgress, frac float64) {
	if on == nil {
		return
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	f := frac
	on(&f)
}

func clearProg(on EncodeProgress) {
	if on == nil {
		return
	}
	on(nil)
}

// runFFmpegProgress runs re-encode ffmpeg with -progress pipe:1 and reports fraction of durationSec.
// Applies fixed Linux nice (no-op elsewhere). durationSec <= 0 skips mid progress.
func runFFmpegProgress(ctx context.Context, args []string, durationSec float64, onSeg func(frac float64)) error {
	args = withFFmpegProgressArgs(args)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	applyReencodeNice(cmd)
	if durationSec > 0 && onSeg != nil {
		_ = scanFFmpegProgress(stdout, durationSec, onSeg)
	} else {
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, truncate(stderr.String(), 400))
	}
	if onSeg != nil {
		onSeg(1)
	}
	return nil
}

// runFFmpegReencode runs re-encode ffmpeg (info cards) with fixed Linux nice; no progress pipe.
func runFFmpegReencode(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	out, err := startWaitReencode(cmd)
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncate(string(out), 400))
	}
	return nil
}

func startWaitReencode(cmd *exec.Cmd) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return buf.Bytes(), err
	}
	applyReencodeNice(cmd)
	err := cmd.Wait()
	return buf.Bytes(), err
}

// WithFFmpegProgressArgs normalizes ffmpeg args to use -progress pipe:1 (and quiet stats).
func WithFFmpegProgressArgs(args []string) []string {
	return withFFmpegProgressArgs(args)
}

// ScanFFmpegProgressPipe maps ffmpeg -progress lines into fraction of durationSec.
func ScanFFmpegProgressPipe(r io.Reader, durationSec float64, onSeg func(frac float64)) error {
	return scanFFmpegProgress(r, durationSec, onSeg)
}

func withFFmpegProgressArgs(args []string) []string {
	out := make([]string, 0, len(args)+4)
	out = append(out, "-y", "-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:1")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-y", "-nostats", "-hide_banner":
			continue
		case "-loglevel", "-progress":
			if i+1 < len(args) {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func scanFFmpegProgress(r io.Reader, durationSec float64, onSeg func(frac float64)) error {
	if durationSec <= 0 || onSeg == nil {
		_, err := io.Copy(io.Discard, r)
		return err
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var last float64 = -1
	for sc.Scan() {
		line := sc.Text()
		sec, ok := parseFFmpegProgressLine(line)
		if !ok {
			continue
		}
		frac := sec / durationSec
		if frac > 1 {
			frac = 1
		}
		if frac < 0 {
			frac = 0
		}
		// Throttle tiny updates.
		if last >= 0 && frac-last < 0.01 && frac < 1 {
			continue
		}
		last = frac
		onSeg(frac)
	}
	return sc.Err()
}

func parseFFmpegProgressLine(line string) (sec float64, ok bool) {
	line = strings.TrimSpace(line)
	// Prefer wall-clock form when present (unambiguous).
	if strings.HasPrefix(line, "out_time=") {
		v := strings.TrimPrefix(line, "out_time=")
		return parseFFmpegClock(v)
	}
	if strings.HasPrefix(line, "out_time_us=") {
		v := strings.TrimPrefix(line, "out_time_us=")
		us, err := strconv.ParseInt(v, 10, 64)
		if err != nil || us < 0 {
			return 0, false
		}
		return float64(us) / 1e6, true
	}
	if strings.HasPrefix(line, "out_time_ms=") {
		// ffmpeg names this *_ms but emits microseconds (same scale as out_time_us).
		v := strings.TrimPrefix(line, "out_time_ms=")
		us, err := strconv.ParseInt(v, 10, 64)
		if err != nil || us < 0 {
			return 0, false
		}
		return float64(us) / 1e6, true
	}
	return 0, false
}

func parseFFmpegClock(v string) (sec float64, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" || v == "N/A" {
		return 0, false
	}
	parts := strings.Split(v, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, e1 := strconv.ParseFloat(parts[0], 64)
	m, e2 := strconv.ParseFloat(parts[1], 64)
	s, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, false
	}
	return h*3600 + m*60 + s, true
}
