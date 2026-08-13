package integrationtest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioCommitFirstCommit proves the first publication: a root
// commit, a state-complete checkpoint pack, If-None-Match creation, no
// increment pack, and an independent pull that observes the files
// (architecture section 11.1, L650).
func TestScenarioCommitFirstCommit(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "alpha", "first commit")

	m := h.Manifest()
	if m.Generation != 1 {
		t.Fatalf("manifest generation = %d, want 1", m.Generation)
	}
	if m.Checkpoint.ID.String() == "" {
		t.Fatal("manifest checkpoint carries no id")
	}
	if len(m.Increments) != 0 {
		t.Fatalf("manifest increments = %d, want none", len(m.Increments))
	}
	rec := h.Recorder()
	if got := rec.CountKey(OpCreate, storage.CurrentKey); got != 1 {
		t.Fatalf("If-None-Match creations = %d, want 1", got)
	}
	if got := rec.Count(OpReplace); got != 0 {
		t.Fatalf("replaces = %d, want none for the first publication", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/checkpoints/"); got != 1 {
		t.Fatalf("checkpoint puts = %d, want the state-complete pack", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/increments/"); got != 0 {
		t.Fatalf("increment puts = %d, want none for the first publication", got)
	}

	b.assertOK(t, b.Pull("", pathB))
	if got := b.FSSnapshot(pathB); got["a.md"] != "alpha" {
		t.Fatalf("independent pull observes %v, want a.md", got)
	}
}

// TestScenarioCommitNormalCommit proves a normal publication: exactly one
// incremental pack precedes the manifest CAS and no full checkpoint is
// published for a single increment (architecture section 11.2, L712).
func TestScenarioCommitNormalCommit(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	commitNext(t, h, path, "b.md", "beta", "c2")

	m := h.Manifest()
	if m.Generation != 2 {
		t.Fatalf("manifest generation = %d, want 2", m.Generation)
	}
	if len(m.Increments) != 1 {
		t.Fatalf("manifest increments = %d, want 1", len(m.Increments))
	}
	rec := h.Recorder()
	if got := rec.CountKey(OpReplace, storage.CurrentKey); got != 1 {
		t.Fatalf("replaces = %d, want the single manifest CAS", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/increments/"); got != 1 {
		t.Fatalf("increment puts = %d, want one", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/checkpoints/"); got != 1 {
		t.Fatalf("checkpoint puts = %d, want the first-commit pack only", got)
	}
	// The pack the manifest names exists, so the upload preceded the CAS
	// that referenced it.
	if !h.ObjectExists(m.Increments[0].Key.String()) {
		t.Fatalf("increment pack %s is absent although the manifest references it", m.Increments[0].Key)
	}
	// An independent client observes the published change.
	reader := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{"a.md": "alpha", "b.md": "beta"})
}

// TestScenarioCommitNoChange proves that a commit whose local tree equals
// the remote state synchronizes L and P without any publication ID, pack,
// commit, or CAS request (architecture section 11.3, L733).
func TestScenarioCommitNoChange(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	before := h.Recorder().Snapshot()
	h.assertOK(t, h.Commit("", path, "no changes here"))
	after := h.Recorder().Snapshot()
	for _, op := range []Op{OpPut, OpCreate, OpReplace, OpDelete} {
		if after[op] != before[op] {
			t.Fatalf("no-change commit mutated the store: %s %d -> %d", op, before[op], after[op])
		}
	}
	assertRemoteGeneration(t, h, path, 1)
}

// TestScenarioCommitWithoutPull proves that a commit on an unmanaged path
// is refused as INVALID_REQUEST before any S3 access (architecture
// section 11.1, L650).
func TestScenarioCommitWithoutPull(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	h.WriteFile(path+"/a.md", "alpha")

	res := h.Commit("", path, "never pulled")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "never pulled",
		Expect: CallExpectation{ErrorCode: "INVALID_REQUEST"},
	}, res)
	h.assertExpectations(t, Expectations{
		S3: S3Assertions{Counts: &CountExpectation{AllZero: true}},
	})
}

// TestScenarioCommitLRewriteAfterCommit proves that a conflict-free commit
// rewrites L to the accepted merged tree: the caller's own edits survive and
// the concurrent remote changes it never saw appear, so no stale bytes
// outlive the acceptance (architecture section 11.1, L650).
//
// B's L is genuinely stale at commit time: A advances the remote AFTER B
// pulled, so the accepted merged tree is a strict superset of B's
// possible local view.
func TestScenarioCommitLRewriteAfterCommit(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	b := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	pathA, pathB := h.Path("notes"), b.Path("notes")

	commitFirst(t, h, pathA, "a.md", "alpha", "c1")
	b.assertOK(t, b.Pull("", pathB))

	// B edits its now-stale view: it deletes a.md and adds b.md.
	b.RemoveFile(pathB + "/a.md")
	b.WriteFile(pathB+"/b.md", "beta")

	// A publishes a disjoint file that B has never seen.
	commitNext(t, h, pathA, "remote.md", "from A", "c2")

	// B's commit merges and is accepted, and B's L is rewritten to the
	// accepted tree: its own delete and add stand, and A's file arrives.
	b.assertOK(t, b.Commit("", pathB, "delete a, add b"))
	assertVisibleFiles(t, b, pathB, map[string]string{
		"b.md":      "beta",
		"remote.md": "from A",
	})
	assertRemoteGeneration(t, b, pathB, 3)

	// The accepted state is exactly what B now sees.
	reader := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{
		"b.md":      "beta",
		"remote.md": "from A",
	})
}

// TestScenarioCommitTwoDisjointCommitsRace proves that two concurrent
// first publications on distinct paths produce one linear accepted state
// carrying both changes: exactly one writer wins the create, the loser
// merges and retries through the manifest CAS, and no update is lost
// (architecture section 11.2, L712).
//
// The race is made deterministic by a barrier rather than by timing: B's
// conditional create of `current` blocks until A's publication is accepted,
// so B necessarily loses its first attempt and must retry.
func TestScenarioCommitTwoDisjointCommitsRace(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, HarnessConfig{})
	pathA, pathB := h.Path("notes-a"), h.Path("notes-b")

	h.assertOK(t, h.Pull("a", pathA))
	h.assertOK(t, h.Pull("b", pathB))
	h.WriteFile(pathA+"/a.md", "A")
	h.WriteFile(pathB+"/b.md", "B")

	// B reaches the conditional create first and parks there; A then
	// publishes generation 1 unobstructed. When B is released, its create
	// fails the If-None-Match precondition and B merges and retries.
	h.Faults().BlockNext(OpCreate, storage.CurrentKey)
	bCh := runCall(t, h, "b", toolCommit, pathB, "B")
	h.eventually(t, settleTimeout, func() error {
		if !h.Faults().Waiting(OpCreate, storage.CurrentKey) {
			return fmt.Errorf("B has not reached the conditional create")
		}
		return nil
	})
	h.assertOK(t, h.Commit("a", pathA, "A"))
	h.Faults().Release(OpCreate, storage.CurrentKey)
	h.awaitCall(t, bCh, ToolCall{Tool: toolCommit, Path: pathB, Message: "B", Expect: CallExpectation{OK: true}})

	// One linear accepted state: A's root publication plus B's retried
	// increment. Both writers observed exactly one create attempt, and only
	// the loser replaced the manifest.
	m := h.Manifest()
	if m.Generation != 2 || len(m.Increments) != 1 {
		t.Fatalf("accepted state = gen %d with %d increments, want 2/1", m.Generation, len(m.Increments))
	}
	rec := h.Recorder()
	if got := rec.CountKey(OpCreate, storage.CurrentKey); got != 2 {
		t.Fatalf("conditional creates = %d, want one per writer (the loser's must fail)", got)
	}
	if got := rec.CountKey(OpReplace, storage.CurrentKey); got != 1 {
		t.Fatalf("manifest replaces = %d, want exactly the loser's retry", got)
	}

	// No lost update: an independent reader observes both files.
	reader := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{"a.md": "A", "b.md": "B"})
}

// TestScenarioCommitTwoOverlappingCommitsRace proves that two concurrent
// first publications of the same relative file with different content yield
// exactly one OK and one CONTENT_CONFLICT naming the shared path: the
// accepted state stays valid and no false success is reported (architecture
// section 11.2, L712; section 12, L763).
//
// The loser is chosen by a barrier, not by scheduling: B parks on the
// conditional create until A's publication is accepted, so B is always the
// writer that must merge and always the one that conflicts.
func TestScenarioCommitTwoOverlappingCommitsRace(t *testing.T) {
	t.Parallel()
	a := NewHarness(t, HarnessConfig{})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	pathA, pathB := a.Path("notes"), b.Path("notes")

	a.assertOK(t, a.Pull("", pathA))
	b.assertOK(t, b.Pull("", pathB))
	a.WriteFile(pathA+"/shared.md", "content A\n")
	b.WriteFile(pathB+"/shared.md", "content B\n")

	b.Faults().BlockNext(OpCreate, storage.CurrentKey)
	bCh := runCall(t, b, "", toolCommit, pathB, "B")
	b.eventually(t, settleTimeout, func() error {
		if !b.Faults().Waiting(OpCreate, storage.CurrentKey) {
			return fmt.Errorf("B has not reached the conditional create")
		}
		return nil
	})
	a.assertOK(t, a.Commit("", pathA, "A"))
	b.Faults().Release(OpCreate, storage.CurrentKey)
	b.awaitCall(t, bCh, ToolCall{
		Tool: toolCommit, Path: pathB, Message: "B",
		Expect: CallExpectation{
			ErrorCode: "CONTENT_CONFLICT",
			Retryable: new(false),
			Files:     []FileExpectation{{Path: "shared.md"}},
		},
	})

	// The accepted state is the winner's, unchanged by the conflicting
	// writer, and an independent reader reads exactly it.
	m := a.Manifest()
	if m.Generation != 1 || len(m.Increments) != 0 {
		t.Fatalf("accepted state = gen %d with %d increments, want the single winner at 1/0", m.Generation, len(m.Increments))
	}
	reader := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{"shared.md": "content A\n"})
}

// TestScenarioCommitCASLossRetry proves that one injected precondition
// failure on the manifest CAS triggers a fresh proposal with a new
// publication ID, generation, key, commit, and pack, and that the losing
// pack is never republished (architecture section 11.3, L733).
func TestScenarioCommitCASLossRetry(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	h.Faults().FailNext(OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	commitNext(t, h, path, "b.md", "beta", "c2")

	m := h.Manifest()
	if m.Generation != 2 {
		t.Fatalf("manifest generation = %d, want 2", m.Generation)
	}
	if len(m.Increments) != 1 {
		t.Fatalf("manifest increments = %d, want the retried proposal", len(m.Increments))
	}
	rec := h.Recorder()
	if got := rec.CountKey(OpReplace, storage.CurrentKey); got != 2 {
		t.Fatalf("manifest replaces = %d, want the lost attempt and the retry", got)
	}

	// The retry is a fresh proposal, not a republication: two DISTINCT
	// gen-2 pack keys were uploaded, each exactly once, and the accepted
	// manifest references exactly one of them. Counting calls alone cannot
	// tell a new publication ID from the same key written twice.
	uploaded := rec.KeysWithPrefix(OpPut, "packs/increments/2-")
	if len(uploaded) != 2 {
		t.Fatalf("distinct gen-2 increment keys = %v, want the losing and the retried publication", uploaded)
	}
	for _, key := range uploaded {
		if got := rec.CountKey(OpPut, key); got != 1 {
			t.Fatalf("pack %s was uploaded %d times, want once", key, got)
		}
	}
	accepted := m.Increments[0].Key.String()
	losing := ""
	for _, key := range uploaded {
		if key != accepted {
			losing = key
		}
	}
	if losing == "" {
		t.Fatalf("the accepted key %s is not among the uploaded keys %v", accepted, uploaded)
	}
	// The losing pack is never republished at a later generation: it is
	// unreferenced by the accepted state.
	if m.Checkpoint.Key.String() == losing {
		t.Fatalf("the losing pack %s became the checkpoint descriptor", losing)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/increments/3-"); got != 0 {
		t.Fatalf("gen-3 increment uploads = %d, want none: the loser must not be republished later", got)
	}
}

// TestScenarioCommitRetryExhaustion proves that a CAS losing every attempt
// up to the configured bound returns REMOTE_BUSY, preserves the caller's
// files, and never returns OK (architecture section 11.3, L733).
func TestScenarioCommitRetryExhaustion(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{RetryLimit: new(2)})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	h.WriteFile(path+"/b.md", "beta")
	h.Faults().FailAlways(OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	res := h.Commit("", path, "will never win")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "will never win",
		Expect: CallExpectation{ErrorCode: "REMOTE_BUSY", Retryable: new(true)},
	}, res)

	got := h.FSSnapshot(path)
	if got["a.md"] != "alpha" || got["b.md"] != "beta" {
		t.Fatalf("L after exhaustion = %v, want the caller's files preserved", got)
	}
	if got := h.Recorder().CountKey(OpReplace, storage.CurrentKey); got != 3 {
		t.Fatalf("manifest replaces = %d, want retryLimit+1 = 3 attempts", got)
	}
	m := h.Manifest()
	if m.Generation != 1 {
		t.Fatalf("manifest generation = %d, want the accepted state 1 unchanged", m.Generation)
	}
}

// TestScenarioCommitAmbiguousPackUpload proves that a pack upload whose
// response is lost is resolved by reading the unique key back and proving
// its bytes: the commit succeeds and never treats the pack alone as a
// publication (architecture section 11.3, L733).
func TestScenarioCommitAmbiguousPackUpload(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	h.Faults().AmbiguousNextOp(OpPut)
	commitNext(t, h, path, "b.md", "beta", "c2")

	m := h.Manifest()
	if len(m.Increments) != 1 {
		t.Fatalf("manifest increments = %d, want the accepted increment", len(m.Increments))
	}
	incKey := m.Increments[0].Key.String()
	if !h.ObjectExists(incKey) {
		t.Fatalf("the ambiguous increment pack %s is absent", incKey)
	}
	// The ambiguity is resolved by reading the unique key back exactly once
	// and comparing the stored bytes; the pack alone is never treated as a
	// publication, which the accepted manifest reference proves.
	if got := h.Recorder().CountKey(OpGet, incKey); got != 1 {
		t.Fatalf("read-backs of %s = %d, want exactly the unique-key proof", incKey, got)
	}
	data, err := h.ReadObject(incKey)
	if err != nil {
		t.Fatalf("read the accepted increment pack: %v", err)
	}
	if uint64(len(data)) != m.Increments[0].Size {
		t.Fatalf("accepted pack size = %d, want the descriptor's %d", len(data), m.Increments[0].Size)
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != m.Increments[0].SHA256.String() {
		t.Fatalf("accepted pack digest = %x, want the descriptor's %s", got, m.Increments[0].SHA256)
	}

	// The read-back must actually decide: when the stored bytes do not
	// match, the commit refuses rather than accepting the upload.
	h.Faults().AmbiguousNextOp(OpPut)
	h.Faults().CorruptReadPrefix("packs/increments/")
	h.WriteFile(path+"/c.md", "gamma")
	res := h.Commit("", path, "c3")
	env := decodeEnvelope(t, ToolCall{Tool: toolCommit, Path: path, Message: "c3"}, res)
	if env.Code != "STORAGE_INTEGRITY" && env.Code != "STORAGE_FAILURE" {
		t.Fatalf("mismatched read-back code = %q, want a refusal category", env.Code)
	}
	if got := h.Manifest().Generation; got != 2 {
		t.Fatalf("manifest generation = %d, want the unaccepted proposal to leave 2", got)
	}
}

// TestScenarioCommitAmbiguousCASIDFound proves that a manifest CAS whose
// response is lost succeeds when the publication ID is found in an accepted
// descriptor, without a duplicate logical change (architecture section
// 11.3).
func TestScenarioCommitAmbiguousCASIDFound(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	h.Faults().AmbiguousNext(OpReplace, storage.CurrentKey)
	commitNext(t, h, path, "b.md", "beta", "c2")

	m := h.Manifest()
	if m.Generation != 2 {
		t.Fatalf("manifest generation = %d, want the ambiguous CAS landed at 2", m.Generation)
	}
	if len(m.Increments) != 1 {
		t.Fatalf("manifest increments = %d, want exactly one publication", len(m.Increments))
	}
}

// TestScenarioCommitUnprovableCAS proves that a landed manifest CAS whose
// follow-up read fails returns STORAGE_FAILURE: the caller's files stay
// preserved, P does not advance, and the proposal is never automatically
// republished (architecture section 11.3, L733).
func TestScenarioCommitUnprovableCAS(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{})
	path := h.Path("notes")
	commitFirst(t, h, path, "a.md", "alpha", "c1")

	h.WriteFile(path+"/b.md", "beta")
	h.Faults().UnprovableNext(storage.CurrentKey)
	res := h.Commit("", path, "acceptance unprovable")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "acceptance unprovable",
		Expect: CallExpectation{ErrorCode: "STORAGE_FAILURE", Retryable: new(true)},
	}, res)

	got := h.FSSnapshot(path)
	if got["a.md"] != "alpha" || got["b.md"] != "beta" {
		t.Fatalf("L after the unprovable CAS = %v, want the caller's files preserved", got)
	}
	assertRemoteGeneration(t, h, path, 1)
	if got := h.Recorder().CountKey(OpReplace, storage.CurrentKey); got != 1 {
		t.Fatalf("manifest replaces = %d, want exactly one without republication", got)
	}
	// The remote manifest did land: the honest assertion is the visible
	// workspace and the replace count, not the remote generation.
	if m := h.Manifest(); m.Generation != 2 {
		t.Fatalf("remote manifest generation = %d, want the landed proposal at 2", m.Generation)
	}
}
