package pull

import (
	"bytes"
	"context"
	"errors"
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
func testOptions(t *testing.T, store storage.ObjectStore) (app.ProcessOptions, *bytes.Buffer, string) {
	t.Helper()
	workspaceRoot := t.TempDir()
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
	}, out, workspaceRoot
}

// configArgs is the flag preamble of one test command line.
func configArgs(t *testing.T, workspaceRoot string) []string {
	t.Helper()
	return []string{
		"--bucket", "test-bucket", "--prefix", "p",
		"--workspace-root", workspaceRoot, "--private-root", t.TempDir(),
	}
}

// TestPullPrintsReport proves the command contract end to end in process:
// a relative path resolves against the working directory, the pull
// materializes the notebook directory, and stdout is the OK-prefixed
// result report. The captured writer is not a terminal, so the report is
// plain text.
func TestPullPrintsReport(t *testing.T) {
	t.Parallel()
	opts, out, root := testOptions(t, fake.New("p"))
	c := Command(git2.New(), opts)
	if err := c.Flagset().Parse(append(configArgs(t, root), "notes")); err != nil {
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
	if fi, err := os.Stat(filepath.Join(root, "notes")); err != nil || !fi.IsDir() {
		t.Fatalf("notebook directory was not materialized: %v", err)
	}
}

// stubEngine records whether the native engine was opened; not opening it
// proves an argument refusal exits before any startup dependency.
type stubEngine struct{ opened bool }

func (e *stubEngine) Open() error {
	e.opened = true
	return errors.New("pull test: the native engine must not open")
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

// TestPullRefusesExtraPaths proves a second path is a Setup refusal that
// never opens the native engine.
func TestPullRefusesExtraPaths(t *testing.T) {
	t.Parallel()
	engine := &stubEngine{}
	opts, _, _ := testOptions(t, fake.New("p"))
	c := Command(engine, opts)
	if err := c.Flagset().Parse([]string{"a", "b"}); err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	err := c.Setup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "at most one notebook path") {
		t.Fatalf("Setup() = %v, want the path refusal", err)
	}
	if engine.opened {
		t.Fatal("the path refusal opened the native engine")
	}
}

// TestPullRunRequiresSetup proves Run without Setup is a refusal, matching
// the serve command contract.
func TestPullRunRequiresSetup(t *testing.T) {
	t.Parallel()
	opts, _, _ := testOptions(t, fake.New("p"))
	if err := Command(git2.New(), opts).Run(context.Background()); err == nil {
		t.Fatal("Run() without Setup = nil, want a refusal")
	}
}
