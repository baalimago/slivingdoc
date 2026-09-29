package tui

import (
	"io"
	"sync"
)

// Guard is a stream a spinner and other writers share, stderr above all:
// a spinner's line has no newline, so any other write, such as a log
// record, first clears it, and the spinner redraws on its next frame.
// Without the guard a warning logged during a pull would be glued to the
// end of the progress line.
type Guard struct {
	mu sync.Mutex
	w  io.Writer
	// status is whether the terminal's current line is a spinner's.
	status bool
}

// NewGuard guards w.
func NewGuard(w io.Writer) *Guard { return &Guard{w: w} }

// Write clears a spinner line, if one is showing, then writes p.
func (g *Guard) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.status {
		g.status = false
		if _, err := io.WriteString(g.w, clearLine); err != nil {
			return 0, err
		}
	}
	return g.w.Write(p)
}

// Fd is the guarded stream's file descriptor, so IsTerminal sees through
// the guard; a stream without one reports an invalid descriptor.
func (g *Guard) Fd() uintptr {
	if f, ok := g.w.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

// showStatus writes a spinner line, which the next Write clears first.
func (g *Guard) showStatus(line string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, err := io.WriteString(g.w, line)
	g.status = err == nil
	return err
}

// clearStatus clears a spinner line that is still showing.
func (g *Guard) clearStatus() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.status {
		return nil
	}
	g.status = false
	_, err := io.WriteString(g.w, clearLine)
	return err
}
