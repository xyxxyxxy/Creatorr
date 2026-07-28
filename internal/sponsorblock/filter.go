package sponsorblock

import (
	"math"
	"sort"
	"strings"
)

// Chapter is a named time range (seconds).
type Chapter struct {
	Start float64
	End   float64
	Title string
}

// FilterOpts mirrors yt-dlp SponsorBlock filter rules for remove/mark.
type FilterOpts struct {
	DurationSec float64 // media duration; 0 = unknown
}

// FilterForRemove returns segments to cut (skip action, non-POI).
func FilterForRemove(segs []Segment, cats []string, opt FilterOpts) []Segment {
	want := toSet(NormalizeCategoryList(cats))
	var out []Segment
	for _, s := range segs {
		if _, ok := want[s.Category]; !ok {
			continue
		}
		if strings.EqualFold(s.ActionType, "poi") || s.Category == "poi_highlight" {
			continue
		}
		if !actionAllowsRemove(s.ActionType) {
			continue
		}
		s2, ok := normalizeSegment(s, opt)
		if !ok {
			continue
		}
		out = append(out, s2)
	}
	return mergeOverlapping(out)
}

// FilterForMark returns segments to chapter-mark.
func FilterForMark(segs []Segment, cats []string, opt FilterOpts) []Segment {
	want := toSet(NormalizeCategoryList(cats))
	var out []Segment
	for _, s := range segs {
		if _, ok := want[s.Category]; !ok {
			continue
		}
		isPOI := s.Category == "poi_highlight" || strings.EqualFold(s.ActionType, "poi")
		if isPOI {
			// Expand POI before normalize so zero-length ranges survive.
			s.End = s.Start + 1
			if opt.DurationSec > 0 && s.End > opt.DurationSec {
				s.End = opt.DurationSec
			}
		}
		s2, ok := normalizeSegment(s, opt)
		if !ok {
			continue
		}
		out = append(out, s2)
	}
	return out
}

func actionAllowsRemove(a string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	return a == "" || a == "skip" || a == "mute"
}

func normalizeSegment(s Segment, opt FilterOpts) (Segment, bool) {
	// duration mismatch vs API videoDuration: drop if wildly off
	if opt.DurationSec > 0 && s.VideoDuration > 0 {
		ratio := opt.DurationSec / s.VideoDuration
		if ratio < 0.9 || ratio > 1.1 {
			return Segment{}, false
		}
	}
	start, end := s.Start, s.End
	if end < start {
		start, end = end, start
	}
	// [0,0] full-video style: treat as entire duration when known
	if start == 0 && end == 0 {
		if opt.DurationSec <= 0 {
			return Segment{}, false
		}
		start, end = 0, opt.DurationSec
	}
	if opt.DurationSec > 0 {
		if start < 0 {
			start = 0
		}
		if end > opt.DurationSec {
			end = opt.DurationSec
		}
		if start >= opt.DurationSec {
			return Segment{}, false
		}
	}
	if end-start < 0.05 {
		return Segment{}, false
	}
	s.Start, s.End = start, end
	return s, true
}

func mergeOverlapping(segs []Segment) []Segment {
	if len(segs) == 0 {
		return nil
	}
	sort.Slice(segs, func(i, j int) bool {
		if segs[i].Start == segs[j].Start {
			return segs[i].End < segs[j].End
		}
		return segs[i].Start < segs[j].Start
	})
	out := []Segment{segs[0]}
	for _, s := range segs[1:] {
		last := &out[len(out)-1]
		if s.Start <= last.End+0.05 {
			if s.End > last.End {
				last.End = s.End
			}
			// keep first category/uuid for attribution of merged span
		} else {
			out = append(out, s)
		}
	}
	return out
}

// KeepRanges returns complementary keep windows for duration after removing cuts.
func KeepRanges(duration float64, cuts []Segment) [][2]float64 {
	if duration <= 0 {
		return nil
	}
	cuts = mergeOverlapping(cuts)
	var keeps [][2]float64
	cur := 0.0
	for _, c := range cuts {
		if c.Start > cur+0.05 {
			keeps = append(keeps, [2]float64{cur, c.Start})
		}
		if c.End > cur {
			cur = c.End
		}
	}
	if cur < duration-0.05 {
		keeps = append(keeps, [2]float64{cur, duration})
	}
	return keeps
}

// MapTime maps a source timestamp through removed cuts into output timeline.
func MapTime(t float64, cuts []Segment) float64 {
	cuts = mergeOverlapping(cuts)
	for _, c := range cuts {
		if t > c.Start && t < c.End {
			t = c.Start
			break
		}
	}
	removed := 0.0
	for _, c := range cuts {
		if c.End <= t {
			removed += c.End - c.Start
		} else {
			break
		}
	}
	return math.Max(0, t-removed)
}

// MapTimeEnd maps an end timestamp; points inside cuts clamp to cut start mapped.
func MapTimeEnd(t float64, cuts []Segment) float64 {
	return MapTime(t, cuts)
}

// RemapChapters maps native chapters through cuts (Creator timeline always kept).
func RemapChapters(chs []Chapter, cuts []Segment) []Chapter {
	if len(cuts) == 0 {
		return chs
	}
	var out []Chapter
	for _, ch := range chs {
		start := MapTime(ch.Start, cuts)
		end := MapTimeEnd(ch.End, cuts)
		if end <= start+0.05 {
			continue
		}
		out = append(out, Chapter{Start: start, End: end, Title: ch.Title})
	}
	return out
}

// RemapSubtitleCue remaps one cue start/end.
func RemapSubtitleCue(start, end float64, cuts []Segment) (float64, float64, bool) {
	if len(cuts) == 0 {
		return start, end, true
	}
	// Drop cues entirely inside a cut
	for _, c := range cuts {
		if start >= c.Start && end <= c.End {
			return 0, 0, false
		}
	}
	ns := MapTime(start, cuts)
	ne := MapTimeEnd(end, cuts)
	if ne <= ns {
		return 0, 0, false
	}
	return ns, ne, true
}

// MergeMarkChapters merges native chapters with SB mark segments (yt-dlp subset).
func MergeMarkChapters(native []Chapter, marks []Segment) []Chapter {
	var sb []Chapter
	for _, m := range marks {
		title := CategoryDisplayName(m.Category)
		sb = append(sb, Chapter{Start: m.Start, End: m.End, Title: title})
	}
	if len(sb) == 0 {
		return collapseChapters(native)
	}
	if len(native) == 0 {
		return collapseChapters(sb)
	}

	// Split native around SB marks, then merge overlaps / adjacent same title / tiny neighbors.
	events := append([]Chapter(nil), native...)
	// For each SB mark, split overlapping native pieces
	for _, m := range sb {
		var next []Chapter
		for _, n := range events {
			if m.End <= n.Start+0.01 || m.Start >= n.End-0.01 {
				next = append(next, n)
				continue
			}
			// overlap: keep native left/right stubs; SB inserted later
			if n.Start < m.Start-0.01 {
				next = append(next, Chapter{Start: n.Start, End: m.Start, Title: n.Title})
			}
			if n.End > m.End+0.01 {
				next = append(next, Chapter{Start: m.End, End: n.End, Title: n.Title})
			}
		}
		events = next
	}
	events = append(events, sb...)
	sort.Slice(events, func(i, j int) bool {
		if events[i].Start == events[j].Start {
			return events[i].End < events[j].End
		}
		return events[i].Start < events[j].Start
	})
	return collapseChapters(events)
}

func collapseChapters(chs []Chapter) []Chapter {
	if len(chs) == 0 {
		return nil
	}
	sort.Slice(chs, func(i, j int) bool {
		if chs[i].Start == chs[j].Start {
			return chs[i].End < chs[j].End
		}
		return chs[i].Start < chs[j].Start
	})
	out := []Chapter{chs[0]}
	for _, c := range chs[1:] {
		last := &out[len(out)-1]
		// merge overlapping into later title preference for SB-style names when same span
		if c.Start < last.End-0.01 {
			if c.End > last.End {
				last.End = c.End
			}
			if last.Title == "" {
				last.Title = c.Title
			}
			// else keep first title (native usually first after sort by start)
			continue
		}
		// adjacent same title
		if math.Abs(c.Start-last.End) < 0.05 && c.Title == last.Title {
			last.End = c.End
			continue
		}
		// tiny neighbor <1s absorbed into previous
		if c.End-c.Start < 1.0 && c.Start-last.End < 0.05 {
			last.End = c.End
			continue
		}
		out = append(out, c)
	}
	return out
}

// OutputDuration after cuts (without cards).
func OutputDuration(duration float64, cuts []Segment) float64 {
	cuts = mergeOverlapping(cuts)
	removed := 0.0
	for _, c := range cuts {
		removed += c.End - c.Start
	}
	out := duration - removed
	if out < 0 {
		return 0
	}
	return out
}
