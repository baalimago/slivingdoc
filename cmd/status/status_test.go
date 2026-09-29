package status

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

func options(t *testing.T) (app.ProcessOptions, *bytes.Buffer, []string) {
	t.Helper()
	root := t.TempDir()
	out := &bytes.Buffer{}
	opts := app.ProcessOptions{
		Env: []string{}, Cwd: root, CacheDir: t.TempDir(), Stdout: out, Stderr: &bytes.Buffer{},
		Signals: make(chan os.Signal, 1),
		StoreFactory: func(context.Context, app.ServiceConfig) (storage.ObjectStore, error) {
			return fake.New("p"), nil
		},
	}
	return opts, out, []string{"--bucket", "b", "--prefix", "p", "--workspace-root", root, "--private-root", t.TempDir()}
}

func TestRunWithoutSetupFails(t *testing.T) {
	t.Parallel()
	opts, _, _ := options(t)
	if err := Command(git2.New(), opts).Run(context.Background()); err == nil {
		t.Fatal("Run() before Setup = nil, want an error")
	}
}

func TestPrintsForAnUnpulledDirectory(t *testing.T) {
	t.Parallel()
	opts, out, args := options(t)
	c := Command(git2.New(), opts)
	if err := c.Flagset().Parse(append(args, "notes")); err != nil {
		t.Fatal(err)
	}
	if err := c.Setup(context.Background()); err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(out.String(), "notes") || !strings.Contains(out.String(), "pull") {
		t.Fatalf("stdout = %q, want the path and a pointer to pull", out.String())
	}
}
