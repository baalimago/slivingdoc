package app

import (
	"io"
	"os"
)

// ANSI SGR colour codes of the CLI report. The codes are the standard
// sequences: green for the success token and insertions, red for the
// error token and deletions, yellow for conflict paths, cyan for the
// success generation summary. Colour is presentation-only; the report
// keeps its plain form everywhere else (architecture section 2 CLI
// report).
const (
	colourGreen  = "\x1b[32m"
	colourRed    = "\x1b[31m"
	colourYellow = "\x1b[33m"
	colourCyan   = "\x1b[36m"
	colourReset  = "\x1b[0m"
)

// painter wraps one report token in an ANSI colour when enabled. With
// colour off the token passes through unchanged, so the rendered report
// is byte-identical to the plain text form.
type painter struct {
	on bool
}

func (p painter) green(s string) string  { return p.paint(s, colourGreen) }
func (p painter) red(s string) string    { return p.paint(s, colourRed) }
func (p painter) yellow(s string) string { return p.paint(s, colourYellow) }
func (p painter) cyan(s string) string   { return p.paint(s, colourCyan) }

func (p painter) paint(s, code string) string {
	if !p.on {
		return s
	}
	return code + s + colourReset
}

// colourEnabled reports whether the CLI report may emit ANSI escapes: out
// must be a real terminal — an *os.File whose mode is a character device,
// not a pipe or a redirected file — and NO_COLOR must be unset or empty,
// following the NO_COLOR convention.
func colourEnabled(out io.Writer, env []string) bool {
	if environ(env)[logEnvNoColor] != "" {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
