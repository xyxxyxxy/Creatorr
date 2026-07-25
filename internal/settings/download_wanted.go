package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
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

// DownloadNewOnScan reports whether tip scans should enqueue new wanted downloads.
// Missing/empty defaults to true (on).
func DownloadNewOnScan(database *db.DB) (bool, error) {
	raw, err := Get(database, KeyDownloadNewOnScan)
	if err != nil {
		return true, err
	}
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return true, nil
	}
	return s == "1" || s == "true" || s == "on" || s == "yes", nil
}

func validateDownloadNewOnScan(value string) error {
	s := strings.TrimSpace(strings.ToLower(value))
	switch s {
	case "0", "1", "true", "false", "on", "off", "yes", "no", "":
		return nil
	default:
		return fmt.Errorf("download_new_on_scan must be 0 or 1")
	}
}

// NormalizeDownloadNewOnScan maps form/storage to "0" or "1".
func NormalizeDownloadNewOnScan(raw string) string {
	if DownloadNewOnScanValue(raw) {
		return "1"
	}
	return "0"
}

// DownloadNewOnScanValue parses a raw setting string (empty = on).
func DownloadNewOnScanValue(raw string) bool {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return true
	}
	return s == "1" || s == "true" || s == "on" || s == "yes"
}

// ParsePositiveInt parses an integer ≥ 1 from a form field.
func ParsePositiveInt(raw, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be an integer ≥ 1", label)
	}
	return n, nil
}
