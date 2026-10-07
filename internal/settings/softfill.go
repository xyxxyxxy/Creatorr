package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xyxxyxxy/Creatorr/internal/db"
)

const (
	KeySoftFillTags      = "softfill_tags"
	KeySoftFillGenres    = "softfill_genres"
	KeySoftFillDomainTag = "softfill_domain_tag"
	KeySoftFillBlocklist = "softfill_blocklist"
)

const (
	DefaultSoftFillTags      = "1"
	DefaultSoftFillGenres    = "1"
	DefaultSoftFillDomainTag = "1"
	DefaultSoftFillBlocklist = "{}"
)

// CatalogField keys for softfill_blocklist JSON and Catalog UI.
const (
	CatalogFieldStudio     = "studio"
	CatalogFieldGenres     = "genres"
	CatalogFieldTags       = "tags"
	CatalogFieldCountry    = "country"
	CatalogFieldMPAA       = "mpaa"
	CatalogFieldActorName  = "actor_name"
	CatalogFieldActorRole  = "actor_role"
)

// ValidCatalogField reports a known Catalog / SoftFill-block field key.
func ValidCatalogField(field string) bool {
	switch field {
	case CatalogFieldStudio, CatalogFieldGenres, CatalogFieldTags,
		CatalogFieldCountry, CatalogFieldMPAA, CatalogFieldActorName, CatalogFieldActorRole:
		return true
	default:
		return false
	}
}

func softFillFlagEnabled(database *db.DB, key, defaultVal string) (bool, error) {
	raw, err := Get(database, key)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(raw) == "" {
		return NormalizeMetadataFlag(defaultVal) == "1", nil
	}
	return NormalizeMetadataFlag(raw) == "1", nil
}

// SoftFillTagsEnabled reports whether site/info.json tags SoftFill is on.
func SoftFillTagsEnabled(database *db.DB) (bool, error) {
	return softFillFlagEnabled(database, KeySoftFillTags, DefaultSoftFillTags)
}

// SoftFillGenresEnabled reports whether categories→genres SoftFill is on.
func SoftFillGenresEnabled(database *db.DB) (bool, error) {
	return softFillFlagEnabled(database, KeySoftFillGenres, DefaultSoftFillGenres)
}

// SoftFillDomainTagEnabled reports whether domain hostname is seeded into source tags.
func SoftFillDomainTagEnabled(database *db.DB) (bool, error) {
	return softFillFlagEnabled(database, KeySoftFillDomainTag, DefaultSoftFillDomainTag)
}

// SoftFillBlocklist is field → blocked SoftFill values (case-insensitive match).
type SoftFillBlocklist map[string][]string

// ParseSoftFillBlocklistJSON decodes softfill_blocklist (invalid → empty).
func ParseSoftFillBlocklistJSON(raw string) SoftFillBlocklist {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return SoftFillBlocklist{}
	}
	var m map[string][]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return SoftFillBlocklist{}
	}
	out := SoftFillBlocklist{}
	for field, vals := range m {
		if !ValidCatalogField(field) {
			continue
		}
		norm := normalizeBlockValues(vals)
		if len(norm) > 0 {
			out[field] = norm
		}
	}
	return out
}

// SoftFillBlocklistJSON encodes a blocklist.
func SoftFillBlocklistJSON(bl SoftFillBlocklist) string {
	if len(bl) == 0 {
		return "{}"
	}
	clean := SoftFillBlocklist{}
	for field, vals := range bl {
		if !ValidCatalogField(field) {
			continue
		}
		norm := normalizeBlockValues(vals)
		if len(norm) > 0 {
			clean[field] = norm
		}
	}
	if len(clean) == 0 {
		return "{}"
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func normalizeBlockValues(raw []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func validateSoftFillBlocklist(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var m map[string][]string
	if err := json.Unmarshal([]byte(value), &m); err != nil {
		return fmt.Errorf("softfill_blocklist: must be a JSON object of string arrays")
	}
	for field := range m {
		if !ValidCatalogField(field) {
			return fmt.Errorf("softfill_blocklist: unknown field %q", field)
		}
	}
	return nil
}

// GetSoftFillBlocklist loads the SoftFill blocklist.
func GetSoftFillBlocklist(database *db.DB) (SoftFillBlocklist, error) {
	raw, err := Get(database, KeySoftFillBlocklist)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		raw = DefaultSoftFillBlocklist
	}
	return ParseSoftFillBlocklistJSON(raw), nil
}

// IsSoftFillBlocked reports whether value is SoftFill-blocked for field (case-insensitive).
func IsSoftFillBlocked(database *db.DB, field, value string) (bool, error) {
	bl, err := GetSoftFillBlocklist(database)
	if err != nil {
		return false, err
	}
	return bl.Blocked(field, value), nil
}

// Blocked reports a case-insensitive SoftFill block hit.
func (bl SoftFillBlocklist) Blocked(field, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !ValidCatalogField(field) {
		return false
	}
	key := strings.ToLower(value)
	for _, v := range bl[field] {
		if strings.ToLower(strings.TrimSpace(v)) == key {
			return true
		}
	}
	return false
}

// ListSoftFillBlocks returns blocked values for one field.
func ListSoftFillBlocks(database *db.DB, field string) ([]string, error) {
	if !ValidCatalogField(field) {
		return nil, fmt.Errorf("unknown catalog field %q", field)
	}
	bl, err := GetSoftFillBlocklist(database)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), bl[field]...), nil
}

// AddSoftFillBlock adds a SoftFill block (idempotent, case-insensitive).
func AddSoftFillBlock(database *db.DB, field, value string) error {
	if !ValidCatalogField(field) {
		return fmt.Errorf("unknown catalog field %q", field)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value required")
	}
	bl, err := GetSoftFillBlocklist(database)
	if err != nil {
		return err
	}
	if bl == nil {
		bl = SoftFillBlocklist{}
	}
	if bl.Blocked(field, value) {
		return nil
	}
	bl[field] = append(bl[field], value)
	return Set(database, KeySoftFillBlocklist, SoftFillBlocklistJSON(bl))
}

// RemoveSoftFillBlock removes a SoftFill block (case-insensitive).
func RemoveSoftFillBlock(database *db.DB, field, value string) error {
	if !ValidCatalogField(field) {
		return fmt.Errorf("unknown catalog field %q", field)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value required")
	}
	bl, err := GetSoftFillBlocklist(database)
	if err != nil {
		return err
	}
	key := strings.ToLower(value)
	vals := bl[field]
	out := vals[:0]
	for _, v := range vals {
		if strings.ToLower(strings.TrimSpace(v)) == key {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		delete(bl, field)
	} else {
		bl[field] = out
	}
	return Set(database, KeySoftFillBlocklist, SoftFillBlocklistJSON(bl))
}

// MoveSoftFillBlock renames a SoftFill block entry from→to (case-insensitive).
func MoveSoftFillBlock(database *db.DB, field, from, to string) error {
	if !ValidCatalogField(field) {
		return fmt.Errorf("unknown catalog field %q", field)
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return fmt.Errorf("from and to required")
	}
	bl, err := GetSoftFillBlocklist(database)
	if err != nil {
		return err
	}
	if !bl.Blocked(field, from) {
		return nil
	}
	fromKey := strings.ToLower(from)
	vals := bl[field]
	out := make([]string, 0, len(vals))
	seenTo := false
	toKey := strings.ToLower(to)
	for _, v := range vals {
		vk := strings.ToLower(strings.TrimSpace(v))
		if vk == fromKey {
			continue
		}
		if vk == toKey {
			seenTo = true
		}
		out = append(out, v)
	}
	if !seenTo {
		out = append(out, to)
	}
	if len(out) == 0 {
		delete(bl, field)
	} else {
		bl[field] = out
	}
	return Set(database, KeySoftFillBlocklist, SoftFillBlocklistJSON(bl))
}

// FilterSoftFillBlockedStrings drops SoftFill-blocked values (preserves order).
func FilterSoftFillBlockedStrings(bl SoftFillBlocklist, field string, values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if bl.Blocked(field, v) {
			continue
		}
		out = append(out, v)
	}
	return out
}
