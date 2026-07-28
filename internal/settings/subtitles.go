package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	KeySubtitleLangs = "subtitle_langs"
	KeySubtitleAuto  = "subtitle_auto"
)

const (
	DefaultSubtitleAuto  = "0"
	DefaultSubtitleLangs = `[]`
)

// SubtitleLangSeed is datalist suggestions: all first, then common codes.
var SubtitleLangSeed = []string{
	"all",
	"en", "en.*",
	"de", "fr", "es", "it", "pt", "nl", "pl", "ru",
	"ja", "ko", "zh", "zh-Hans", "zh-Hant",
}

// NormalizeSubtitleLangs trims, drops empty, dedupes, sorts alpha (all stays sortable).
func NormalizeSubtitleLangs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// SubtitleLangsJSON encodes a normalized lang list as JSON array.
func SubtitleLangsJSON(raw []string) string {
	norm := NormalizeSubtitleLangs(raw)
	if len(norm) == 0 {
		return "[]"
	}
	b, err := json.Marshal(norm)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseSubtitleLangsJSON decodes subtitle_langs (invalid → empty).
func ParseSubtitleLangsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return nil
	}
	return NormalizeSubtitleLangs(vals)
}

// NormalizeSubtitleAuto returns "1" or "0".
func NormalizeSubtitleAuto(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "1" || s == "true" || s == "on" || s == "yes" {
		return "1"
	}
	return "0"
}

// SubtitleOpts is resolved Library subtitle settings for yt-dlp.
// Sidecars are always converted to SRT (--convert-subs srt).
type SubtitleOpts struct {
	Langs []string
	Auto  bool
}

// GetSubtitleOpts loads subtitle settings (defaults when missing).
func GetSubtitleOpts(database *db.DB) (SubtitleOpts, error) {
	langsRaw, err := Get(database, KeySubtitleLangs)
	if err != nil {
		return SubtitleOpts{}, err
	}
	if strings.TrimSpace(langsRaw) == "" {
		langsRaw = DefaultSubtitleLangs
	}
	autoRaw, err := Get(database, KeySubtitleAuto)
	if err != nil {
		return SubtitleOpts{}, err
	}
	return SubtitleOpts{
		Langs: ParseSubtitleLangsJSON(langsRaw),
		Auto:  NormalizeSubtitleAuto(autoRaw) == "1",
	}, nil
}

func validateSubtitleLangs(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(value), &vals); err != nil {
		return fmt.Errorf("subtitle_langs: must be a JSON string array")
	}
	return nil
}

func validateSubtitleAuto(value string) error {
	_ = NormalizeSubtitleAuto(value)
	return nil
}
