package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
)

// TestMain silences the router's own usage and error output. ancli writes to
// the process stdout and stderr rather than to an injected writer, so these
// unit tests assert exit codes and dependency effects; the process scenarios
// in internal/integrationtest capture the real streams and assert the text.
func TestMain(m *testing.M) {
	ancli.Silent = true
	os.Exit(m.Run())
}

// stubEngine records whether the native engine was ever opened. Opening it
// is the first startup dependency, so "not opened" is the observable proof
// that a command exited early.
type stubEngine struct{ opened bool }

func (e *stubEngine) Open() error {
	e.opened = true
	return errors.New("cli test: the native engine must not open")
}
func (e *stubEngine) Close() error                    { return nil }
func (e *stubEngine) Version() (string, error)        { return "", errors.New("unused") }
func (e *stubEngine) Features() (git.Features, error) { return git.Features{}, errors.New("unused") }

func (e *stubEngine) CreateRepo(string) (git.Repository, error) {
	return nil, errors.New("unused")
}

func (e *stubEngine) OpenRepo(string) (git.Repository, error) {
	return nil, errors.New("unused")
}

// run routes one command line and returns the exit code with whatever the
// command wrote to the injected stdout.
func run(t *testing.T, engine git.Engine, env []string, args ...string) (int, string) {
	t.Helper()
	var out strings.Builder
	code := Run(context.Background(), append([]string{"slivingdoc"}, args...), engine, app.ProcessOptions{
		Args:    args,
		Env:     env,
		Cwd:     t.TempDir(),
		Stdout:  &out,
		Stderr:  &strings.Builder{},
		Signals: make(chan os.Signal, 1),
	})
	return code, out.String()
}

// TestVersionTouchesNoStartupDependency proves the version command prints
// the exact contract line without opening the native engine, whatever the
// configuration says. The npm launcher and the release smoke test read this
// line, so its shape is part of the public surface.
func TestVersionTouchesNoStartupDependency(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		env  []string
	}{
		{name: "no configuration"},
		{
			name: "invalid configuration",
			env:  []string{"SLIVINGDOC_BUCKET=", "SLIVINGDOC_CHECKPOINT_PACKS=0"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			engine := &stubEngine{}
			code, out := run(t, engine, row.env, "version")
			if code != 0 {
				t.Fatalf("version = exit %d, want 0", code)
			}
			if out != "slivingdoc "+app.Version+"\n" {
				t.Fatalf("version stdout = %q, want the exact version line", out)
			}
			if engine.opened {
				t.Fatal("version opened the native engine")
			}
		})
	}
}

// TestRouterRefusals proves the router surface: a missing command, an
// unknown command, an unknown flag, and an invalid configuration are all
// nonzero exits that never reach the native engine.
func TestRouterRefusals(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		args []string
		env  []string
	}{
		{name: "no command"},
		{name: "unknown command", args: []string{"frobnicate"}},
		{name: "unknown flag", args: []string{"serve", "--frobnicate"}},
		{
			name: "invalid configuration",
			args: []string{"serve"},
			env:  []string{"SLIVINGDOC_BUCKET="},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			engine := &stubEngine{}
			code, _ := run(t, engine, row.env, row.args...)
			if code == 0 {
				t.Fatalf("%v = exit 0, want a refusal", row.args)
			}
			if engine.opened {
				t.Fatalf("%v opened the native engine", row.args)
			}
		})
	}
}

// TestServeHelpExitsCleanly proves -h on the serve command is a successful
// early exit that resolves no configuration and opens no engine.
func TestServeHelpExitsCleanly(t *testing.T) {
	t.Parallel()
	engine := &stubEngine{}
	if code, _ := run(t, engine, nil, "serve", "-h"); code != 0 {
		t.Fatalf("serve -h = exit %d, want 0", code)
	}
	if engine.opened {
		t.Fatal("serve -h opened the native engine")
	}
}

// TestDebugPerfCapturesTheCommand proves the DEBUG_PERF surface on the
// router: one invocation with an explicit base directory writes exactly
// one run directory holding the CPU profile, the heap profile, and the
// execution trace, and never disturbs the command's own exit code.
//
// Not parallel: the CPU profile and the execution trace are
// process-global recordings.
func TestDebugPerfCapturesTheCommand(t *testing.T) {
	base := filepath.Join(t.TempDir(), "perf")
	engine := &stubEngine{}
	code, out := run(t, engine, []string{"DEBUG_PERF=" + base}, "version")
	if code != 0 {
		t.Fatalf("version under DEBUG_PERF = exit %d, want 0", code)
	}
	if out != "slivingdoc "+app.Version+"\n" {
		t.Fatalf("version stdout = %q, want the exact version line; profiling must stay on stderr", out)
	}
	runs, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read the capture base: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("capture runs = %d, want exactly one per invocation", len(runs))
	}
	for _, name := range []string{"cpu.pprof", "heap.pprof", "trace.out"} {
		info, err := os.Stat(filepath.Join(base, runs[0].Name(), name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

// TestCommandsCoverTheDocumentedSurface pins the command map: every command
// carries a description for the usage table and a non-nil flag set, which
// the router requires.
func TestCommandsCoverTheDocumentedSurface(t *testing.T) {
	t.Parallel()
	commands := Commands(&stubEngine{}, app.ProcessOptions{})
	if len(commands) != 4 {
		t.Fatalf("commands = %d, want serve, pull, commit, and version only", len(commands))
	}
	for name, command := range commands {
		if command.Flagset() == nil {
			t.Fatalf("%s: nil flag set; the router refuses to parse it", name)
		}
		if strings.TrimSpace(command.Describe()) == "" {
			t.Fatalf("%s: empty description leaves a blank row in the usage table", name)
		}
		if strings.TrimSpace(command.Help()) == "" {
			t.Fatalf("%s: empty help", name)
		}
	}
	for _, want := range []string{"serve|s", "pull|p", "commit|c", "version|v"} {
		if _, ok := commands[want]; !ok {
			t.Fatalf("command %q is missing; its shortcut is part of the CLI surface", want)
		}
	}
}
