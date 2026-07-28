package sponsorblock

import "testing"

func TestValidateMarkRemoveDisjoint(t *testing.T) {
	if err := ValidateMarkRemove([]string{"intro"}, []string{"sponsor"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMarkRemove([]string{"sponsor"}, []string{"sponsor"}); err == nil {
		t.Fatal("expected overlap error")
	}
	if err := ValidateMarkRemove([]string{"poi_highlight"}, []string{"sponsor"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMarkRemove(nil, []string{"poi_highlight"}); err == nil {
		t.Fatal("poi not removable")
	}
}

func TestMapTime(t *testing.T) {
	cuts := []Segment{{Start: 10, End: 20, Category: "sponsor"}}
	if g := MapTime(5, cuts); g != 5 {
		t.Fatalf("before cut: got %v", g)
	}
	if g := MapTime(15, cuts); g != 10 {
		t.Fatalf("inside cut: got %v want 10", g)
	}
	if g := MapTime(25, cuts); g != 15 {
		t.Fatalf("after cut: got %v want 15", g)
	}
}

func TestFilterPOIAndZero(t *testing.T) {
	segs := []Segment{
		{Category: "sponsor", ActionType: "skip", Start: 0, End: 0, VideoDuration: 100},
		{Category: "poi_highlight", ActionType: "poi", Start: 50, End: 50},
		{Category: "sponsor", ActionType: "skip", Start: 10, End: 20, VideoDuration: 100},
	}
	opt := FilterOpts{DurationSec: 100}
	rm := FilterForRemove(segs, []string{"sponsor"}, opt)
	if len(rm) < 1 {
		t.Fatalf("remove got %d %#v", len(rm), rm)
	}
	// [0,0] → full? actually yt-dlp treats [0,0] specially - we expand to full duration which merges with 10-20
	// After merge overlapping may become one span 0-100 - check
	mk := FilterForMark(segs, []string{"poi_highlight"}, opt)
	if len(mk) != 1 || mk[0].End-mk[0].Start < 0.9 {
		t.Fatalf("poi mark: %#v", mk)
	}
}

func TestDurationMismatchDrop(t *testing.T) {
	segs := []Segment{{Category: "sponsor", ActionType: "skip", Start: 1, End: 2, VideoDuration: 50}}
	rm := FilterForRemove(segs, []string{"sponsor"}, FilterOpts{DurationSec: 100})
	if len(rm) != 0 {
		t.Fatalf("expected drop on mismatch, got %#v", rm)
	}
}

func TestMergeMarkChaptersSplit(t *testing.T) {
	native := []Chapter{{Start: 0, End: 60, Title: "Full"}}
	marks := []Segment{{Category: "sponsor", Start: 10, End: 20}}
	out := MergeMarkChapters(native, marks)
	if len(out) < 2 {
		t.Fatalf("expected split, got %#v", out)
	}
}

func TestKeepRanges(t *testing.T) {
	cuts := []Segment{{Start: 10, End: 20}}
	k := KeepRanges(50, cuts)
	if len(k) != 2 || k[0] != [2]float64{0, 10} || k[1] != [2]float64{20, 50} {
		t.Fatalf("%#v", k)
	}
}

func TestPlaybackDurationCards(t *testing.T) {
	p := PlanFromCuts("abc", []Segment{{Start: 10, End: 20, Category: "sponsor"}}, true, 2, 100)
	d := PlaybackDuration(100, p)
	// 100-10 + 2 = 92
	if d < 91.9 || d > 92.1 {
		t.Fatalf("got %v", d)
	}
}

func TestFormatSkipDuration(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0.2, "1 sec"},
		{30, "30 sec"},
		{64, "1 min 4 sec"},
		{120, "2 min"},
		{3661, "1 h 1 min 1 sec"},
	}
	for _, tc := range cases {
		if got := FormatSkipDuration(tc.sec); got != tc.want {
			t.Fatalf("FormatSkipDuration(%v)=%q want %q", tc.sec, got, tc.want)
		}
	}
}

func TestCardText(t *testing.T) {
	got := CardText("sponsor", 64)
	want := "SponsorBlock\nskipped 1 min 4 sec of Sponsor"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
