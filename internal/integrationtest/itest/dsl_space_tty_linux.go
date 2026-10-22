//go:build linux

package itest

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
type TtyStep struct {
	// after is the text the child must have written before the action.
	After string
	// typed is written to the terminal when signal is nil.
	Typed string
	// signal is sent to the child instead of typed bytes, so a scenario
	// can prove the process's default signal behavior.
	Signal os.Signal
}

// runCLIOnTypedTTY runs one CLI process with its stdin, stdout, and stderr
// on one pseudo-terminal. Each step waits for its marker before it writes
// or signals, so a script never races the picker's draw. The bounded
// drain returns every byte the child wrote, and a signal exit is reported
// as 128+signal.
func RunCLIOnTypedTTY(t *testing.T, mode string, env []string, steps []TtyStep, args ...string) (int, string) {
	t.Helper()
	master, slave := OpenPTY(t)
	defer master.Close()

	workspaceRoot := t.TempDir()
	privateRoot := t.TempDir()
	cacheDir := t.TempDir()
	fullEnv := append(
		SanitizedEnv(),
		"SLIVINGDOC_INTEGRATION_HELPER="+mode,
		HelperCacheEnv+"="+cacheDir,
		"SLIVINGDOC_BUCKET=integration-bucket",
		"SLIVINGDOC_PREFIX=integration-prefix",
		"SLIVINGDOC_WORKSPACE_ROOT="+workspaceRoot,
		"SLIVINGDOC_PRIVATE_ROOT="+privateRoot,
	)
	fullEnv = OverrideEnv(fullEnv, env)
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
	tty := NewTTYProcess(proc)
	t.Cleanup(tty.Stop)

	chunks, readErr := DrainTTY(master)
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
		waitFor(step.After)
		if step.Signal != nil {
			if err := proc.Signal(step.Signal); err != nil {
				t.Fatalf("signal %v: %v", step.Signal, err)
			}
			continue
		}
		if _, err := master.WriteString(step.Typed); err != nil {
			t.Fatalf("type %q: %v", step.Typed, err)
		}
	}
	tty.Await(t)
	deadline := time.After(30 * time.Second)
	for {
		select {
		case data, ok := <-chunks:
			if !ok {
				if err := <-readErr; err != nil {
					t.Fatalf("read helper terminal: %v", err)
				}
				return tty.ExitCode(), out.String()
			}
			out.Write(data)
		case <-deadline:
			t.Fatalf("terminal read did not end within 30s; output %q", out.String())
		}
	}
}

// drainTTY reads the master until the child's slave closes. The read
// error is reported once, after the chunk channel closes.
func DrainTTY(master *os.File) (<-chan []byte, <-chan error) {
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
type TtyProcess struct {
	Proc   *os.Process
	status unix.WaitStatus
	err    error
	done   chan struct{}
}

func NewTTYProcess(proc *os.Process) *TtyProcess {
	p := &TtyProcess{Proc: proc, done: make(chan struct{})}
	go p.Reap()
	return p
}

func (p *TtyProcess) Reap() {
	for {
		_, p.err = unix.Wait4(p.Proc.Pid, &p.status, 0, nil)
		if !errors.Is(p.err, unix.EINTR) {
			break
		}
	}
	close(p.done)
}

// await waits for the child within the timeout and fails the test if it
// does not end. The cleanup stops a child that survives that failure.
func (p *TtyProcess) Await(t *testing.T) {
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
func (p *TtyProcess) Stop() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.Proc.Kill()
	<-p.done
}

// exitCode is the shell's view of the child's end: 128+signal when a
// signal ended it.
func (p *TtyProcess) ExitCode() int {
	if p.status.Signaled() {
		return 128 + int(p.status.Signal())
	}
	return p.status.ExitStatus()
}
