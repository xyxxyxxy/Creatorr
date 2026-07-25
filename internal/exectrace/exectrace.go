// Package exectrace records subprocess argv on a context for worker tasks.
package exectrace

import (
	"context"
	"strconv"
	"strings"
	"unicode"
)

type ctxKey struct{}

// Recorder receives a shell-formatted command line (bin + args).
type Recorder func(line string)

// With attaches a Recorder to ctx. Record is a no-op when absent.
func With(ctx context.Context, r Recorder) context.Context {
	if ctx == nil || r == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, r)
}

// Record formats bin+args and invokes the ctx Recorder when present.
func Record(ctx context.Context, bin string, args ...string) {
	if ctx == nil {
		return
	}
	r, _ := ctx.Value(ctxKey{}).(Recorder)
	if r == nil {
		return
	}
	line := Format(bin, args...)
	if line == "" {
		return
	}
	r(line)
}

// Format returns a shell-copyable command line. Empty bin yields "".
func Format(bin string, args ...string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, quoteArg(bin))
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// FormatPretty is Format with a newline and two-space indent before each -- flag.
func FormatPretty(bin string, args ...string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(quoteArg(bin))
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			b.WriteString("\n  ")
			b.WriteString(quoteArg(a))
		} else {
			b.WriteByte(' ')
			b.WriteString(quoteArg(a))
		}
	}
	return b.String()
}

// Pretty inserts a newline and two-space indent before each " --" in a stored line.
// Quote-aware enough for typical yt-dlp/ffmpeg argv (does not split inside "..." ).
func Pretty(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	var b strings.Builder
	inDouble := false
	inSingle := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(c)
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			b.WriteByte(c)
			continue
		}
		if !inDouble && !inSingle && c == ' ' && i+2 < len(line) && line[i+1] == '-' && line[i+2] == '-' {
			b.WriteString("\n  ")
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	if needsQuote(s) {
		return strconv.Quote(s)
	}
	return s
}

func needsQuote(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '"', '\'', '\\', '$', '`', '!', '&', '|', ';', '<', '>', '(', ')', '{', '}', '[', ']', '*', '?', '~', '#':
			return true
		}
	}
	return false
}
