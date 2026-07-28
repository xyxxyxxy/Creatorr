package settings

import (
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// PO token fetch mode values stored in pot_fetch (youtube:fetch_pot).
const (
	PotFetchAuto   = "auto"
	PotFetchAlways = "always"
	PotFetchNever  = "never"
)

// PotFetchOption is one General settings dropdown row.
type PotFetchOption struct {
	Value string
	Label string
}

// PotFetchOptions is the closed set for pot_fetch.
func PotFetchOptions() []PotFetchOption {
	return []PotFetchOption{
		{Value: PotFetchAuto, Label: "Auto (when needed)"},
		{Value: PotFetchAlways, Label: "Always"},
		{Value: PotFetchNever, Label: "Never"},
	}
}

// NormalizePotFetch maps a stored value to a dropdown option.
func NormalizePotFetch(raw string) string {
	switch strings.TrimSpace(raw) {
	case PotFetchAlways, PotFetchNever:
		return strings.TrimSpace(raw)
	default:
		return PotFetchAuto
	}
}

func validatePotFetch(value string) error {
	v := strings.TrimSpace(value)
	switch v {
	case PotFetchAuto, PotFetchAlways, PotFetchNever:
		return nil
	default:
		return fmt.Errorf("pot_fetch must be auto, always, or never")
	}
}

// EffectivePotFetch returns the fetch mode to pass to yt-dlp.
// When providerURL is empty, always never (avoids plugin hitting localhost).
func EffectivePotFetch(database *db.DB, providerURL string) (string, error) {
	if strings.TrimSpace(providerURL) == "" {
		return PotFetchNever, nil
	}
	raw, err := Get(database, KeyPotFetch)
	if err != nil {
		return PotFetchAuto, err
	}
	return NormalizePotFetch(raw), nil
}
