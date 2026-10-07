package settings

import (
	"github.com/xyxxyxxy/Creatorr/internal/cronexpr"
)

func validateValue(key, value string) error {
	if CronKeys[key] {
		return cronexpr.Validate(value)
	}
	if key == KeyPotFetch {
		return validatePotFetch(value)
	}
	if key == KeyYoutubePlayerClient {
		return validateYoutubePlayerClient(value)
	}
	if key == KeySubtitleLangs {
		return validateSubtitleLangs(value)
	}
	if key == KeySubtitleAuto {
		return validateSubtitleAuto(value)
	}
	if key == KeyArchiveFallback || key == KeySoftFillTags || key == KeySoftFillGenres || key == KeySoftFillDomainTag {
		return validateMetadataFlag(value)
	}
	if key == KeySoftFillBlocklist {
		return validateSoftFillBlocklist(value)
	}
	return nil
}
