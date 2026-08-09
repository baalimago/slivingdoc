package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// withExdevRename forces the copy fallback by making every rename fail
// with EXDEV, as a private root on a different device would.
func withExdevRename(t *testing.T) {
	t.Helper()
	orig := renameFn
	renameFn = func(_, _ string) error { return &os.LinkError{Op: "rename", Err: unix.EXDEV} }
	t.Cleanup(func() { renameFn = orig })
}

func TestCopyFallbackMaterializesTree(t *testing.T) {
	withExdevRename(t)
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	// Seed obsolete state through the rename path first.
	first := buildTree(t, w, map[string]string{"old.md": "old", "gone/sub/x.md": "x", "dirswap/f.md": "file"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
		t.Fatalf("first Accept() = %v", err)
	}
	second := buildTree(t, w, map[string]string{"keep.md": "k", "dirswap.md": "now-file", "newdir/a.md": "a", "empty.txt": ""})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second}); err != nil {
		t.Fatalf("copy-fallback Accept() = %v", err)
	}
	for path, want := range map[string]string{"keep.md": "k", "dirswap.md": "now-file", "newdir/a.md": "a", "empty.txt": ""} {
		if got := readFileBytes(t, filepath.Join(w.Path(), filepath.FromSlash(path))); string(got) != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
	for _, gone := range []string{"old.md", "gone", "gone/sub", "dirswap"} {
		if _, err := os.Stat(filepath.Join(w.Path(), filepath.FromSlash(gone))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete %q still present: %v", gone, err)
		}
	}
	for _, transient := range []string{stagingDirName, backupDirName} {
		if _, err := os.Stat(filepath.Join(w.privDir, transient)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transient %q left behind: %v", transient, err)
		}
	}
	if w.RecoveryRequired() {
		t.Fatal("copy fallback left the workspace requiring recovery")
	}
}

func TestCopyFallbackBreaksHardLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link creation needs privileges on Windows")
	}
	withExdevRename(t)
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	a := filepath.Join(w.Path(), "a.md")
	if err := os.WriteFile(a, []byte("shared"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	b := filepath.Join(w.Path(), "b.md")
	if err := os.Link(a, b); err != nil {
		t.Fatalf("Link(): %v", err)
	}
	tree := buildTree(t, w, map[string]string{"a.md": "shared", "b.md": "shared"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat a.md: %v", err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat b.md: %v", err)
	}
	if os.SameFile(ia, ib) {
		t.Fatal("copy fallback preserved the hard link")
	}
}

// TestSymlinkSubstitutionCannotEscapeRoot proves that a symlink swapped
// into the visible tree cannot redirect an operation outside the workspace
// root: the scan rejects it and the replacement never touches the target.
func TestSymlinkSubstitutionCannotEscapeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	cfg := testConfig(t, newFakeEngine(), "notes")
	w := openWorkspace(t, cfg)
	marker := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(marker, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	link := filepath.Join(w.Path(), "a.md")
	if err := os.WriteFile(link, []byte("a"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	// Substitute an escaping symlink for the scanned file.
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove a.md: %v", err)
	}
	if err := os.Symlink(marker, link); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}
	// The scan must reject the symlink and never read the target.
	if _, err := w.Snapshot(context.Background()); !errors.Is(err, ErrSymlink) {
		t.Fatalf("Snapshot() error = %v, want ErrSymlink", err)
	}
	// Replacement moves the link aside and removes the link itself; the
	// target outside the root stays untouched.
	tree := buildTree(t, w, map[string]string{"a.md": "new"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: tree}); err != nil {
		t.Fatalf("Accept() = %v", err)
	}
	if got := readFileBytes(t, marker); string(got) != "secret" {
		t.Fatalf("symlink target outside the root was modified: %q", got)
	}
	if got := readFileBytes(t, link); string(got) != "new" {
		t.Fatalf("a.md after replacement = %q", got)
	}
}
