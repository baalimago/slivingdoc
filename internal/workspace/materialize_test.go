package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestApplyMaterializesTree proves the in-place materialization covers
// every shape of change in one operation: new files, obsolete files,
// obsolete directory trees, a directory replaced by a file of the same
// name, and an empty file. The transient private directories are cleaned
// up and the workspace is left healthy.
func TestApplyMaterializesTree(t *testing.T) {
	w := openWorkspace(t, testConfig(t, newFakeEngine(), "notes"))
	first := buildTree(t, w, map[string]string{"old.md": "old", "gone/sub/x.md": "x", "dirswap/f.md": "file"})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
		t.Fatalf("first Accept() = %v", err)
	}
	second := buildTree(t, w, map[string]string{"keep.md": "k", "dirswap.md": "now-file", "newdir/a.md": "a", "empty.txt": ""})
	if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second}); err != nil {
		t.Fatalf("second Accept() = %v", err)
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
		t.Fatal("materialization left the workspace requiring recovery")
	}
}

// TestApplyPreservesVisibleDirectoryIdentity is the regression guard for
// the directory-swap bug: materialization must never replace L itself. An
// earlier design renamed L aside and renamed a staged tree into its place,
// which left every outside holder of L — a shell's working directory, an
// open editor, a file watcher — pointing at a removed inode. The symptom
// was a working directory where getcwd(2) fails and every relative path
// resolves to nothing.
//
// The test holds an open descriptor on L across two materializations and
// requires it to still name the live directory afterwards, which is the
// same reference a working directory is.
func TestApplyPreservesVisibleDirectoryIdentity(t *testing.T) {
	for _, rel := range []string{"notes", "."} {
		t.Run("rel="+rel, func(t *testing.T) {
			cfg := testConfig(t, newFakeEngine(), rel)
			w := openWorkspace(t, cfg)

			before, err := os.Stat(w.Path())
			if err != nil {
				t.Fatalf("stat before: %v", err)
			}
			held, err := os.Open(w.Path())
			if err != nil {
				t.Fatalf("open visible directory: %v", err)
			}
			defer held.Close()

			first := buildTree(t, w, map[string]string{"a.md": "one", "sub/b.md": "b"})
			if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 1, Head: oidTest("c"), Tree: first}); err != nil {
				t.Fatalf("first Accept() = %v", err)
			}
			second := buildTree(t, w, map[string]string{"a.md": "two"})
			if err := w.Accept(context.Background(), Baseline{RemoteGeneration: 2, Head: oidTest("d"), Tree: second}); err != nil {
				t.Fatalf("second Accept() = %v", err)
			}

			after, err := os.Stat(w.Path())
			if err != nil {
				t.Fatalf("stat after: %v", err)
			}
			if !os.SameFile(before, after) {
				t.Fatal("materialization replaced the visible directory; every outside holder of it is now orphaned")
			}

			// The held descriptor is the shell's working directory in
			// miniature: reading through it must see the new tree, not an
			// unlinked one.
			names, err := held.Readdirnames(-1)
			if err != nil {
				t.Fatalf("read held directory: %v", err)
			}
			if len(names) == 0 {
				t.Fatal("the held directory reads as empty: it was unlinked and replaced")
			}
			if got := readFileBytes(t, filepath.Join(w.Path(), "a.md")); string(got) != "two" {
				t.Fatalf("a.md = %q, want the materialized content", got)
			}
		})
	}
}

// TestApplyBreaksHardLinks proves each file is replaced through its own
// temporary file, so a hard link into the notebook does not leak the new
// content to its other name.
func TestApplyBreaksHardLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link creation needs privileges on Windows")
	}
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
		t.Fatal("materialization preserved the hard link")
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
	// Materialization removes the link itself; the target outside the root
	// stays untouched.
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
