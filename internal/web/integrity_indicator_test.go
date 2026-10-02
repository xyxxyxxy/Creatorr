package web

import (
	"testing"
	"time"
)

func TestIntegrityIndicatorState(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		verify   bool
		hash     bool
		schedule bool
		fileFail bool
		want     string
	}{
		{"failed overrides all", "downloaded_integrity_failed", true, true, true, false, integrityIndFailed},
		{"failed even verify off", "downloaded_integrity_failed", false, false, false, false, integrityIndFailed},
		{"derived file fail", "downloaded", true, true, true, true, integrityIndFailed},
		{"wanted off", "wanted", true, true, true, false, integrityIndOff},
		{"missing off", "missing", true, true, true, false, integrityIndOff},
		{"downloaded verify off", "downloaded", false, true, true, false, integrityIndOff},
		{"eligible no hash", "downloaded", true, false, true, false, integrityIndEligible},
		{"hashed schedule off", "downloaded", true, true, false, false, integrityIndHashed},
		{"monitored", "downloaded", true, true, true, false, integrityIndMonitored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := integrityIndicatorState(tc.status, tc.verify, tc.hash, tc.schedule, tc.fileFail)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIntegrityScheduleOn(t *testing.T) {
	if integrityScheduleOn("") || integrityScheduleOn("  ") || integrityScheduleOn("never") || integrityScheduleOn("Never") {
		t.Fatal("empty/never should be off")
	}
	if !integrityScheduleOn("@quarterly") || !integrityScheduleOn("0 0 1 */3 *") {
		t.Fatal("cron should be on")
	}
}

func TestIntegrityIndicatorTipStateOnly(t *testing.T) {
	tip := integrityIndicatorTip(integrityIndMonitored, "downloaded", true)
	if tip != "'File integrity' monitored" {
		t.Fatalf("tip=%q", tip)
	}
	noLast := integrityIndicatorTip(integrityIndEligible, "downloaded", true)
	if noLast != "'File integrity' eligible; no hash yet" {
		t.Fatalf("tip=%q", noLast)
	}
	offPacked := integrityIndicatorTip(integrityIndOff, "downloaded", false)
	if offPacked != "This series quality profile has 'File integrity' turned off" {
		t.Fatalf("tip=%q", offPacked)
	}
	offWanted := integrityIndicatorTip(integrityIndOff, "wanted", true)
	if offWanted != "No packed media" {
		t.Fatalf("tip=%q", offWanted)
	}
}

func TestBuildIntegrityIndicatorViewLastOK(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Hour).Format(time.RFC3339)
	v := buildIntegrityIndicatorView(integrityIndMonitored, "downloaded", true, last, now)
	if v.Tip != "'File integrity' monitored" {
		t.Fatalf("tip=%q", v.Tip)
	}
	if v.LastOK == "" {
		t.Fatal("want LastOK relative")
	}
	empty := buildIntegrityIndicatorView(integrityIndEligible, "downloaded", true, "", now)
	if empty.LastOK != "" {
		t.Fatalf("LastOK=%q", empty.LastOK)
	}
}
