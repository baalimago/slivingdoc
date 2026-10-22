package sync

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioPullImportsOnlyMissingPacks proves a pack already imported
// into the private repository is never fetched or imported again
// (architecture/pull.md): with the private pack cache emptied and every pack
// download failing, a pull of unchanged remote state succeeds without one
// pack read, and a later publication costs exactly its own new pack.
func TestScenarioPullImportsOnlyMissingPacks(t *testing.T) {
	t.Parallel()
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{})
	CommitFirst(t, a, "a.md", "alpha", "c1")
	CommitNext(t, a, "b.md", "beta", "c2")
	b.AssertOK(t, b.Pull(""))
	before := b.Recorder().CountKeyPrefix(OpGet, "packs/")

	// Forget the bytes and forbid every download: only the repository can
	// satisfy the next pull.
	if err := os.RemoveAll(b.PackCacheDir()); err != nil {
		t.Fatalf("remove the private pack cache: %v", err)
	}
	b.Faults().FailAlwaysPrefix(OpGet, "packs/", storage.ErrTransport)
	b.AssertOK(t, b.Pull(""))
	if got := b.Recorder().CountKeyPrefix(OpGet, "packs/"); got != before {
		t.Fatalf("pack reads for an unchanged tail = %d, want none beyond the %d of the first pull", got, before)
	}
	AssertVisibleFiles(t, b, map[string]string{"a.md": "alpha", "b.md": "beta"})

	// A new publication is the only pack the next pull needs: it is fetched
	// (and fails) alone, then succeeds alone once downloads work again.
	CommitNext(t, a, "c.md", "gamma", "c3")
	newKey := a.Manifest().Increments[1].Key.String()
	if res := b.Pull(""); !res.IsError {
		t.Fatal("pull with the new pack undownloadable succeeded")
	}
	b.Faults().ClearFailures()
	b.AssertOK(t, b.Pull(""))
	if got := b.Recorder().CountKey(OpGet, newKey); got != 2 {
		t.Fatalf("reads of the new pack = %d, want one failed and one successful", got)
	}
	if got := b.Recorder().CountKeyPrefix(OpGet, "packs/"); got != before+2 {
		t.Fatalf("pack reads = %d, want %d: the older packs were re-fetched", got, before+2)
	}
	AssertVisibleFiles(t, b, map[string]string{"a.md": "alpha", "b.md": "beta", "c.md": "gamma"})
	AssertRemoteGeneration(t, b, 3)
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
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{})
	pathB := b.WorkspaceRoot()
	CommitFirst(t, a, "a.md", "alpha", "c1")
	CommitNext(t, a, "b.md", "beta", "c2")
	b.AssertOK(t, b.Pull(""))
	want := map[string]string{"a.md": "alpha", "b.md": "beta"}
	packDir := filepath.Join(b.PrivateDir(), "repo", ".git", "objects", "pack")
	reopen := func() *Harness {
		return NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{
			WorkspaceRoot: b.WorkspaceRoot(), PrivateRoot: b.PrivateRoot(),
		})
	}

	// Partial damage: the checkpoint pack (a.md's blob) is gone while the
	// increment pack holding the head survives, and a.md is deleted locally.
	RemovePackOfSize(t, packDir, a.Manifest().Checkpoint.Size)
	b.RemoveFile(filepath.Join(pathB, "a.md"))
	c := reopen()
	c.AssertOK(t, c.Pull(""))
	AssertVisibleFiles(t, c, map[string]string{"b.md": "beta"})
	if got := CountPacks(t, packDir); got != 2 {
		t.Fatalf("pack files after the repair = %d, want the checkpoint pack restored (2)", got)
	}
	// The local deletion is deliberately not published: a commit would make
	// the head a locally built commit whose objects live loose, and the
	// total-damage case below must hit an imported head.
	want = map[string]string{"b.md": "beta"}

	// Total damage: every pack file is gone.
	for _, f := range PackFiles(t, packDir) {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	d := reopen()
	d.AssertOK(t, d.Pull(""))
	AssertVisibleFiles(t, d, want)
	if got := CountPacks(t, packDir); got != 2 {
		t.Fatalf("pack files after the full repair = %d, want 2", got)
	}
	AssertRemoteGeneration(t, d, 2)
}
