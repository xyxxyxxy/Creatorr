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
	if key == KeyEpisodeFormat {
		return ValidateEpisodeFormat(value)
	}
	if key == KeySubtitleLangs {
		return validateSubtitleLangs(value)
	}
	if key == KeySubtitleAuto {
		return validateSubtitleAuto(value)
	}
	if key == KeyMetadataDomainTag || key == KeyMetadataGenresFromCategories {
		return validateMetadataFlag(value)
	}
	return nil
}
