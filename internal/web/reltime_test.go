package web

import (
	"testing"
	"time"
)

func TestFormatAgo(t *testing.T) {
	now := time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		then time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-1 * time.Minute), "1 minute ago"},
		{now.Add(-3*time.Minute - 20*time.Second), "3 minutes ago"},
		{now.Add(-1*time.Hour - 3*time.Minute), "1 hour and 3 minutes ago"},
		{now.Add(-2*time.Hour - 45*time.Minute), "2 hours and 45 minutes ago"},
		{now.Add(-26 * time.Hour), "1 day and 2 hours ago"},
		{now.AddDate(0, 0, -7), "7 days ago"},
		{now.AddDate(-1, -4, 0), "1 year and 4 months ago"},
		{now.AddDate(-2, 0, 0), "2 years ago"},
	}
	for _, tc := range cases {
		got := formatAgo(tc.then, now)
		if got != tc.want {
			t.Fatalf("formatAgo(%v)=%q want %q", tc.then, got, tc.want)
		}
	}
}

func TestFormatAgoShort(t *testing.T) {
	now := time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		then time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-1 * time.Minute), "1 m ago"},
		{now.Add(-3*time.Minute - 20*time.Second), "3 m ago"},
		{now.Add(-1*time.Hour - 3*time.Minute), "1 h 3 m ago"},
		{now.Add(-9*time.Hour - 54*time.Minute), "9 h 54 m ago"},
		{now.Add(-26 * time.Hour), "1 d 2 h ago"},
		{now.AddDate(0, 0, -7), "7 d ago"},
		{now.AddDate(-1, -4, 0), "1 y 4 mo ago"},
		{now.AddDate(-2, 0, 0), "2 y ago"},
	}
	for _, tc := range cases {
		got := formatAgoShort(tc.then, now)
		if got != tc.want {
			t.Fatalf("formatAgoShort(%v)=%q want %q", tc.then, got, tc.want)
		}
	}
}

func TestFormatInShort(t *testing.T) {
	now := time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		then time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now, "now"},
		{now.Add(30 * time.Second), "now"},
		{now.Add(1 * time.Minute), "1 m"},
		{now.Add(1*time.Hour + 3*time.Minute), "1 h 3 m"},
		{now.Add(26 * time.Hour), "1 d 2 h"},
		{now.AddDate(0, 0, 7), "7 d"},
	}
	for _, tc := range cases {
		got := formatInShort(now, tc.then)
		if got != tc.want {
			t.Fatalf("formatInShort(%v)=%q want %q", tc.then, got, tc.want)
		}
	}
}

func TestFormatAbsoluteTip(t *testing.T) {
	ts := time.Date(2026, 7, 21, 15, 7, 51, 720616528, time.UTC)
	want := "Jul 21, 2026, 3:07:51 PM UTC"
	got := formatAbsoluteTip(ts)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	abs, ago := createdAgoPair(ts.Format(time.RFC3339Nano), ts.Add(6*time.Minute))
	if abs != want {
		t.Fatalf("createdAgoPair abs=%q", abs)
	}
	if ago != "6 minutes ago" {
		t.Fatalf("ago=%q", ago)
	}
}

func TestFormatDurationProse(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "less than 1 second"},
		{3 * time.Second, "3 seconds"},
		{1*time.Minute + 3*time.Second, "1 minute and 3 seconds"},
		{2*time.Minute + 5*time.Second, "2 minutes and 5 seconds"},
		{1*time.Hour + 8*time.Minute + 3*time.Second, "1 hour and 8 minutes"},
		{26*time.Hour + 10*time.Minute, "1 day and 2 hours"},
	}
	for _, tc := range cases {
		got := formatDurationProse(tc.d)
		if got != tc.want {
			t.Fatalf("formatDurationProse(%v)=%q want %q", tc.d, got, tc.want)
		}
	}
}

func TestTaskQueuedAndRuntimeLabel(t *testing.T) {
	created := time.Date(2026, 7, 21, 20, 43, 46, 0, time.UTC).Format(time.RFC3339Nano)
	started := time.Date(2026, 7, 21, 20, 43, 49, 0, time.UTC).Format(time.RFC3339Nano)
	finished := time.Date(2026, 7, 21, 20, 45, 54, 0, time.UTC).Format(time.RFC3339Nano)
	if got, muted := taskQueuedLabel(created, started); got != "3 seconds queued" || muted {
		t.Fatalf("queued=%q muted=%v", got, muted)
	}
	if got, muted := taskRuntimeLabel(started, finished); got != "2 minutes and 5 seconds runtime" || muted {
		t.Fatalf("runtime=%q muted=%v", got, muted)
	}
	same := time.Date(2026, 7, 21, 20, 43, 49, 100*1e6, time.UTC).Format(time.RFC3339Nano)
	if got, muted := taskQueuedLabel(same, same); got != "less than 1 second queued" || !muted {
		t.Fatalf("subsecond queued=%q muted=%v", got, muted)
	}
	if got, muted := taskRuntimeLabel(same, same); got != "less than 1 second runtime" || !muted {
		t.Fatalf("subsecond runtime=%q muted=%v", got, muted)
	}
	if got, muted := taskQueuedLabel(created, ""); got != "" || muted {
		t.Fatalf("no start=%q muted=%v", got, muted)
	}
	if got, muted := taskRuntimeLabel(started, ""); got != "" || muted {
		t.Fatalf("no finish=%q muted=%v", got, muted)
	}
}
