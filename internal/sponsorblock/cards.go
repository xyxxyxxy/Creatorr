package sponsorblock

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed fonts/DejaVuSans.ttf
var embeddedFont []byte

// FontPath writes the bundled font to destDir if needed and returns its path.
func FontPath(destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(destDir, "DejaVuSans.ttf")
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(embeddedFont)) {
		return path, nil
	}
	if err := os.WriteFile(path, embeddedFont, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// FormatSkipDuration formats a skipped span for card text (e.g. "1 min 4 sec").
func FormatSkipDuration(sec float64) string {
	n := int(sec + 0.5)
	if n < 1 {
		n = 1
	}
	h := n / 3600
	m := (n % 3600) / 60
	s := n % 60
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d h", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%d min", m))
	}
	if s > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d sec", s))
	}
	return strings.Join(parts, " ")
}

// CardText builds hard-cut info card text: "SponsorBlock" then detail on line 2.
func CardText(category string, skippedSec float64) string {
	return "SponsorBlock\nskipped " + FormatSkipDuration(skippedSec) + " of " + CategoryDisplayName(category)
}

func escapeDrawtext(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func evenDim(v, fallback int) int {
	if v <= 0 {
		v = fallback
	}
	if v%2 != 0 {
		v--
	}
	return v
}

func audioRate(p MediaProbe) int {
	if p.SampleRate <= 0 {
		return 48000
	}
	return p.SampleRate
}

func audioChannels(p MediaProbe) int {
	if p.Channels <= 0 {
		return 2
	}
	return p.Channels
}

// RenderSkipCard writes a short card matching EncodePlan codecs/bitrate.
// Multi-line copy uses stacked drawtext filters (text=/textfile= newline escapes are unreliable).
func RenderSkipCard(ctx context.Context, outPath string, plan EncodePlan, text string, durationSec float64, fontFile string) error {
	if durationSec <= 0 {
		durationSec = DefaultCardDurationSec
	}
	draw := cardDrawtext(text, fontFile)
	vf := plan.VideoFilter() + "," + draw

	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%g:d=%g", plan.Width, plan.Height, plan.FPS, durationSec),
	}
	if plan.HasAudio {
		layout := "stereo"
		if plan.Channels == 1 {
			layout = "mono"
		}
		args = append(args,
			"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=%d:cl=%s:d=%g", plan.SampleRate, layout, durationSec),
		)
	}
	args = append(args, "-vf", vf)
	args = plan.AppendVideoEncode(args)
	if plan.HasAudio {
		args = append(args,
			"-c:a", plan.AudioEncoder,
			"-ac", fmt.Sprintf("%d", plan.Channels),
			"-ar", fmt.Sprintf("%d", plan.SampleRate),
			"-b:a", bitrateK(plan.AudioBitrate),
			"-shortest",
		)
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-t", fmt.Sprintf("%.3f", durationSec), outPath)
	if err := runFFmpegReencode(ctx, args); err != nil {
		return fmt.Errorf("render card: %w", err)
	}
	return nil
}

// CutResult is the outcome of CutArchive.
type CutResult struct {
	CardsOK bool
	Warning string
}

// CutArchive removes cut spans. When reencode is false, stream-copies (keyframe snap) and ignores cards.
// When reencode is true, accurate-seek re-encodes with bitrate-matched H.264 EncodePlan; cards only if wantCards.
// onProg reports fractions during the single-pass re-encode ffmpeg (nil-safe). Copy-cut never reports fractions.
func CutArchive(ctx context.Context, inPath, outPath string, cuts []Segment, cardDur float64, fontDir string, reencode, wantCards bool, onProg EncodeProgress) (CutResult, error) {
	var res CutResult
	cuts = mergeOverlapping(cuts)
	probe, err := ProbeMedia(ctx, inPath)
	if err != nil {
		return res, err
	}
	if probe.Duration <= 0 {
		return res, fmt.Errorf("unknown media duration")
	}
	keeps := KeepRanges(probe.Duration, cuts)
	if len(keeps) == 0 {
		return res, SoftError{Msg: "SponsorBlock remove would delete entire video"}
	}
	if !reencode {
		if err := CutCopy(ctx, inPath, outPath, keeps); err != nil {
			return res, err
		}
		return res, nil
	}
	if cardDur <= 0 {
		cardDur = DefaultCardDurationSec
	}
	wantCards = wantCards && reencode

	plan := BuildEncodePlan(probe)
	res.Warning = plan.Warning

	cutErr := cutReencode(ctx, inPath, outPath, cuts, probe, plan, cardDur, fontDir, wantCards, &res, onProg)
	if cutErr != nil {
		fb := plan.FallbackH264AAC()
		res.Warning = fb.Warning
		if err2 := cutReencode(ctx, inPath, outPath, cuts, probe, fb, cardDur, fontDir, wantCards, &res, onProg); err2 != nil {
			if cerr := CutCopy(ctx, inPath, outPath, keeps); cerr == nil {
				res.CardsOK = false
				if res.Warning == "" {
					res.Warning = "SponsorBlock: re-encode failed, cut copy only"
				}
				return res, SoftError{Msg: res.Warning}
			}
			return res, cutErr
		}
	}
	clearProg(onProg)
	return res, nil
}

func cutReencode(ctx context.Context, inPath, outPath string, cuts []Segment, probe MediaProbe, plan EncodePlan, cardDur float64, fontDir string, wantCards bool, res *CutResult, onProg EncodeProgress) error {
	useCards := wantCards
	var fontPath string
	if useCards {
		fp, ferr := FontPath(fontDir)
		if ferr != nil {
			useCards = false
			res.CardsOK = false
		} else {
			fontPath = fp
		}
	}

	keeps := KeepRanges(probe.Duration, cuts)
	var totalKeep float64
	for _, k := range keeps {
		d := k[1] - k[0]
		if d > 0 {
			totalKeep += d
		}
	}
	if totalKeep <= 0 {
		return SoftError{Msg: "SponsorBlock remove would delete entire video"}
	}

	tl := PlanFromCuts("", cuts, useCards, cardDur, probe.Duration)
	pieces := PlayTimeline(probe.Duration, tl)
	if len(pieces) == 0 {
		return SoftError{Msg: "SponsorBlock remove would delete entire video"}
	}

	outEst := totalKeep
	if useCards {
		outEst = totalKeep + float64(len(cuts))*cardDur
	}
	args, dur, err := buildSinglePassCutArgs(inPath, outPath, pieces, plan, fontPath, outEst)
	if err != nil {
		return err
	}
	if err := runFFmpegProgress(ctx, args, dur, func(local float64) {
		reportProg(onProg, local)
	}); err != nil {
		return fmt.Errorf("ffmpeg single-pass cut: %w", err)
	}
	if useCards {
		res.CardsOK = true
	} else {
		res.CardsOK = false
	}
	clearProg(onProg)
	return nil
}

// CutWithCards is a convenience wrapper used by older call sites/tests (always reencode when cards wanted).
func CutWithCards(ctx context.Context, inPath, outPath string, cuts []Segment, cardDur float64, fontDir string, wantCards bool) (cardsOK bool, err error) {
	r, err := CutArchive(ctx, inPath, outPath, cuts, cardDur, fontDir, true, wantCards, nil)
	return r.CardsOK, err
}
