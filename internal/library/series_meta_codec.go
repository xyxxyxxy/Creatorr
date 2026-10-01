package library

import (
	"encoding/json"
	"strings"
)

func encodeStringSlice(ss []string) string {
	if ss == nil {
		ss = []string{}
	}
	b, _ := json.Marshal(ss)
	return string(b)
}

func decodeStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func encodeActors(actors []SeriesActor) string {
	if actors == nil {
		actors = []SeriesActor{}
	}
	b, _ := json.Marshal(actors)
	return string(b)
}

func decodeActors(raw string) []SeriesActor {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []SeriesActor
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ParseCSVList splits comma-separated values (trim, drop empty).
func ParseCSVList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseStringListFields builds a string list from repeated form fields (genres/tags editors).
// Duplicate values (case-insensitive) are dropped; first wins.
func ParseStringListFields(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, v := range values {
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

// ParseActorsForm parses lines "Name" or "Name|Role".
func ParseActorsForm(raw string) []SeriesActor {
	var out []SeriesActor
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, role, _ := strings.Cut(line, "|")
		name = strings.TrimSpace(name)
		role = strings.TrimSpace(role)
		if name == "" {
			continue
		}
		out = append(out, SeriesActor{Name: name, Role: role, Order: i})
	}
	return out
}

// ParseActorsFromFields builds actors from parallel name/role form fields (UI add/remove editor).
// Duplicate names (case-insensitive) are dropped; first wins.
func ParseActorsFromFields(names, roles []string) []SeriesActor {
	n := len(names)
	if len(roles) > n {
		n = len(roles)
	}
	var out []SeriesActor
	seen := map[string]struct{}{}
	for i := 0; i < n; i++ {
		name := ""
		role := ""
		if i < len(names) {
			name = strings.TrimSpace(names[i])
		}
		if i < len(roles) {
			role = strings.TrimSpace(roles[i])
		}
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, SeriesActor{Name: name, Role: role, Order: len(out)})
	}
	return out
}

// FormatActorsForm is the inverse of ParseActorsForm.
func FormatActorsForm(actors []SeriesActor) string {
	var lines []string
	for _, a := range actors {
		if strings.TrimSpace(a.Name) == "" {
			continue
		}
		if strings.TrimSpace(a.Role) != "" {
			lines = append(lines, a.Name+"|"+a.Role)
		} else {
			lines = append(lines, a.Name)
		}
	}
	return strings.Join(lines, "\n")
}
