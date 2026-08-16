package commit

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// testOptions is one isolated process environment over the given store:
// per-test roots, an empty environment, and captured stdout.
func testOptions(t *testing.T, store storage.ObjectStore, workspaceRoot string) (app.ProcessOptions, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	return app.ProcessOptions{
		Env:      []string{},
		Cwd:      workspaceRoot,
		CacheDir: t.TempDir(),
		Stdout:   out,
		Stderr:   &bytes.Buffer{},
		Signals:  make(chan os.Signal, 1),
		StoreFactory: func(context.Context, app.ServiceConfig) (storage.ObjectStore, error) {
			return store, nil
		},
	}, out
}

// configArgs is the flag preamble of one test command line.
func configArgs(workspaceRoot, privateRoot string) []string {
	return []string{
		"--bucket", "test-bucket", "--prefix", "p",
		"--workspace-root", workspaceRoot, "--private-root", privateRoot,
	}
}

// pullFirst performs the managed pull the commit contract requires, over
// the same store and roots the command under test uses.
func pullFirst(t *testing.T, store storage.ObjectStore, workspaceRoot, privateRoot, path string) {
	t.Helper()
	opts, _ := testOptions(t, store, workspaceRoot)
	flags := app.NewFlags()
	fs := flag.NewFlagSet("pull-first", flag.ContinueOnError)
	flags.Bind(fs)
	if err := fs.Parse(configArgs(workspaceRoot, privateRoot)); err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	rt, err := app.Setup(git2.New(), flags, opts)
	if err != nil {
		t.Fatalf("app.Setup() = %v", err)
	}
	defer rt.Close()
	if _, err := rt.Pull(context.Background(), path); err != nil {
		t.Fatalf("Pull(%s) = %v", path, err)
	}
}

// TestCommitPublishesAndPrintsReport proves the command contract end to
// end in process: after a managed pull, commit publishes the edit over the
// shared fake store and stdout is the OK-prefixed result report. The
// captured writer is not a terminal, so the report is plain text.
func TestCommitPublishesAndPrintsReport(t *testing.T) {
	t.Parallel()
	store := fake.New("p")
	workspaceRoot, privateRoot := t.TempDir(), t.TempDir()
	notes := filepath.Join(workspaceRoot, "notes")
	pullFirst(t, store, workspaceRoot, privateRoot, notes)
	if err := os.WriteFile(filepath.Join(notes, "a.md"), []byte("committed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	opts, out := testOptions(t, store, workspaceRoot)
	c := Command(git2.New(), opts)
	args := append(configArgs(workspaceRoot, privateRoot), "notes", "-m", "unit commit")
	if err := c.Flagset().Parse(args); err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if got := out.String(); !strings.HasPrefix(got, "OK  generation ") || !strings.Contains(got, "files changed") {
		t.Fatalf("stdout = %q, want the OK-prefixed result report", got)
	}
}

// TestCommitReportsMarkerConflict proves the domain-error path: a complete
// conflict-marker block prints the structured report with the relative
// path and line range, and Run returns the terse category.
func TestCommitReportsMarkerConflict(t *testing.T) {
	t.Parallel()
	store := fake.New("p")
	workspaceRoot, privateRoot := t.TempDir(), t.TempDir()
	notes := filepath.Join(workspaceRoot, "notes")
	pullFirst(t, store, workspaceRoot, privateRoot, notes)
	marker := "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n"
	if err := os.WriteFile(filepath.Join(notes, "a.md"), []byte(marker), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	opts, out := testOptions(t, store, workspaceRoot)
	c := Command(git2.New(), opts)
	args := append(configArgs(workspaceRoot, privateRoot), "notes", "-m", "markers")
	if err := c.Flagset().Parse(args); err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	err := c.Run(context.Background())
	if err == nil || err.Error() != "CONTENT_CONFLICT" {
		t.Fatalf("Run() = %v, want the terse CONTENT_CONFLICT category", err)
	}
	for _, want := range []string{"CONTENT_CONFLICT", "retryable: false", "a.md: lines 1-5"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("report %q does not contain %q", out.String(), want)
		}
	}
}

// stubEngine records whether the native engine was opened; not opening it
// proves an argument refusal exits before any startup dependency.
type stubEngine struct{ opened bool }

func (e *stubEngine) Open() error {
	e.opened = true
	return errors.New("commit test: the native engine must not open")
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

// TestCommitRefusesWithoutMessage proves a missing -m is a Setup refusal
// that never opens the native engine.
func TestCommitRefusesWithoutMessage(t *testing.T) {
	t.Parallel()
	engine := &stubEngine{}
	opts, _ := testOptions(t, fake.New("p"), t.TempDir())
	c := Command(engine, opts)
	if err := c.Flagset().Parse([]string{"notes"}); err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	err := c.Setup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "-m") {
		t.Fatalf("Setup() = %v, want the message refusal", err)
	}
	if engine.opened {
		t.Fatal("the message refusal opened the native engine")
	}
}
