package sponsorblock

import (
	"strings"
	"testing"
)

func TestBuildEncodePlanAlwaysH264(t *testing.T) {
	base := MediaProbe{
		Width: 854, Height: 480, FPS: 30,
		VideoBitrate: 400_000, HasAudio: true,
		AudioCodec: "opus", AudioBitrate: 160_000,
		SampleRate: 48000, Channels: 2,
	}

	t.Run("h264", func(t *testing.T) {
		p := base
		p.VideoCodec = "h264"
		got := BuildEncodePlan(p)
		if got.VideoEncoder != "libx264" {
			t.Fatalf("VideoEncoder=%q want libx264", got.VideoEncoder)
		}
		if got.VideoBitrate != 400_000 {
			t.Fatalf("VideoBitrate=%d want 400000 (no bump)", got.VideoBitrate)
		}
		if got.Warning != "" {
			t.Fatalf("Warning=%q want empty", got.Warning)
		}
		if got.AudioEncoder != "libopus" {
			t.Fatalf("AudioEncoder=%q want libopus", got.AudioEncoder)
		}
	})

	t.Run("av1", func(t *testing.T) {
		p := base
		p.VideoCodec = "av1"
		got := BuildEncodePlan(p)
		if got.VideoEncoder != "libx264" {
			t.Fatalf("VideoEncoder=%q want libx264", got.VideoEncoder)
		}
		want := int64(float64(400_000) * efficientCodecBitrateFactor)
		if got.VideoBitrate != want {
			t.Fatalf("VideoBitrate=%d want %d (1.5x bump)", got.VideoBitrate, want)
		}
		if got.Warning != "" {
			t.Fatalf("Warning=%q want empty", got.Warning)
		}
	})

	t.Run("vp9", func(t *testing.T) {
		p := base
		p.VideoCodec = "vp9"
		got := BuildEncodePlan(p)
		if got.VideoEncoder != "libx264" {
			t.Fatalf("VideoEncoder=%q want libx264", got.VideoEncoder)
		}
		want := int64(float64(400_000) * efficientCodecBitrateFactor)
		if got.VideoBitrate != want {
			t.Fatalf("VideoBitrate=%d want %d (1.5x bump)", got.VideoBitrate, want)
		}
	})

	t.Run("vp8", func(t *testing.T) {
		p := base
		p.VideoCodec = "vp8"
		got := BuildEncodePlan(p)
		if got.VideoEncoder != "libx264" {
			t.Fatalf("VideoEncoder=%q want libx264", got.VideoEncoder)
		}
		want := int64(float64(400_000) * efficientCodecBitrateFactor)
		if got.VideoBitrate != want {
			t.Fatalf("VideoBitrate=%d want %d (1.5x bump)", got.VideoBitrate, want)
		}
	})
}

func TestAppendVideoEncodeLibx264Only(t *testing.T) {
	plan := EncodePlan{VideoEncoder: "libsvtav1", VideoBitrate: 307_000}
	args := plan.AppendVideoEncode(nil)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "libx264") || !strings.Contains(joined, "fast") {
		t.Fatalf("args=%v want libx264 preset fast", args)
	}
	if strings.Contains(joined, "libsvtav1") || strings.Contains(joined, "libvpx-vp9") {
		t.Fatalf("args=%v must not emit AV1/VP9 encoders", args)
	}
	// 307k * 1.1 maxrate uses bitrateK on plan bitrate as-is
	if !strings.Contains(joined, "-b:v 307k") {
		t.Fatalf("args=%v want -b:v 307k", args)
	}
}
