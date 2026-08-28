//go:build linux

package integrationtest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// TestScenarioCLIColourOnTerminal proves the process-level colour
// contract: the one-shot CLI renders ANSI when its stdout is a real
// terminal (a pseudo-terminal). The piped scenarios prove the plain side
// of the gate, and the unit-level colour gate tests prove that NO_COLOR
// disables a terminal; this scenario closes the loop with a genuine
// character device and the exact byte output.
func TestScenarioCLIColourOnTerminal(t *testing.T) {
	t.Parallel()
	env, root := cliRoots(t)
	notes := filepath.Join(root, "notes")
	code, stdout := runCLITTY(t, "fake", env, "pull", notes)
	if code != 0 {
		t.Fatalf("pull on a terminal = exit %d, want 0; stdout: %q", code, stdout)
	}
	want := "\x1b[32mOK\x1b[0m  \x1b[36mgeneration 0\x1b[0m  " + notes + "\n" +
		"0 files changed, 0 insertions(+), 0 deletions(-)\n"
	if stdout != want {
		t.Fatalf("pull on a terminal stdout = %q, want the ANSI report %q", stdout, want)
	}
}

// runCLITTY runs one one-shot CLI process whose stdout is a
// pseudo-terminal and returns the exit code and every byte the child
// wrote to the terminal. The PTY proves the character-device branch of
// the colour gate end to end.
func runCLITTY(t *testing.T, mode string, env []string, args ...string) (int, string) {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()

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
	fullEnv := append(sanitizedEnv(),
		"SLIVINGDOC_INTEGRATION_HELPER="+mode,
		helperCacheEnv+"="+cacheDir,
		"SLIVINGDOC_BUCKET=integration-bucket",
		"SLIVINGDOC_PREFIX=integration-prefix",
		"SLIVINGDOC_WORKSPACE_ROOT="+workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT="+privateRoot,
	)
	fullEnv = overrideEnv(fullEnv, env)
	argv := append([]string{os.Args[0]}, args...)
	proc, err := os.StartProcess(os.Args[0], argv, &os.ProcAttr{
		Env:   fullEnv,
		Files: []*os.File{childIn, slave, stderr},
	})
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	// The parent keeps only the master; the child holds the slave until it
	// exits, at which point the concurrent master read observes the hangup.
	childIn.Close()
	stdin.Close()
	slave.Close()
	h := &helperProc{proc: proc}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = h.reap()
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
	code := h.waitExit(t)
	res := <-drained
	if res.err != nil {
		t.Fatalf("read helper terminal: %v", res.err)
	}
	return code, string(res.data)
}

// openPTY allocates a pseudo-terminal pair on Linux: /dev/ptmx as the
// master, the unlocked /dev/pts/<n> as the slave. A host without a
// pseudo-terminal device names that capability and skips.
func openPTY(t *testing.T) (master, slave *os.File) {
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
