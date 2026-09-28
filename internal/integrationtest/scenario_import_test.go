package integrationtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioPullImportsOnlyMissingPacks proves a pack already imported
// into the private repository is never fetched or imported again
// (architecture/pull.md): with the private pack cache emptied and every pack
// download failing, a pull of unchanged remote state succeeds without one
// pack read, and a later publication costs exactly its own new pack.
func TestScenarioPullImportsOnlyMissingPacks(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "a.md", "alpha", "c1")
	commitNext(t, a, pathA, "b.md", "beta", "c2")
	b.assertOK(t, b.Pull("", pathB))
	before := b.Recorder().CountKeyPrefix(OpGet, "packs/")

	// Forget the bytes and forbid every download: only the repository can
	// satisfy the next pull.
	if err := os.RemoveAll(b.PackCacheDir(pathB)); err != nil {
		t.Fatalf("remove the private pack cache: %v", err)
	}
	b.Faults().FailAlwaysPrefix(OpGet, "packs/", storage.ErrTransport)
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKeyPrefix(OpGet, "packs/"); got != before {
		t.Fatalf("pack reads for an unchanged tail = %d, want none beyond the %d of the first pull", got, before)
	}
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha", "b.md": "beta"})

	// A new publication is the only pack the next pull needs: it is fetched
	// (and fails) alone, then succeeds alone once downloads work again.
	commitNext(t, a, pathA, "c.md", "gamma", "c3")
	newKey := a.Manifest().Increments[1].Key.String()
	if res := b.Pull("", pathB); !res.IsError {
		t.Fatal("pull with the new pack undownloadable succeeded")
	}
	b.Faults().ClearFailures()
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKey(OpGet, newKey); got != 2 {
		t.Fatalf("reads of the new pack = %d, want one failed and one successful", got)
	}
	if got := b.Recorder().CountKeyPrefix(OpGet, "packs/"); got != before+2 {
		t.Fatalf("pack reads = %d, want %d: the older packs were re-fetched", got, before+2)
	}
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha", "b.md": "beta", "c.md": "gamma"})
	assertRemoteGeneration(t, b, pathB, 3)
}

// TestScenarioPullRepairsDamagedRepository proves the private repository is
// a cache, never an authority (architecture/pull.md): a new process over a
// private state whose pack files were partly or wholly deleted re-imports
// from verified bytes and pulls the exact accepted state. The partial case
// keeps the pack holding the head, so the head's presence alone would lie;
// the caller has also deleted a.md locally, so its blob is not rewritten
// from the visible directory and the head tree really has a gap.
func TestScenarioPullRepairsDamagedRepository(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "a.md", "alpha", "c1")
	commitNext(t, a, pathA, "b.md", "beta", "c2")
	b.assertOK(t, b.Pull("", pathB))
	want := map[string]string{"a.md": "alpha", "b.md": "beta"}
	packDir := filepath.Join(b.PrivateDir(pathB), "repo", ".git", "objects", "pack")
	reopen := func() *Harness {
		return newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{
			WorkspaceRoot: b.WorkspaceRoot(), PrivateRoot: b.PrivateRoot(),
		})
	}

	// Partial damage: the checkpoint pack (a.md's blob) is gone while the
	// increment pack holding the head survives, and a.md is deleted locally.
	removePackOfSize(t, packDir, a.Manifest().Checkpoint.Size)
	b.RemoveFile(filepath.Join(pathB, "a.md"))
	c := reopen()
	c.assertOK(t, c.Pull("", pathB))
	assertVisibleFiles(t, c, pathB, map[string]string{"b.md": "beta"})
	if got := countPacks(t, packDir); got != 2 {
		t.Fatalf("pack files after the repair = %d, want the checkpoint pack restored (2)", got)
	}
	// The local deletion is deliberately not published: a commit would make
	// the head a locally built commit whose objects live loose, and the
	// total-damage case below must hit an imported head.
	want = map[string]string{"b.md": "beta"}

	// Total damage: every pack file is gone.
	for _, f := range packFiles(t, packDir) {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	d := reopen()
	d.assertOK(t, d.Pull("", pathB))
	assertVisibleFiles(t, d, pathB, want)
	if got := countPacks(t, packDir); got != 2 {
		t.Fatalf("pack files after the full repair = %d, want 2", got)
	}
	assertRemoteGeneration(t, d, pathB, 2)
}

// packFiles lists the pack and index files of a private repository.
func packFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "pack-*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func countPacks(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, f := range packFiles(t, dir) {
		if filepath.Ext(f) == ".pack" {
			n++
		}
	}
	return n
}

// removePackOfSize deletes the one imported pack whose bytes are the
// descriptor's (libgit2 stores an imported pack verbatim) and its index.
func removePackOfSize(t *testing.T, dir string, size uint64) {
	t.Helper()
	var victims []string
	for _, f := range packFiles(t, dir) {
		if filepath.Ext(f) != ".pack" {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(info.Size()) == size {
			victims = append(victims, f)
		}
	}
	if len(victims) != 1 {
		t.Fatalf("packs of %d bytes = %v, want exactly one", size, victims)
	}
	for _, f := range []string{victims[0], victims[0][:len(victims[0])-len(".pack")] + ".idx"} {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
}
