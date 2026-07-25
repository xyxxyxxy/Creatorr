package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	// DefaultStreamPlaybackCache is on (1).
	DefaultStreamPlaybackCache = "1"
	// DefaultStreamPlaybackCacheMaxHours is the seed / fallback for the budget slider.
	DefaultStreamPlaybackCacheMaxHours = 20
	// MinStreamPlaybackCacheMaxHours is the slider minimum.
	MinStreamPlaybackCacheMaxHours = 10
	// MaxStreamPlaybackCacheMaxHours is the slider maximum.
	MaxStreamPlaybackCacheMaxHours = 100
	// StreamPlaybackCacheMaxHoursStep is the slider / validation step.
	StreamPlaybackCacheMaxHoursStep = 10
)

// StreamPlaybackCacheEnabled reports whether progressive on-play caching is on.
func StreamPlaybackCacheEnabled(database *db.DB) (bool, error) {
	raw, err := Get(database, KeyStreamPlaybackCache)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(raw) == "1", nil
}

func validateStreamPlaybackCache(value string) error {
	v := strings.TrimSpace(value)
	if v != "0" && v != "1" {
		return fmt.Errorf("stream_playback_cache must be 0 or 1")
	}
	return nil
}

// StreamPlaybackCacheMaxHours returns the rolling progressive content budget in hours.
func StreamPlaybackCacheMaxHours(database *db.DB) (int, error) {
	raw, err := Get(database, KeyStreamPlaybackCacheMaxHours)
	if err != nil {
		return DefaultStreamPlaybackCacheMaxHours, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < MinStreamPlaybackCacheMaxHours {
		return DefaultStreamPlaybackCacheMaxHours, nil
	}
	if n > MaxStreamPlaybackCacheMaxHours {
		return MaxStreamPlaybackCacheMaxHours, nil
	}
	return n, nil
}

func validateStreamPlaybackCacheMaxHours(value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < MinStreamPlaybackCacheMaxHours || n > MaxStreamPlaybackCacheMaxHours || n%StreamPlaybackCacheMaxHoursStep != 0 {
		return fmt.Errorf("stream_playback_cache_max_hours must be a multiple of %d between %d and %d",
			StreamPlaybackCacheMaxHoursStep, MinStreamPlaybackCacheMaxHours, MaxStreamPlaybackCacheMaxHours)
	}
	return nil
}

// NormalizeStreamPlaybackCacheMaxHours snaps to the nearest valid step (10–100, step 10).
func NormalizeStreamPlaybackCacheMaxHours(raw string) string {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < MinStreamPlaybackCacheMaxHours {
		return strconv.Itoa(DefaultStreamPlaybackCacheMaxHours)
	}
	if n > MaxStreamPlaybackCacheMaxHours {
		n = MaxStreamPlaybackCacheMaxHours
	}
	step := StreamPlaybackCacheMaxHoursStep
	n = ((n + step/2) / step) * step
	if n < MinStreamPlaybackCacheMaxHours {
		n = MinStreamPlaybackCacheMaxHours
	}
	if n > MaxStreamPlaybackCacheMaxHours {
		n = MaxStreamPlaybackCacheMaxHours
	}
	return strconv.Itoa(n)
}
