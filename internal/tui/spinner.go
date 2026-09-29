package tui

import (
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// spinnerFrames are the braille frames of the progress spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerInterval is how often the spinner redraws its line.
const SpinnerInterval = 100 * time.Millisecond

const clearLine = "\r\x1b[K"

// Spinner redraws one progress line on a terminal until it is stopped.
// A plain style gets a spinner that writes nothing, so callers need no
// branch of their own.
type Spinner struct {
	stop chan struct{}
	done chan error
}

// Spin starts a spinner on out that shows a frame and label() every
// SpinnerInterval; label is read again on each frame, so it can count
// down. Stop ends it.
func (s Style) Spin(out io.Writer, label func() string) *Spinner {
	sp := &Spinner{stop: make(chan struct{}), done: make(chan error, 1)}
	if s.mode != Styled {
		sp.done <- nil
		return sp
	}
	go sp.run(out, s, label)
	return sp
}

func (sp *Spinner) run(out io.Writer, s Style, label func() string) {
	ticker := time.NewTicker(SpinnerInterval)
	defer ticker.Stop()
	frame := 0
	guard, guarded := out.(*Guard)
	width := terminalWidth(out)
	draw := func() error {
		// A line wider than the terminal wraps, and clearLine clears only
		// the last row of it, so each frame would leave a row behind.
		line := clearLine + "  " + s.Brand(spinnerFrames[frame%len(spinnerFrames)]) + " " + fit(label(), width-spinnerPrefix)
		frame++
		if guarded {
			return guard.showStatus(line)
		}
		_, err := io.WriteString(out, line)
		return err
	}
	clear := func() error {
		if guarded {
			return guard.clearStatus()
		}
		_, err := io.WriteString(out, clearLine)
		return err
	}
	if err := draw(); err != nil {
		sp.done <- err
		return
	}
	for {
		select {
		case <-sp.stop:
			sp.done <- clear()
			return
		case <-ticker.C:
			if err := draw(); err != nil {
				sp.done <- err
				return
			}
		}
	}
}

// spinnerPrefix is the width of the indent, the frame and the space
// before the label, plus one column so the cursor never wraps.
const spinnerPrefix = 5

// terminalWidth is out's width in columns, or 0 when it has none.
func terminalWidth(out io.Writer) int {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// fit cuts label to width visible columns, ending it with "…"; escape
// sequences are kept whole and take no column. A width below one leaves
// the label as it is.
func fit(label string, width int) string {
	if width < 1 || visibleWidth(label) <= width {
		return label
	}
	var b strings.Builder
	cols := 0
	for i := 0; i < len(label); {
		if n := escapeLen(label[i:]); n > 0 {
			b.WriteString(label[i : i+n])
			i += n
			continue
		}
		if cols == width-1 {
			break
		}
		r, size := utf8.DecodeRuneInString(label[i:])
		b.WriteRune(r)
		cols++
		i += size
	}
	return b.String() + "…" + codeReset
}

// visibleWidth counts the runes of text outside its escape sequences.
func visibleWidth(text string) int {
	cols := 0
	for i := 0; i < len(text); {
		if n := escapeLen(text[i:]); n > 0 {
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		cols++
		i += size
	}
	return cols
}

// escapeLen is the length of the CSI sequence text starts with, or 0.
func escapeLen(text string) int {
	if !strings.HasPrefix(text, "\x1b[") {
		return 0
	}
	end := strings.IndexFunc(text[2:], func(r rune) bool { return r >= '@' && r <= '~' })
	if end < 0 {
		return 0
	}
	return 2 + end + 1
}

// Stop ends the spinner, clears its line, and returns the first write
// error, if any. It must be called exactly once.
func (sp *Spinner) Stop() error {
	close(sp.stop)
	return <-sp.done
}
