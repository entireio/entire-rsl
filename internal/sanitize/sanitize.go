// Package sanitize makes untrusted strings safe to print to a terminal.
//
// Repository data reaches entire-rsl's text output through ref names, custom
// fields, annotation messages and committer identities, all attacker
// controlled. Printed raw, an escape sequence can rewrite earlier lines,
// change the window title or hide text, and a bidirectional override can make
// one ref name read as another. Every repository-sourced string in text
// output goes through this package. JSON output relies on standard escaping
// instead.
package sanitize

import (
	"strings"
	"unicode/utf8"
)

// replacement stands in for invalid UTF-8, and ellipsis marks a truncation.
const (
	replacement = utf8.RuneError
	ellipsis    = "\u2026"
)

// Line returns s with escape sequences, control characters and invisible
// formatting characters removed, and line breaks and tabs turned into
// spaces, so the result stays on one line.
func Line(s string) string {
	return clean(s, false)
}

// Lines is Line but keeps line feeds, for multi-line text such as
// annotation messages. Carriage returns are dropped so a line cannot be
// overwritten in place.
func Lines(s string) string {
	return clean(s, true)
}

// Truncate shortens s to at most n runes, marking the cut with an ellipsis.
func Truncate(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + ellipsis
}

func clean(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			b.WriteRune(replacement)
			i++
			continue
		}

		if r == 0x1b {
			i += escapeLen(s[i:])
			continue
		}

		switch {
		case r == '\n' && keepNewlines:
			b.WriteRune('\n')
		case r == '\n', r == '\t':
			b.WriteRune(' ')
		case isStripped(r):
		default:
			b.WriteRune(r)
		}
		i += size
	}

	return b.String()
}

// escapeLen returns the length of the escape sequence at the start of s,
// which begins with ESC. Unterminated sequences run to the end of s, so their
// payload is dropped rather than printed.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}

	switch s[1] {
	case '[': // CSI: parameters and intermediates, then a final byte 0x40-0x7E.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']', 'P', '^', '_', 'X': // OSC, DCS, PM, APC, SOS: until BEL or ST.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default: // Two-byte sequence such as ESC c.
		_, size := utf8.DecodeRuneInString(s[1:])
		return 1 + size
	}
}

// isStripped reports whether r is a control or invisible formatting
// character that must not reach the terminal.
func isStripped(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f: // C0 controls and DEL. Newline and tab are handled by the caller.
		return true
	case r >= 0x80 && r <= 0x9f: // C1 controls, including the 8-bit CSI.
		return true
	case r == 0x061c, r == 0x200e, r == 0x200f: // Bidi marks.
		return true
	case r >= 0x202a && r <= 0x202e: // Bidi embeddings and overrides.
		return true
	case r >= 0x2066 && r <= 0x2069: // Bidi isolates.
		return true
	case r == 0x200b, r == 0x2060, r == 0xfeff: // Zero-width space, word joiner, BOM.
		return true
	case r == 0x2028, r == 0x2029: // Line and paragraph separators.
		return true
	}
	return false
}
