package sponsorblock

import (
	"strings"
	"testing"
)

func TestReportAndClearProg(t *testing.T) {
	var last *float64
	var calls int
	on := func(f *float64) {
		calls++
		last = f
	}
	reportProg(on, 0.5)
	if calls != 1 || last == nil || *last != 0.5 {
		t.Fatalf("report: calls=%d last=%v", calls, last)
	}
	clearProg(on)
	if calls != 2 || last != nil {
		t.Fatalf("clear: calls=%d last=%v", calls, last)
	}
	reportProg(nil, 1)
	clearProg(nil)
}

func TestParseFFmpegProgressLine(t *testing.T) {
	cases := []struct {
		line string
		sec  float64
		ok   bool
	}{
		// out_time_ms is misnamed: values are microseconds (same as out_time_us).
		{"out_time_ms=1500000", 1.5, true},
		{"out_time_us=2500000", 2.5, true},
		{"out_time=00:01:04.500000", 64.5, true},
		{"progress=continue", 0, false},
		{"out_time_ms=N/A", 0, false},
	}
	for _, tc := range cases {
		sec, ok := parseFFmpegProgressLine(tc.line)
		if ok != tc.ok {
			t.Fatalf("%q ok=%v want %v", tc.line, ok, tc.ok)
		}
		if ok && sec != tc.sec {
			t.Fatalf("%q sec=%v want %v", tc.line, sec, tc.sec)
		}
	}
}

func TestWithFFmpegProgressArgsDedupes(t *testing.T) {
	got := withFFmpegProgressArgs([]string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", "in.mkv", "-c", "copy", "out.mkv",
	})
	joined := strings.Join(got, " ")
	for _, want := range []string{"-progress", "pipe:1", "-nostats", "-i", "in.mkv"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	nY, nLog := 0, 0
	for _, g := range got {
		if g == "-y" {
			nY++
		}
		if g == "-loglevel" {
			nLog++
		}
	}
	if nY != 1 || nLog != 1 {
		t.Fatalf("dup flags: %s", joined)
	}
}
