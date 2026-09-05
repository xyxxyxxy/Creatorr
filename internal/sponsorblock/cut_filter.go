package sponsorblock

import (
	"fmt"
	"strings"
)

// cardDrawtext returns stacked drawtext filter(s) for info-card copy (same as RenderSkipCard).
func cardDrawtext(text, fontFile string) string {
	fontEsc := escapeDrawtext(fontFile)
	lines := strings.Split(text, "\n")
	line1 := ""
	if len(lines) > 0 {
		line1 = lines[0]
	}
	line2 := strings.TrimSpace(strings.Join(lines[1:], " "))
	if line2 == "" {
		return fmt.Sprintf("drawtext=fontfile='%s':text='%s':fontsize=28:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2",
			fontEsc, escapeDrawtext(line1))
	}
	return fmt.Sprintf(
		"drawtext=fontfile='%s':text='%s':fontsize=28:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2-22,"+
			"drawtext=fontfile='%s':text='%s':fontsize=28:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2+22",
		fontEsc, escapeDrawtext(line1), fontEsc, escapeDrawtext(line2),
	)
}

// buildSinglePassCutArgs builds one ffmpeg argv: source -i + filter_complex (keeps + optional cards) + encode to outPath.
// pieces must be keep/card interleaved (e.g. PlayTimeline). outEstSec is expected output duration for -progress.
func buildSinglePassCutArgs(inPath, outPath string, pieces []PlayPiece, plan EncodePlan, fontPath string, outEstSec float64) ([]string, float64, error) {
	if len(pieces) == 0 {
		return nil, 0, fmt.Errorf("no pieces to encode")
	}
	keepN := 0
	for _, p := range pieces {
		if p.Kind == "keep" {
			keepN++
		}
	}
	if keepN == 0 {
		return nil, 0, SoftError{Msg: "SponsorBlock remove would delete entire video"}
	}

	layout := "stereo"
	if plan.Channels == 1 {
		layout = "mono"
	}
	vfNorm := plan.VideoFilter()

	var fc strings.Builder
	if keepN > 1 {
		fmt.Fprintf(&fc, "[0:v]split=%d", keepN)
		for i := 0; i < keepN; i++ {
			fmt.Fprintf(&fc, "[ksrc%d]", i)
		}
		fc.WriteByte(';')
		if plan.HasAudio {
			fmt.Fprintf(&fc, "[0:a]asplit=%d", keepN)
			for i := 0; i < keepN; i++ {
				fmt.Fprintf(&fc, "[asrc%d]", i)
			}
			fc.WriteByte(';')
		}
	}

	keepIdx := 0
	for i, p := range pieces {
		switch p.Kind {
		case "keep":
			vIn := "[0:v]"
			aIn := "[0:a]"
			if keepN > 1 {
				vIn = fmt.Sprintf("[ksrc%d]", keepIdx)
				aIn = fmt.Sprintf("[asrc%d]", keepIdx)
			}
			keepIdx++
			fmt.Fprintf(&fc,
				"%strim=start=%.6f:end=%.6f,setpts=PTS-STARTPTS,%s[v%d];",
				vIn, p.Start, p.End, vfNorm, i,
			)
			if plan.HasAudio {
				fmt.Fprintf(&fc,
					"%satrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS,aformat=sample_rates=%d:channel_layouts=%s[a%d];",
					aIn, p.Start, p.End, plan.SampleRate, layout, i,
				)
			}
		case "card":
			if fontPath == "" {
				return nil, 0, fmt.Errorf("card piece without font")
			}
			dur := p.PlayDur
			if dur <= 0 {
				dur = DefaultCardDurationSec
			}
			text := CardText(p.Category, p.SkipSec)
			draw := cardDrawtext(text, fontPath)
			// Match RenderSkipCard: color -> VideoFilter -> drawtext.
			fmt.Fprintf(&fc,
				"color=c=black:s=%dx%d:r=%g:d=%.6f,%s,%s,setpts=PTS-STARTPTS[v%d];",
				plan.Width, plan.Height, plan.FPS, dur, vfNorm, draw, i,
			)
			if plan.HasAudio {
				fmt.Fprintf(&fc,
					"anullsrc=r=%d:cl=%s:d=%.6f,aformat=sample_rates=%d:channel_layouts=%s,asetpts=PTS-STARTPTS[a%d];",
					plan.SampleRate, layout, dur, plan.SampleRate, layout, i,
				)
			}
		default:
			return nil, 0, fmt.Errorf("unknown play piece kind %q", p.Kind)
		}
	}

	for i := range pieces {
		fmt.Fprintf(&fc, "[v%d]", i)
		if plan.HasAudio {
			fmt.Fprintf(&fc, "[a%d]", i)
		}
	}
	n := len(pieces)
	if plan.HasAudio {
		fmt.Fprintf(&fc, "concat=n=%d:v=1:a=1[vout][aout]", n)
	} else {
		fmt.Fprintf(&fc, "concat=n=%d:v=1:a=0[vout]", n)
	}

	args := []string{"-i", inPath, "-filter_complex", fc.String(), "-map", "[vout]"}
	args = plan.AppendVideoEncode(args)
	if plan.HasAudio {
		args = append(args, "-map", "[aout]",
			"-c:a", plan.AudioEncoder,
			"-ac", fmt.Sprintf("%d", plan.Channels),
			"-ar", fmt.Sprintf("%d", plan.SampleRate),
			"-b:a", bitrateK(plan.AudioBitrate),
		)
	} else {
		args = append(args, "-an")
	}
	args = append(args, outPath)

	if outEstSec <= 0 {
		for _, p := range pieces {
			outEstSec += p.PlayDur
		}
	}
	if outEstSec <= 0 {
		outEstSec = 1
	}
	return args, outEstSec, nil
}
