//go:build linux

package itest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"golang.org/x/sys/unix"
)

// ANSIEscapeRE matches one ANSI SGR escape sequence.
var ANSIEscapeRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes every ANSI SGR escape sequence from s.
func StripANSI(s string) string {
	return ANSIEscapeRE.ReplaceAllString(s, "")
}

// RunCLITTY runs one one-shot CLI process whose stdout is a
// pseudo-terminal and returns the exit code and every byte the child
// wrote to the terminal. The PTY proves the character-device branch of
// the colour gate end to end.
func RunCLITTY(t *testing.T, mode string, env []string, args ...string) (int, string) {
	t.Helper()
	return RunCLIOnTTY(t, mode, env, StdoutOnly, args...)
}

// ttyStreams says which of the child's output streams is the terminal.
type TtyStreams int

const (
	StdoutOnly TtyStreams = iota
	StdoutAndStderr
)

// RunCLIOnTTY is RunCLITTY with a choice of streams: with StdoutAndStderr
// the child's stderr is the same terminal, so its progress and header
// lines interleave with the report in the returned bytes.
func RunCLIOnTTY(t *testing.T, mode string, env []string, streams TtyStreams, args ...string) (int, string) {
	t.Helper()
	return RunCLIOnTTYSize(t, mode, env, streams, TtySize{}, args...)
}

// TtySize is a terminal's window size in columns and rows; the zero value
// keeps the pseudo-terminal's default, which reports no width at all.
type TtySize struct {
	Cols, Rows int
}

// RunCLIOnTTYSize is RunCLIOnTTY with the terminal's window size set, so a
// scenario proves the rendering that adapts to it.
func RunCLIOnTTYSize(t *testing.T, mode string, env []string, streams TtyStreams, size TtySize, args ...string) (int, string) {
	t.Helper()
	master, slave := OpenPTY(t)
	defer master.Close()
	if size != (TtySize{}) {
		winsize := &unix.Winsize{Col: uint16(size.Cols), Row: uint16(size.Rows)}
		if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, winsize); err != nil {
			t.Fatalf("set the terminal size: %v", err)
		}
	}

	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	cacheDir := t.TempDir()
	stderr, err := os.CreateTemp(t.TempDir(), "helper-stderr")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	childIn, stdin, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	fullEnv := append(
		SanitizedEnv(),
		"SLIVINGDOC_INTEGRATION_HELPER="+mode,
		HelperCacheEnv+"="+cacheDir,
		"SLIVINGDOC_BUCKET=integration-bucket",
		"SLIVINGDOC_PREFIX=integration-prefix",
		"SLIVINGDOC_WORKSPACE_ROOT="+workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT="+privateRoot,
		// A configuration directory of its own, and an empty home, so a
		// developer's login, S3 default, or ~/.aws files never reach a
		// terminal scenario (main_test.go SpawnHelperIn).
		credentials.DirEnv+"="+PrivateTempDir(t),
		"HOME="+PrivateTempDir(t),
	)
	fullEnv = OverrideEnv(fullEnv, env)
	childStderr := stderr
	if streams == StdoutAndStderr {
		childStderr = slave
	}
	argv := append([]string{os.Args[0]}, args...)
	proc, err := os.StartProcess(os.Args[0], argv, &os.ProcAttr{
		Env:   fullEnv,
		Files: []*os.File{childIn, slave, childStderr},
	})
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	// The parent keeps only the master; the child holds the slave until it
	// exits, at which point the concurrent master read observes the hangup.
	childIn.Close()
	stdin.Close()
	slave.Close()
	h := &HelperProc{Proc: proc}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = h.Reap()
	})

	// Drain the master concurrently with the wait: the pty buffer can hold
	// only a bounded amount, so a large report must not deadlock the child.
	type ttyResult struct {
		data []byte
		err  error
	}
	drained := make(chan ttyResult, 1)
	go func() {
		var b bytes.Buffer
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				if errors.Is(err, unix.EIO) || errors.Is(err, io.EOF) {
					break // the slave closed: the terminal is drained
				}
				drained <- ttyResult{b.Bytes(), err}
				return
			}
		}
		drained <- ttyResult{b.Bytes(), nil}
	}()
	code := h.WaitExit(t)
	res := <-drained
	if res.err != nil {
		t.Fatalf("read helper terminal: %v", res.err)
	}
	return code, string(res.data)
}

// openPTY allocates a pseudo-terminal pair on Linux: /dev/ptmx as the
// master, the unlocked /dev/pts/<n> as the slave. A host without a
// pseudo-terminal device names that capability and skips.
func OpenPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("colour-on-terminal requires a pseudo-terminal: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Skipf("colour-on-terminal requires a pseudo-terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Skipf("colour-on-terminal requires a pseudo-terminal: %v", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR, 0)
	if err != nil {
		master.Close()
		t.Skipf("colour-on-terminal requires a pseudo-terminal: %v", err)
	}
	// Disable output post-processing on the slave before the child inherits
	// it, so the terminal never rewrites the report bytes (no LF-to-CRLF
	// translation) and the captured output is byte-exact.
	termios, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		master.Close()
		slave.Close()
		t.Skipf("colour-on-terminal requires a configurable terminal: %v", err)
	}
	termios.Oflag &^= unix.OPOST
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, termios); err != nil {
		master.Close()
		slave.Close()
		t.Skipf("colour-on-terminal requires a configurable terminal: %v", err)
	}
	return master, slave
}
