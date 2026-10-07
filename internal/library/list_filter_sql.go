package library

import "strings"

func appendAndOrGroup(b *strings.Builder, parts []string) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		b.WriteString(` AND ` + parts[0])
		return
	}
	b.WriteString(` AND (`)
	for i, p := range parts {
		if i > 0 {
			b.WriteString(` OR `)
		}
		b.WriteString(p)
	}
	b.WriteString(`)`)
}

func trimNonEmptyStrings(vals []string) []string {
	if len(vals) == 0 {
		return nil
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func uniqPositiveInt64s(vals []int64) []int64 {
	if len(vals) == 0 {
		return nil
	}
	out := make([]int64, 0, len(vals))
	seen := map[int64]struct{}{}
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func uniqNonZeroInts(vals []int) []int {
	if len(vals) == 0 {
		return nil
	}
	out := make([]int, 0, len(vals))
	seen := map[int]struct{}{}
	for _, v := range vals {
		if v == 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func appendStringsIn(b *strings.Builder, args *[]any, expr string, vals []string, nocase bool) {
	vals = trimNonEmptyStrings(vals)
	if len(vals) == 0 {
		return
	}
	if len(vals) == 1 {
		if nocase {
			b.WriteString(` AND ` + expr + ` = ? COLLATE NOCASE`)
		} else {
			b.WriteString(` AND ` + expr + ` = ?`)
		}
		*args = append(*args, vals[0])
		return
	}
	if nocase {
		b.WriteString(` AND ` + expr + ` COLLATE NOCASE IN (` + sqlIntPlaceholders(len(vals)) + `)`)
	} else {
		b.WriteString(` AND ` + expr + ` IN (` + sqlIntPlaceholders(len(vals)) + `)`)
	}
	for _, v := range vals {
		*args = append(*args, v)
	}
}

func appendInt64In(b *strings.Builder, args *[]any, expr string, vals []int64) {
	vals = uniqPositiveInt64s(vals)
	if len(vals) == 0 {
		return
	}
	if len(vals) == 1 {
		b.WriteString(` AND ` + expr + ` = ?`)
		*args = append(*args, vals[0])
		return
	}
	b.WriteString(` AND ` + expr + ` IN (` + sqlIntPlaceholders(len(vals)) + `)`)
	for _, v := range vals {
		*args = append(*args, v)
	}
}

func appendIntsIn(b *strings.Builder, args *[]any, expr string, vals []int) {
	vals = uniqNonZeroInts(vals)
	if len(vals) == 0 {
		return
	}
	if len(vals) == 1 {
		b.WriteString(` AND ` + expr + ` = ?`)
		*args = append(*args, vals[0])
		return
	}
	b.WriteString(` AND ` + expr + ` IN (` + sqlIntPlaceholders(len(vals)) + `)`)
	for _, v := range vals {
		*args = append(*args, v)
	}
}
