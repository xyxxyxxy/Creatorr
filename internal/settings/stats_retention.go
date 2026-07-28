package settings

import (
	"fmt"
	"strings"
)

// Stats retention dropdown values stored in stats_retention_days.
const (
	StatsRetentionThreeMonths = "90"
	StatsRetentionYear        = "365"
	StatsRetentionForever     = "-1"
)

// StatsRetentionOption is one General settings dropdown row.
type StatsRetentionOption struct {
	Value string
	Label string
}

// StatsRetentionOptions is the closed set for stats_retention_days.
func StatsRetentionOptions() []StatsRetentionOption {
	return []StatsRetentionOption{
		{Value: StatsRetentionThreeMonths, Label: "3 months"},
		{Value: StatsRetentionYear, Label: "1 year"},
		{Value: StatsRetentionForever, Label: "Forever"},
	}
}

// NormalizeStatsRetention maps a stored value to a dropdown option.
// Unknown and legacy values (0, 7, 30, …) map to 1 year.
func NormalizeStatsRetention(raw string) string {
	v := strings.TrimSpace(raw)
	switch v {
	case StatsRetentionThreeMonths, StatsRetentionYear, StatsRetentionForever:
		return v
	default:
		return StatsRetentionYear
	}
}

// ParseStatsRetentionDays returns days to keep: -1 = forever, else N days.
// Unknown values default to 365 (1 year).
func ParseStatsRetentionDays(raw string) int {
	switch NormalizeStatsRetention(raw) {
	case StatsRetentionThreeMonths:
		return 90
	case StatsRetentionYear:
		return 365
	case StatsRetentionForever:
		return -1
	default:
		return 365
	}
}

func validateStatsRetention(value string) error {
	v := strings.TrimSpace(value)
	switch v {
	case StatsRetentionThreeMonths, StatsRetentionYear, StatsRetentionForever:
		return nil
	default:
		return fmt.Errorf("stats_retention_days must be 90, 365, or -1 (forever)")
	}
}
