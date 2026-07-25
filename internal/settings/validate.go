package settings

import (
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
)

func validateValue(key, value string) error {
	if CronKeys[key] {
		return cronexpr.Validate(value)
	}
	if key == KeyStatsRetentionDays {
		return validateStatsRetention(value)
	}
	if key == KeyPotFetch {
		return validatePotFetch(value)
	}
	if key == KeyDownloadWantedOrder {
		return validateDownloadWantedOrder(value)
	}
	if key == KeyDownloadNewOnScan {
		return validateDownloadNewOnScan(value)
	}
	if key == KeyCacheBeginningSeconds {
		return validateCacheBeginningSeconds(value)
	}
	if key == KeyStreamPlaybackCache {
		return validateStreamPlaybackCache(value)
	}
	if key == KeyStreamPlaybackCacheMaxHours {
		return validateStreamPlaybackCacheMaxHours(value)
	}
	if key == KeySourceDownloadErrorThreshold {
		return validateSourceDownloadErrorThreshold(value)
	}
	if key == KeyEpisodeFormat {
		return ValidateEpisodeFormat(value)
	}
	if key == KeyExternalBaseURL {
		// Empty allowed (disables stream). Non-empty is normalized on Set via callers;
		// accept any string (scheme validation left soft for LAN IPs / hostnames).
		return nil
	}
	if key == KeySubtitleLangs {
		return validateSubtitleLangs(value)
	}
	if key == KeySubtitleAuto {
		return validateSubtitleAuto(value)
	}
	return nil
}
