package library

import (
	"strings"
)

// MergeDomainTag prepends NamingDomain(sourceURL) when missing; no-op when domain unknown.
func MergeDomainTag(tags []string, sourceURL string) []string {
	host := NamingDomain(sourceURL)
	if host == "" {
		return ParseStringListFields(tags)
	}
	return mergeStringListFirst(ParseStringListFields(tags), host)
}

// SeedDomainIntoTags prepends NamingDomain(url) into a tag list when missing.
func SeedDomainIntoTags(tags []string, sourceURL string) []string {
	return MergeDomainTag(tags, sourceURL)
}

// mergeStringListFirst prepends first when missing; moves to index 0 when present elsewhere.
func mergeStringListFirst(items []string, first string) []string {
	first = strings.TrimSpace(first)
	if first == "" {
		return items
	}
	firstFold := strings.ToLower(first)
	var rest []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.ToLower(item) == firstFold {
			continue
		}
		rest = append(rest, item)
	}
	return append([]string{first}, rest...)
}

// OperatorStringListItems returns items not in the managed set (case-insensitive).
func OperatorStringListItems(all, managed []string) []string {
	managed = ParseStringListFields(managed)
	if len(managed) == 0 {
		return ParseStringListFields(all)
	}
	managedFold := make(map[string]struct{}, len(managed))
	for _, m := range managed {
		managedFold[strings.ToLower(m)] = struct{}{}
	}
	var out []string
	for _, item := range ParseStringListFields(all) {
		if _, ok := managedFold[strings.ToLower(item)]; ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ManagedPipe joins managed string-list values for client scripts (| separator).
func ManagedPipe(items []string) string {
	items = ParseStringListFields(items)
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "|")
}

// OrderManagedFirst orders display/save lists with managed values first (single or many).
func OrderManagedFirst(items []string, managed []string) []string {
	managed = ParseStringListFields(managed)
	if len(managed) == 0 {
		return ParseStringListFields(items)
	}
	if len(managed) == 1 {
		return mergeStringListFirst(ParseStringListFields(items), managed[0])
	}
	return MergeCategoryGenres(items, managed)
}
