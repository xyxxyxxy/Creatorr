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

func audioLayout(p MediaProbe) string {
	if audioChannels(p) == 1 {
		return "mono"
	}
	return "stereo"
}

// RenderSkipCard writes a short card matching EncodePlan codecs/bitrate.
// Multi-line copy uses stacked drawtext filters (text=/textfile= newline escapes are unreliable).
func RenderSkipCard(ctx context.Context, outPath string, plan EncodePlan, text string, durationSec float64, fontFile string) error {
	if durationSec <= 0 {
		durationSec = DefaultCardDurationSec
	}
	fontEsc := escapeDrawtext(fontFile)
	lines := strings.Split(text, "\n")
	line1 := ""
	if len(lines) > 0 {
		line1 = lines[0]
	}
	line2 := strings.TrimSpace(strings.Join(lines[1:], " "))
	var draw string
	if line2 == "" {
		draw = fmt.Sprintf("drawtext=fontfile='%s':text='%s':fontsize=36:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2",
			fontEsc, escapeDrawtext(line1))
	} else {
		draw = fmt.Sprintf(
			"drawtext=fontfile='%s':text='%s':fontsize=36:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2-28,"+
				"drawtext=fontfile='%s':text='%s':fontsize=36:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2+28",
			fontEsc, escapeDrawtext(line1), fontEsc, escapeDrawtext(line2),
		)
	}
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
// When reencode is true, accurate-seek re-encodes with bitrate/codec-matched EncodePlan; cards only if wantCards.
// onProg reports fractions only during keep-segment re-encode ffmpeg (nil-safe); cards/stitch clear to spinner.
// Copy-cut never reports fractions.
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
	dir := filepath.Dir(outPath)
	useCards := wantCards
	var fontPath string
	if useCards {
		fp, ferr := FontPath(fontDir)
		if ferr != nil {
			useCards = false
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
	// When info cards force filter-stitch, keep encode + stitch are two full-ish passes.
	// Weight progress across both so the bar does not freeze after keeps.
	willFilterStitch := useCards
	outEst := totalKeep
	if willFilterStitch {
		outEst = totalKeep + float64(len(cuts))*cardDur
	}
	encDenom := totalKeep
	if willFilterStitch {
		encDenom = totalKeep + outEst
	}
	if encDenom <= 0 {
		encDenom = 1
	}
	var doneKeep float64

	var piecePaths []string
	var temps []string
	defer func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}()

	addKeep := func(start, end float64, idx int) error {
		part := filepath.Join(dir, fmt.Sprintf("sb-keep-%03d.mkv", idx))
		dur := end - start
		args := []string{
			"-i", inPath,
			"-ss", fmt.Sprintf("%.3f", start),
			"-t", fmt.Sprintf("%.3f", dur),
			"-vf", plan.VideoFilter(),
		}
		args = plan.AppendVideoEncode(args)
		args = plan.AppendAudioEncode(args)
		args = append(args, part)
		base := doneKeep
		segDur := dur
		err := runFFmpegProgress(ctx, args, segDur, func(local float64) {
			overall := (base + local*segDur) / encDenom
			reportProg(onProg, overall)
		})
		if err != nil {
			return fmt.Errorf("ffmpeg keep: %w", err)
		}
		doneKeep += segDur
		reportProg(onProg, doneKeep/encDenom)
		temps = append(temps, part)
		piecePaths = append(piecePaths, part)
		return nil
	}

	addCard := func(c Segment, idx int) {
		if !useCards {
			return
		}
		clearProg(onProg)
		cardPath := filepath.Join(dir, fmt.Sprintf("sb-card-%03d.mkv", idx))
		text := CardText(c.Category, c.End-c.Start)
		if err := RenderSkipCard(ctx, cardPath, plan, text, cardDur, fontPath); err != nil {
			useCards = false
			willFilterStitch = false
			encDenom = totalKeep
			res.CardsOK = false
			// Drop any cards already queued so stitch stays consistent.
			filtered := piecePaths[:0]
			for _, p := range piecePaths {
				base := filepath.Base(p)
				if strings.HasPrefix(base, "sb-card-") {
					_ = os.Remove(p)
					continue
				}
				filtered = append(filtered, p)
			}
			piecePaths = filtered
			return
		}
		temps = append(temps, cardPath)
		piecePaths = append(piecePaths, cardPath)
		res.CardsOK = true
	}

	cur := 0.0
	keepIdx := 0
	for i, c := range cuts {
		if c.Start > cur+0.05 {
			if err := addKeep(cur, c.Start, keepIdx); err != nil {
				return err
			}
			keepIdx++
		}
		addCard(c, i)
		if c.End > cur {
			cur = c.End
		}
	}
	if cur < probe.Duration-0.05 {
		if err := addKeep(cur, probe.Duration, keepIdx); err != nil {
			return err
		}
	}
	if len(piecePaths) == 0 {
		return SoftError{Msg: "SponsorBlock remove would delete entire video"}
	}

	if len(piecePaths) == 1 {
		clearProg(onProg)
		data, err := os.ReadFile(piecePaths[0])
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0o644)
	}

	// Card pieces change AV1/Opus sequence headers; stream-copy concat often makes
	// players stop at the card seam. Re-encode stitch when any info card is present.
	hasCard := false
	for _, p := range piecePaths {
		if strings.HasPrefix(filepath.Base(p), "sb-card-") {
			hasCard = true
			break
		}
	}
	stitchDur := 0.0
	for _, p := range piecePaths {
		if pr, err := ProbeMedia(ctx, p); err == nil && pr.Duration > 0 {
			stitchDur += pr.Duration
		}
	}
	if stitchDur <= 0 {
		stitchDur = outEst
	}
	runStitch := func(mapIntoTail bool) error {
		return concatFilter(ctx, piecePaths, outPath, plan, stitchDur, func(local float64) {
			var overall float64
			if mapIntoTail {
				overall = 0.9 + local*0.1
			} else {
				overall = (doneKeep + local*stitchDur) / encDenom
			}
			reportProg(onProg, overall)
		})
	}
	if hasCard {
		if err := runStitch(false); err != nil {
			return err
		}
		clearProg(onProg)
		return nil
	}
	if err := concatCopy(ctx, piecePaths, outPath, dir); err != nil {
		if err2 := runStitch(true); err2 != nil {
			return err2
		}
	}
	clearProg(onProg)
	return nil
}

// CutWithCards is a convenience wrapper used by older call sites/tests (always reencode when cards wanted).
func CutWithCards(ctx context.Context, inPath, outPath string, cuts []Segment, cardDur float64, fontDir string, wantCards bool) (cardsOK bool, err error) {
	r, err := CutArchive(ctx, inPath, outPath, cuts, cardDur, fontDir, true, wantCards, nil)
	return r.CardsOK, err
}
