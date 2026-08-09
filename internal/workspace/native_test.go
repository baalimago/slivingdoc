package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git2"
)

// newNativeConfig builds a config backed by the real libgit2 engine.
func newNativeConfig(t *testing.T, notes string) Config {
	t.Helper()
	e := git2.New()
	if err := e.Open(); err != nil {
		t.Fatalf("engine Open() = %v", err)
	}
	t.Cleanup(func() { e.Close() })
	cfg := testConfig(t, e, notes)
	cfg.Engine = e
	return cfg
}

func TestNativeAcceptReopenRoundTrip(t *testing.T) {
	cfg := newNativeConfig(t, "notes")
	w := openWorkspace(t, cfg)
	if _, err := os.Stat(filepath.Join(w.privDir, repoDirName, ".git", "objects")); err != nil {
		t.Fatalf("private repository objects missing: %v", err)
	}
	tree := buildTree(t, w, map[string]string{"a.md": "one\r\ntwo\n", "sub/é.md": "café", "empty": ""})
	baseline := Baseline{RemoteGeneration: 4, Head: oidTest("c"), Tree: tree}
	if err := w.Accept(context.Background(), baseline); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	// L equals the tree byte for byte.
	snap, err := w.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if len(snap.Files) != 3 {
		t.Fatalf("Snapshot() has %d files", len(snap.Files))
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	reopened, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	defer reopened.Close()
	if reopened.RecoveryRequired() {
		t.Fatal("reopened workspace requires recovery")
	}
	if got := reopened.Baseline(); got != baseline {
		t.Fatalf("Baseline() = %+v, want %+v", got, baseline)
	}
	diff, err := reopened.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff() = %v", err)
	}
	if len(diff.Added)+len(diff.Modified)+len(diff.Deleted) != 0 {
		t.Fatalf("Diff() = %+v, want empty", diff)
	}
}

// TestNativeEmptyTreeMaterialization proves that accepting the canonical
// empty tree empties L and that the baseline tree object exists in the
// private repository.
func TestNativeEmptyTreeMaterialization(t *testing.T) {
	cfg := newNativeConfig(t, "notes")
	w := openWorkspace(t, cfg)
	tree := buildTree(t, w, map[string]string{"a.md": "a"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: EmptyTreeID}); err != nil {
		t.Fatalf("empty Accept() = %v", err)
	}
	entries, err := os.ReadDir(w.Path())
	if err != nil {
		t.Fatalf("ReadDir(L) = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("L is not empty after empty-tree acceptance: %v", entries)
	}
	if _, err := w.Repo().ReadTree(EmptyTreeID); err != nil {
		t.Fatalf("empty tree object missing from the private repository: %v", err)
	}
}

// TestNativeHardLinksBrokenAfterRewrite proves the rename-based
// replacement does not preserve hard-link identity.
func TestNativeHardLinksBrokenAfterRewrite(t *testing.T) {
	cfg := newNativeConfig(t, "notes")
	w := openWorkspace(t, cfg)
	a := filepath.Join(w.Path(), "a.md")
	if err := os.WriteFile(a, []byte("shared"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	b := filepath.Join(w.Path(), "b.md")
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	tree := buildTree(t, w, map[string]string{"a.md": "shared", "b.md": "shared"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	ia, _ := os.Stat(a)
	ib, _ := os.Stat(b)
	if os.SameFile(ia, ib) {
		t.Fatal("rename replacement preserved the hard link")
	}
}

// TestNativeSubprocessLock proves that a process exiting while holding the
// operation lock releases it: a later process can open the same path and
// run operations.
func TestNativeSubprocessLock(t *testing.T) {
	cfg := newNativeConfig(t, "notes")
	// The helper process opens the workspace and holds the operation lock
	// inside a Snapshot blocked by a Scan failpoint, then signals readiness.
	// os.StartProcess spawns the test binary itself: the process never
	// invokes an external executable.
	ready := filepath.Join(t.TempDir(), "ready")
	argv := []string{os.Args[0], "--", cfg.WorkspaceRoot, cfg.Path, cfg.PrivateRoot, ready}
	helper, err := os.StartProcess(os.Args[0], argv, &os.ProcAttr{
		Env: append(os.Environ(), lockHelperEnv+"=1"),
	})
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		helper.Kill()
		helper.Wait()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never took the operation lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The lock is held: opening the same path must time out.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, err = Open(ctx, cfg)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "operation lock") {
		t.Fatalf("Open() while the helper holds the lock = %v, want deadline exceeded", err)
	}

	// The helper exits; the OS releases the advisory lock.
	if err := helper.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if _, err := helper.Wait(); err != nil {
		t.Fatalf("wait for the killed helper = %v", err)
	}
	w, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open() after helper exit = %v", err)
	}
	defer w.Close()
	if _, err := w.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() after handover = %v", err)
	}
}

// TestMain dispatches the lock-handover helper before any test runs. The
// helper is a process body, not a test: routing it through TestMain keeps it
// out of the suite entirely instead of reporting a skipped test on every
// normal run.
func TestMain(m *testing.M) {
	if os.Getenv(lockHelperEnv) == "1" {
		if err := runLockHelper(os.Args); err != nil {
			fmt.Fprintf(os.Stderr, "lock helper: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const lockHelperEnv = "SLIVINGDOC_LOCK_HELPER"

// runLockHelper opens the workspace named by the arguments after "--" and
// blocks inside a Snapshot that holds the operation lock, after touching the
// readiness file. The parent kills it to prove the OS releases the lock.
func runLockHelper(argv []string) error {
	args := argv
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) != 4 {
		return fmt.Errorf("args = %v, want workspace root, path, private root, readiness file", args)
	}
	engine := git2.New()
	if err := engine.Open(); err != nil {
		return fmt.Errorf("engine Open(): %w", err)
	}
	defer engine.Close()

	block := make(chan struct{})
	cfg := Config{
		WorkspaceRoot: args[0],
		Path:          args[1],
		PrivateRoot:   args[2],
		Identity:      testIdentity(),
		Engine:        engine,
		Failpoints: &Failpoints{Scan: func() error {
			// Signal that the operation lock is held, then block.
			if err := os.WriteFile(args[3], []byte("ready"), 0o600); err != nil {
				return err
			}
			<-block
			return nil
		}},
	}
	w, err := Open(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("Open(): %w", err)
	}
	defer w.Close()
	if _, err := w.Snapshot(context.Background()); err != nil {
		return fmt.Errorf("Snapshot(): %w", err)
	}
	return nil
}
