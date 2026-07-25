package cronexpr_test

import (
	"testing"

	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
)

func TestScanScheduleRoundTrip(t *testing.T) {
	for _, opt := range cronexpr.ScanScheduleOptions() {
		cron, err := cronexpr.CronFromScanSchedule(opt.Expr)
		if err != nil {
			t.Fatalf("%s: %v", opt.Expr, err)
		}
		if cron != "" {
			if err := cronexpr.Validate(cron); err != nil {
				t.Fatalf("%s invalid cron %q: %v", opt.Expr, cron, err)
			}
		}
		got := cronexpr.ScanScheduleFromCron(cron)
		if got != opt.Expr {
			t.Fatalf("FromCron(%q)=%q want %q", cron, got, opt.Expr)
		}
	}
}

func TestScanScheduleFromCronLegacy(t *testing.T) {
	if got := cronexpr.ScanScheduleFromCron("*/30 * * * *"); got != cronexpr.ScanScheduleHourly {
		t.Fatalf("legacy */30 → hourly, got %q", got)
	}
	if got := cronexpr.ScanScheduleFromCron(""); got != cronexpr.ScanScheduleNever {
		t.Fatalf("empty → never, got %q", got)
	}
	if got := cronexpr.ScanScheduleFromCron("0 3 1 1 *"); got != cronexpr.ScanScheduleQuarterly {
		t.Fatalf("legacy yearly → quarterly, got %q", got)
	}
}

func TestNormalizeScanCron(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"never", ""},
		{"weekly", cronexpr.ScanCronWeekly},
		{"@weekly", "@weekly"},
		{"@annually", "@annually"},
		{"0 3 * * *", "0 3 * * *"},
	}
	for _, tc := range cases {
		got, err := cronexpr.NormalizeScanCron(tc.in)
		if err != nil {
			t.Fatalf("NormalizeScanCron(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeScanCron(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	if _, err := cronexpr.NormalizeScanCron("not a cron"); err == nil {
		t.Fatal("want error")
	}
	if got := cronexpr.DescribeScan(""); got != "Never (manual only)." {
		t.Fatalf("DescribeScan empty: %q", got)
	}
}

func TestCronFromScanScheduleReject(t *testing.T) {
	if _, err := cronexpr.CronFromScanSchedule("biweekly"); err == nil {
		t.Fatal("want error")
	}
	if _, err := cronexpr.CronFromScanSchedule("yearly"); err == nil {
		t.Fatal("yearly removed; want error")
	}
}
