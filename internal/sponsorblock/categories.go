package sponsorblock

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultCardDurationSec is the v1 hard-cut info card length.
const DefaultCardDurationSec = 2

// Category display names (yt-dlp / SB wiki).
var categoryNames = map[string]string{
	"sponsor":        "Sponsor",
	"intro":          "Intermission/Intro Animation",
	"outro":          "Endcards/Credits",
	"selfpromo":      "Unpaid/Self Promotion",
	"preview":        "Preview/Recap",
	"filler":         "Filler Tangent",
	"interaction":    "Interaction Reminder",
	"music_offtopic": "Non-Music Section",
	"hook":           "Hook/Greetings",
	"poi_highlight":  "Highlight",
	"chapter":        "Chapter",
}

// MarkCategories are allowed on sponsorblock_mark.
var MarkCategories = []string{
	"sponsor", "selfpromo", "interaction", "intro", "outro", "preview",
	"hook", "filler", "music_offtopic", "poi_highlight", "chapter",
}

// RemoveCategories are allowed on sponsorblock_remove.
var RemoveCategories = []string{
	"sponsor", "selfpromo", "interaction", "intro", "outro", "preview",
	"hook", "filler", "music_offtopic",
}

var markSet = toSet(MarkCategories)
var removeSet = toSet(RemoveCategories)

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// CategoryDisplayName returns the human label for a category.
func CategoryDisplayName(cat string) string {
	if n, ok := categoryNames[cat]; ok {
		return n
	}
	return cat
}

// ParseCategoryListJSON parses a JSON string array of categories.
func ParseCategoryListJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("invalid category JSON: %w", err)
	}
	return NormalizeCategoryList(out), nil
}

// NormalizeCategoryList trims, lowercases, dedupes preserving order.
func NormalizeCategoryList(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ValidateMarkRemove checks allowlists and disjointness.
func ValidateMarkRemove(mark, remove []string) error {
	mark = NormalizeCategoryList(mark)
	remove = NormalizeCategoryList(remove)
	for _, c := range mark {
		if _, ok := markSet[c]; !ok {
			return fmt.Errorf("invalid sponsorblock_mark category %q", c)
		}
	}
	for _, c := range remove {
		if _, ok := removeSet[c]; !ok {
			return fmt.Errorf("invalid sponsorblock_remove category %q", c)
		}
	}
	rm := toSet(remove)
	for _, c := range mark {
		if _, ok := rm[c]; ok {
			return fmt.Errorf("category %q cannot be in both sponsorblock_mark and sponsorblock_remove", c)
		}
	}
	return nil
}

// CategoryListJSON encodes categories as a JSON array string.
func CategoryListJSON(cats []string) string {
	cats = NormalizeCategoryList(cats)
	if len(cats) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(cats)
	return string(b)
}

// SBEnabled reports whether mark or remove is non-empty.
func SBEnabled(mark, remove []string) bool {
	return len(NormalizeCategoryList(mark)) > 0 || len(NormalizeCategoryList(remove)) > 0
}
