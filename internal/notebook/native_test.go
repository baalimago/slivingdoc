package notebook

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// openNativeEngine opens one real libgit2 engine and registers its close
// with the test, so the cgo suites share the exact engine bootstrap.
func openNativeEngine(t *testing.T) git.Engine {
	t.Helper()
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// newNativeNotebook builds a notebook over the real libgit2 engine and a
// real workspace. The object store stays faked; the publication order runs
// against real Git objects and packs.
func newNativeNotebook(t *testing.T, store storage.ObjectStore, ids *testIDSource) (*Notebook, *workspace.Workspace, workspace.Config) {
	t.Helper()
	return newNotebook(t, nbConfig{store: store, engine: openNativeEngine(t), ids: ids})
}

// TestNativeFirstCommitAndPullRoundTrip proves the complete publication
// order with real libgit2: a first commit creates a checkpoint pack and an
// independent pull imports and verifies the accepted result.
func TestNativeFirstCommitAndPullRoundTrip(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNativeNotebook(t, store, ids)

	writeLocal(t, aw, map[string]string{"a.md": "hello native", "sub/b.md": "nested"})
	pullOK(t, a)
	commitOK(t, a, "first")

	m := readManifest(t, store)
	if m.Generation != 1 || len(m.Increments) != 0 {
		t.Fatalf("manifest = generation %d increments %d, want the first checkpoint", m.Generation, len(m.Increments))
	}
	rc, _, err := store.ReadObject(context.Background(), m.Checkpoint.Key.String())
	if err != nil {
		t.Fatalf("read checkpoint pack = %v", err)
	}
	packBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read pack body = %v", err)
	}
	rc.Close()
	if len(packBytes) != int(m.Checkpoint.Size) {
		t.Fatalf("pack size = %d, want %d", len(packBytes), m.Checkpoint.Size)
	}

	// An independent pull imports the checkpoint pack into a fresh
	// repository and verifies every accepted commit result.
	b, bw, _ := newNativeNotebook(t, store, ids)
	pullOK(t, b)
	got := localSnapshot(t, bw)
	if got["a.md"] != "hello native" || got["sub/b.md"] != "nested" {
		t.Fatalf("L after independent pull = %v, want the published files", got)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}
}

// TestNativeTwoWriterRace proves two writers over real libgit2 converge on
// one linear accepted state: the loser merges the winner's change and
// retries with a fresh proposal.
func TestNativeTwoWriterRace(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNativeNotebook(t, store, ids)
	b, bw, _ := newNativeNotebook(t, store, ids)

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)

	writeLocal(t, aw, map[string]string{"a.md": "A"})
	commitOK(t, a, "A change")

	writeLocal(t, bw, map[string]string{"b.md": "B"})
	store.FailNextKey(fake.OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	commitOK(t, b, "B change")

	m := readManifest(t, store)
	if m.Generation != 3 {
		t.Fatalf("generation = %d, want 3", m.Generation)
	}
	got := localSnapshot(t, bw)
	if got["shared.md"] != "base" || got["a.md"] != "A" || got["b.md"] != "B" {
		t.Fatalf("L after the race = %v, want both changes", got)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 3 {
		t.Fatalf("B baseline generation = %d, want 3", gen)
	}
}

// TestNativePackBeforeCAS proves with real libgit2 that the immutable pack
// is stored before the manifest CAS accepts the proposal.
func TestNativePackBeforeCAS(t *testing.T) {
	store := fake.New("")
	gate := &casGateStore{ObjectStore: store, entered: make(chan struct{}), release: make(chan struct{})}
	ids := &testIDSource{}
	nb, w, _ := newNativeNotebook(t, gate, ids)

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	done := make(chan error, 1)
	go func() { done <- errOnly(nb.Commit(context.Background(), "first")) }()
	<-gate.entered

	cpKey := "packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack"
	if _, _, err := gate.ReadObject(context.Background(), cpKey); err != nil {
		t.Fatalf("checkpoint pack absent while the CAS is blocked: %v", err)
	}
	if _, _, err := gate.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("current exists before the CAS ran: %v", err)
	}
	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	m := readManifest(t, gate)
	if m.Checkpoint.Key.String() != cpKey {
		t.Fatalf("manifest references %q, want %q", m.Checkpoint.Key, cpKey)
	}
}
