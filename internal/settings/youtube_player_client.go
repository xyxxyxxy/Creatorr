package settings

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

// DefaultYoutubePlayerClient is the seed for youtube_player_client (empty = omit
// youtube:player_client and let yt-dlp choose).
const DefaultYoutubePlayerClient = ""

const maxYoutubePlayerClientLen = 120

// NormalizeYoutubePlayerClient trims and collapses commas; empty stays empty.
func NormalizeYoutubePlayerClient(raw string) string {
	parts := splitPlayerClientTokens(raw)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

func validateYoutubePlayerClient(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	if len(v) > maxYoutubePlayerClientLen {
		return fmt.Errorf("youtube_player_client must be at most %d characters", maxYoutubePlayerClientLen)
	}
	if strings.ContainsAny(v, ";=") {
		return fmt.Errorf("youtube_player_client must not contain ; or =")
	}
	parts := splitPlayerClientTokens(v)
	if len(parts) == 0 {
		return fmt.Errorf("youtube_player_client needs at least one client name")
	}
	for _, p := range parts {
		if !validPlayerClientToken(p) {
			return fmt.Errorf("youtube_player_client token %q is invalid (use letters, digits, underscore)", p)
		}
	}
	return nil
}

func splitPlayerClientTokens(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func validPlayerClientToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return false
	}
	return true
}

// EffectiveYoutubePlayerClient returns the value for youtube:player_client=
// (empty means omit the extractor arg).
func EffectiveYoutubePlayerClient(database *db.DB) (string, error) {
	raw, err := Get(database, KeyYoutubePlayerClient)
	if err != nil {
		return DefaultYoutubePlayerClient, err
	}
	return NormalizeYoutubePlayerClient(raw), nil
}
