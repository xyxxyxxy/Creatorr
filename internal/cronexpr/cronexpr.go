// Package cronexpr validates and evaluates standard 5-field cron expressions.
package cronexpr

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Validate returns nil if expr is empty (schedule off) or a valid standard cron string.
func Validate(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	_, err := cron.ParseStandard(expr)
	if err != nil {
		return fmt.Errorf("invalid cron %q: %w", expr, err)
	}
	return nil
}

// Due reports whether a fire should run at now given the last successful run time.
// Empty expr = never due. Zero last = due (never ran in this process).
// Schedulers that must not catch up missed fires after downtime should set last
// to process start before the first tick (see internal/scheduler).
func Due(expr string, last, now time.Time) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, nil
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return false, fmt.Errorf("invalid cron %q: %w", expr, err)
	}
	if last.IsZero() {
		return true, nil
	}
	next := sched.Next(last)
	return !next.After(now), nil
}

// Next returns the next fire time after `after` for expr.
// Empty/invalid expr or zero after → zero time (caller treats as unknown / due).
// Fire times are evaluated in UTC (stored cron fields are UTC wall clock).
func Next(expr string, after time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || after.IsZero() {
		return time.Time{}, nil
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron %q: %w", expr, err)
	}
	return sched.Next(after.UTC()), nil
}

// Descriptors are robfig @… shortcuts for Settings cron datalists (not @reboot / yearly).
func Descriptors() []string {
	return []string{
		"@hourly",
		"@daily",
		"@midnight",
		"@weekly",
		"@monthly",
	}
}

// ScanDescriptors are @… suggestions for per-source Scan (same set as Settings).
func ScanDescriptors() []string {
	return Descriptors()
}

// DescriptorLabels maps each Descriptors() value to Describe text (for UI datalist / JS).
func DescriptorLabels() map[string]string {
	return labelsFor(Descriptors())
}

// ScanDescriptorLabels maps ScanDescriptors() to Describe text.
func ScanDescriptorLabels() map[string]string {
	return labelsFor(ScanDescriptors())
}

func labelsFor(exprs []string) map[string]string {
	out := make(map[string]string, len(exprs))
	for _, d := range exprs {
		out[d] = Describe(d)
	}
	return out
}

// Presets are legacy 5-field examples still recognized by Describe.
// Labels use process local time (see Describe); cron fields stay UTC.
func Presets() []Preset {
	return presetsIn(time.Local)
}

func presetsIn(loc *time.Location) []Preset {
	loc = displayLoc(loc)
	return []Preset{
		{Label: "Every 15 minutes", Expr: "*/15 * * * *"},
		{Label: "Every 30 minutes", Expr: "*/30 * * * *"},
		{Label: "Hourly", Expr: "0 * * * *"},
		{Label: "Every 6 hours", Expr: "0 */6 * * *"},
		{Label: "Every 12 hours", Expr: "0 */12 * * *"},
		{Label: labelDaily(3, 0, loc), Expr: "0 3 * * *"},
		{Label: labelDaily(4, 0, loc), Expr: "0 4 * * *"},
		{Label: labelDaily(6, 0, loc), Expr: "0 6 * * *"},
		{Label: labelWeekly(0, 4, 0, loc), Expr: "0 4 * * 0"},
		{Label: labelWeekly(1, 3, 0, loc), Expr: "0 3 * * 1"},
	}
}

// Describe returns a short human-readable summary for a cron expression.
// Empty → schedule off. Known @-descriptors and legacy 5-field presets use fixed labels.
// Clock faces are shown in process local time (TZ); fires stay UTC.
// Invalid → error text. Other valid expressions → "Custom schedule."
func Describe(expr string) string {
	return DescribeIn(expr, time.Local)
}

// DescribeIn is Describe with an explicit display location (nil → UTC).
func DescribeIn(expr string, loc *time.Location) string {
	loc = displayLoc(loc)
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "Off (empty)."
	}
	switch strings.ToLower(expr) {
	case "@hourly":
		return "Hourly (top of every hour)"
	case "@daily", "@midnight":
		return labelDailyParen(0, 0, loc)
	case "@weekly":
		return labelWeeklyParen(0, 0, 0, loc)
	case "@monthly":
		return labelMonthlyParen(0, 0, loc)
	case "@yearly", "@annually":
		return labelYearlyParen(0, 0, loc)
	case ScanCronHourly:
		return "Hourly (top of every hour)"
	case ScanCronDaily:
		return labelDailyParen(3, 0, loc)
	case ScanCronWeekly:
		return labelWeeklyParen(0, 3, 0, loc)
	case ScanCronMonthly:
		return labelMonthlyAtParen(3, 0, loc)
	case ScanCronQuarterly:
		return labelQuarterlyParen(3, 0, loc)
	}
	for _, p := range presetsIn(loc) {
		if p.Expr == expr {
			return p.Label
		}
	}
	if err := Validate(expr); err != nil {
		return "Invalid cron."
	}
	return "Custom schedule."
}

func displayLoc(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}

// utcWall is a UTC instant used only to render the matching local wall clock.
func utcWall(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, time.UTC)
}

func labelDaily(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.June, 15, hour, min).In(loc)
	return "Daily " + t.Format("15:04 MST")
}

func labelDailyParen(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.June, 15, hour, min).In(loc)
	return "Daily (" + t.Format("15:04 MST") + ")"
}

// dow: 0=Sunday … 6=Saturday (robfig standard). Sample week: 2024-01-07 is Sunday UTC.
func labelWeekly(dow, hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.January, 7+dow, hour, min).In(loc)
	return "Weekly " + t.Format("Monday 15:04 MST")
}

func labelWeeklyParen(dow, hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.January, 7+dow, hour, min).In(loc)
	return "Weekly (" + t.Format("Monday 15:04 MST") + ")"
}

func labelMonthlyParen(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.June, 1, hour, min).In(loc)
	if t.Day() == 1 {
		return "Monthly (1st " + t.Format("15:04 MST") + ")"
	}
	return "Monthly (" + t.Format("Jan 2 15:04 MST") + ")"
}

func labelMonthlyAtParen(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.June, 1, hour, min).In(loc)
	if t.Day() == 1 {
		return "Monthly (1st " + t.Format("15:04 MST") + ")"
	}
	return "Monthly (" + t.Format("Jan 2 15:04 MST") + ")"
}

func labelYearlyParen(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.January, 1, hour, min).In(loc)
	return "Yearly (" + t.Format("Jan 2 15:04 MST") + ")"
}

func labelQuarterlyParen(hour, min int, loc *time.Location) string {
	t := utcWall(2024, time.January, 1, hour, min).In(loc)
	return "Quarterly (1st Jan/Apr/Jul/Oct " + t.Format("15:04 MST") + ")"
}

// Preset is one cron select option.
type Preset struct {
	Label string
	Expr  string
}
