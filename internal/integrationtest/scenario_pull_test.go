package integrationtest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// emptyTreeID is the canonical empty Git tree (architecture section 10, L603):
// the first-pull baseline and the generation-0 remote tree.
const emptyTreeID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// TestScenarioPullFirstPull proves the first pull into a nonempty visible
// directory against an empty remote: OK with an empty success stat (the
// local additions are retained unchanged, so nothing changed on disk),
// local additions retained, the empty-tree generation-0 baseline recorded,
// and no remote state created (architecture section 10, L603).
func TestScenarioPullFirstPull(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	h.WriteFile(path+"/local.md", "keep me")

	h.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: path,
		Expect: CallExpectation{
			OK: true,
			Success: &SuccessExpectation{
				Generation: 0, FilesChanged: 0, Insertions: 0, Deletions: 0, Files: []FileStatExpectation{},
			},
		},
	}, h.Pull("", path))
	assertPulledMarker(t, h, path)
	assertRemoteGeneration(t, h, path, 0)
	rec := h.StateRecord(t, path)
	if rec.BaselineHead != "" {
		t.Fatalf("state baselineHead = %q, want the empty remote head", rec.BaselineHead)
	}
	if rec.BaselineTree != emptyTreeID {
		t.Fatalf("state baselineTree = %q, want the canonical empty tree %q", rec.BaselineTree, emptyTreeID)
	}
	if got := h.FSSnapshot(path); len(got) != 1 || got["local.md"] != "keep me" {
		t.Fatalf("L after the first pull = %v, want local.md retained", got)
	}
	h.assertExpectations(t, Expectations{
		S3: S3Assertions{
			NoCurrent: new(true),
			NoPacks:   new(true),
			Counts:    &CountExpectation{Ops: map[Op]int{OpGet: 1}},
		},
	})
}

// TestScenarioPullWarmPull proves that a pull with no local changes and an
// unchanged remote advances the baseline and reuses the cached pack bytes:
// a second pull performs no pack download (architecture section 10, L603).
func TestScenarioPullWarmPull(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "alpha", "c1")
	packKey := h.Manifest().Checkpoint.Key.String()

	// Cold pull: the empty cache downloads the checkpoint pack once.
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("cold pull pack gets = %d, want 1", got)
	}
	// Warm pull: the cache hit keeps the pack gets at one.
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 1 {
		t.Fatalf("warm pull pack gets = %d, want 1", got)
	}
	assertRemoteGeneration(t, b, pathB, 1)
	if got := b.FSSnapshot(pathB); got["a.md"] != "alpha" {
		t.Fatalf("L after warm pull = %v, want a.md", got)
	}
}

// TestScenarioPullAfterRemoteAdvance proves that a pull rebases local
// additions, modifications, and deletions on the remote head without
// discarding any mergeable local change (architecture section 10, L603).
func TestScenarioPullAfterRemoteAdvance(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "A1", "c1")
	commitNext(t, h, pathA, "c.md", "C", "c2")
	b.assertOK(t, b.Pull("", pathB))

	// B modifies a.md, deletes c.md, and adds b.md; A pulls clean.
	b.WriteFile(pathB+"/a.md", "B edit")
	b.RemoveFile(pathB + "/c.md")
	b.WriteFile(pathB+"/b.md", "B add")
	h.assertOK(t, h.Pull("", pathA))

	h.assertOK(t, b.Pull("", pathB))
	got := b.FSSnapshot(pathB)
	if got["a.md"] != "B edit" || got["b.md"] != "B add" {
		t.Fatalf("L after the rebased pull = %v, want the local changes on top of R", got)
	}
	if _, ok := got["c.md"]; ok {
		t.Fatalf("L after the rebased pull still contains the deleted c.md")
	}
	assertRemoteGeneration(t, b, pathB, 2)
}

// TestScenarioPullColdPull proves that a cold pull downloads only the
// missing descriptor packs and never reconstructs state by LIST
// (architecture section 10, L603): the list counter stays zero.
func TestScenarioPullColdPull(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "alpha", "c1")
	packKey := h.Manifest().Checkpoint.Key.String()
	commitNext(t, h, pathA, "b.md", "beta", "c2")
	m := h.Manifest()
	incKey := m.Increments[0].Key.String()

	b.assertOK(t, b.Pull("", pathB))
	rec := b.Recorder()
	if got := rec.Count(OpList); got != 0 {
		t.Fatalf("cold pull list count = %d, want 0 (state is never reconstructed by LIST)", got)
	}
	for _, key := range []string{packKey, incKey} {
		if got := rec.CountKey(OpGet, key); got != 1 {
			t.Fatalf("cold pull gets of %s = %d, want exactly one", key, got)
		}
	}
	if got := b.FSSnapshot(pathB); got["a.md"] != "alpha" || got["b.md"] != "beta" {
		t.Fatalf("L after the cold pull = %v, want both files", got)
	}
}

// TestScenarioPullConflict proves a conflicting pull: CONTENT_CONFLICT with
// the exact relative path and marker ranges, markers materialized into L,
// R recorded as the new baseline, local-only files preserved, and L never
// reverted (architecture section 10, L603).
func TestScenarioPullConflict(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "shared.md", "base\nline2\nline3\n", "c1")
	b.assertOK(t, b.Pull("", pathB))
	b.WriteFile(pathB+"/local-only.md", "mine")
	b.WriteFile(pathB+"/shared.md", "B\nline2\nline3\n")

	h.WriteFile(pathA+"/shared.md", "A\nline2\nline3\n")
	h.assertOK(t, h.Commit("", pathA, "c2"))

	res := b.Pull("", pathB)
	b.assertEnvelope(t, ToolCall{
		Tool: toolPull, Path: pathB,
		Expect: CallExpectation{
			ErrorCode: "CONTENT_CONFLICT",
			Files:     []FileExpectation{{Path: "shared.md", Ranges: []RangeExpectation{{Start: 1, End: 5}}}},
		},
	}, res)

	// L carries the exact marker grammar with the local side first and the
	// remote side second, over the reported one-based inclusive range.
	// Asserting the exact bytes is what makes the reported range meaningful.
	assertVisibleFiles(t, b, pathB, map[string]string{
		"local-only.md": "mine",
		"shared.md": "<<<<<<< local\n" +
			"B\n" +
			"=======\n" +
			"A\n" +
			">>>>>>> remote\n" +
			"line2\nline3\n",
	})
	assertRemoteGeneration(t, b, pathB, 2)
	assertPulledMarker(t, b, pathB)
}

// TestScenarioPullStaleReader proves the stale-reader restart (architecture
// section 10, L603): a reader blocked on a pack GET observes the pack deleted
// by a concurrent writer's checkpoint cleanup, rereads current, restarts,
// and completes with the current head. The barrier keeps the race
// deterministic over the real S3 backend.
func TestScenarioPullStaleReader(t *testing.T) {
	t.Parallel()
	a := NewHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{CheckpointPacks: new(1024)})
	pathA, pathB := a.Path("notes"), b.Path("notes")

	commitFirst(t, a, pathA, "f1.md", "v1", "c1")
	p1Key := a.Manifest().Checkpoint.Key.String()

	// B's pull blocks on the GET of P1; by then B's readCurrent already
	// observed the manifest referencing P1.
	b.Faults().BlockNext(OpGet, p1Key)
	pullCh := runCall(t, b, "", toolPull, pathB, "")
	b.eventually(t, settleTimeout, func() error {
		if !b.Faults().Waiting(OpGet, p1Key) {
			return fmt.Errorf("B is not waiting on the %s get", p1Key)
		}
		return nil
	})

	// A advances through two checkpoints; the second cleanup deletes P1.
	commitNext(t, a, pathA, "f2.md", "v2", "c2")
	commitNext(t, a, pathA, "f3.md", "v3", "c3")
	commitNext(t, a, pathA, "f4.md", "v4", "c4")
	commitNext(t, a, pathA, "f5.md", "v5", "c5")
	requireObjectGone(t, a, p1Key)

	currentReads := b.Recorder().CountKey(OpGet, storage.CurrentKey)
	listsBefore := b.Recorder().Count(OpList)
	b.Faults().Release(OpGet, p1Key)
	b.awaitCall(t, pullCh, ToolCall{Tool: toolPull, Path: pathB, Expect: CallExpectation{OK: true}})

	// The restart is driven by rereading the authoritative manifest, never
	// by inferring state from the object names still in the bucket.
	if got := b.Recorder().CountKey(OpGet, storage.CurrentKey); got <= currentReads {
		t.Fatalf("current reads after the deletion = %d, want a reread that restarts the pull", got-currentReads)
	}
	if got := b.Recorder().Count(OpList) - listsBefore; got != 0 {
		t.Fatalf("LIST calls during the stale restart = %d, want none", got)
	}

	// The restarted pull lands exactly on the current head.
	assertVisibleFiles(t, b, pathB, map[string]string{
		"f1.md": "v1", "f2.md": "v2", "f3.md": "v3", "f4.md": "v4", "f5.md": "v5",
	})
	assertRemoteGeneration(t, b, pathB, 7)
}

// TestScenarioPullCacheCorruption proves that a corrupt cached pack is
// never a false hit: the next pull discards it, re-downloads the verified
// bytes, and heals the cache (architecture section 8.3, L369).
func TestScenarioPullCacheCorruption(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "alpha", "c1")
	m := h.Manifest()
	packKey := m.Checkpoint.Key.String()
	b.assertOK(t, b.Pull("", pathB))

	// Corrupt the cached bytes of the checkpoint pack.
	cacheFile := filepath.Join(b.PackCacheDir(pathB), m.Checkpoint.SHA256.String())
	b.WriteFile(cacheFile, "corrupt cache entry")

	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 2 {
		t.Fatalf("pack gets after cache corruption = %d, want the re-download", got)
	}
	wantBytes, err := b.ReadObject(packKey)
	if err != nil {
		t.Fatalf("read pack through the raw store: %v", err)
	}
	gotBytes, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("cache entry after the pull: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("cache entry is not healed to the verified pack bytes")
	}
	// The corrupt bytes never reached the visible directory.
	assertVisibleFiles(t, b, pathB, map[string]string{"a.md": "alpha"})
	// The next pull is warm again: the healed cache stops the download.
	b.assertOK(t, b.Pull("", pathB))
	if got := b.Recorder().CountKey(OpGet, packKey); got != 2 {
		t.Fatalf("pack gets after the healed cache = %d, want the cached hit", got)
	}
}
