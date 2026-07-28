package sponsorblock

import (
	"fmt"
	"os/exec"
	"strings"
)

// EncodePlan holds bitrate/codec-matched ffmpeg args for accurate re-encode cuts.
type EncodePlan struct {
	VideoEncoder string // libx264, libvpx-vp9, libsvtav1, …
	AudioEncoder string // aac, libopus, or "" if no audio
	VideoBitrate int64  // bits per second
	AudioBitrate int64
	Width        int
	Height       int
	FPS          float64
	SampleRate   int
	Channels     int
	HasAudio     bool
	Warning      string // non-empty when falling back from preferred codec
}

const (
	minVideoBitrate = int64(300_000)
	maxVideoBitrate = int64(40_000_000)
	defaultAudioBR  = int64(160_000)
)

// BuildEncodePlan picks encoders and bitrates from a probe (prefer matching source codecs).
func BuildEncodePlan(probe MediaProbe) EncodePlan {
	p := EncodePlan{
		Width:      evenDim(probe.Width, 1280),
		Height:     evenDim(probe.Height, 720),
		FPS:        probe.FPS,
		SampleRate: audioRate(probe),
		Channels:   audioChannels(probe),
		HasAudio:   probe.HasAudio,
	}
	if p.FPS <= 0 {
		p.FPS = 30
	}
	p.VideoBitrate = clampBitrate(probe.VideoBitrate, minVideoBitrate, maxVideoBitrate)
	if probe.HasAudio {
		ab := probe.AudioBitrate
		if ab <= 0 {
			ab = defaultAudioBR
		}
		p.AudioBitrate = clampBitrate(ab, 64_000, 512_000)
	}

	vc := strings.ToLower(probe.VideoCodec)
	switch {
	case vc == "h264" || vc == "avc" || strings.Contains(vc, "h264"):
		p.VideoEncoder = "libx264"
	case vc == "vp9" || vc == "vp8":
		p.VideoEncoder = "libvpx-vp9"
	case vc == "av1":
		if encoderAvailable("libsvtav1") {
			p.VideoEncoder = "libsvtav1"
		} else {
			p.VideoEncoder = "libx264"
			p.VideoBitrate = clampBitrate(int64(float64(p.VideoBitrate)*1.3), minVideoBitrate, maxVideoBitrate)
			p.Warning = "SponsorBlock: AV1 re-encode fell back to H.264 at ~1.3× bitrate"
		}
	default:
		p.VideoEncoder = "libx264"
	}

	if !probe.HasAudio {
		return p
	}
	ac := strings.ToLower(probe.AudioCodec)
	switch ac {
	case "aac":
		p.AudioEncoder = "aac"
	case "opus":
		p.AudioEncoder = "libopus"
	default:
		p.AudioEncoder = "aac"
	}
	return p
}

// FallbackH264AAC forces libx264 + aac at the plan's bitrate targets.
func (p EncodePlan) FallbackH264AAC() EncodePlan {
	out := p
	out.VideoEncoder = "libx264"
	if out.HasAudio {
		out.AudioEncoder = "aac"
	}
	if out.Warning == "" {
		out.Warning = "SponsorBlock: re-encode fell back to H.264/AAC"
	}
	return out
}

func clampBitrate(v, lo, hi int64) int64 {
	if v <= 0 {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func bitrateK(bps int64) string {
	k := bps / 1000
	if k < 1 {
		k = 1
	}
	return fmt.Sprintf("%dk", k)
}

func encoderAvailable(name string) bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

// VideoFilter returns the normalize vf for re-encode keeps/stitch.
func (p EncodePlan) VideoFilter() string {
	return fmt.Sprintf(
		"fps=%g,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p",
		p.FPS, p.Width, p.Height, p.Width, p.Height,
	)
}

// AppendVideoEncode appends video codec args (after -vf if any).
func (p EncodePlan) AppendVideoEncode(args []string) []string {
	vb := bitrateK(p.VideoBitrate)
	maxr := bitrateK(int64(float64(p.VideoBitrate) * 1.1))
	buf := bitrateK(p.VideoBitrate * 2)
	switch p.VideoEncoder {
	case "libvpx-vp9":
		return append(args,
			"-c:v", "libvpx-vp9", "-b:v", vb, "-maxrate", maxr, "-bufsize", buf,
			"-row-mt", "1", "-deadline", "good", "-cpu-used", "4",
		)
	case "libsvtav1":
		return append(args,
			"-c:v", "libsvtav1", "-b:v", vb, "-maxrate", maxr, "-bufsize", buf,
			"-preset", "8",
		)
	default: // libx264
		return append(args,
			"-c:v", "libx264", "-preset", "fast", "-b:v", vb, "-maxrate", maxr, "-bufsize", buf,
			"-pix_fmt", "yuv420p",
		)
	}
}

// AppendAudioEncode appends audio codec args, or -an.
func (p EncodePlan) AppendAudioEncode(args []string) []string {
	if !p.HasAudio || p.AudioEncoder == "" {
		return append(args, "-an")
	}
	layout := "stereo"
	if p.Channels == 1 {
		layout = "mono"
	}
	args = append(args,
		"-af", fmt.Sprintf("aformat=sample_rates=%d:channel_layouts=%s", p.SampleRate, layout),
		"-c:a", p.AudioEncoder,
		"-ac", fmt.Sprintf("%d", p.Channels),
		"-ar", fmt.Sprintf("%d", p.SampleRate),
		"-b:a", bitrateK(p.AudioBitrate),
	)
	return args
}
