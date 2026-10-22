package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// maskedCase is one input for the masked reader and the bytes and line it
// must produce.
type maskedCase struct {
	name  string
	input string
	// out is the drawing, one '*' per accepted character, with the line
	// terminator the raw terminal needs.
	out  string
	line string
	err  error
}

func TestReadMaskedLine(t *testing.T) {
	t.Parallel()
	for _, row := range []maskedCase{
		{name: "typed line ends at carriage return", input: "abc\r", out: "***\r\n", line: "abc"},
		{name: "newline ends the line too", input: "abc\n", out: "***\r\n", line: "abc"},
		{name: "empty answer is an empty line", input: "\r", out: "\r\n"},
		{name: "delete erases character and star", input: "ab\x7fc\r", out: "**\b \b*\r\n", line: "ac"},
		{name: "backspace erases too", input: "ab\x08c\r", out: "**\b \b*\r\n", line: "ac"},
		{name: "erase on an empty line draws nothing", input: "\x7f\r", out: "\r\n"},
		{name: "multi byte rune draws one star and erases in one step", input: "\xc3\xa9\x7f\r", out: "*\b \b\r\n"},
		{name: "multi byte rune is kept whole", input: "a\xc3\xa9\r", out: "**\r\n", line: "a\u00e9"},
		{name: "incomplete rune is not drawn and is dropped", input: "a\xc3\r", out: "*\r\n", line: "a"},
		{name: "erase drops an incomplete rune without an erase", input: "a\xc3\x7f\r", out: "*\r\n", line: "a"},
		{name: "invalid byte is one masked character", input: "\x80\r", out: "*\r\n", line: "\x80"},
		{name: "arrow key sequence is dropped", input: "a\x1b[Ab\r", out: "**\r\n", line: "ab"},
		{name: "cursor sequence with parameters is dropped", input: "\x1b[1;5Cx\r", out: "*\r\n", line: "x"},
		{name: "function key sequence is dropped", input: "\x1bOPy\r", out: "*\r\n", line: "y"},
		{name: "stray escape drops one byte", input: "\x1bxb\r", out: "*\r\n", line: "b"},
		{name: "interrupt reports no answer", input: "ab\x03", out: "**", err: ErrInterrupt},
		{name: "end of input returns the typed bytes", input: "ab\x04", out: "**", line: "ab", err: io.EOF},
		{name: "control bytes are ignored", input: "a\x01b\tc\r", out: "***\r\n", line: "abc"},
		{name: "closed reader returns the typed bytes", input: "ab", out: "**", line: "ab", err: io.EOF},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			line, err := readMaskedLine(strings.NewReader(row.input), &out, '*')
			if !errors.Is(err, row.err) {
				t.Fatalf("readMaskedLine(%q) error = %v, want %v", row.input, err, row.err)
			}
			if string(line) != row.line {
				t.Fatalf("readMaskedLine(%q) line = %q, want %q", row.input, line, row.line)
			}
			if out.String() != row.out {
				t.Fatalf("readMaskedLine(%q) drew %q, want %q", row.input, out.String(), row.out)
			}
		})
	}
}

// refuseAfter accepts the first limit bytes and refuses the rest, so a test
// can reach the erase and the line end behind a successful star.
type refuseAfter struct {
	limit int
	wrote int
}

func (w *refuseAfter) Write(p []byte) (int, error) {
	if w.wrote >= w.limit {
		return 0, errors.New("write refused")
	}
	w.wrote += len(p)
	return len(p), nil
}

// failingRead refuses every read.
type failingRead struct{}

func (failingRead) Read([]byte) (int, error) { return 0, errors.New("read refused") }

func TestReadMaskedLineFailures(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		input string
		allow int
		read  io.Reader
		want  string
	}{
		{name: "a failed mask write stops the read", input: "a\r", want: "write refused"},
		{name: "a failed line end stops the read", input: "\r", want: "write refused"},
		{name: "a failed erase stops the read", input: "a\x7fb\r", allow: 1, want: "write refused"},
		{name: "a read failure is returned", read: failingRead{}, want: "read refused"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			out := &refuseAfter{limit: row.allow}
			reader := row.read
			if reader == nil {
				reader = strings.NewReader(row.input)
			}
			_, err := readMaskedLine(reader, out, '*')
			if err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("readMaskedLine() error = %v, want %q", err, row.want)
			}
		})
	}
}

// TestReadMaskedOffTTY proves a stream that is not a terminal reports the
// refusal instead of hanging on a raw-mode read nobody can answer.
func TestReadMaskedOffTTY(t *testing.T) {
	t.Parallel()
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "passphrase"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open the plain file: %v", err)
	}
	defer file.Close()
	if _, err := ReadMasked(file, io.Discard, "Passphrase: ", '*'); err == nil || !strings.Contains(err.Error(), "raw") {
		t.Fatalf("ReadMasked() over a plain file = %v, want the raw-mode refusal", err)
	}
}
