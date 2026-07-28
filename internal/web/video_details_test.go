package web

import "testing"

func TestFormatDetailDuration(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0.4, "<1s"},
		{1, "1s"},
		{62, "1min 2s"},
		{602, "10min 2s"},
		{3661, "1h 1min 1s"},
	}
	for _, tc := range cases {
		got := formatDetailDuration(tc.sec)
		if got != tc.want {
			t.Fatalf("formatDetailDuration(%v)=%q want %q", tc.sec, got, tc.want)
		}
	}
}

func TestFormatDurationClock(t *testing.T) {
	if got := formatDurationClock(65); got != "1:05" {
		t.Fatalf("got %q", got)
	}
	if got := formatDurationClock(3661); got != "1:01:01" {
		t.Fatalf("got %q", got)
	}
	if got := formatDurationClock(0); got != "" {
		t.Fatalf("got %q", got)
	}
}
