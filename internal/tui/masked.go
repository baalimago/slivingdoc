package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

// ErrInterrupt reports that Ctrl-C ended a masked read: the caller reports no
// answer and never adopts the bytes typed so far (architecture/tui.md, The
// masked read).
var ErrInterrupt = errors.New("tui: the read was interrupted")

// ReadMasked reads one line from the terminal f without echoing it, printing
// label once the echo is off and one mask byte per accepted character to out.
// It returns the typed bytes without the line terminator. A read the person
// interrupts returns ErrInterrupt; Ctrl-D returns the bytes typed so far with
// io.EOF.
func ReadMasked(f *os.File, out io.Writer, label string, mask byte) (line []byte, err error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("tui: make the terminal raw: %w", err)
	}
	defer func() {
		if restoreErr := term.Restore(fd, state); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("tui: restore the terminal: %w", restoreErr))
		}
	}()
	if _, err := io.WriteString(out, label); err != nil {
		return nil, err
	}
	return readMaskedLine(f, out, mask)
}

// readMaskedLine is the byte-at-a-time core of ReadMasked, over any reader so
// the decision table is testable without a terminal. MakeRaw clears the
// output post-processing of the terminal, so it ends the line with an
// explicit carriage return.
func readMaskedLine(r io.Reader, out io.Writer, mask byte) ([]byte, error) {
	var (
		line    []byte
		pending []byte
		escape  escapeState
		buf     [1]byte
	)
	for {
		n, err := r.Read(buf[:])
		if n == 0 {
			if err != nil {
				return line, err
			}
			continue
		}
		b := buf[0]
		if escape != escapeNone {
			escape = escape.step(b)
			continue
		}
		switch {
		case b == '\r' || b == '\n':
			if _, err := io.WriteString(out, "\r\n"); err != nil {
				return line, err
			}
			return line, nil
		case b == 0x7f || b == 0x08:
			var err error
			if line, pending, err = eraseRune(line, pending, out); err != nil {
				return line, err
			}
		case b == 0x03:
			return nil, ErrInterrupt
		case b == 0x04:
			return line, io.EOF
		case b == 0x1b:
			escape = escapeStart
		case b < 0x20:
			// Another control byte carries no character of the secret.
		default:
			var stars int
			line, pending, stars = appendMasked(line, pending, b)
			if _, err := out.Write(bytes.Repeat([]byte{mask}, stars)); err != nil {
				return line, err
			}
		}
	}
}

// appendMasked appends b to the runes typed so far and reports how many
// characters it completed: one star per character, so a multi-byte rune draws
// one mask byte and an incomplete tail draws none.
func appendMasked(line, pending []byte, b byte) ([]byte, []byte, int) {
	pending = append(pending, b)
	stars := 0
	for len(pending) > 0 {
		if !utf8.FullRune(pending) {
			break
		}
		_, size := utf8.DecodeRune(pending)
		line = append(line, pending[:size]...)
		pending = pending[size:]
		stars++
	}
	return line, pending, stars
}

// eraseRune drops the last typed character and its star from the terminal. An
// incomplete rune has no star yet, so it is dropped without an erase.
func eraseRune(line, pending []byte, out io.Writer) ([]byte, []byte, error) {
	if len(pending) > 0 {
		return line, pending[:0], nil
	}
	if len(line) == 0 {
		return line, pending, nil
	}
	_, size := utf8.DecodeLastRune(line)
	line = line[:len(line)-size]
	_, err := io.WriteString(out, "\b \b")
	return line, pending, err
}

// escapeState tracks the remainder of a terminal escape sequence. Its bytes
// are dropped rather than masked: an arrow key must not become a character of
// the secret.
type escapeState uint8

const (
	escapeNone  escapeState = iota
	escapeStart             // ESC seen, kind unknown
	escapeCSI               // ESC [ seen: parameters run to a final byte
	escapeSS3               // ESC O seen: exactly one more byte
)

// step consumes b and returns the state after it.
func (s escapeState) step(b byte) escapeState {
	switch s {
	case escapeStart:
		switch b {
		case '[':
			return escapeCSI
		case 'O':
			return escapeSS3
		default:
			return escapeNone
		}
	case escapeCSI:
		if b >= 0x40 && b <= 0x7e {
			return escapeNone
		}
		return escapeCSI
	default:
		return escapeNone
	}
}
