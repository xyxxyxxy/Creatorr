package settings

import (
	"fmt"
	"strconv"
	"strings"
)

const DefaultSourceDownloadErrorThreshold = 2

// NormalizeSourceDownloadErrorThreshold returns a positive integer string.
// Empty/invalid → default "2"; values below 1 → "1".
func NormalizeSourceDownloadErrorThreshold(raw string) string {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return strconv.Itoa(DefaultSourceDownloadErrorThreshold)
	}
	if n < 1 {
		return "1"
	}
	return strconv.Itoa(n)
}

func validateSourceDownloadErrorThreshold(value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("source_download_error_threshold: must be an integer ≥ 1")
	}
	if n < 1 {
		return fmt.Errorf("source_download_error_threshold: must be an integer ≥ 1")
	}
	return nil
}
