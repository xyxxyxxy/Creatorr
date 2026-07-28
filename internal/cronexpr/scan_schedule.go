package cronexpr

import (
	"fmt"
	"strings"
)

// Scan schedule presets (per feed source dropdown).
// Stored value remains a cron expression (or empty for never).
const (
	ScanScheduleHourly    = "hourly"
	ScanScheduleDaily     = "daily"
	ScanScheduleWeekly    = "weekly"
	ScanScheduleMonthly   = "monthly"
	ScanScheduleQuarterly = "quarterly"
	ScanScheduleNever     = "never"

	ScanCronHourly    = "0 * * * *"         // top of every hour UTC
	ScanCronDaily     = "0 3 * * *"         // 03:00 UTC daily
	ScanCronWeekly    = "0 3 * * 0"         // 03:00 UTC Sunday
	ScanCronMonthly   = "0 3 1 * *"         // 03:00 UTC on the 1st
	ScanCronQuarterly = "0 3 1 1,4,7,10 *" // 03:00 UTC Jan/Apr/Jul/Oct 1st
	scanCronYearly    = "0 3 1 1 *"         // legacy; maps to quarterly in UI
)

// ScanScheduleOptions is the fixed dropdown for scan_cron.
func ScanScheduleOptions() []Preset {
	return []Preset{
		{Label: "Hourly", Expr: ScanScheduleHourly},
		{Label: "Daily", Expr: ScanScheduleDaily},
		{Label: "Weekly", Expr: ScanScheduleWeekly},
		{Label: "Monthly", Expr: ScanScheduleMonthly},
		{Label: "Quarterly", Expr: ScanScheduleQuarterly},
		{Label: "Once, then manual", Expr: ScanScheduleNever},
	}
}

// CronFromScanSchedule maps hourly|daily|…|never → cron (empty for never).
// Prefer NormalizeScanCron for form input (aliases + free-form cron).
func CronFromScanSchedule(schedule string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(schedule)) {
	case ScanScheduleNever, "":
		return "", nil
	case ScanScheduleHourly:
		return ScanCronHourly, nil
	case ScanScheduleDaily:
		return ScanCronDaily, nil
	case ScanScheduleWeekly:
		return ScanCronWeekly, nil
	case ScanScheduleMonthly:
		return ScanCronMonthly, nil
	case ScanScheduleQuarterly:
		return ScanCronQuarterly, nil
	default:
		return "", fmt.Errorf("scan schedule must be hourly, daily, weekly, monthly, quarterly, or never")
	}
}

// NormalizeScanCron accepts empty/never (off), legacy schedule aliases, or a valid cron / @ descriptor.
func NormalizeScanCron(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, ScanScheduleNever) {
		return "", nil
	}
	switch strings.ToLower(raw) {
	case ScanScheduleHourly, ScanScheduleDaily, ScanScheduleWeekly, ScanScheduleMonthly, ScanScheduleQuarterly:
		return CronFromScanSchedule(raw)
	}
	if err := Validate(raw); err != nil {
		return "", fmt.Errorf("invalid scan cron: %w", err)
	}
	return raw, nil
}

// DescribeScan is Describe for per-source Scan; empty means never / manual only.
func DescribeScan(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" || strings.EqualFold(expr, ScanScheduleNever) {
		return "Never (manual only)."
	}
	return Describe(expr)
}

// ScanScheduleFromCron maps a stored cron back to the dropdown value.
// Unknown legacy expressions map to hourly so the UI stays on the closed set.
// Deprecated for new UI (free-form cron); kept for tests / compat.
func ScanScheduleFromCron(expr string) string {
	switch strings.TrimSpace(expr) {
	case "", "never":
		return ScanScheduleNever
	case ScanCronHourly, ScanScheduleHourly:
		return ScanScheduleHourly
	case ScanCronDaily, ScanScheduleDaily:
		return ScanScheduleDaily
	case ScanCronWeekly, ScanScheduleWeekly:
		return ScanScheduleWeekly
	case ScanCronMonthly, ScanScheduleMonthly:
		return ScanScheduleMonthly
	case ScanCronQuarterly, ScanScheduleQuarterly:
		return ScanScheduleQuarterly
	case scanCronYearly, "yearly":
		return ScanScheduleQuarterly // yearly removed from UI
	default:
		return ScanScheduleHourly
	}
}
