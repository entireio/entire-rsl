package cli

// ANSI colours. Every code has the same length, and plain wraps text in the
// terminal's default colour, so a table column whose every cell, header
// included, is styled keeps its alignment: tabwriter counts escape bytes as
// width.
const (
	ansiGreen   = "\x1b[32m"
	ansiRed     = "\x1b[31m"
	ansiYellow  = "\x1b[33m"
	ansiDefault = "\x1b[39m"
	ansiReset   = "\x1b[0m"
)

// styles colours verification outcomes: green for verified or passing, red
// for failed. It applies only to text written to a terminal, and never to
// repository data, which is sanitized first.
type styles struct {
	enabled bool
}

// styles reports how this invocation colours output: only on a terminal,
// and not when NO_COLOR, TERM=dumb or the Entire CLI's ACCESSIBLE mode asks
// for plain text.
func (a *app) styles() styles {
	enabled := a.opts.ColorStdout != nil && a.opts.ColorStdout() &&
		a.opts.Getenv("NO_COLOR") == "" &&
		a.opts.Getenv("TERM") != "dumb" &&
		a.opts.Getenv("ACCESSIBLE") == ""
	return styles{enabled: enabled}
}

func (s styles) wrap(code, text string) string {
	if !s.enabled {
		return text
	}
	return code + text + ansiReset
}

func (s styles) good(text string) string  { return s.wrap(ansiGreen, text) }
func (s styles) bad(text string) string   { return s.wrap(ansiRed, text) }
func (s styles) warn(text string) string  { return s.wrap(ansiYellow, text) }
func (s styles) plain(text string) string { return s.wrap(ansiDefault, text) }
