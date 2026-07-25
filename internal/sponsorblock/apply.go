package sponsorblock

import (
	"context"
	"fmt"
	"path/filepath"
)

// ProfileConfig is the SB slice of a quality profile.
type ProfileConfig struct {
	Mark        []string
	Remove      []string
	ReencodeCut bool
	InfoCards   bool
}

// ApplyResult is the outcome of archive SB processing.
type ApplyResult struct {
	MediaPath   string
	PlanPath    string
	Warning     string // non-empty when degraded
	DidCut      bool
	DidMark     bool
	DidEmbed    bool
	CardsOK     bool
}

// ApplyArchive runs remove/mark/embed on remuxed media using info.json for creator chapters.
// infoPath may be empty. Never mutates info.json contents.
// onProg reports keep-segment re-encode fractions only when cfg.ReencodeCut (nil-safe); copy-cut ignores it.
func ApplyArchive(ctx context.Context, mediaPath, infoPath, pageURL, remoteID string, cfg ProfileConfig, workDir string, onProg EncodeProgress) (ApplyResult, error) {
	res := ApplyResult{MediaPath: mediaPath}
	mark := NormalizeCategoryList(cfg.Mark)
	remove := NormalizeCategoryList(cfg.Remove)
	if err := ValidateMarkRemove(mark, remove); err != nil {
		return res, err
	}
	needFetch := SBEnabled(mark, remove)
	ytID := YouTubeVideoID(pageURL, remoteID)

	var duration float64
	if p, err := ProbeMedia(ctx, mediaPath); err == nil {
		duration = p.Duration
	}
	if duration <= 0 {
		duration = DurationFromInfoJSON(infoPath)
	}
	opt := FilterOpts{DurationSec: duration}

	var segs []Segment
	if needFetch {
		if ytID == "" {
			res.Warning = "SponsorBlock: no YouTube video id"
		} else {
			cats := append([]string{}, mark...)
			cats = append(cats, remove...)
			cats = NormalizeCategoryList(cats)
			fetched, err := DefaultClient.FetchSegments(ctx, ytID, cats)
			if err != nil {
				res.Warning = "SponsorBlock: " + err.Error()
			} else {
				segs = fetched
			}
		}
	} else {
		// Still embed creator chapters when present (Creatorr-always chapter embed).
		native, _ := ChaptersFromInfoJSON(infoPath)
		if len(native) > 0 {
			if err := EmbedChapters(ctx, res.MediaPath, native); err != nil {
				res.Warning = "SponsorBlock: chapter embed failed"
			} else {
				res.DidEmbed = true
			}
		}
		return res, nil
	}

	cuts := FilterForRemove(segs, remove, opt)
	marks := FilterForMark(segs, mark, opt)

	// Remap mark segments into post-cut timeline when cutting.
	if len(cuts) > 0 && OutputDuration(duration, cuts) < 0.5 {
		res.Warning = "SponsorBlock: remove would delete entire video"
		cuts = nil
	}

	fontDir := workDir
	if fontDir == "" {
		fontDir = filepath.Dir(mediaPath)
	}

	if len(cuts) > 0 {
		ext := filepath.Ext(mediaPath)
		if cfg.ReencodeCut {
			ext = ".mkv"
		}
		out := filepath.Join(filepath.Dir(mediaPath), "sb-cut"+ext)
		wantCards := cfg.ReencodeCut && cfg.InfoCards && len(remove) > 0
		var cutProg EncodeProgress
		if cfg.ReencodeCut {
			cutProg = onProg
		}
		cutRes, err := CutArchive(ctx, mediaPath, out, cuts, DefaultCardDurationSec, fontDir, cfg.ReencodeCut, wantCards, cutProg)
		if cutRes.Warning != "" && res.Warning == "" {
			res.Warning = cutRes.Warning
		}
		if err != nil {
			if IsSoft(err) {
				if res.Warning == "" {
					res.Warning = err.Error()
				}
				// entire-video or cards-failed-with-fallback may still have written out
				if _, statErr := ProbeMedia(ctx, out); statErr != nil {
					// try cut-only copy
					keeps := KeepRanges(duration, cuts)
					if cerr := CutCopy(ctx, mediaPath, out, keeps); cerr != nil {
						if res.Warning == "" {
							res.Warning = "SponsorBlock: cut failed"
						}
						cuts = nil
					} else {
						res.DidCut = true
						res.MediaPath = out
					}
				} else {
					res.DidCut = true
					res.MediaPath = out
					res.CardsOK = cutRes.CardsOK
				}
			} else {
				return res, err
			}
		} else {
			res.DidCut = true
			res.MediaPath = out
			res.CardsOK = cutRes.CardsOK
			if wantCards && !cutRes.CardsOK && res.Warning == "" {
				res.Warning = "SponsorBlock: cards failed, cut only"
			}
		}
		if res.DidCut {
			plan := PlanFromCuts(ytID, cuts, wantCards && res.CardsOK, DefaultCardDurationSec, duration)
			if pp, err := WritePlan(res.MediaPath, plan); err == nil {
				res.PlanPath = pp
			}
			// Remap mark times through cuts (+ cards shift roughly: each card adds cardDur at cut)
			if res.CardsOK && wantCards {
				marks = shiftMarksForCards(marks, cuts, DefaultCardDurationSec)
			} else {
				var remapped []Segment
				for _, m := range marks {
					ns := MapTime(m.Start, cuts)
					ne := MapTimeEnd(m.End, cuts)
					if ne > ns+0.05 {
						m.Start, m.End = ns, ne
						remapped = append(remapped, m)
					}
				}
				marks = remapped
			}
		}
	}

	native, _ := ChaptersFromInfoJSON(infoPath)
	if res.DidCut && len(cuts) > 0 && !res.CardsOK {
		native = RemapChapters(native, cuts)
	} else if res.DidCut && res.CardsOK {
		native = RemapChaptersWithCards(native, cuts, DefaultCardDurationSec)
	}

	merged := MergeMarkChapters(native, marks)
	if len(marks) > 0 {
		res.DidMark = true
	}
	if len(merged) > 0 {
		if err := EmbedChapters(ctx, res.MediaPath, merged); err != nil {
			if res.Warning == "" {
				res.Warning = "SponsorBlock: chapter embed failed"
			}
		} else {
			res.DidEmbed = true
		}
	}

	return res, nil
}

func shiftMarksForCards(marks []Segment, cuts []Segment, cardDur float64) []Segment {
	cuts = mergeOverlapping(cuts)
	var out []Segment
	for _, m := range marks {
		ns := MapTimeWithCards(m.Start, cuts, cardDur)
		ne := MapTimeWithCards(m.End, cuts, cardDur)
		if ne > ns+0.05 {
			m.Start, m.End = ns, ne
			out = append(out, m)
		}
	}
	return out
}

// MapTimeWithCards maps source time through cuts and inserts cardDur after each fully passed cut.
func MapTimeWithCards(t float64, cuts []Segment, cardDur float64) float64 {
	cuts = mergeOverlapping(cuts)
	for _, c := range cuts {
		if t > c.Start && t < c.End {
			t = c.Start
			break
		}
	}
	out := t
	cards := 0.0
	for _, c := range cuts {
		if c.End <= t {
			out -= c.End - c.Start
			cards += cardDur
		} else {
			break
		}
	}
	return out + cards
}

// RemapChaptersWithCards remaps chapters accounting for inserted cards.
func RemapChaptersWithCards(chs []Chapter, cuts []Segment, cardDur float64) []Chapter {
	var out []Chapter
	for _, ch := range chs {
		start := MapTimeWithCards(ch.Start, cuts, cardDur)
		end := MapTimeWithCards(ch.End, cuts, cardDur)
		if end <= start+0.05 {
			continue
		}
		out = append(out, Chapter{Start: start, End: end, Title: ch.Title})
	}
	return out
}

// PlaybackDuration returns playable length after cuts (± cards).
func PlaybackDuration(sourceDur float64, plan AppliedCutPlan) float64 {
	cuts := plan.Cuts()
	base := OutputDuration(sourceDur, cuts)
	if plan.InfoCards && plan.HasCuts() {
		card := plan.CardDurationSec
		if card <= 0 {
			card = DefaultCardDurationSec
		}
		base += float64(len(plan.Segments)) * card
	}
	return base
}

// BuildStreamPlan fetches remove segments for stream pack (no cut on disk).
func BuildStreamPlan(ctx context.Context, pageURL, remoteID string, cfg ProfileConfig, sourceDur float64) (AppliedCutPlan, string, error) {
	remove := NormalizeCategoryList(cfg.Remove)
	if len(remove) == 0 {
		return AppliedCutPlan{}, "", nil
	}
	ytID := YouTubeVideoID(pageURL, remoteID)
	if ytID == "" {
		return AppliedCutPlan{}, "SponsorBlock: no YouTube video id", nil
	}
	segs, err := DefaultClient.FetchSegments(ctx, ytID, remove)
	if err != nil {
		msg := err.Error()
		return AppliedCutPlan{}, "SponsorBlock: " + msg, nil
	}
	cuts := FilterForRemove(segs, remove, FilterOpts{DurationSec: sourceDur})
	if len(cuts) == 0 {
		return AppliedCutPlan{}, "", nil
	}
	if OutputDuration(sourceDur, cuts) < 0.5 && sourceDur > 0 {
		return AppliedCutPlan{}, "SponsorBlock: remove would delete entire video", nil
	}
	wantCards := cfg.ReencodeCut && cfg.InfoCards
	plan := PlanFromCuts(ytID, cuts, wantCards, DefaultCardDurationSec, sourceDur)
	return plan, "", nil
}

// EnsureDir is a tiny helper for callers.
func EnsureWorkSubdir(base, name string) string {
	return filepath.Join(base, name)
}

// FormatWarn returns warning or empty.
func FormatWarn(prefix string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %v", prefix, err)
}
