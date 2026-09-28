// Package exectrace records subprocess argv on a context for worker tasks.
package exectrace

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// RedactedSecret replaces password values in recorded command / log lines.
const RedactedSecret = "********"

type ctxKey struct{}

// Recorder receives the binary name and a shell-formatted command line.
type Recorder func(bin, line string)

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
	r(strings.TrimSpace(bin), line)
}

// Format returns a shell-copyable command line. Empty bin yields "".
// Password flag values are replaced with RedactedSecret before formatting.
func Format(bin string, args ...string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	args = RedactArgs(bin, args)
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, quoteArg(bin))
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// RedactArgs returns a copy of args with password flag values replaced by RedactedSecret.
// Long-form yt-dlp secrets always redact. Short -p only when bin looks like yt-dlp
// (ffmpeg and others use -p for unrelated options).
func RedactArgs(bin string, args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	shortP := looksLikeYtDlp(bin)
	for i := 0; i < len(out); i++ {
		a := out[i]
		switch {
		case isPasswordFlag(a) || (shortP && a == "-p"):
			if i+1 < len(out) && !strings.HasPrefix(out[i+1], "-") {
				out[i+1] = RedactedSecret
				i++
			}
		default:
			if flag, ok := passwordFlagEquals(a); ok {
				out[i] = flag + "=" + RedactedSecret
			}
		}
	}
	return out
}

// RedactLine replaces password values in an already-formatted shell command line
// (task Commands / Logs / History). Safe to call repeatedly.
func RedactLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return line
	}
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "password") && !hasYtDlpShortPassword(line) {
		return line
	}
	tokens := splitShellTokens(line)
	if len(tokens) == 0 {
		return line
	}
	shortP := looksLikeYtDlp(unquoteToken(tokens[0]))
	changed := false
	for i := 0; i < len(tokens); i++ {
		raw := unquoteToken(tokens[i])
		switch {
		case isPasswordFlag(raw) || (shortP && raw == "-p"):
			if i+1 < len(tokens) {
				next := unquoteToken(tokens[i+1])
				if strings.HasPrefix(next, "-") {
					continue
				}
				if tokens[i+1] != RedactedSecret && next != RedactedSecret {
					tokens[i+1] = RedactedSecret
					changed = true
				}
				i++
			}
		default:
			if flag, ok := passwordFlagEquals(raw); ok {
				want := flag + "=" + RedactedSecret
				if tokens[i] != want {
					tokens[i] = want
					changed = true
				}
			}
		}
	}
	if !changed {
		return line
	}
	return strings.Join(tokens, " ")
}

func isPasswordFlag(a string) bool {
	switch a {
	case "--password", "--video-password", "--ap-password":
		return true
	default:
		return false
	}
}

func passwordFlagEquals(a string) (flag string, ok bool) {
	for _, f := range []string{"--password", "--video-password", "--ap-password"} {
		prefix := f + "="
		if strings.HasPrefix(a, prefix) && len(a) > len(prefix) {
			return f, true
		}
	}
	return "", false
}

func looksLikeYtDlp(bin string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(bin)))
	base = strings.TrimSuffix(base, ".exe")
	return strings.Contains(base, "yt-dlp") || base == "ytdlp"
}

func hasYtDlpShortPassword(line string) bool {
	tokens := splitShellTokens(line)
	if len(tokens) == 0 || !looksLikeYtDlp(unquoteToken(tokens[0])) {
		return false
	}
	for _, tok := range tokens[1:] {
		if unquoteToken(tok) == "-p" {
			return true
		}
	}
	return false
}

// Fingerprint returns a dedupe key for a shell-formatted command line by replacing
// absolute filesystem path tokens with $PATH (quote-aware tokenization).
func Fingerprint(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	tokens := splitShellTokens(line)
	for i, tok := range tokens {
		raw := unquoteToken(tok)
		if looksLikeAbsPath(raw) {
			tokens[i] = "$PATH"
		}
	}
	return strings.Join(tokens, " ")
}

func looksLikeAbsPath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "/") && strings.Contains(s[1:], "/") {
		return true
	}
	// Windows drive path.
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return false
}

func unquoteToken(tok string) string {
	if len(tok) >= 2 {
		if (tok[0] == '"' && tok[len(tok)-1] == '"') || (tok[0] == '\'' && tok[len(tok)-1] == '\'') {
			inner := tok[1 : len(tok)-1]
			if tok[0] == '"' {
				if u, err := strconv.Unquote(tok); err == nil {
					return u
				}
			}
			return inner
		}
	}
	return tok
}

func splitShellTokens(line string) []string {
	var out []string
	var b strings.Builder
	inDouble, inSingle := false, false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
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
		if !inDouble && !inSingle && c == ' ' {
			flush()
			continue
		}
		b.WriteByte(c)
	}
	flush()
	return out
}

// FormatPretty is Format with a newline and two-space indent before each - / -- flag.
func FormatPretty(bin string, args ...string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	args = RedactArgs(bin, args)
	var b strings.Builder
	b.WriteString(quoteArg(bin))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			b.WriteString("\n  ")
			b.WriteString(quoteArg(a))
		} else {
			b.WriteByte(' ')
			b.WriteString(quoteArg(a))
		}
	}
	return b.String()
}

// Pretty inserts a newline and two-space indent before each " -" / " --" flag in a stored line.
// Quote-aware enough for typical yt-dlp/ffmpeg argv (does not split inside "..." ).
// Password values in the line are redacted before pretty-printing.
func Pretty(line string) string {
	line = RedactLine(line)
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
		if !inDouble && !inSingle && c == ' ' && i+1 < len(line) && line[i+1] == '-' {
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
