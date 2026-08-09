package integrationtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// TestScenarioCheckpointThresholdTrigger proves that an accepted tail at
// the configured threshold schedules one checkpoint: the manifest advances
// to the compacted state with a fresh checkpoint ID, an empty tail, and
// the new cutoff checkpoint pack, and no lock object ever appears
// (architecture section 13.1, L815).
func TestScenarioCheckpointThresholdTrigger(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	m1 := h.Manifest()
	commitNext(t, h, path, "f2.md", "v2", "c2")
	commitNext(t, h, path, "f3.md", "v3", "c3")

	m := h.Manifest()
	if m.Generation != 4 {
		t.Fatalf("manifest generation = %d, want the compacted 4", m.Generation)
	}
	if m.Checkpoint.ID.String() == m1.Checkpoint.ID.String() {
		t.Fatal("checkpoint id did not change across the compaction")
	}
	if m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("checkpoint throughGeneration = %d, want the cutoff 3", m.Checkpoint.ThroughGeneration)
	}
	if len(m.Increments) != 0 {
		t.Fatalf("manifest increments = %d, want the compacted empty tail", len(m.Increments))
	}
	// The pack the manifest names is present, and it was uploaded before
	// the manifest that references it could be read back.
	if !h.ObjectExists(m.Checkpoint.Key.String()) {
		t.Fatalf("the cutoff checkpoint pack %s is absent", m.Checkpoint.Key)
	}
	// No writer lock: every object in the bucket lives in the manifest key
	// or one of the two pack namespaces. The namespaces are compared as
	// literal prefixes rather than through the server's own key parser, so
	// a self-consistently wrong key grammar cannot satisfy the assertion.
	for _, key := range h.ListObjects("") {
		switch {
		case key == storage.CurrentKey:
		case strings.HasPrefix(key, "packs/checkpoints/"):
		case strings.HasPrefix(key, "packs/increments/"):
		default:
			t.Fatalf("unexpected object %q: the protocol has no lock or registry object", key)
		}
	}
}

// TestScenarioCheckpointWritersAdvanceDuringBuild proves that later
// increments accepted while a checkpoint builds survive the compaction: the
// checkpoint CAS replaces only its stable selected prefix and preserves the
// later tail (architecture section 13.3, L864).
func TestScenarioCheckpointWritersAdvanceDuringBuild(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{CheckpointPacks: new(1024)})
	pathA, pathB := a.Path("notes"), b.Path("notes")

	commitFirst(t, a, pathA, "f1.md", "v1", "c1")
	commitNext(t, a, pathA, "f2.md", "v2", "c2")
	b.assertOK(t, b.Pull("", pathB))
	b.WriteFile(pathB+"/b.md", "B")
	a.WriteFile(pathA+"/f3.md", "v3")

	// A's third commit publishes M3 and then blocks inside its checkpoint
	// pack upload; the published M3 is the proof that B's later increment
	// builds on the pre-checkpoint state.
	a.Faults().BlockPrefix(OpPut, "packs/checkpoints/3-")
	aCh := runCall(t, a, "", toolCommit, pathA, "c3")
	a.eventually(t, settleTimeout, func() error {
		if !a.Faults().WaitingPrefix(OpPut, "packs/checkpoints/3-") {
			return fmt.Errorf("A is not blocked in the checkpoint upload")
		}
		if m := a.Manifest(); m.Generation != 3 {
			return fmt.Errorf("M3 not yet published (generation %d)", m.Generation)
		}
		return nil
	})

	// B commits while A's checkpoint is blocked: B reads M3 and publishes
	// increment I4 with no checkpoint of its own (tail 3 below 1024).
	b.assertOK(t, b.Commit("", pathB, "B advances"))
	if m := a.Manifest(); m.Generation != 4 {
		t.Fatalf("manifest generation = %d, want B's increment at 4", m.Generation)
	}

	a.Faults().ReleasePrefix(OpPut, "packs/checkpoints/3-")
	a.awaitCall(t, aCh, ToolCall{Tool: toolCommit, Path: pathA, Message: "c3", Expect: CallExpectation{OK: true}})

	m := a.Manifest()
	if m.Generation != 5 {
		t.Fatalf("manifest generation = %d, want the checkpoint CAS at 5", m.Generation)
	}
	if m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("checkpoint throughGeneration = %d, want 3", m.Checkpoint.ThroughGeneration)
	}
	if len(m.Increments) != 1 || m.Increments[0].Generation != 4 {
		t.Fatalf("manifest increments = %+v, want B's I4 preserved after the compaction", m.Increments)
	}
	if got := b.FSSnapshot(pathB); got["b.md"] != "B" || got["f3.md"] != "v3" {
		t.Fatalf("B L after the compaction = %v, want the later increment preserved", got)
	}
}

// TestScenarioCheckpointCompetingWorkers proves that two workers compacting
// the same prefix leave exactly one physical index: one CAS wins, the loser
// discards its proposal after observing the prefix gone, and cleanup
// deletes the unreferenced checkpoint pack (architecture section 13.3, L864).
func TestScenarioCheckpointCompetingWorkers(t *testing.T) {
	t.Parallel()
	a := NewHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	b := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{CheckpointPacks: new(2)})
	pathA, pathB := a.Path("notes"), b.Path("notes")

	commitFirst(t, a, pathA, "f1.md", "v1", "c1")
	commitNext(t, a, pathA, "f2.md", "v2", "c2")
	b.assertOK(t, b.Pull("", pathB))
	b.WriteFile(pathB+"/b.md", "B")
	a.WriteFile(pathA+"/f3.md", "v3")

	a.Faults().BlockPrefix(OpPut, "packs/checkpoints/3-")
	b.Faults().BlockPrefix(OpPut, "packs/checkpoints/3-")
	aCh := runCall(t, a, "", toolCommit, pathA, "c3")
	a.eventually(t, settleTimeout, func() error {
		if !a.Faults().WaitingPrefix(OpPut, "packs/checkpoints/3-") {
			return fmt.Errorf("A is not blocked in the checkpoint upload")
		}
		return nil
	})
	bCh := runCall(t, b, "", toolCommit, pathB, "B competes")
	b.eventually(t, settleTimeout, func() error {
		if !b.Faults().WaitingPrefix(OpPut, "packs/checkpoints/3-") {
			return fmt.Errorf("B is not blocked in the checkpoint upload")
		}
		return nil
	})

	// Both workers are now parked in the checkpoint upload, which means each
	// has already published its own increment: B's next manifest CAS is its
	// checkpoint CAS. Blocking it here makes A the compaction winner on
	// every run. Without it B could win and run its cleanup while A was
	// still blocked in the upload, leaving A's proposal unreferenced with no
	// later checkpoint to collect it. Cleanup is best-effort and generation
	// fenced, so that outcome is permitted by the contract; the scenario has
	// to pin the ordering rather than assume it.
	b.Faults().BlockNext(OpReplace, storage.CurrentKey)

	// Release B's upload and wait until its pack object exists and B has
	// reached the blocked checkpoint CAS. The winner's CAS runs cleanup
	// inline, which enumerates the checkpoint namespace, so both proposals
	// must be visible before A is allowed to publish.
	b.Faults().ReleasePrefix(OpPut, "packs/checkpoints/3-")
	b.eventually(t, settleTimeout, func() error {
		if got := len(b.ListObjects("packs/checkpoints/3-")); got != 1 {
			return fmt.Errorf("B's checkpoint proposal is not uploaded yet (%d objects)", got)
		}
		if !b.Faults().Waiting(OpReplace, storage.CurrentKey) {
			return fmt.Errorf("B has not reached its manifest CAS")
		}
		return nil
	})
	// A now publishes uncontested and collects B's unreferenced proposal;
	// releasing B afterwards makes it lose on a stale ETag and retry.
	a.Faults().ReleasePrefix(OpPut, "packs/checkpoints/3-")
	a.awaitCall(t, aCh, ToolCall{Tool: toolCommit, Path: pathA, Message: "c3", Expect: CallExpectation{OK: true}})
	b.Faults().Release(OpReplace, storage.CurrentKey)
	b.awaitCall(t, bCh, ToolCall{Tool: toolCommit, Path: pathB, Message: "B competes", Expect: CallExpectation{OK: true}})

	m := a.Manifest()
	if m.Generation != 5 {
		t.Fatalf("manifest generation = %d, want the winning compaction at 5", m.Generation)
	}
	if m.Checkpoint.ThroughGeneration != 3 || len(m.Increments) != 1 {
		t.Fatalf("winning manifest = through %d, increments %d, want through 3 with I4", m.Checkpoint.ThroughGeneration, len(m.Increments))
	}
	// Both workers uploaded a gen-3 proposal, and exactly one physical
	// index survives: the loser's object is unreferenced and collected.
	if got := a.Recorder().CountKeyPrefix(OpPut, "packs/checkpoints/3-"); got != 1 {
		t.Fatalf("A's gen-3 checkpoint uploads = %d, want one proposal", got)
	}
	if got := b.Recorder().CountKeyPrefix(OpPut, "packs/checkpoints/3-"); got != 1 {
		t.Fatalf("B's gen-3 checkpoint uploads = %d, want one proposal", got)
	}
	a.eventually(t, settleTimeout, func() error {
		cpKeys := a.ListObjects("packs/checkpoints/3-")
		if len(cpKeys) != 1 {
			return fmt.Errorf("gen-3 checkpoint objects = %v, want exactly the survivor", cpKeys)
		}
		if cpKeys[0] != m.Checkpoint.Key.String() {
			return fmt.Errorf("surviving checkpoint object %q is not the referenced %q", cpKeys[0], m.Checkpoint.Key)
		}
		return nil
	})
	// No duplicate logical commit: the accepted state carries one gen-3
	// compaction and one gen-4 increment, and a fresh reader reads it.
	reader := newSharedHarness(t, a.Raw(), a.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{
		"f1.md": "v1", "f2.md": "v2", "f3.md": "v3", "b.md": "B",
	})
}

// TestScenarioCheckpointRetention proves the retention contract: after the
// second checkpoint, the active checkpoint plus one retained previous
// generation reconstruct the replaced state, and the older unretained
// descriptors are deleted best-effort (architecture section 14, L893).
func TestScenarioCheckpointRetention(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	m1 := h.Manifest()
	p1Key := m1.Checkpoint.Key.String()
	commitNext(t, h, path, "f2.md", "v2", "c2")
	m2 := h.Manifest()
	i2Key := m2.Increments[0].Key.String()
	commitNext(t, h, path, "f3.md", "v3", "c3")
	// The c3 commit schedules the first checkpoint synchronously, so the
	// current manifest is already the compacted state: the retained entry
	// carries the replaced P1 and the compacted increments I2/I3.
	m3 := h.Manifest()
	retained := m3.Retained[0]
	if retained.Checkpoint.Key.String() != p1Key || len(retained.Increments) != 2 {
		t.Fatalf("retained after the first compaction = checkpoint %s, increments %d; want P1 and I2/I3", retained.Checkpoint.Key, len(retained.Increments))
	}
	i3Key := retained.Increments[1].Key.String()
	commitNext(t, h, path, "f4.md", "v4", "c4")
	commitNext(t, h, path, "f5.md", "v5", "c5")

	m := h.Manifest()
	if m.Generation != 7 || len(m.Increments) != 0 || len(m.Retained) != 1 {
		t.Fatalf("final manifest = gen %d, increments %d, retained %d; want 7/0/1", m.Generation, len(m.Increments), len(m.Retained))
	}
	finalRetained := m.Retained[0]
	roots := map[string]bool{
		m.Checkpoint.Key.String():             true,
		finalRetained.Checkpoint.Key.String(): true,
	}
	for _, inc := range finalRetained.Increments {
		roots[inc.Key.String()] = true
	}
	// The retention contract keeps the active checkpoint plus one previous
	// generation readable: the retained chain (P3 plus I4/I5) reconstructs
	// the state P5 replaced, and the first compaction's P1/I2/I3 are
	// unretained (architecture section 14, L893).
	if len(roots) != 2+len(finalRetained.Increments) {
		t.Fatalf("retained roots = %v, want active plus one previous generation", roots)
	}
	if roots[p1Key] || roots[i2Key] || roots[i3Key] {
		t.Fatalf("retained roots = %v, want P1/I2/I3 unretained", roots)
	}
	h.eventually(t, settleTimeout, func() error {
		for _, key := range []string{p1Key, i2Key, i3Key} {
			gone, err := objectGone(h, key)
			if err != nil {
				return err
			}
			if !gone {
				return fmt.Errorf("unretained descriptor %s still present", key)
			}
		}
		for key := range roots {
			if !h.ObjectExists(key) {
				return fmt.Errorf("retained or active root %s was deleted", key)
			}
		}
		return nil
	})
}

// TestScenarioCheckpointCleanupAfterCAS proves the cleanup contract after a
// successful checkpoint CAS over real MinIO (architecture section 14, L893):
// unreferenced generations at or before the cutoff are deleted best effort,
// the active and retained descriptors are never deleted, the cleanup roots
// are reread from `current` before deleting, and a cold reader still
// reconstructs the head.
func TestScenarioCheckpointCleanupAfterCAS(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")
	commitNext(t, h, path, "f3.md", "v3", "c3")
	first := h.Manifest() // the first compaction: its chain becomes retained
	commitNext(t, h, path, "f4.md", "v4", "c4")
	commitNext(t, h, path, "f5.md", "v5", "c5")

	m := h.Manifest()
	if m.Generation != 7 || len(m.Retained) != 1 {
		t.Fatalf("final manifest = gen %d, retained %d; want 7/1", m.Generation, len(m.Retained))
	}

	// The active checkpoint and the whole retained chain survive: cleanup
	// never deletes a live root, and never hands one to a delete batch.
	live := []string{m.Checkpoint.Key.String(), m.Retained[0].Checkpoint.Key.String()}
	for _, inc := range m.Retained[0].Increments {
		live = append(live, inc.Key.String())
	}
	for _, key := range live {
		if !h.ObjectExists(key) {
			t.Fatalf("live root %s was deleted by cleanup", key)
		}
		if got := h.Recorder().CountKey(OpDelete, key); got != 0 {
			t.Fatalf("live root %s was handed to %d delete batches", key, got)
		}
	}

	// The chain the FIRST compaction retained (the root checkpoint and the
	// two increments it replaced) fell out of retention when the second
	// compaction replaced it, so it is now an unreferenced candidate at or
	// before the cutoff and is collected.
	stale := first.Retained[0]
	staleKeys := []string{stale.Checkpoint.Key.String()}
	for _, inc := range stale.Increments {
		staleKeys = append(staleKeys, inc.Key.String())
	}
	if len(staleKeys) != 3 {
		t.Fatalf("first retained chain = %v, want the root checkpoint and its two increments", staleKeys)
	}
	for _, key := range staleKeys {
		requireObjectGone(t, h, key)
	}

	// Cleanup rereads the roots from `current` before each delete batch, so
	// the set it protects is never a stale snapshot. The rereads are visible
	// as manifest reads beyond the ones the publications themselves need.
	reads := h.Recorder().CountKey(OpGet, storage.CurrentKey)
	writes := h.Recorder().CountKey(OpCreate, storage.CurrentKey) + h.Recorder().CountKey(OpReplace, storage.CurrentKey)
	if reads <= writes {
		t.Fatalf("manifest reads = %d for %d writes, want the cleanup rereads on top of the publication reads", reads, writes)
	}

	reader := newSharedHarness(t, h.Raw(), h.cfg.Prefix, HarnessConfig{})
	readPath := reader.Path("notes")
	reader.assertOK(t, reader.Pull("", readPath))
	assertVisibleFiles(t, reader, readPath, map[string]string{
		"f1.md": "v1", "f2.md": "v2", "f3.md": "v3", "f4.md": "v4", "f5.md": "v5",
	})
}

// TestScenarioCheckpointCleanupFence proves that cleanup considers only
// candidate generations at or before the successful checkpoint cutoff: a
// proposal after the cutoff is never touched (architecture section 14, L893).
func TestScenarioCheckpointCleanupFence(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	id1, _ := storage.NewUUIDv7()
	id999, _ := storage.NewUUIDv7()
	junkBefore := "packs/increments/1-" + id1.String() + ".pack"
	junkAfter := "packs/increments/999-" + id999.String() + ".pack"
	putJunk(t, h, junkBefore)
	putJunk(t, h, junkAfter)

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")
	commitNext(t, h, path, "f3.md", "v3", "c3")

	h.eventually(t, settleTimeout, func() error {
		gone, err := objectGone(h, junkBefore)
		if err != nil {
			return err
		}
		if !gone {
			return fmt.Errorf("candidate %s still present after cleanup", junkBefore)
		}
		return nil
	})
	if !h.ObjectExists(junkAfter) {
		t.Fatalf("post-cutoff proposal %s was touched by cleanup", junkAfter)
	}
}

// TestScenarioCheckpointMalformedKeys proves that malformed keys in the
// pack namespaces are never parsed as cleanup candidates and stay untouched
// (architecture section 14, L893).
//
// A well-formed unreferenced pre-cutoff pack is seeded beside the malformed
// keys as a positive control: without it, a run in which cleanup never
// happened at all would satisfy the "untouched" assertion vacuously.
func TestScenarioCheckpointMalformedKeys(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	const junkCP = "packs/checkpoints/not-a-key"
	const junkInc = "packs/increments/5-bad"
	putJunk(t, h, junkCP)
	putJunk(t, h, junkInc)
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("generate publication id: %v", err)
	}
	control := "packs/increments/1-" + id.String() + ".pack"
	putJunk(t, h, control)

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")
	commitNext(t, h, path, "f3.md", "v3", "c3")

	// The control proves cleanup ran and collected a well-formed candidate.
	requireObjectGone(t, h, control)
	if !h.ObjectExists(junkCP) || !h.ObjectExists(junkInc) {
		t.Fatal("malformed keys were touched by cleanup")
	}
	for _, key := range []string{junkCP, junkInc} {
		if got := h.Recorder().CountKey(OpDelete, key); got != 0 {
			t.Fatalf("malformed key %s was handed to %d delete batches", key, got)
		}
	}
}

// TestScenarioCheckpointCleanupFailure proves that a failing cleanup batch
// never changes the already accepted commit result: the commit returns OK,
// the accepted manifest is the compacted one, and the failure is observable
// as a warning naming the failed step (architecture section 14, L893).
func TestScenarioCheckpointCleanupFailure(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	// Seed one unreferenced pre-cutoff pack so the checkpoint cleanup after
	// the c3 commit has a delete candidate whose failure must be observable.
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("generate publication id: %v", err)
	}
	doomed := "packs/increments/1-" + id.String() + ".pack"
	putJunk(t, h, doomed)

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")

	// The third commit must publish an increment so the tail reaches the
	// threshold and schedules the checkpoint whose cleanup then fails.
	h.WriteFile(path+"/f3.md", "v3")
	h.Faults().FailDeletes(storage.ErrTransport)
	h.assertOK(t, h.Commit("", path, "c3"))

	// The failure is the delete step, not a listing or reread regression:
	// the notebook emits the same message for all three.
	assertWarning(t, h, "cleanup failed", "reason", "delete cleanup batch")

	// The commit result stands: the compaction is accepted and the
	// uncollected candidate simply waits for a later cleanup.
	m := h.Manifest()
	if m.Generation != 4 || len(m.Increments) != 0 {
		t.Fatalf("accepted state = gen %d with %d increments, want the compaction at 4/0", m.Generation, len(m.Increments))
	}
	if !h.ObjectExists(doomed) {
		t.Fatalf("candidate %s was deleted although every delete failed", doomed)
	}
}

// TestScenarioCheckpointUploadFailure proves that a failed checkpoint pack
// upload leaves the accepted state exactly as the commit published it: the
// compaction is a best-effort background effort and can never undo or fail
// an accepted commit (architecture section 13, L813).
func TestScenarioCheckpointUploadFailure(t *testing.T) {
	t.Parallel()
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2)})
	path := h.Path("notes")

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")

	// The third commit reaches the threshold and schedules a checkpoint
	// whose pack upload fails.
	h.WriteFile(path+"/f3.md", "v3")
	h.Faults().FailAlwaysPrefix(OpPut, "packs/checkpoints/3-", storage.ErrTransport)
	h.assertOK(t, h.Commit("", path, "c3"))
	assertWarning(t, h, "checkpoint failed", "reason", "upload checkpoint pack")

	// The accepted state is the uncompacted increment chain: generation 3
	// with the two-increment tail, and no gen-3 checkpoint object exists.
	m := h.Manifest()
	if m.Generation != 3 || len(m.Increments) != 2 {
		t.Fatalf("accepted state = gen %d with %d increments, want the uncompacted 3/2", m.Generation, len(m.Increments))
	}
	if got := h.ListObjects("packs/checkpoints/3-"); len(got) != 0 {
		t.Fatalf("gen-3 checkpoint objects = %v, want none after the failed upload", got)
	}

	// A later trigger retries the effort. The plan always selects the oldest
	// threshold increments, so the retried cutoff is still generation 3; the
	// later increment stays in the tail.
	h.Faults().ClearFailures()
	commitNext(t, h, path, "f4.md", "v4", "c4")
	m = h.Manifest()
	if m.Checkpoint.ThroughGeneration != 3 || len(m.Increments) != 1 {
		t.Fatalf("after the retry = through %d with %d increments, want the compaction through 3 with the later increment", m.Checkpoint.ThroughGeneration, len(m.Increments))
	}
	if !h.ObjectExists(m.Checkpoint.Key.String()) {
		t.Fatalf("the retried checkpoint pack %s is absent", m.Checkpoint.Key)
	}
}

// TestScenarioCheckpointCASExhaustion proves that a checkpoint whose CAS
// loses every attempt gives up within the configured bound instead of
// looping, leaves the accepted commit untouched, and compacts on a later
// trigger once the contention passes (architecture section 13, L813).
func TestScenarioCheckpointCASExhaustion(t *testing.T) {
	t.Parallel()
	const retries = 1
	h := newFakeHarness(t, HarnessConfig{CheckpointPacks: new(2), RetryLimit: new(retries)})
	path := h.Path("notes")

	commitFirst(t, h, path, "f1.md", "v1", "c1")
	commitNext(t, h, path, "f2.md", "v2", "c2")

	// Only the checkpoint's CAS may lose; the commit's own publication must
	// succeed first. Blocking the checkpoint pack upload gives an exact
	// point after the publication and before the checkpoint CAS at which to
	// arm the permanent precondition failure.
	h.WriteFile(path+"/f3.md", "v3")
	h.Faults().BlockPrefix(OpPut, "packs/checkpoints/3-")
	ch := runCall(t, h, "", toolCommit, path, "c3")
	h.eventually(t, settleTimeout, func() error {
		if !h.Faults().WaitingPrefix(OpPut, "packs/checkpoints/3-") {
			return fmt.Errorf("the checkpoint upload has not started")
		}
		return nil
	})
	published := h.Recorder().CountKey(OpReplace, storage.CurrentKey)
	h.Faults().FailAlways(OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	h.Faults().ReleasePrefix(OpPut, "packs/checkpoints/3-")
	h.awaitCall(t, ch, ToolCall{Tool: toolCommit, Path: path, Message: "c3", Expect: CallExpectation{OK: true}})

	// The effort is bounded: retryLimit+1 attempts and then it gives up.
	assertWarning(t, h, "checkpoint failed", "reason", "checkpoint CAS lost all attempts")
	if got := h.Recorder().CountKey(OpReplace, storage.CurrentKey) - published; got != retries+1 {
		t.Fatalf("checkpoint CAS attempts = %d, want the bounded retryLimit+1 = %d", got, retries+1)
	}

	// The accepted commit stands, uncompacted.
	m := h.Manifest()
	if m.Generation != 3 || len(m.Increments) != 2 {
		t.Fatalf("accepted state = gen %d with %d increments, want the uncompacted 3/2", m.Generation, len(m.Increments))
	}

	// A later trigger retries the effort once the contention passes. The
	// plan selects the oldest threshold increments, so the cutoff is still
	// generation 3 and the later increment stays in the tail.
	h.Faults().ClearFailures()
	commitNext(t, h, path, "f4.md", "v4", "c4")
	if m := h.Manifest(); m.Checkpoint.ThroughGeneration != 3 || len(m.Increments) != 1 {
		t.Fatalf("after the retry = through %d with %d increments, want the compaction through 3 with the later increment", m.Checkpoint.ThroughGeneration, len(m.Increments))
	}
}

// assertWarning polls until a warning record with the message and the exact
// attribute value appears in the harness capture.
func assertWarning(t *testing.T, h *Harness, msg, attr, value string) {
	t.Helper()
	h.eventually(t, settleTimeout, func() error {
		for _, rec := range h.Logs().Warnings() {
			if rec.Msg == msg && fmt.Sprint(rec.Attrs[attr]) == value {
				return nil
			}
		}
		return fmt.Errorf("no warning %q with %s=%q in:\n%s", msg, attr, value, h.Logs())
	})
}
