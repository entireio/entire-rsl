package sanitize

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "refs/heads/main", want: "refs/heads/main"},
		{name: "unicode kept", in: "caf\u00E9 \u2713 \u65E5\u672C", want: "caf\u00E9 \u2713 \u65E5\u672C"},
		{name: "sgr colour", in: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "cursor movement", in: "ok\x1b[2K\x1b[1Afake", want: "okfake"},
		{name: "osc title with bel", in: "a\x1b]0;pwned\x07b", want: "ab"},
		{name: "osc hyperlink with st", in: "\x1b]8;;https://evil\x1b\\click\x1b]8;;\x1b\\", want: "click"},
		{name: "dcs", in: "a\x1bPq#0;2;0;0;0\x1b\\b", want: "ab"},
		{name: "unterminated csi", in: "a\x1b[31", want: "a"},
		{name: "unterminated osc", in: "a\x1b]0;title", want: "a"},
		{name: "two byte escape", in: "a\x1bcb", want: "ab"},
		{name: "lone escape", in: "a\x1b", want: "a"},
		{name: "c0 controls", in: "a\x00b\x07c\x08d\re", want: "abcde"},
		{name: "del", in: "a\x7fb", want: "ab"},
		{name: "c1 csi", in: "a\u009b31mb", want: "a31mb"},
		{name: "c1 next line", in: "a\u0085b", want: "ab"},
		{name: "newline and tab become spaces", in: "a\nb\tc", want: "a b c"},
		{name: "bidi override", in: "main\u202Egnp.exe", want: "maingnp.exe"},
		{name: "bidi isolate", in: "\u2066x\u2069", want: "x"},
		{name: "bidi marks", in: "a\u200Eb\u200Fc\u061Cd", want: "abcd"},
		{name: "zero width and bom", in: "a\u200Bb\uFEFFc\u2060d", want: "abcd"},
		{name: "line separators", in: "a\u2028b\u2029c", want: "abc"},
		{name: "invalid utf8", in: "a\xffb\xc3", want: "a\uFFFDb\uFFFD"},
		{name: "raw 8-bit csi byte", in: "a\x9b31mb", want: "a\uFFFD31mb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Line(tt.in))
		})
	}
}

func TestLinesKeepsNewlinesOnly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "line one\nline two two", Lines("line one\r\nline two\x1b[1A\ttwo"))
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc", Truncate("abc", 3))
	assert.Equal(t, "ab\u2026", Truncate("abcd", 3))
	assert.Equal(t, "\u65E5\u672C\u2026", Truncate("\u65E5\u672C\u8A9E\u3067\u3059", 3))
	assert.Equal(t, "abcd", Truncate("abcd", 0))
}

func FuzzLine(f *testing.F) {
	for _, seed := range []string{"refs/heads/main", "\x1b[31mx", "\x1b]0;t\x07", "\u202E", "a\xffb", "\x1b", "\x1bP\x1b\\"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		out := Line(in)

		assert.True(t, utf8.ValidString(out))
		for _, r := range out {
			assert.False(t, isStripped(r), "stripped rune %U survived", r)
			assert.NotEqual(t, rune(0x1b), r)
			assert.NotEqual(t, '\n', r)
		}
	})
}
