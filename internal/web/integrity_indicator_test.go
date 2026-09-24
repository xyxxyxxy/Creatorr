package web

import (
	"testing"
	"time"
)

func TestIntegrityIndicatorState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		status                 string
		verify, hash, schedule bool
		want                   string
	}{
		{"failed overrides all", "integrity_check_failed", true, true, true, integrityIndFailed},
		{"failed even verify off", "integrity_check_failed", false, false, false, integrityIndFailed},
		{"wanted off", "wanted", true, true, true, integrityIndOff},
		{"missing off", "missing", true, true, true, integrityIndOff},
		{"downloaded verify off", "downloaded", false, true, true, integrityIndOff},
		{"eligible no hash", "downloaded", true, false, true, integrityIndEligible},
		{"hashed schedule off", "downloaded", true, true, false, integrityIndHashed},
		{"monitored", "downloaded", true, true, true, integrityIndMonitored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := integrityIndicatorState(tc.status, tc.verify, tc.hash, tc.schedule)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIntegrityScheduleOn(t *testing.T) {
	t.Parallel()
	if integrityScheduleOn("") || integrityScheduleOn("  ") || integrityScheduleOn("never") || integrityScheduleOn("Never") {
		t.Fatal("empty/never must be off")
	}
	if !integrityScheduleOn("@quarterly") || !integrityScheduleOn("0 0 1 */3 *") {
		t.Fatal("cron must be on")
	}
}

func TestIntegrityIndicatorTipLastChecked(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 19, 18, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Hour).Format(time.RFC3339)
	tip := integrityIndicatorTip(integrityIndMonitored, "downloaded", true, last, now)
	if tip != "'File integrity' monitored · Last checked 2 hours ago" {
		t.Fatalf("tip=%q", tip)
	}
	noLast := integrityIndicatorTip(integrityIndEligible, "downloaded", true, "", now)
	if noLast != "'File integrity' eligible; no hash yet" {
		t.Fatalf("noLast=%q", noLast)
	}
	offPacked := integrityIndicatorTip(integrityIndOff, "downloaded", false, "", now)
	if offPacked != "This series quality profile has 'File integrity' turned off" {
		t.Fatalf("offPacked=%q", offPacked)
	}
	offWanted := integrityIndicatorTip(integrityIndOff, "wanted", true, "", now)
	if offWanted != "No packed media" {
		t.Fatalf("offWanted=%q", offWanted)
	}
	// Off / eligible ignore stale last-checked history (no current media hash).
	offStale := integrityIndicatorTip(integrityIndOff, "downloaded", false, last, now)
	if offStale != offPacked {
		t.Fatalf("off must omit last-checked, got %q", offStale)
	}
	eligibleStale := integrityIndicatorTip(integrityIndEligible, "downloaded", true, last, now)
	if eligibleStale != "'File integrity' eligible; no hash yet" {
		t.Fatalf("eligible must omit last-checked, got %q", eligibleStale)
	}
}
