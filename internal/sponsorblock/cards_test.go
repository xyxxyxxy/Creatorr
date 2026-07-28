package sponsorblock

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func haveFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

func synthMedia(t *testing.T, out string, durSec float64, withAudio bool) {
	t.Helper()
	dur := fmt.Sprintf("%.3f", durSec)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:r=25:d=" + dur,
	}
	if withAudio {
		args = append(args, "-f", "lavfi", "-i", "sine=f=440:r=48000:d="+dur)
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", dur)
	if withAudio {
		args = append(args, "-c:a", "aac", "-shortest")
	} else {
		args = append(args, "-an")
	}
	args = append(args, out)
	cmd := exec.Command("ffmpeg", args...)
	if outb, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synth media: %v: %s", err, outb)
	}
}

func TestCutWithCardsContinuousDuration(t *testing.T) {
	haveFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	out := filepath.Join(dir, "out.mkv")
	synthMedia(t, in, 10, true)

	cuts := []Segment{{Start: 3, End: 5, Category: "sponsor"}}
	cardsOK, err := CutWithCards(context.Background(), in, out, cuts, 1.5, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !cardsOK {
		t.Fatal("expected cardsOK")
	}
	probe, err := ProbeMedia(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	// 10s source - 2s cut + 1.5s card ≈ 9.5s
	if probe.Duration < 9.0 || probe.Duration > 10.2 {
		t.Fatalf("duration=%v want ~9.5", probe.Duration)
	}
	if !probe.HasVideo || !probe.HasAudio {
		t.Fatalf("missing streams: %+v", probe)
	}
}

func TestCutArchiveReencodeNoCards(t *testing.T) {
	haveFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	out := filepath.Join(dir, "out.mkv")
	synthMedia(t, in, 10, true)

	cuts := []Segment{{Start: 3, End: 5, Category: "sponsor"}}
	res, err := CutArchive(context.Background(), in, out, cuts, 1.5, dir, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CardsOK {
		t.Fatal("expected no cards")
	}
	probe, err := ProbeMedia(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	// 10 - 2 = 8s
	if probe.Duration < 7.5 || probe.Duration > 8.8 {
		t.Fatalf("duration=%v want ~8", probe.Duration)
	}
}

func TestRenderSkipCardMultiline(t *testing.T) {
	haveFFmpeg(t)
	dir := t.TempDir()
	font, err := FontPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "card.mkv")
	plan := EncodePlan{
		Width: 320, Height: 180, FPS: 25,
		VideoEncoder: "libx264", VideoBitrate: 500_000,
		HasAudio: false,
	}
	text := CardText("sponsor", 64)
	if err := RenderSkipCard(context.Background(), out, plan, text, 0.5, font); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "frame.png")
	cmd := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", out, "-frames:v", "1", png)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("extract frame: %v: %s", err, b)
	}
	// Single-line glue ("SponsorBlockskipped") is one solid band; two lines leave a dark gap.
	if !pngHasTwoTextBands(t, png) {
		t.Fatal("expected two text lines on info card (newline not rendered)")
	}
}

func pngHasTwoTextBands(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	var litRows []int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		lit := false
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0x8000 || g > 0x8000 || bl > 0x8000 {
				lit = true
				break
			}
		}
		if lit {
			litRows = append(litRows, y)
		}
	}
	if len(litRows) < 2 {
		return false
	}
	// Collapse contiguous lit rows into bands; need ≥2 bands separated by dark rows.
	bands := 1
	for i := 1; i < len(litRows); i++ {
		if litRows[i] > litRows[i-1]+1 {
			bands++
		}
	}
	return bands >= 2
}

func TestCutArchiveCopyIgnoresCards(t *testing.T) {
	haveFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	out := filepath.Join(dir, "out.mkv")
	synthMedia(t, in, 8, true)

	cuts := []Segment{{Start: 2, End: 4, Category: "sponsor"}}
	res, err := CutArchive(context.Background(), in, out, cuts, 1, dir, false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CardsOK {
		t.Fatal("copy mode must ignore cards")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

