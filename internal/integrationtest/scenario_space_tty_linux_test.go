//go:build linux

package integrationtest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ttyStep is one scripted action after a marker appears on the terminal.
type ttyStep struct {
	// after is the text the child must have written before the action.
	after string
	// typed is written to the terminal when signal is nil.
	typed string
	// signal is sent to the child instead of typed bytes, so a scenario
	// can prove the process's default signal behavior.
	signal os.Signal
}

// TestTerminalHarnessRunCLIOnTypedTTY proves the harness itself: both
// streams are the child's terminal and the exit status survives the run.
// The picker scenarios prove the stdin side.
func TestTerminalHarnessRunCLIOnTypedTTY(t *testing.T) {
	t.Parallel()
	t.Run("stdout and exit code", func(t *testing.T) {
		t.Parallel()
		code, out := runCLIOnTypedTTY(t, "fake", nil, nil, "version")
		if code != 0 || !strings.HasPrefix(stripANSI(out), "slivingdoc ") {
			t.Fatalf("version on a terminal = exit %d, output %q", code, out)
		}
	})
	t.Run("stderr and exit code", func(t *testing.T) {
		t.Parallel()
		env := []string{"SLIVINGDOC_CONFIG_DIR=" + t.TempDir()}
		code, out := runCLIOnTypedTTY(t, "fake", env, nil, "space")
		if code != 1 || !strings.Contains(stripANSI(out), "no backend is reachable") {
			t.Fatalf("space without a backend = exit %d, output %q", code, out)
		}
	})
}

func TestScenarioSpacePickerSkipOnTerminal(t *testing.T) {
	t.Parallel()
	g, site, env, _ := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")

	code, out := runCLIOnTypedTTY(t, "real", env, []ttyStep{
		{after: "+ Create a new hosted space", typed: "q\n"},
	}, "space")
	if code != 0 {
		t.Fatalf("space on a terminal = exit %d, want 0; output %q", code, out)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "Nothing was changed") {
		t.Fatalf("a left picker = %q, want the skip line", plain)
	}
	runCLIExact(t, "real", env, "HOSTED  "+g.URL()+"  "+loginAccount+"\n"+
		"* team-notes  write  "+loginAccount+"\n", "space")
}

func TestScenarioSpacePickerCreateOnTerminal(t *testing.T) {
	t.Parallel()
	g, site, env, _ := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")

	code, out := runCLIOnTypedTTY(t, "real", env, []ttyStep{
		{after: "+ Create a new hosted space", typed: "1\n"},
		{after: "Name for the new hosted space: ", typed: "research\n"},
	}, "space")
	if code != 0 {
		t.Fatalf("space on a terminal = exit %d, want 0; output %q", code, out)
	}
	plain := stripANSI(out)
	// The terminal renders the same create and default lines as the
	// non-interactive path's terminal form, with their marks.
	create := "✓ Created hosted space research · (write, " + loginAccount + ") at " + g.URL() + "\n"
	selectDefault := "✓ Default space research · (write, " + loginAccount + ")\n"
	if !strings.Contains(plain, create) || !strings.Contains(plain, selectDefault) {
		t.Fatalf("create on a terminal = %q, want %q then %q", plain, create, selectDefault)
	}
	runCLIExact(t, "real", env, "HOSTED  "+g.URL()+"  "+loginAccount+"\n"+
		"* research  write  "+loginAccount+"\n"+
		"  team-notes  write  "+loginAccount+"\n", "space")
}

func TestScenarioSpacePickerInterruptAtTable(t *testing.T) {
	t.Parallel()
	g, site, env, _ := loginEnv(t)
	approve(site, g, loginKey, "write", loginExpiry)
	runLogin(t, env, site, loggedIn(g, "read and write", withDefault), "--no-browser")
	before, err := os.ReadFile(credentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}

	code, out := runCLIOnTypedTTY(t, "real", env, []ttyStep{
		{after: "+ Create a new hosted space", signal: os.Interrupt},
	}, "space")
	if code != 130 {
		t.Fatalf("Ctrl-C at the table = exit %d, want 130; output %q", code, out)
	}
	plain := stripANSI(out)
	if strings.Contains(plain, "Nothing was changed") || strings.Contains(plain, "The default space is now") {
		t.Fatalf("Ctrl-C at the table = %q, want no result line", plain)
	}
	after, err := os.ReadFile(credentialsPath(env))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Ctrl-C at the table changed the stored defaults")
	}
}

// runCLIOnTypedTTY runs one CLI process with its stdin, stdout, and stderr
// on one pseudo-terminal. Each step waits for its marker before it writes
// or signals, so a script never races the picker's draw. The bounded
// drain returns every byte the child wrote, and a signal exit is reported
// as 128+signal.
func runCLIOnTypedTTY(t *testing.T, mode string, env []string, steps []ttyStep, args ...string) (int, string) {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()

	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	cacheDir := t.TempDir()
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
		Files: []*os.File{slave, slave, slave},
	})
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	// The parent keeps only the master; the child holds the slave until it
	// exits, at which point the read observes the hangup.
	slave.Close()
	tty := newTTYProcess(proc)
	t.Cleanup(tty.stop)

	chunks, readErr := drainTTY(master)
	var out strings.Builder
	waitFor := func(marker string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !strings.Contains(out.String(), marker) {
			select {
			case data, ok := <-chunks:
				if !ok {
					t.Fatalf("%v ended before %q; output %q (read error: %v)", args, marker, out.String(), <-readErr)
				}
				out.Write(data)
			case <-deadline:
				t.Fatalf("%v did not write %q within 10s; output %q", args, marker, out.String())
			}
		}
	}
	for _, step := range steps {
		waitFor(step.after)
		if step.signal != nil {
			if err := proc.Signal(step.signal); err != nil {
				t.Fatalf("signal %v: %v", step.signal, err)
			}
			continue
		}
		if _, err := master.WriteString(step.typed); err != nil {
			t.Fatalf("type %q: %v", step.typed, err)
		}
	}
	tty.await(t)
	deadline := time.After(30 * time.Second)
	for {
		select {
		case data, ok := <-chunks:
			if !ok {
				if err := <-readErr; err != nil {
					t.Fatalf("read helper terminal: %v", err)
				}
				return tty.exitCode(), out.String()
			}
			out.Write(data)
		case <-deadline:
			t.Fatalf("terminal read did not end within 30s; output %q", out.String())
		}
	}
}

// drainTTY reads the master until the child's slave closes. The read
// error is reported once, after the chunk channel closes.
func drainTTY(master *os.File) (<-chan []byte, <-chan error) {
	chunks := make(chan []byte, 256)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				chunks <- bytes.Clone(buf[:n])
			}
			if err == nil {
				continue
			}
			if !errors.Is(err, unix.EIO) && !errors.Is(err, io.EOF) {
				errs <- err
			}
			return
		}
	}()
	return chunks, errs
}

// ttyProcess reaps one picker child with unix.Wait4, so a signal exit
// keeps its raw status. os.ProcessState exposes that status only through
// syscall, which TestNoGitExecutableOrGit2goImport forbids process-wide.
type ttyProcess struct {
	proc   *os.Process
	status unix.WaitStatus
	err    error
	done   chan struct{}
}

func newTTYProcess(proc *os.Process) *ttyProcess {
	p := &ttyProcess{proc: proc, done: make(chan struct{})}
	go p.reap()
	return p
}

func (p *ttyProcess) reap() {
	for {
		_, p.err = unix.Wait4(p.proc.Pid, &p.status, 0, nil)
		if !errors.Is(p.err, unix.EINTR) {
			break
		}
	}
	close(p.done)
}

// await waits for the child within the timeout and fails the test if it
// does not end. The cleanup stops a child that survives that failure.
func (p *ttyProcess) await(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("wait for helper: %v", p.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("helper did not exit within 30s")
	}
}

// stop kills a child that is still running and waits for its status.
func (p *ttyProcess) stop() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.proc.Kill()
	<-p.done
}

// exitCode is the shell's view of the child's end: 128+signal when a
// signal ended it.
func (p *ttyProcess) exitCode() int {
	if p.status.Signaled() {
		return 128 + int(p.status.Signal())
	}
	return p.status.ExitStatus()
}
