package sponsorblock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/exectrace"
)

// MediaProbe holds ffprobe fields used for matched info cards / re-encode.
type MediaProbe struct {
	Width         int
	Height        int
	FPS           float64
	PixFmt        string
	SampleRate    int
	Channels      int
	Duration      float64
	HasAudio      bool
	HasVideo      bool
	VideoCodec    string
	AudioCodec    string
	VideoBitrate  int64 // bits/sec; 0 if unknown
	AudioBitrate  int64
	FormatBitrate int64
	SizeBytes     int64
}

// ProbeMedia runs ffprobe on path.
func ProbeMedia(ctx context.Context, path string) (MediaProbe, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	exectrace.Record(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return MediaProbe{}, fmt.Errorf("ffprobe: %w", err)
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
			Size     string `json:"size"`
		} `json:"format"`
		Streams []struct {
			CodecType    string `json:"codec_type"`
			CodecName    string `json:"codec_name"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			PixFmt       string `json:"pix_fmt"`
			AvgFrameRate string `json:"avg_frame_rate"`
			RFrameRate   string `json:"r_frame_rate"`
			SampleRate   string `json:"sample_rate"`
			Channels     int    `json:"channels"`
			BitRate      string `json:"bit_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return MediaProbe{}, err
	}
	var p MediaProbe
	if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		p.Duration = d
	}
	if br, err := strconv.ParseInt(raw.Format.BitRate, 10, 64); err == nil {
		p.FormatBitrate = br
	}
	if sz, err := strconv.ParseInt(raw.Format.Size, 10, 64); err == nil {
		p.SizeBytes = sz
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if p.HasVideo {
				continue
			}
			p.HasVideo = true
			p.Width, p.Height = s.Width, s.Height
			p.PixFmt = s.PixFmt
			p.VideoCodec = s.CodecName
			p.FPS = parseFPS(s.AvgFrameRate)
			if p.FPS <= 0 {
				p.FPS = parseFPS(s.RFrameRate)
			}
			if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
				p.VideoBitrate = br
			}
		case "audio":
			if p.HasAudio {
				continue
			}
			p.HasAudio = true
			p.Channels = s.Channels
			p.AudioCodec = s.CodecName
			if sr, err := strconv.Atoi(s.SampleRate); err == nil {
				p.SampleRate = sr
			}
			if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
				p.AudioBitrate = br
			}
		}
	}
	if p.FPS <= 0 {
		p.FPS = 30
	}
	if p.PixFmt == "" {
		p.PixFmt = "yuv420p"
	}
	if p.SampleRate <= 0 {
		p.SampleRate = 48000
	}
	if p.Channels <= 0 {
		p.Channels = 2
	}
	// Estimate missing stream bitrates from container.
	if p.VideoBitrate <= 0 && p.FormatBitrate > 0 {
		p.VideoBitrate = p.FormatBitrate - p.AudioBitrate
		if p.VideoBitrate < minVideoBitrate && p.Duration > 0 && p.SizeBytes > 0 {
			p.VideoBitrate = int64(float64(p.SizeBytes*8)/p.Duration) - p.AudioBitrate
		}
	}
	if p.VideoBitrate <= 0 && p.Duration > 0 && p.SizeBytes > 0 {
		est := int64(float64(p.SizeBytes*8) / p.Duration)
		if p.HasAudio && p.AudioBitrate <= 0 {
			p.AudioBitrate = defaultAudioBR
		}
		p.VideoBitrate = est - p.AudioBitrate
	}
	return p, nil
}

func parseFPS(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0/0" {
		return 0
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 2 {
		a, e1 := strconv.ParseFloat(parts[0], 64)
		b, e2 := strconv.ParseFloat(parts[1], 64)
		if e1 == nil && e2 == nil && b != 0 {
			return a / b
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// CutCopy cuts media to keep ranges with -c copy (keyframe snap). No cards.
func CutCopy(ctx context.Context, inPath, outPath string, keeps [][2]float64) error {
	if len(keeps) == 0 {
		return fmt.Errorf("no keep ranges")
	}
	dir := filepath.Dir(outPath)
	var parts []string
	for i, k := range keeps {
		part := filepath.Join(dir, fmt.Sprintf("sb-keep-%03d%s", i, filepath.Ext(inPath)))
		dur := k[1] - k[0]
		if dur <= 0 {
			continue
		}
		args := []string{
			"-y", "-hide_banner", "-loglevel", "error",
			"-ss", fmt.Sprintf("%.3f", k[0]),
			"-i", inPath,
			"-t", fmt.Sprintf("%.3f", dur),
			"-c", "copy",
			"-avoid_negative_ts", "make_zero",
			part,
		}
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		exectrace.Record(ctx, "ffmpeg", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg cut part %d: %w: %s", i, err, truncate(string(out), 300))
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return fmt.Errorf("no keep parts produced")
	}
	if len(parts) == 1 {
		if err := os.Rename(parts[0], outPath); err != nil {
			data, rerr := os.ReadFile(parts[0])
			if rerr != nil {
				return err
			}
			if werr := os.WriteFile(outPath, data, 0o644); werr != nil {
				return werr
			}
			_ = os.Remove(parts[0])
		}
		return nil
	}
	if err := concatCopy(ctx, parts, outPath, dir); err != nil {
		return err
	}
	for _, p := range parts {
		_ = os.Remove(p)
	}
	return nil
}

// concatCopy stitches same-codec pieces with stream copy.
func concatCopy(ctx context.Context, piecePaths []string, outPath, dir string) error {
	var b strings.Builder
	for _, p := range piecePaths {
		fmt.Fprintf(&b, "file '%s'\n", escapeConcatPath(p))
	}
	listPath := filepath.Join(dir, "sb-concat-copy.txt")
	if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	defer os.Remove(listPath)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", outPath,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	exectrace.Record(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg concat: %w: %s", err, truncate(string(out), 300))
	}
	return nil
}

// concatFilter stitches pieces with filter_complex using EncodePlan (timestamp-clean continuous stream).
// durationSec is expected output length for -progress; onSeg is nil-safe.
func concatFilter(ctx context.Context, piecePaths []string, outPath string, plan EncodePlan, durationSec float64, onSeg func(frac float64)) error {
	n := len(piecePaths)
	if n == 0 {
		return fmt.Errorf("no pieces to concat")
	}
	args := []string{}
	for _, p := range piecePaths {
		args = append(args, "-i", p)
	}
	layout := "stereo"
	if plan.Channels == 1 {
		layout = "mono"
	}
	var fc strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc,
			"[%d:v]fps=%g,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v%d];",
			i, plan.FPS, plan.Width, plan.Height, plan.Width, plan.Height, i,
		)
		if plan.HasAudio {
			fmt.Fprintf(&fc,
				"[%d:a]aformat=sample_rates=%d:channel_layouts=%s,asetpts=PTS-STARTPTS[a%d];",
				i, plan.SampleRate, layout, i,
			)
		}
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc, "[v%d]", i)
		if plan.HasAudio {
			fmt.Fprintf(&fc, "[a%d]", i)
		}
	}
	if plan.HasAudio {
		fmt.Fprintf(&fc, "concat=n=%d:v=1:a=1[vout][aout]", n)
	} else {
		fmt.Fprintf(&fc, "concat=n=%d:v=1:a=0[vout]", n)
	}
	args = append(args, "-filter_complex", fc.String(), "-map", "[vout]")
	args = plan.AppendVideoEncode(args)
	if plan.HasAudio {
		args = append(args, "-map", "[aout]")
		// audio already filtered; append codec without second -af
		args = append(args,
			"-c:a", plan.AudioEncoder,
			"-ac", fmt.Sprintf("%d", plan.Channels),
			"-ar", fmt.Sprintf("%d", plan.SampleRate),
			"-b:a", bitrateK(plan.AudioBitrate),
		)
	} else {
		args = append(args, "-an")
	}
	args = append(args, outPath)
	if err := runFFmpegProgress(ctx, args, durationSec, onSeg); err != nil {
		return fmt.Errorf("ffmpeg stitch: %w", err)
	}
	return nil
}

func escapeConcatPath(p string) string {
	return strings.ReplaceAll(p, "'", "'\\''")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
