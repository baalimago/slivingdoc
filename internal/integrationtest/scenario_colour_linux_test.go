//go:build linux

package integrationtest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

	t.Run("success report", func(t *testing.T) {
		t.Parallel()
		env, root := cliRoots(t)
		notes := filepath.Join(root, "notes")
		code, stdout := runCLITTY(t, "fake", env, "pull", notes)
		if code != 0 {
			t.Fatalf("pull on a terminal = exit %d, want 0; stdout: %q", code, stdout)
		}
		want := "\x1b[32m✓\x1b[0m \x1b[32mOK\x1b[0m  \x1b[34mgeneration 0\x1b[0m  " + notes + "\n" +
			"\x1b[2m0 files changed, 0 insertions(+), 0 deletions(-)\x1b[0m\n"
		if stdout != want {
			t.Fatalf("pull on a terminal stdout = %q, want the ANSI report %q", stdout, want)
		}
	})

	// The read-only refusal on a pseudo-terminal carries the plain bytes
	// proven by TestScenarioCLIReadOnlyCommit, with the terminal's marks.
	t.Run("read-only refusal report", func(t *testing.T) {
		t.Parallel()
		env, root, prefix := realCLIEnv(t, "integrationtest-colour-readonly")
		env = append(env, "SLIVINGDOC_READ_ONLY_PATHS=docs")

		writer := NewHarness(t, HarnessConfig{Prefix: prefix})
		seed := writer.Path("seed")
		writer.WriteFile(filepath.Join(seed, "docs", "faq.md"), "Q: a\nA: 1\n")
		writer.assertOK(t, writer.Pull("", seed))
		writer.assertOK(t, writer.Commit("", seed, "seed"))

		notes := filepath.Join(root, "notes")
		runCLIExact(t, "real", env,
			"OK  generation 1  "+notes+"\n"+
				"  docs/faq.md  +2\n"+
				"1 files changed, 2 insertions(+), 0 deletions(-)\n"+
				"read-only: docs\n",
			"pull", notes)
		writeCLIFile(t, filepath.Join(notes, "docs", "faq.md"), "A: 2\n")

		code, stdout := runCLITTY(t, "real", env, "commit", notes, "-m", "m")
		if code != 1 {
			t.Fatalf("read-only commit on a terminal = exit %d, want 1; stdout: %q", code, stdout)
		}
		want := "\x1b[31m✗\x1b[0m \x1b[31mINVALID_REQUEST\x1b[0m · \x1b[2mREAD_ONLY_PATH\x1b[0m\n" +
			"docs is read-only in this server. Your changes there were discarded and the files reset. " +
			"Write outside the read-only paths, then commit again.\n" +
			"  \x1b[33mdocs/faq.md\x1b[0m  \x1b[2mread-only\x1b[0m\n" +
			"\x1b[34m→\x1b[0m edit the files, then commit\n" +
			"\x1b[2mretryable: false\x1b[0m\n" +
			"\x1b[2mread-only:\x1b[0m docs\n"
		if stdout != want {
			t.Fatalf("read-only refusal on a terminal stdout = %q, want the ANSI report %q", stdout, want)
		}
		plain := stripANSI(stdout)
		// Stripped of colour, the terminal report is the plain one with
		// its marks: a cross before the code, an arrow for "next:".
		wantPlain := "✗ INVALID_REQUEST · READ_ONLY_PATH\n" +
			"docs is read-only in this server. Your changes there were discarded and the files reset. " +
			"Write outside the read-only paths, then commit again.\n" +
			"  docs/faq.md  read-only\n" +
			"→ edit the files, then commit\n" +
			"retryable: false\n" +
			"read-only: docs\n"
		if plain != wantPlain {
			t.Fatalf("stripped read-only refusal = %q, want the plain report %q", plain, wantPlain)
		}
	})
}

// TestScenarioCLIHomeOnTerminal proves the router's terminal surface: a
// bare slivingdoc on a terminal is the home screen with the login state
// and the grouped commands, and a command's help gets the header, while
// the same lines stay plain on a pipe (TestScenarioConfigRefusals).
func TestScenarioCLIHomeOnTerminal(t *testing.T) {
	t.Parallel()
	t.Run("home screen", func(t *testing.T) {
		t.Parallel()
		code, stdout := runCLITTY(t, "fake", []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()})
		if code != 1 {
			t.Fatalf("bare slivingdoc on a terminal = exit %d, want 1", code)
		}
		plain := stripANSI(stdout)
		for _, want := range []string{
			"◆ slivingdoc · v", "Not logged in to hosted storage · → slivingdoc login\n",
			"  Sync     pull     write the current notebook into a directory\n",
			"  Account  login    log in to hosted storage through the browser\n",
		} {
			if !strings.Contains(plain, want) {
				t.Fatalf("home on a terminal = %q, want it to contain %q", plain, want)
			}
		}
	})
	t.Run("command help", func(t *testing.T) {
		t.Parallel()
		code, stdout := runCLITTY(t, "fake", nil, "space", "-h")
		if code != 0 || !strings.HasPrefix(stdout, "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[1mspace\x1b[0m \x1b[2m· list the login's spaces") ||
			!strings.Contains(stdout, "\x1b[34mUsage:\x1b[0m\n") {
			t.Fatalf("space -h on a terminal = exit %d stdout %q", code, stdout)
		}
	})
}

// TestScenarioCLIStderrOnTerminal proves the stderr side of the terminal
// presentation: pull draws its progress line and clears it before the
// report, and serve announces that it is ready, only because stderr is a
// terminal (architecture/tui.md).
func TestScenarioCLIStderrOnTerminal(t *testing.T) {
	t.Parallel()
	t.Run("pull progress", func(t *testing.T) {
		t.Parallel()
		env, root := cliRoots(t)
		notes := filepath.Join(root, "notes")
		code, out := runCLIOnTTY(t, "fake", env, stdoutAndStderr, "pull", notes)
		if code != 0 {
			t.Fatalf("pull = exit %d; output %q", code, out)
		}
		progress := "\r\x1b[K  \x1b[34m⠋\x1b[0m Pulling " + notes + " \x1b[2m· bucket integration-bucket · "
		if !strings.Contains(out, progress) || !strings.Contains(out, "\r\x1b[K\x1b[32m✓\x1b[0m \x1b[32mOK\x1b[0m") {
			t.Fatalf("pull on a terminal = %q, want the progress line %q cleared before the report", out, progress)
		}
	})
	t.Run("serve ready line", func(t *testing.T) {
		t.Parallel()
		code, out := runCLIOnTTY(t, "fake", []string{"LOG_LEVEL=warn"}, stdoutAndStderr, "serve")
		if code != 0 {
			t.Fatalf("serve = exit %d; output %q", code, out)
		}
		plain := stripANSI(out)
		if !strings.HasPrefix(plain, "◆ slivingdoc serve · bucket integration-bucket · ") || !strings.Contains(plain, " · waiting for an MCP client on stdio\n") {
			t.Fatalf("serve on a terminal = %q, want the ready line", plain)
		}
	})
}

// ansiEscapeRE matches one ANSI SGR escape sequence.
var ansiEscapeRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes every ANSI SGR escape sequence from s.
func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

// runCLITTY runs one one-shot CLI process whose stdout is a
// pseudo-terminal and returns the exit code and every byte the child
// wrote to the terminal. The PTY proves the character-device branch of
// the colour gate end to end.
func runCLITTY(t *testing.T, mode string, env []string, args ...string) (int, string) {
	t.Helper()
	return runCLIOnTTY(t, mode, env, stdoutOnly, args...)
}

// ttyStreams says which of the child's output streams is the terminal.
type ttyStreams int

const (
	stdoutOnly ttyStreams = iota
	stdoutAndStderr
)

// runCLIOnTTY is runCLITTY with a choice of streams: with stdoutAndStderr
// the child's stderr is the same terminal, so its progress and header
// lines interleave with the report in the returned bytes.
func runCLIOnTTY(t *testing.T, mode string, env []string, streams ttyStreams, args ...string) (int, string) {
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
	childStderr := stderr
	if streams == stdoutAndStderr {
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
