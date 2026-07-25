package sponsorblock

// PlayPiece is one stitch element on the play timeline (source keep or info card).
type PlayPiece struct {
	Kind     string  // "keep" or "card"
	Start    float64 // source start (keep)
	End      float64 // source end (keep)
	Category string  // card category
	SkipSec  float64 // card skipped length (source)
	PlayDur  float64 // duration on play timeline
}

// PlayTimeline builds keep[+card]+keep pieces for stream play / beginning.
func PlayTimeline(sourceDur float64, plan AppliedCutPlan) []PlayPiece {
	cuts := mergeOverlapping(plan.Cuts())
	cardDur := plan.CardDurationSec
	if cardDur <= 0 {
		cardDur = DefaultCardDurationSec
	}
	wantCards := plan.InfoCards
	var out []PlayPiece
	cur := 0.0
	for _, c := range cuts {
		if c.Start > cur+0.05 {
			out = append(out, PlayPiece{Kind: "keep", Start: cur, End: c.Start, PlayDur: c.Start - cur})
		}
		if wantCards {
			out = append(out, PlayPiece{
				Kind: "card", Category: c.Category, SkipSec: c.End - c.Start, PlayDur: cardDur,
			})
		}
		if c.End > cur {
			cur = c.End
		}
	}
	if sourceDur <= 0 {
		sourceDur = plan.SourceDuration
	}
	if sourceDur > 0 && cur < sourceDur-0.05 {
		out = append(out, PlayPiece{Kind: "keep", Start: cur, End: sourceDur, PlayDur: sourceDur - cur})
	}
	return out
}

// SourceOffsetForPlay returns the source timestamp at play offset playSec (for live handoff).
// When playSec lands on a card, returns the source time at the following keep start.
func SourceOffsetForPlay(playSec float64, sourceDur float64, plan AppliedCutPlan) float64 {
	if !plan.HasCuts() {
		return playSec
	}
	pieces := PlayTimeline(sourceDur, plan)
	acc := 0.0
	for _, p := range pieces {
		if playSec < acc+p.PlayDur-0.001 {
			if p.Kind == "keep" {
				return p.Start + (playSec - acc)
			}
			// on card: hand off at next keep start (end of this cut)
			return p.Start // unused for card; fall through
		}
		acc += p.PlayDur
		if p.Kind == "card" {
			continue
		}
	}
	// After all pieces or on card: find last keep end or next keep
	acc = 0.0
	for i, p := range pieces {
		next := acc + p.PlayDur
		if playSec < next-0.001 && p.Kind == "card" {
			// handoff at following keep start
			for j := i + 1; j < len(pieces); j++ {
				if pieces[j].Kind == "keep" {
					return pieces[j].Start
				}
			}
			if sourceDur > 0 {
				return sourceDur
			}
			return 0
		}
		if playSec < next-0.001 && p.Kind == "keep" {
			return p.Start + (playSec - acc)
		}
		acc = next
	}
	if sourceDur > 0 {
		return sourceDur
	}
	return playSec
}

// TipSourceWindows returns source keep windows (and optional cards) that fill wantPlaySec of play time.
func TipSourceWindows(wantPlaySec float64, sourceDur float64, plan AppliedCutPlan) []PlayPiece {
	if wantPlaySec <= 0 {
		return nil
	}
	if !plan.HasCuts() {
		end := wantPlaySec
		if sourceDur > 0 && end > sourceDur {
			end = sourceDur
		}
		return []PlayPiece{{Kind: "keep", Start: 0, End: end, PlayDur: end}}
	}
	pieces := PlayTimeline(sourceDur, plan)
	var out []PlayPiece
	acc := 0.0
	for _, p := range pieces {
		if acc >= wantPlaySec-0.05 {
			break
		}
		remain := wantPlaySec - acc
		if p.PlayDur <= remain+0.05 {
			out = append(out, p)
			acc += p.PlayDur
			continue
		}
		if p.Kind == "keep" {
			out = append(out, PlayPiece{Kind: "keep", Start: p.Start, End: p.Start + remain, PlayDur: remain})
			acc += remain
		} else {
			out = append(out, p)
			acc += p.PlayDur
		}
	}
	return out
}
