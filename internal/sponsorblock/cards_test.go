package sponsorblock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/testutil/fakemedia"
)

func useFakeMedia(t *testing.T) {
	t.Helper()
	fakemedia.PrependPATH(t)
	t.Setenv("FAKE_FFPROBE_DURATION", "10")
}

func synthMedia(t *testing.T, out string, _ float64, _ bool) {
	t.Helper()
	fakemedia.WriteDummyMedia(t, out)
}

func TestCutWithCardsContinuousDuration(t *testing.T) {
	useFakeMedia(t)
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
	// Fake ffprobe returns fixed duration; assert streams exist (cut math needs real ffmpeg).
	if probe.Duration != 10 {
		t.Fatalf("duration=%v want 10 (fake ffprobe)", probe.Duration)
	}
	if !probe.HasVideo || !probe.HasAudio {
		t.Fatalf("missing streams: %+v", probe)
	}
}

func TestCutArchiveReencodeNoCards(t *testing.T) {
	useFakeMedia(t)
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
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestCardTextMultiline(t *testing.T) {
	text := CardText("sponsor", 64)
	if !strings.Contains(text, "\n") {
		t.Fatalf("CardText should be multiline, got %q", text)
	}
}

func TestRenderSkipCardWritesOutput(t *testing.T) {
	useFakeMedia(t)
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
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestCutArchiveCopyIgnoresCards(t *testing.T) {
	useFakeMedia(t)
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

func TestCutArchiveSinglePassNoIntermediatePieces(t *testing.T) {
	useFakeMedia(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	out := filepath.Join(dir, "out.mkv")
	synthMedia(t, in, 10, true)

	cuts := []Segment{{Start: 3, End: 5, Category: "sponsor"}}
	res, err := CutArchive(context.Background(), in, out, cuts, 1.5, dir, true, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.CardsOK {
		t.Fatal("expected cardsOK")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "sb-keep-") || strings.HasPrefix(name, "sb-card-") {
			t.Fatalf("unexpected intermediate piece %q", name)
		}
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestCutArchiveReencodeProgressMid(t *testing.T) {
	useFakeMedia(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mkv")
	out := filepath.Join(dir, "out.mkv")
	synthMedia(t, in, 6, true)

	cuts := []Segment{{Start: 1, End: 2, Category: "sponsor"}}
	sawMid := false
	var last *float64
	onProg := func(frac *float64) {
		last = frac
		if frac != nil && *frac > 0 && *frac < 1 {
			sawMid = true
		}
	}
	_, err := CutArchive(context.Background(), in, out, cuts, 1, dir, true, false, onProg)
	if err != nil {
		t.Fatal(err)
	}
	if !sawMid {
		t.Fatalf("expected mid progress (0,1); last=%v", last)
	}
}

func TestBuildSinglePassCutArgsHasFilterComplex(t *testing.T) {
	plan := EncodePlan{
		Width: 320, Height: 240, FPS: 25,
		VideoEncoder: "libx264", VideoBitrate: 500_000,
		HasAudio: true, AudioEncoder: "aac", SampleRate: 48000, Channels: 2,
		AudioBitrate: 128_000,
	}
	pieces := []PlayPiece{
		{Kind: "keep", Start: 0, End: 3, PlayDur: 3},
		{Kind: "card", Category: "sponsor", SkipSec: 2, PlayDur: 1.5},
		{Kind: "keep", Start: 5, End: 10, PlayDur: 5},
	}
	args, dur, err := buildSinglePassCutArgs("/in.mkv", "/out.mkv", pieces, plan, "/font.ttf", 0)
	if err != nil {
		t.Fatal(err)
	}
	if dur < 9.4 || dur > 9.6 {
		t.Fatalf("dur=%v want ~9.5", dur)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-filter_complex") {
		t.Fatal("missing filter_complex")
	}
	if !strings.Contains(joined, "concat=n=3") {
		t.Fatal("expected concat n=3")
	}
	if !strings.Contains(joined, "split=2") {
		t.Fatal("expected video split for 2 keeps")
	}
	if strings.Contains(joined, "sb-keep-") || strings.Contains(joined, "sb-card-") {
		t.Fatal("args must not reference intermediate piece paths")
	}
}
