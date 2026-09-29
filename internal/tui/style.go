// Package tui is the shared presentation of every human-facing command:
// the slivingdoc palette, the status marks, aligned columns, the progress
// spinner and the list picker (architecture/tui.md). Each piece renders
// to plain text when its stream is not a terminal or NO_COLOR is set, so
// pipes and CI see no escape sequence at all.
package tui

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Mode is whether a stream gets the styled or the plain rendering.
type Mode int

const (
	// Plain renders text only: no colour, no mark, no spinner.
	Plain Mode = iota
	// Styled renders the palette, the marks and the spinner.
	Styled
)

// Depth is how many colours a styled stream shows.
type Depth int

const (
	// Basic is the 16-colour ANSI palette every terminal understands.
	Basic Depth = iota
	// TrueColor is the 24-bit palette of slivingdoc.dev, announced by
	// COLORTERM=truecolor or COLORTERM=24bit.
	TrueColor
)

// Style renders the tokens of one stream. The zero value is plain.
type Style struct {
	mode  Mode
	depth Depth
}

// New returns a style with the given mode and depth.
func New(mode Mode, depth Depth) Style { return Style{mode: mode, depth: depth} }

// Detect chooses the style of out: styled only when out is a real terminal
// (IsTerminal) and NO_COLOR is unset or empty, following the NO_COLOR convention.
// getenv reads the process environment.
func Detect(out io.Writer, getenv func(string) string) Style {
	if getenv("NO_COLOR") != "" || !IsTerminal(out) {
		return Style{}
	}
	depth := Basic
	switch getenv("COLORTERM") {
	case "truecolor", "24bit":
		depth = TrueColor
	}
	return Style{mode: Styled, depth: depth}
}

// IsTerminal reports whether out is a real terminal: it has a file
// descriptor (an *os.File, or a Guard over one) that the terminal ioctl
// accepts. A pipe, a file, a buffer and /dev/null are not.
func IsTerminal(out io.Writer) bool {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok || f == (*os.File)(nil) {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Mode reports whether the style renders styled or plain.
func (s Style) Mode() Mode { return s.mode }

// hue is one palette colour in both depths.
type hue struct {
	basic string
	true  string
}

// The palette of slivingdoc.dev (the site's --glow, --ok and --warn
// tokens, plus a failure red of slivingdoc's own), with the nearest
// 16-colour code for terminals that do not announce true colour.
var (
	hueBrand = hue{basic: "\x1b[34m", true: "\x1b[38;2;90;162;255m"}
	hueGood  = hue{basic: "\x1b[32m", true: "\x1b[38;2;25;184;118m"}
	hueWarn  = hue{basic: "\x1b[33m", true: "\x1b[38;2;240;160;60m"}
	hueBad   = hue{basic: "\x1b[31m", true: "\x1b[38;2;242;109;109m"}
)

const (
	codeDim   = "\x1b[2m"
	codeBold  = "\x1b[1m"
	codeReset = "\x1b[0m"
)

// Brand paints the slivingdoc blue: headers, labels, the next step.
func (s Style) Brand(t string) string { return s.hue(t, hueBrand) }

// Good paints success green.
func (s Style) Good(t string) string { return s.hue(t, hueGood) }

// Warn paints the amber of a warning or a conflicted path.
func (s Style) Warn(t string) string { return s.hue(t, hueWarn) }

// Bad paints failure red.
func (s Style) Bad(t string) string { return s.hue(t, hueBad) }

// Dim paints secondary detail.
func (s Style) Dim(t string) string { return s.code(t, codeDim) }

// Bold paints what the reader must check, such as an account.
func (s Style) Bold(t string) string { return s.code(t, codeBold) }

func (s Style) hue(t string, h hue) string {
	if s.depth == TrueColor {
		return s.code(t, h.true)
	}
	return s.code(t, h.basic)
}

func (s Style) code(t, code string) string {
	if s.mode != Styled || t == "" {
		return t
	}
	return code + t + codeReset
}

// Mark is a status mark that leads a line.
type Mark int

const (
	// Done marks a finished step or a success.
	Done Mark = iota
	// Failed marks an error.
	Failed
	// Caution marks a warning the reader must heed.
	Caution
	// Next marks the next step to take.
	Next
	// Brand marks the slivingdoc header.
	Brand
)

// Mark returns the mark and a space in its colour when styled, and
// nothing when plain, so a plain line reads as it did without marks.
func (s Style) Mark(m Mark) string {
	if s.mode != Styled {
		return ""
	}
	switch m {
	case Done:
		return s.Good("✓") + " "
	case Failed:
		return s.Bad("✗") + " "
	case Caution:
		return s.Warn("▲") + " "
	case Next:
		return s.Brand("→") + " "
	default:
		return s.Brand("◆") + " "
	}
}

// Header is the slivingdoc header line of a command, "◆ slivingdoc
// <command> · <detail>", when styled; plain output has no header, so it
// returns the empty string.
func (s Style) Header(command, detail string) string {
	if s.mode != Styled {
		return ""
	}
	line := s.Mark(Brand) + s.Brand("slivingdoc")
	if command != "" {
		line += " " + s.Bold(command)
	}
	if detail != "" {
		line += " " + s.Dim("· "+detail)
	}
	return line + "\n"
}
