package web

import (
	"fmt"
	"strings"
	"time"
)

// formatAgo returns a static relative past time using at most two units.
// Smallest unit is minutes; under one minute → "just now" (no live UI updates).
func formatAgo(then, now time.Time) string {
	then = then.UTC()
	now = now.UTC()
	if then.After(now) {
		then, now = now, then
	}

	years := 0
	for {
		next := then.AddDate(years+1, 0, 0)
		if next.After(now) {
			break
		}
		years++
	}
	then = then.AddDate(years, 0, 0)

	months := 0
	for {
		next := then.AddDate(0, months+1, 0)
		if next.After(now) {
			break
		}
		months++
	}
	then = then.AddDate(0, months, 0)

	d := now.Sub(then)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)

	type part struct {
		n    int
		one  string
		many string
	}
	var parts []part
	add := func(n int, one, many string) {
		if n > 0 {
			parts = append(parts, part{n, one, many})
		}
	}
	add(years, "year", "years")
	add(months, "month", "months")
	add(days, "day", "days")
	add(hours, "hour", "hours")
	add(minutes, "minute", "minutes")

	if len(parts) == 0 {
		return "just now"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	var b strings.Builder
	for i, p := range parts {
		if i == 1 {
			b.WriteString(" and ")
		}
		label := p.many
		if p.n == 1 {
			label = p.one
		}
		fmt.Fprintf(&b, "%d %s", p.n, label)
	}
	b.WriteString(" ago")
	return b.String()
}

// formatAgoShort returns a compact relative past time (at most two units).
// Examples: "just now", "3 m ago", "1 h 3 m ago", "1 d 2 h ago", "7 d ago", "1 y 4 mo ago".
func formatAgoShort(then, now time.Time) string {
	then = then.UTC()
	now = now.UTC()
	if then.After(now) {
		then, now = now, then
	}

	years := 0
	for {
		next := then.AddDate(years+1, 0, 0)
		if next.After(now) {
			break
		}
		years++
	}
	then = then.AddDate(years, 0, 0)

	months := 0
	for {
		next := then.AddDate(0, months+1, 0)
		if next.After(now) {
			break
		}
		months++
	}
	then = then.AddDate(0, months, 0)

	d := now.Sub(then)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)

	type part struct {
		n int
		u string
	}
	var parts []part
	add := func(n int, u string) {
		if n > 0 {
			parts = append(parts, part{n, u})
		}
	}
	add(years, "y")
	add(months, "mo")
	add(days, "d")
	add(hours, "h")
	add(minutes, "m")

	if len(parts) == 0 {
		return "just now"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d %s", p.n, p.u)
	}
	b.WriteString(" ago")
	return b.String()
}

// formatInShort returns a compact relative future span (at most two units).
// Examples: "now", "3 m", "1 h 3 m", "1 d 2 h".
func formatInShort(now, then time.Time) string {
	now = now.UTC()
	then = then.UTC()
	if !then.After(now) {
		return "now"
	}

	years := 0
	for {
		next := now.AddDate(years+1, 0, 0)
		if next.After(then) {
			break
		}
		years++
	}
	now = now.AddDate(years, 0, 0)

	months := 0
	for {
		next := now.AddDate(0, months+1, 0)
		if next.After(then) {
			break
		}
		months++
	}
	now = now.AddDate(0, months, 0)

	d := then.Sub(now)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)

	type part struct {
		n int
		u string
	}
	var parts []part
	add := func(n int, u string) {
		if n > 0 {
			parts = append(parts, part{n, u})
		}
	}
	add(years, "y")
	add(months, "mo")
	add(days, "d")
	add(hours, "h")
	add(minutes, "m")

	if len(parts) == 0 {
		return "now"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d %s", p.n, p.u)
	}
	return b.String()
}

// parseActivityTime parses RFC3339 / RFC3339Nano activity timestamps.
func parseActivityTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// formatAbsoluteTip is a human absolute time for tooltip data-tip (UTC, no fractional seconds).
func formatAbsoluteTip(t time.Time) string {
	return t.UTC().Format("Jan 2, 2006, 3:04:05 PM UTC")
}

// cooldownWaitTip is the Tasks lane cooldown tooltip / aria-label.
func cooldownWaitTip(remSec int) string {
	if remSec < 1 {
		remSec = 1
	}
	return "Waiting " + formatDurationCompact(time.Duration(remSec)*time.Second)
}

// formatDurationCompact is a short span like "3min 2sec" (at most two units).
func formatDurationCompact(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Second {
		return "1sec"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	type part struct {
		n int
		u string
	}
	var parts []part
	add := func(n int, u string) {
		if n > 0 {
			parts = append(parts, part{n, u})
		}
	}
	add(days, "d")
	add(hours, "h")
	add(minutes, "min")
	if days == 0 {
		add(seconds, "sec")
	}
	if len(parts) == 0 {
		return "1sec"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d%s", p.n, p.u)
	}
	return b.String()
}

// formatDurationProse is a human span like "1 minute and 3 seconds" (at most two units).
func formatDurationProse(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Second {
		return "less than 1 second"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	type part struct {
		n    int
		one  string
		many string
	}
	var parts []part
	add := func(n int, one, many string) {
		if n > 0 {
			parts = append(parts, part{n, one, many})
		}
	}
	add(days, "day", "days")
	add(hours, "hour", "hours")
	add(minutes, "minute", "minutes")
	if days == 0 {
		add(seconds, "second", "seconds")
	}
	if len(parts) == 0 {
		return "less than 1 second"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	var b strings.Builder
	for i, p := range parts {
		if i == 1 {
			b.WriteString(" and ")
		}
		label := p.many
		if p.n == 1 {
			label = p.one
		}
		fmt.Fprintf(&b, "%d %s", p.n, label)
	}
	return b.String()
}

// taskQueuedLabel is created→started wait ("1 minute and 3 seconds queued").
// muted is true when the wait is under one second.
func taskQueuedLabel(created, started string) (label string, muted bool) {
	c, ok := parseActivityTime(created)
	if !ok {
		return "", false
	}
	s, ok := parseActivityTime(started)
	if !ok {
		return "", false
	}
	d := s.Sub(c)
	return formatDurationProse(d) + " queued", d < time.Second
}

// taskRuntimeLabel is started→finished ("2 minutes and 5 seconds runtime").
// muted is true when the runtime is under one second.
func taskRuntimeLabel(started, finished string) (label string, muted bool) {
	s, ok := parseActivityTime(started)
	if !ok {
		return "", false
	}
	f, ok := parseActivityTime(finished)
	if !ok {
		return "", false
	}
	d := f.Sub(s)
	return formatDurationProse(d) + " runtime", d < time.Second
}
