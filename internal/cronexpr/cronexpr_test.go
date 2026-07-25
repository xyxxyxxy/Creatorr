package cronexpr_test

import (
	"testing"
	"time"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
)

func TestValidate(t *testing.T) {
	if err := cronexpr.Validate(""); err != nil {
		t.Fatal(err)
	}
	if err := cronexpr.Validate("0 3 * * *"); err != nil {
		t.Fatal(err)
	}
	if err := cronexpr.Validate("not a cron"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDescriptors(t *testing.T) {
	for _, d := range cronexpr.Descriptors() {
		if err := cronexpr.Validate(d); err != nil {
			t.Fatalf("Validate(%q): %v", d, err)
		}
		if got := cronexpr.Describe(d); got == "" || got == "Custom schedule." || got == "Invalid cron." {
			t.Fatalf("Describe(%q)=%q", d, got)
		}
	}
	labels := cronexpr.DescriptorLabels()
	if len(labels) != len(cronexpr.Descriptors()) {
		t.Fatalf("labels=%d descriptors=%d", len(labels), len(cronexpr.Descriptors()))
	}
	for _, d := range cronexpr.ScanDescriptors() {
		if err := cronexpr.Validate(d); err != nil {
			t.Fatalf("Scan Validate(%q): %v", d, err)
		}
	}
	if len(cronexpr.ScanDescriptors()) != len(cronexpr.Descriptors()) {
		t.Fatal("ScanDescriptors should match Descriptors")
	}
}

func TestDescribe(t *testing.T) {
	utc := time.UTC
	cases := []struct {
		in, want string
	}{
		{"", "Off (empty)."},
		{"  ", "Off (empty)."},
		{"*/30 * * * *", "Every 30 minutes"},
		{"0 3 * * *", "Daily (03:00 UTC)"},
		{"@hourly", "Hourly (top of every hour)"},
		{"@daily", "Daily (00:00 UTC)"},
		{"@weekly", "Weekly (Sunday 00:00 UTC)"},
		{"not a cron", "Invalid cron."},
		{"5 4 * * *", "Custom schedule."},
		{cronexpr.ScanCronWeekly, "Weekly (Sunday 03:00 UTC)"},
	}
	for _, tc := range cases {
		if got := cronexpr.DescribeIn(tc.in, utc); got != tc.want {
			t.Fatalf("DescribeIn(%q, UTC)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestDescribeLocalOffset(t *testing.T) {
	loc := time.FixedZone("CEST", 2*60*60)
	got := cronexpr.DescribeIn("@weekly", loc)
	want := "Weekly (Sunday 02:00 CEST)"
	if got != want {
		t.Fatalf("DescribeIn(@weekly, CEST)=%q want %q", got, want)
	}
	got = cronexpr.DescribeIn("0 3 * * *", loc)
	want = "Daily (05:00 CEST)"
	if got != want {
		t.Fatalf("DescribeIn(daily 03 UTC, CEST)=%q want %q", got, want)
	}
	// US Pacific: Sunday 00:00 UTC → Saturday evening.
	la := time.FixedZone("PST", -8*60*60)
	got = cronexpr.DescribeIn("@weekly", la)
	want = "Weekly (Saturday 16:00 PST)"
	if got != want {
		t.Fatalf("DescribeIn(@weekly, PST)=%q want %q", got, want)
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 7, 18, 3, 0, 5, 0, time.UTC)
	ok, err := cronexpr.Due("0 3 * * *", time.Time{}, now)
	if err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	last := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	ok, err = cronexpr.Due("0 3 * * *", last, now)
	if err != nil || ok {
		// next fire is tomorrow 03:00
		t.Fatalf("same day after fire: due=%v err=%v", ok, err)
	}
	tomorrow := time.Date(2026, 7, 19, 3, 0, 1, 0, time.UTC)
	ok, err = cronexpr.Due("0 3 * * *", last, tomorrow)
	if err != nil || !ok {
		t.Fatalf("next day: %v %v", ok, err)
	}
}

func TestNext(t *testing.T) {
	last := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	next, err := cronexpr.Next("0 3 * * *", last)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 19, 3, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next=%v want %v", next, want)
	}
	zero, err := cronexpr.Next("", last)
	if err != nil || !zero.IsZero() {
		t.Fatalf("empty: %v %v", zero, err)
	}
	zero, err = cronexpr.Next("0 3 * * *", time.Time{})
	if err != nil || !zero.IsZero() {
		t.Fatalf("zero after: %v %v", zero, err)
	}
}
