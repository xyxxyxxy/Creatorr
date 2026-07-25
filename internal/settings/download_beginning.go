package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// DefaultCacheBeginningSeconds is the seed / fallback for cache_beginning_seconds (0 = off).
const DefaultCacheBeginningSeconds = 20

// MaxCacheBeginningSeconds is the upper bound for the setting.
const MaxCacheBeginningSeconds = 120

// CacheBeginningStepSeconds is the slider / validation step.
const CacheBeginningStepSeconds = 10

// CacheBeginningSeconds returns how many seconds of stream media to cache (0 = off).
func CacheBeginningSeconds(database *db.DB) (int, error) {
	raw, err := Get(database, KeyCacheBeginningSeconds)
	if err != nil {
		return DefaultCacheBeginningSeconds, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return DefaultCacheBeginningSeconds, nil
	}
	if n > MaxCacheBeginningSeconds {
		return MaxCacheBeginningSeconds, nil
	}
	return n, nil
}

func validateCacheBeginningSeconds(value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 || n > MaxCacheBeginningSeconds || n%CacheBeginningStepSeconds != 0 {
		return fmt.Errorf("cache_beginning_seconds must be a multiple of %d between 0 and %d", CacheBeginningStepSeconds, MaxCacheBeginningSeconds)
	}
	return nil
}

// NormalizeCacheBeginningSeconds snaps a stored value to the nearest valid step (0–120, step 10).
func NormalizeCacheBeginningSeconds(raw string) string {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return strconv.Itoa(DefaultCacheBeginningSeconds)
	}
	if n > MaxCacheBeginningSeconds {
		n = MaxCacheBeginningSeconds
	}
	// Round to nearest step (ties up).
	step := CacheBeginningStepSeconds
	n = ((n + step/2) / step) * step
	if n > MaxCacheBeginningSeconds {
		n = MaxCacheBeginningSeconds
	}
	return strconv.Itoa(n)
}
