package app

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// TestPainter proves the ANSI wrapper: with colour on each token is
// wrapped in its code and reset, with colour off the token passes through
// byte-identically, so the rendered report keeps its plain form.
func TestPainter(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		on   bool
		p    func(p painter) string
		want string
	}{
		{name: "green off", p: func(p painter) string { return p.green("OK") }, want: "OK"},
		{name: "red off", p: func(p painter) string { return p.red("CONTENT_CONFLICT") }, want: "CONTENT_CONFLICT"},
		{name: "yellow off", p: func(p painter) string { return p.yellow("a.md") }, want: "a.md"},
		{name: "cyan off", p: func(p painter) string { return p.cyan("generation 18") }, want: "generation 18"},
		{name: "green on", on: true, p: func(p painter) string { return p.green("OK") }, want: "\x1b[32mOK\x1b[0m"},
		{name: "red on", on: true, p: func(p painter) string { return p.red("CONTENT_CONFLICT") }, want: "\x1b[31mCONTENT_CONFLICT\x1b[0m"},
		{name: "yellow on", on: true, p: func(p painter) string { return p.yellow("a.md") }, want: "\x1b[33ma.md\x1b[0m"},
		{name: "cyan on", on: true, p: func(p painter) string { return p.cyan("generation 18") }, want: "\x1b[36mgeneration 18\x1b[0m"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := row.p(painter{on: row.on}); got != row.want {
				t.Fatalf("paint() = %q, want %q", got, row.want)
			}
		})
	}
}

// TestColourEnabled proves the colour gate: colour needs a writer whose
// mode is a character device (a real terminal) and an unset or empty
// NO_COLOR. Pipes, buffers, and a non-empty NO_COLOR stay plain.
func TestColourEnabled(t *testing.T) {
	t.Parallel()
	char := charDevice(t)
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() = %v", err)
	}
	defer pipeR.Close()
	defer pipeW.Close()
	for _, row := range []struct {
		name string
		out  io.Writer
		env  []string
		want bool
	}{
		{name: "character device is a terminal", out: char, want: true},
		{name: "NO_COLOR disables a terminal", out: char, env: []string{"NO_COLOR=1"}, want: false},
		{name: "empty NO_COLOR keeps a terminal", out: char, env: []string{"NO_COLOR="}, want: true},
		{name: "a pipe is not a terminal", out: pipeW, want: false},
		{name: "a buffer is not an os.File", out: &bytes.Buffer{}, want: false},
		{name: "nil writer", out: nil, want: false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := colourEnabled(row.out, row.env); got != row.want {
				t.Fatalf("colourEnabled() = %v, want %v", got, row.want)
			}
		})
	}
}

// charDevice opens a character-device file for the colour gating tests.
// The host must provide one; Windows has no /dev/null character device,
// so the test names that platform capability gap and skips there.
func charDevice(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("colour gating requires a character device: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
