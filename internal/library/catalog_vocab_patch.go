package library

import (
	"strings"
)

func patchCatalogScalar(raw, from, to string, remove bool) (string, bool) {
	cur := strings.TrimSpace(raw)
	if cur == "" || !catalogFoldMatch(cur, from) {
		return raw, false
	}
	if remove {
		return "", true
	}
	return strings.TrimSpace(to), true
}

func patchCatalogStringList(raw, from, to string, remove bool) (string, bool) {
	items := decodeStringSlice(raw)
	if len(items) == 0 {
		return raw, false
	}
	changed := false
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if catalogFoldMatch(item, from) {
			changed = true
			if remove {
				continue
			}
			item = strings.TrimSpace(to)
			if item == "" {
				continue
			}
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			changed = true
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if !changed {
		return raw, false
	}
	return encodeStringSlice(out), true
}

func patchCatalogActors(raw, field, from, to string, remove bool) (string, bool) {
	actors := decodeActors(raw)
	if len(actors) == 0 {
		return raw, false
	}
	changed := false
	var out []SeriesActor
	switch field {
	case CatalogFieldActorName:
		for _, a := range actors {
			if catalogFoldMatch(a.Name, from) {
				changed = true
				if remove {
					continue
				}
				a.Name = strings.TrimSpace(to)
				if a.Name == "" {
					continue
				}
			}
			out = append(out, a)
		}
		if changed {
			out = normalizeActorsList(out)
		}
	case CatalogFieldActorRole:
		for _, a := range actors {
			if catalogFoldMatch(a.Role, from) {
				changed = true
				if remove {
					a.Role = ""
				} else {
					a.Role = strings.TrimSpace(to)
				}
			}
			out = append(out, a)
		}
	default:
		return raw, false
	}
	if !changed {
		return raw, false
	}
	return encodeActors(out), true
}
