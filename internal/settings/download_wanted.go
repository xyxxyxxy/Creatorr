package settings

import (
	"fmt"
	"strconv"
	"strings"
)

// Download wanted order: newest or oldest release first within each series.
const (
	DownloadWantedOrderNewest = "newest"
	DownloadWantedOrderOldest = "oldest"
)

// DownloadWantedOrderOption is one General settings dropdown row.
type DownloadWantedOrderOption struct {
	Value string
	Label string
}

// DownloadWantedOrderOptions is the closed set for download_wanted_order.
func DownloadWantedOrderOptions() []DownloadWantedOrderOption {
	return []DownloadWantedOrderOption{
		{Value: DownloadWantedOrderOldest, Label: "Oldest first"},
		{Value: DownloadWantedOrderNewest, Label: "Newest first"},
	}
}

// NormalizeDownloadWantedOrder maps a stored value to a known option.
func NormalizeDownloadWantedOrder(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case DownloadWantedOrderNewest:
		return DownloadWantedOrderNewest
	default:
		return DownloadWantedOrderOldest
	}
}

func validateDownloadWantedOrder(value string) error {
	v := strings.TrimSpace(strings.ToLower(value))
	if v != DownloadWantedOrderNewest && v != DownloadWantedOrderOldest {
		return fmt.Errorf("download_wanted_order must be newest or oldest")
	}
	return nil
}

// DefaultMaxDownloadQueue is the seed / fallback for max_download_queue on domains.
const DefaultMaxDownloadQueue = 8

// ParsePositiveInt parses an integer ≥ 1 from a form field.
func ParsePositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be an integer ≥ 1", label)
	}
	return n, nil
}
