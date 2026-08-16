package notebook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// hexOID builds a deterministic object ID from a short hex suffix.
func hexOID(t *testing.T, hex string) git.OID {
	t.Helper()
	id, err := git.ParseOID(strings.Repeat("0", 40-len(hex)) + hex)
	if err != nil {
		t.Fatalf("ParseOID(%q) = %v", hex, err)
	}
	return id
}

// waitBlocked waits until an operation on key has consumed its barrier and
// is waiting for Unblock, so race tests never sleep on timing.
func waitBlocked(t *testing.T, store *fake.Store, op fake.Op, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !store.Waiting(op, key) {
		if time.Now().After(deadline) {
			t.Fatalf("operation %s %q never blocked", op, key)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestCheckpointTriggersAtThresholdAndRetainsPrevious proves the threshold
// contract: no effort below the threshold, one bounded effort when the
// accepted tail reaches it, a stable-prefix replacement that keeps the
// previous generation physically present, and the checkpoint metrics.
func TestCheckpointTriggersAtThresholdAndRetainsPrevious(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	m := readManifest(t, store)
	if m.Generation != 1 || len(m.Increments) != 0 {
		t.Fatalf("after first commit = gen %d increments %d, want the checkpoint-only state", m.Generation, len(m.Increments))
	}
	if got := nb.Metrics().CheckpointRuns.Load(); got != 0 {
		t.Fatalf("checkpoint runs after the first publication = %d, want 0", got)
	}

	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second")
	m = readManifest(t, store)
	if m.Generation != 2 || len(m.Increments) != 1 {
		t.Fatalf("after second commit = gen %d increments %d, want one increment below the threshold", m.Generation, len(m.Increments))
	}
	if got := nb.Metrics().CheckpointRuns.Load(); got != 0 {
		t.Fatalf("checkpoint runs below the threshold = %d, want 0", got)
	}
	if got := nb.Metrics().TailCount.Load(); got != 1 {
		t.Fatalf("tail count = %d, want 1", got)
	}
	if got, want := nb.Metrics().TailBytes.Load(), m.Increments[0].Size; got != want {
		t.Fatalf("tail bytes = %d, want %d", got, want)
	}

	writeLocal(t, w, map[string]string{"a.md": "v3"})
	commitOK(t, nb, "third")
	m = readManifest(t, store)
	if m.Generation != 4 {
		t.Fatalf("generation = %d, want 4 (three increments plus the checkpoint)", m.Generation)
	}
	cp := m.Checkpoint
	if cp.ThroughGeneration != 3 || cp.Head != m.Head || len(m.Increments) != 0 {
		t.Fatalf("checkpoint = %+v head %s increments %d, want through 3 with an empty tail", cp, m.Head, len(m.Increments))
	}
	if cp.Publication != testUUIDv7(4) {
		t.Fatalf("checkpoint publication = %s, want the final compacted increment id 4", cp.Publication)
	}
	if cp.ID != testUUIDv7(5) {
		t.Fatalf("checkpoint id = %s, want 5", cp.ID)
	}
	if len(m.Retained) != 1 {
		t.Fatalf("retained = %d entries, want 1", len(m.Retained))
	}
	ret := m.Retained[0]
	if ret.RetiredAtGeneration != 4 || ret.Head != cp.Head {
		t.Fatalf("retained = %+v, want retired at 4 with the compacted head", ret)
	}
	if ret.Checkpoint.ID != testUUIDv7(2) || len(ret.Increments) != 2 {
		t.Fatalf("retained chain = %+v, want the first checkpoint with two compacted increments", ret)
	}
	if ret.Increments[0].Generation != 2 || ret.Increments[1].Generation != 3 {
		t.Fatalf("retained increments = %+v, want generations 2 and 3", ret.Increments)
	}

	// Retention keeps the previous generation physically present: cleanup
	// after this checkpoint must not delete the retained descriptors.
	for _, key := range []string{
		"packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(3).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(4).String() + ".pack",
		"packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack",
	} {
		if _, _, err := store.ReadObject(context.Background(), key); err != nil {
			t.Fatalf("retained or active pack %s missing: %v", key, err)
		}
	}

	mets := nb.Metrics()
	if mets.CheckpointRuns.Load() != 1 || mets.CheckpointFailures.Load() != 0 {
		t.Fatalf("checkpoint metrics = runs %d failures %d, want 1 and 0", mets.CheckpointRuns.Load(), mets.CheckpointFailures.Load())
	}
	if mets.CheckpointSize.Load() == 0 {
		t.Fatal("checkpoint size metric is zero")
	}
	if mets.CheckpointDurationNanos.Load() < 0 {
		t.Fatal("checkpoint duration metric is negative")
	}
	if mets.TailCount.Load() != 0 || mets.TailBytes.Load() != 0 {
		t.Fatalf("tail metrics = count %d bytes %d, want the empty tail after compaction", mets.TailCount.Load(), mets.TailBytes.Load())
	}
	if mets.CleanupRuns.Load() != 1 || mets.CleanupDeleted.Load() != 0 {
		t.Fatalf("cleanup metrics = runs %d deleted %d, want one run with nothing to delete", mets.CleanupRuns.Load(), mets.CleanupDeleted.Load())
	}
}

// TestCheckpointCompactedStateReconstructsAndExtends proves a checkpoint
// pack alone reconstructs its exact head and complete files in a fresh
// repository, that new increments descend from and import after the shallow
// checkpoint, and that an independent reader sees the extension.
func TestCheckpointCompactedStateReconstructsAndExtends(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	writeLocal(t, aw, map[string]string{"a.md": "v1", "sub/b.md": "nested"})
	pullOK(t, a)
	commitOK(t, a, "first")
	writeLocal(t, aw, map[string]string{"a.md": "v2"})
	commitOK(t, a, "second")
	writeLocal(t, aw, map[string]string{"a.md": "v3"})
	commitOK(t, a, "third")
	m := readManifest(t, store)
	if m.Generation != 4 || len(m.Increments) != 0 {
		t.Fatalf("manifest = gen %d increments %d, want the checkpointed state", m.Generation, len(m.Increments))
	}

	// A fresh repository reconstructs the complete state from the
	// checkpoint pack alone.
	c, cw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	pullOK(t, c)
	got := localSnapshot(t, cw)
	want := map[string]string{"a.md": "v3", "sub/b.md": "nested"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("L after fresh pull = %v, want %v", got, want)
	}
	if gen := cw.Baseline().RemoteGeneration; gen != 4 {
		t.Fatalf("baseline generation = %d, want 4", gen)
	}

	// New increments descend from and import after the shallow checkpoint.
	writeLocal(t, cw, map[string]string{"c.md": "after checkpoint"})
	commitOK(t, c, "extend")
	m2 := readManifest(t, store)
	if m2.Generation != 5 || len(m2.Increments) != 1 {
		t.Fatalf("after extension = gen %d increments %d, want one increment", m2.Generation, len(m2.Increments))
	}
	if m2.Increments[0].Parent != m.Checkpoint.Head {
		t.Fatalf("extension parent = %s, want the checkpoint head %s", m2.Increments[0].Parent, m.Checkpoint.Head)
	}

	d, dw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	pullOK(t, d)
	got = localSnapshot(t, dw)
	if got["a.md"] != "v3" || got["c.md"] != "after checkpoint" {
		t.Fatalf("L after the extension pull = %v, want both states", got)
	}
}

// TestCheckpointSecondCompactionCleansOldGeneration proves retention and
// cleanup across two checkpoint generations: the oldest unretained
// generation is deleted best effort while the active and retained
// generations stay readable.
func TestCheckpointSecondCompactionCleansOldGeneration(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	for i := 1; i <= 5; i++ {
		writeLocal(t, w, map[string]string{"a.md": fmt.Sprintf("v%d", i)})
		commitOK(t, nb, fmt.Sprintf("commit %d", i))
	}

	m := readManifest(t, store)
	if m.Generation != 7 || m.Checkpoint.ThroughGeneration != 5 {
		t.Fatalf("manifest = gen %d checkpoint through %d, want 7 and 5", m.Generation, m.Checkpoint.ThroughGeneration)
	}
	if len(m.Retained) != 1 || m.Retained[0].RetiredAtGeneration != 7 {
		t.Fatalf("retained = %+v, want the newest generation retired at 7", m.Retained)
	}
	if m.Retained[0].Checkpoint.ID != testUUIDv7(5) {
		t.Fatalf("retained checkpoint id = %s, want the first compacted checkpoint 5", m.Retained[0].Checkpoint.ID)
	}

	// The first generation's physical packs were deleted; the retained
	// generation and the active pack stay.
	for _, gone := range []string{
		"packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(3).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(4).String() + ".pack",
	} {
		if _, _, err := store.ReadObject(context.Background(), gone); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("old generation pack %s still present (err %v), want deleted", gone, err)
		}
	}
	for _, kept := range []string{
		"packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack",
		"packs/increments/4-" + testUUIDv7(6).String() + ".pack",
		"packs/increments/5-" + testUUIDv7(7).String() + ".pack",
		"packs/checkpoints/5-" + testUUIDv7(8).String() + ".pack",
	} {
		if _, _, err := store.ReadObject(context.Background(), kept); err != nil {
			t.Fatalf("retained or active pack %s missing: %v", kept, err)
		}
	}
	mets := nb.Metrics()
	if mets.CleanupRuns.Load() != 2 || mets.CleanupDeleted.Load() != 3 {
		t.Fatalf("cleanup metrics = runs %d deleted %d, want 2 runs deleting 3 keys", mets.CleanupRuns.Load(), mets.CleanupDeleted.Load())
	}
}

// TestCheckpointThresholdOneCompactsEveryCommit proves the minimum
// threshold: every accepted increment is compacted immediately.
func TestCheckpointThresholdOneCompactsEveryCommit(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 1})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second")

	m := readManifest(t, store)
	if m.Generation != 3 {
		t.Fatalf("generation = %d, want 3", m.Generation)
	}
	if m.Checkpoint.ThroughGeneration != 2 || len(m.Increments) != 0 {
		t.Fatalf("checkpoint = through %d increments %d, want through 2 with an empty tail", m.Checkpoint.ThroughGeneration, len(m.Increments))
	}
	if len(m.Retained) != 1 || m.Retained[0].Head != m.Checkpoint.Head {
		t.Fatalf("retained = %+v, want the replaced state with the compacted head", m.Retained)
	}
}

// TestCheckpointPreservesIncrementsAcceptedDuringBuild proves stable-prefix
// replacement: an increment accepted while the checkpoint pack is being
// built survives the compaction, and a lost CAS only rewrites the manifest
// against the new tail — the checkpoint pack is never rebuilt.
func TestCheckpointPreservesIncrementsAcceptedDuringBuild(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	puts := &putCounter{ObjectStore: store}
	a, aw, _ := newNotebook(t, nbConfig{store: puts, ids: ids, checkpointPacks: 2})
	b, bw, _ := newNotebook(t, nbConfig{store: puts, ids: ids, checkpointPacks: DefaultCheckpointPacks})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base") // gen 1, ids 1 and 2
	writeLocal(t, aw, map[string]string{"a.md": "A"})
	commitOK(t, a, "second") // gen 2, id 3
	pullOK(t, b)

	// A's third commit publishes gen 3 and blocks its checkpoint pack
	// upload; while it is blocked, B accepts another increment.
	writeLocal(t, aw, map[string]string{"a.md": "A2"})
	cpKey := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	store.BlockNext(fake.OpPut, cpKey)
	done := make(chan error, 1)
	go func() { done <- errOnly(a.Commit(context.Background(), "third")) }()
	waitBlocked(t, store, fake.OpPut, cpKey)

	writeLocal(t, bw, map[string]string{"b.md": "B"})
	commitOK(t, b, "B change") // gen 4, id 6

	store.Unblock(fake.OpPut, cpKey)
	if err := <-done; err != nil {
		t.Fatalf("A Commit() = %v", err)
	}

	m := readManifest(t, store)
	if m.Generation != 5 {
		t.Fatalf("generation = %d, want 5", m.Generation)
	}
	if m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("checkpoint through = %d, want the selected cutoff 3", m.Checkpoint.ThroughGeneration)
	}
	if len(m.Increments) != 1 || m.Increments[0].Generation != 4 || m.Increments[0].Publication != testUUIDv7(6) {
		t.Fatalf("later increment = %+v, want B's generation-4 increment preserved", m.Increments)
	}
	if m.Head != m.Increments[0].Head {
		t.Fatalf("manifest head = %s, want the preserved increment head", m.Head)
	}
	if len(m.Retained) != 1 || len(m.Retained[0].Increments) != 2 {
		t.Fatalf("retained = %+v, want the compacted prefix of two increments", m.Retained)
	}
	if got := puts.Count(cpKey); got != 1 {
		t.Fatalf("checkpoint pack uploads = %d, want exactly 1 (the pack is never rebuilt)", got)
	}
	mets := a.Metrics()
	if mets.CheckpointCASAttempts.Load() != 1 {
		t.Fatalf("checkpoint CAS attempts = %d, want the single winning attempt against the later manifest", mets.CheckpointCASAttempts.Load())
	}
	if mets.CheckpointFailures.Load() != 0 {
		t.Fatalf("checkpoint failures = %d, want 0", mets.CheckpointFailures.Load())
	}
}

// TestCheckpointCASExhaustionStopsBoundedEffort proves a checkpoint whose
// CAS keeps losing stops after the configured bound without rebuilding its
// pack, records the failure, and retries on a later trigger: the later
// effort compacts the same prefix and cleanup collects the failed
// proposal pack.
func TestCheckpointCASExhaustionStopsBoundedEffort(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	// Replaces 1 and 2 are the second and third commits' publications;
	// replaces 3..5 are the first checkpoint effort's three CAS attempts
	// with retryLimit 2. The later trigger's CAS (replace 7) succeeds.
	failStore := &failFromReplace{ObjectStore: store, failFrom: 3, failTo: 5, err: storage.ErrPreconditionFailed}
	nb, w, _ := newNotebook(t, nbConfig{store: failStore, ids: ids, checkpointPacks: 2, retryLimit: 2})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first") // gen 1 (create)
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second") // gen 2, replace 1
	writeLocal(t, w, map[string]string{"a.md": "v3"})
	commitOK(t, nb, "third") // gen 3, replace 2; checkpoint CAS 3..5 all lose

	m := readManifest(t, store)
	if m.Generation != 3 || len(m.Increments) != 2 {
		t.Fatalf("current = gen %d increments %d, want the accepted state unchanged", m.Generation, len(m.Increments))
	}
	mets := nb.Metrics()
	if mets.CheckpointFailures.Load() != 1 || mets.CheckpointCASAttempts.Load() != 3 {
		t.Fatalf("checkpoint metrics = failures %d CAS %d, want 1 failure over 3 attempts", mets.CheckpointFailures.Load(), mets.CheckpointCASAttempts.Load())
	}
	// The failed effort uploaded its proposal pack before the CAS bound.
	cpA := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	if _, _, err := store.ReadObject(context.Background(), cpA); err != nil {
		t.Fatalf("failed checkpoint proposal pack missing: %v", err)
	}

	// A later trigger retries the same prefix and succeeds.
	writeLocal(t, w, map[string]string{"a.md": "v4"})
	commitOK(t, nb, "fourth") // gen 4, replace 6; checkpoint CAS replace 7
	m = readManifest(t, store)
	if m.Generation != 5 || m.Checkpoint.ID != testUUIDv7(7) || m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("after retry = gen %d checkpoint %s through %d, want 5 with the second effort", m.Generation, m.Checkpoint.ID, m.Checkpoint.ThroughGeneration)
	}
	if len(m.Increments) != 1 || m.Increments[0].Generation != 4 {
		t.Fatalf("tail = %+v, want the generation-4 increment preserved", m.Increments)
	}
	if mets.CheckpointRuns.Load() != 2 || mets.CheckpointFailures.Load() != 1 {
		t.Fatalf("checkpoint metrics = runs %d failures %d, want 2 runs and 1 failure", mets.CheckpointRuns.Load(), mets.CheckpointFailures.Load())
	}
	// The failed proposal is now at or before the successful cutoff and
	// unreferenced: the retry's cleanup collected it.
	if _, _, err := store.ReadObject(context.Background(), cpA); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("failed checkpoint proposal pack survived the later cleanup (err %v), want deleted", err)
	}
}

// TestCheckpointDiscardsWhenCompetingWorkerCompactsFirst proves two
// checkpoint workers racing on the same prefix: one manifest replacement
// wins, the losing worker discards its proposal when the prefix disappears,
// and no accepted increment is lost or reordered.
func TestCheckpointDiscardsWhenCompetingWorkerCompactsFirst(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base") // gen 1, ids 1 and 2
	writeLocal(t, aw, map[string]string{"a.md": "A"})
	commitOK(t, a, "second") // gen 2, id 3
	pullOK(t, b)

	// A's checkpoint pack upload is blocked; B accepts an increment and
	// compacts the same prefix first.
	writeLocal(t, aw, map[string]string{"a.md": "A2"})
	cpA := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	store.BlockNext(fake.OpPut, cpA)
	done := make(chan error, 1)
	go func() { done <- errOnly(a.Commit(context.Background(), "third")) }()
	waitBlocked(t, store, fake.OpPut, cpA)

	writeLocal(t, bw, map[string]string{"b.md": "B"})
	commitOK(t, b, "B change") // gen 4, id 6; B's checkpoint wins at gen 5 (id 7)

	store.Unblock(fake.OpPut, cpA)
	if err := <-done; err != nil {
		t.Fatalf("A Commit() = %v", err)
	}

	m := readManifest(t, store)
	if m.Generation != 5 {
		t.Fatalf("generation = %d, want 5", m.Generation)
	}
	if m.Checkpoint.ID != testUUIDv7(7) || m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("checkpoint = id %s through %d, want B's winning effort through 3", m.Checkpoint.ID, m.Checkpoint.ThroughGeneration)
	}
	if len(m.Increments) != 1 || m.Increments[0].Publication != testUUIDv7(6) {
		t.Fatalf("tail = %+v, want B's generation-4 increment", m.Increments)
	}
	if m.Head != m.Increments[0].Head {
		t.Fatalf("manifest head = %s, want the final increment head", m.Head)
	}
	if m.Checkpoint.Key.String() == cpA {
		t.Fatal("manifest references the losing worker's pack")
	}
	// A's proposal landed after B's cleanup: it is an orphan, not a
	// duplicate logical commit.
	if _, _, err := store.ReadObject(context.Background(), cpA); err != nil {
		t.Fatalf("losing checkpoint pack missing: %v", err)
	}
	mets := a.Metrics()
	if mets.CheckpointRuns.Load() != 1 || mets.CheckpointFailures.Load() != 0 {
		t.Fatalf("A checkpoint metrics = runs %d failures %d, want one clean discard", mets.CheckpointRuns.Load(), mets.CheckpointFailures.Load())
	}
}

// TestCheckpointPackCreationFailureLeavesStateUnchanged proves a failed
// checkpoint pack creation (fake Git failure) never changes the accepted
// state, and a later trigger retries successfully.
func TestCheckpointPackCreationFailureLeavesStateUnchanged(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	failEng := &lastCommitFailEngine{inner: newFakeEngine()}
	nb, w, _ := newNotebook(t, nbConfig{store: store, engine: failEng, ids: ids, checkpointPacks: 2})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first") // gen 1
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second") // gen 2
	writeLocal(t, w, map[string]string{"a.md": "v3"})
	failEng.arm()
	commitOK(t, nb, "third") // gen 3; the checkpoint pack export fails
	failEng.disarm()

	m := readManifest(t, store)
	if m.Generation != 3 || len(m.Increments) != 2 {
		t.Fatalf("current = gen %d increments %d, want the accepted state unchanged", m.Generation, len(m.Increments))
	}
	if got := nb.Metrics().CheckpointFailures.Load(); got != 1 {
		t.Fatalf("checkpoint failures = %d, want 1", got)
	}
	cpA := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	if _, _, err := store.ReadObject(context.Background(), cpA); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("checkpoint pack exists after a failed creation (err %v), want none", err)
	}

	writeLocal(t, w, map[string]string{"a.md": "v4"})
	commitOK(t, nb, "fourth") // gen 4; the later trigger compacts
	m = readManifest(t, store)
	if m.Generation != 5 || m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("after retry = gen %d checkpoint through %d, want 5 and 3", m.Generation, m.Checkpoint.ThroughGeneration)
	}
	if got := nb.Metrics().CheckpointRuns.Load(); got != 2 {
		t.Fatalf("checkpoint runs = %d, want 2", got)
	}
}

// TestCheckpointUploadFailureLeavesStateUnchanged proves a failed
// checkpoint pack upload (fake storage failure) never changes the accepted
// state.
func TestCheckpointUploadFailureLeavesStateUnchanged(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first") // gen 1
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second") // gen 2
	writeLocal(t, w, map[string]string{"a.md": "v3"})
	cpKey := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	store.FailNextKey(fake.OpPut, cpKey, storage.ErrTransport)
	commitOK(t, nb, "third")

	m := readManifest(t, store)
	if m.Generation != 3 || len(m.Increments) != 2 {
		t.Fatalf("current = gen %d increments %d, want the accepted state unchanged", m.Generation, len(m.Increments))
	}
	if got := nb.Metrics().CheckpointFailures.Load(); got != 1 {
		t.Fatalf("checkpoint failures = %d, want 1", got)
	}
	if _, _, err := store.ReadObject(context.Background(), cpKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("checkpoint pack exists after a failed upload (err %v), want none", err)
	}
}

// TestCleanupDeletesOnlyUnreferencedAtOrBeforeCutoff proves the cleanup
// fence: only valid protocol keys at or before the successful checkpoint
// cutoff are candidates, malformed keys are ignored, and active and
// retained descriptors are the complete root set.
func TestCleanupDeletesOnlyUnreferencedAtOrBeforeCutoff(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, _, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	cpKey := func(gen, id uint64) storage.Key {
		return storage.Key{Kind: storage.KindCheckpoint, Generation: gen, ID: testUUIDv7(id)}
	}
	incKey := func(gen, id uint64) storage.Key {
		return storage.Key{Kind: storage.KindIncrement, Generation: gen, ID: testUUIDv7(id)}
	}
	var sum storage.SHA256
	h1, h2, h3, h4, h5 := hexOID(t, "1"), hexOID(t, "2"), hexOID(t, "3"), hexOID(t, "4"), hexOID(t, "5")

	m := storage.Manifest{
		Version:    1,
		Generation: 5,
		Head:       h5,
		Checkpoint: storage.Checkpoint{
			ID: testUUIDv7(10), Publication: testUUIDv7(11), ThroughGeneration: 3, Head: h3,
			Key: cpKey(3, 10), SHA256: sum, Size: 10,
		},
		Increments: []storage.Increment{
			{Generation: 4, Publication: testUUIDv7(12), Parent: h3, Head: h4, Key: incKey(4, 12), SHA256: sum, Size: 10},
			{Generation: 5, Publication: testUUIDv7(13), Parent: h4, Head: h5, Key: incKey(5, 13), SHA256: sum, Size: 10},
		},
		Retained: []storage.Retained{{
			RetiredAtGeneration: 4,
			Head:                h3,
			Checkpoint: storage.Checkpoint{
				ID: testUUIDv7(14), Publication: testUUIDv7(15), ThroughGeneration: 1, Head: h1,
				Key: cpKey(1, 14), SHA256: sum, Size: 10,
			},
			Increments: []storage.Increment{
				{Generation: 2, Publication: testUUIDv7(16), Parent: h1, Head: h2, Key: incKey(2, 16), SHA256: sum, Size: 10},
				{Generation: 3, Publication: testUUIDv7(17), Parent: h2, Head: h3, Key: incKey(3, 17), SHA256: sum, Size: 10},
			},
		}},
	}
	data, err := storage.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	if _, err := store.CreateObject(context.Background(), storage.CurrentKey, data); err != nil {
		t.Fatalf("CreateObject(current) = %v", err)
	}
	place := func(key string) {
		if err := store.PutObject(context.Background(), key, strings.NewReader("bytes"), storage.Metadata{}); err != nil {
			t.Fatalf("PutObject(%q) = %v", key, err)
		}
	}
	for _, key := range []string{
		"packs/checkpoints/1-" + testUUIDv7(14).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(16).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(17).String() + ".pack",
		"packs/checkpoints/3-" + testUUIDv7(10).String() + ".pack",
		"packs/increments/4-" + testUUIDv7(12).String() + ".pack",
		"packs/increments/5-" + testUUIDv7(13).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(20).String() + ".pack", // orphan at 2
		"packs/increments/4-" + testUUIDv7(21).String() + ".pack", // orphan at 4
		"packs/increments/6-" + testUUIDv7(22).String() + ".pack", // proposal after the cutoff
		"packs/increments/not-a-key.pack",                         // malformed key
	} {
		place(key)
	}

	nb.cleanup(context.Background(), 4)

	present := func(key string) bool {
		_, _, err := store.ReadObject(context.Background(), key)
		return err == nil
	}
	for _, gone := range []string{
		"packs/increments/2-" + testUUIDv7(20).String() + ".pack",
		"packs/increments/4-" + testUUIDv7(21).String() + ".pack",
	} {
		if present(gone) {
			t.Fatalf("unreferenced candidate %s survived cleanup", gone)
		}
	}
	for _, kept := range []string{
		"packs/checkpoints/1-" + testUUIDv7(14).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(16).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(17).String() + ".pack",
		"packs/checkpoints/3-" + testUUIDv7(10).String() + ".pack",
		"packs/increments/4-" + testUUIDv7(12).String() + ".pack",
		"packs/increments/5-" + testUUIDv7(13).String() + ".pack",
		"packs/increments/6-" + testUUIDv7(22).String() + ".pack",
		"packs/increments/not-a-key.pack",
	} {
		if !present(kept) {
			t.Fatalf("protected object %s was deleted", kept)
		}
	}
	mets := nb.Metrics()
	if mets.CleanupRuns.Load() != 1 || mets.CleanupCandidates.Load() != 7 || mets.CleanupDeleted.Load() != 2 || mets.CleanupErrors.Load() != 0 {
		t.Fatalf("cleanup metrics = runs %d candidates %d deleted %d errors %d, want 1/7/2/0",
			mets.CleanupRuns.Load(), mets.CleanupCandidates.Load(), mets.CleanupDeleted.Load(), mets.CleanupErrors.Load())
	}
}

// TestCleanupRereadsCurrentBeforeEachBatch proves the list/CAS race
// guard: a candidate listed before current moved is not deleted when a
// reread shows it is now referenced by the authoritative manifest.
func TestCleanupRereadsCurrentBeforeEachBatch(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}

	cpKey := func(gen, id uint64) storage.Key {
		return storage.Key{Kind: storage.KindCheckpoint, Generation: gen, ID: testUUIDv7(id)}
	}
	incKey := func(gen, id uint64) storage.Key {
		return storage.Key{Kind: storage.KindIncrement, Generation: gen, ID: testUUIDv7(id)}
	}
	var sum storage.SHA256
	h1, h2, h3, h4, h5 := hexOID(t, "1"), hexOID(t, "2"), hexOID(t, "3"), hexOID(t, "4"), hexOID(t, "5")
	orphanA := storage.Increment{
		Generation: 2, Publication: testUUIDv7(20), Parent: h1, Head: hexOID(t, "a"),
		Key: incKey(2, 20), SHA256: sum, Size: 10,
	}
	orphanB := storage.Increment{
		Generation: 4, Publication: testUUIDv7(21), Parent: h3, Head: hexOID(t, "b"),
		Key: incKey(4, 21), SHA256: sum, Size: 10,
	}

	build := func(retained []storage.Retained) storage.Manifest {
		return storage.Manifest{
			Version:    1,
			Generation: 5,
			Head:       h5,
			Checkpoint: storage.Checkpoint{
				ID: testUUIDv7(10), Publication: testUUIDv7(11), ThroughGeneration: 3, Head: h3,
				Key: cpKey(3, 10), SHA256: sum, Size: 10,
			},
			Increments: []storage.Increment{
				{Generation: 4, Publication: testUUIDv7(12), Parent: h3, Head: h4, Key: incKey(4, 12), SHA256: sum, Size: 10},
				{Generation: 5, Publication: testUUIDv7(13), Parent: h4, Head: h5, Key: incKey(5, 13), SHA256: sum, Size: 10},
			},
			Retained: retained,
		}
	}
	oldRetained := storage.Retained{
		RetiredAtGeneration: 4,
		Head:                h3,
		Checkpoint: storage.Checkpoint{
			ID: testUUIDv7(14), Publication: testUUIDv7(15), ThroughGeneration: 1, Head: h1,
			Key: cpKey(1, 14), SHA256: sum, Size: 10,
		},
		Increments: []storage.Increment{
			{Generation: 2, Publication: testUUIDv7(16), Parent: h1, Head: h2, Key: incKey(2, 16), SHA256: sum, Size: 10},
			{Generation: 3, Publication: testUUIDv7(17), Parent: h2, Head: h3, Key: incKey(3, 17), SHA256: sum, Size: 10},
		},
	}
	// M2 additionally retains orphanA: a reread after the list must keep it.
	newRetained := storage.Retained{
		RetiredAtGeneration: 5,
		Head:                orphanA.Head,
		Checkpoint: storage.Checkpoint{
			ID: testUUIDv7(30), Publication: testUUIDv7(31), ThroughGeneration: 1, Head: h1,
			Key: cpKey(1, 30), SHA256: sum, Size: 10,
		},
		Increments: []storage.Increment{orphanA},
	}
	encode := func(m storage.Manifest) []byte {
		data, err := storage.EncodeManifest(m)
		if err != nil {
			t.Fatalf("EncodeManifest() = %v", err)
		}
		return data
	}
	if _, err := store.CreateObject(context.Background(), storage.CurrentKey, encode(build([]storage.Retained{oldRetained}))); err != nil {
		t.Fatalf("CreateObject(current) = %v", err)
	}
	place := func(key string) {
		if err := store.PutObject(context.Background(), key, strings.NewReader("bytes"), storage.Metadata{}); err != nil {
			t.Fatalf("PutObject(%q) = %v", key, err)
		}
	}
	for _, key := range []string{
		"packs/checkpoints/1-" + testUUIDv7(14).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(16).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(17).String() + ".pack",
		"packs/checkpoints/3-" + testUUIDv7(10).String() + ".pack",
		"packs/increments/4-" + testUUIDv7(12).String() + ".pack",
		"packs/increments/5-" + testUUIDv7(13).String() + ".pack",
		orphanA.Key.String(), // candidate at 2
		orphanB.Key.String(), // candidate at 4
	} {
		place(key)
	}

	// The wrapper replaces current right after the first list, exactly the
	// window between LIST and the delete batch.
	replace := &replaceOnListStore{Store: store, current: encode(build([]storage.Retained{newRetained, oldRetained}))}
	nb2, _, _ := newNotebook(t, nbConfig{store: replace, ids: ids})
	nb2.cleanup(context.Background(), 4)

	if _, _, err := store.ReadObject(context.Background(), orphanA.Key.String()); err != nil {
		t.Fatalf("orphanA became referenced before the batch but was deleted: %v", err)
	}
	if _, _, err := store.ReadObject(context.Background(), orphanB.Key.String()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("orphanB survived (err %v), want the still-unreferenced candidate deleted", err)
	}
}

// replaceOnListStore replaces current with newCurrent after the first
// listing, simulating a concurrent checkpoint CAS between LIST and DELETE.
// Any replacement failure is recorded so the test can assert the race
// guard ran as designed.
type replaceOnListStore struct {
	*fake.Store
	current    []byte
	replaceErr error
	once       sync.Once
}

func (s *replaceOnListStore) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	err := s.Store.ListObjects(ctx, prefix, fn)
	s.once.Do(func() {
		rc, info, rerr := s.Store.ReadObject(ctx, storage.CurrentKey)
		if rerr != nil {
			s.replaceErr = rerr
			return
		}
		rc.Close()
		_, s.replaceErr = s.Store.ReplaceObject(ctx, storage.CurrentKey, info.ETag, s.current)
	})
	return err
}

// TestCleanupDeleteFailureRecordedAndStatePreserved proves an injected
// delete failure is recorded in metrics, preserves the obsolete storage,
// and never changes the checkpoint or commit result.
func TestCleanupDeleteFailureRecordedAndStatePreserved(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2, retained: 0, retainedSet: true})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first") // gen 1
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second") // gen 2
	writeLocal(t, w, map[string]string{"a.md": "v3"})
	store.FailNext(fake.OpDelete, storage.ErrTransport)
	commitOK(t, nb, "third") // gen 3, checkpoint gen 4, cleanup delete fails

	m := readManifest(t, store)
	if m.Generation != 4 || m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("manifest = gen %d checkpoint through %d, want the checkpointed state", m.Generation, m.Checkpoint.ThroughGeneration)
	}
	mets := nb.Metrics()
	if mets.CleanupRuns.Load() != 1 || mets.CleanupErrors.Load() != 1 || mets.CleanupDeleted.Load() != 0 {
		t.Fatalf("cleanup metrics = runs %d errors %d deleted %d, want 1/1/0", mets.CleanupRuns.Load(), mets.CleanupErrors.Load(), mets.CleanupDeleted.Load())
	}
	for _, key := range []string{
		"packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack",
		"packs/increments/2-" + testUUIDv7(3).String() + ".pack",
		"packs/increments/3-" + testUUIDv7(4).String() + ".pack",
	} {
		if _, _, err := store.ReadObject(context.Background(), key); err != nil {
			t.Fatalf("obsolete pack %s missing after the failed delete: %v", key, err)
		}
	}
}

// TestStaleReaderRestartsAfterCheckpointCleanup proves the stale-reader
// restart with a deterministic deletion barrier: a reader blocked on a pack
// that checkpoint cleanup deletes discards its stale observation, rereads
// current, and reconstructs the final state.
func TestStaleReaderRestartsAfterCheckpointCleanup(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2, retained: 0, retainedSet: true})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: DefaultCheckpointPacks})

	writeLocal(t, aw, map[string]string{"shared.md": "base", "a.md": "A"})
	pullOK(t, a)
	commitOK(t, a, "first") // gen 1, ids 1 and 2
	writeLocal(t, aw, map[string]string{"a.md": "A2"})
	commitOK(t, a, "second") // gen 2, id 3
	pullOK(t, b)             // B imports and caches the checkpoint and increment packs

	inc3Key := "packs/increments/3-" + testUUIDv7(4).String() + ".pack"
	cp3Key := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	store.BlockNext(fake.OpPut, cp3Key)
	store.BlockNext(fake.OpGet, inc3Key)

	// A publishes gen 3 and blocks its checkpoint pack upload; B observes
	// the pre-checkpoint manifest and blocks on the increment pack that
	// checkpoint cleanup is about to delete.
	writeLocal(t, aw, map[string]string{"a.md": "A3"})
	done := make(chan error, 1)
	go func() { done <- errOnly(a.Commit(context.Background(), "third")) }()
	waitBlocked(t, store, fake.OpPut, cp3Key)
	pullDone := make(chan error, 1)
	go func() { pullDone <- errOnly(b.Pull(context.Background())) }()
	waitBlocked(t, store, fake.OpGet, inc3Key)

	// A's checkpoint replaces the prefix and cleanup (retention 0) deletes
	// the pack B is blocked on.
	store.Unblock(fake.OpPut, cp3Key)
	if err := <-done; err != nil {
		t.Fatalf("A Commit() = %v", err)
	}
	store.Unblock(fake.OpGet, inc3Key)
	if err := <-pullDone; err != nil {
		t.Fatalf("B Pull() = %v", err)
	}

	m := readManifest(t, store)
	if m.Generation != 4 || m.Checkpoint.ThroughGeneration != 3 {
		t.Fatalf("manifest = gen %d checkpoint through %d, want the checkpointed state", m.Generation, m.Checkpoint.ThroughGeneration)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 4 {
		t.Fatalf("B baseline generation = %d, want 4", gen)
	}
	got := localSnapshot(t, bw)
	want := map[string]string{"shared.md": "base", "a.md": "A3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("L after the stale restart = %v, want %v", got, want)
	}
}

// TestMetricsExposeTailShape proves the tail metrics track the active
// increment count and byte sum of the last observed manifest.
func TestMetricsExposeTailShape(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second")
	m := readManifest(t, store)
	mets := nb.Metrics()
	if mets.TailCount.Load() != 1 {
		t.Fatalf("tail count = %d, want 1", mets.TailCount.Load())
	}
	if mets.TailBytes.Load() != m.Increments[0].Size {
		t.Fatalf("tail bytes = %d, want %d", mets.TailBytes.Load(), m.Increments[0].Size)
	}
}

// putCounter counts PutObject calls per protocol key, so a test can assert
// exactly how many times one unique pack key was uploaded.
type putCounter struct {
	storage.ObjectStore
	mu   sync.Mutex
	puts map[string]int
}

func (s *putCounter) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	s.mu.Lock()
	if s.puts == nil {
		s.puts = map[string]int{}
	}
	s.puts[key]++
	s.mu.Unlock()
	return s.ObjectStore.PutObject(ctx, key, r, meta)
}

func (s *putCounter) Count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts[key]
}

// failFromReplace fails every ReplaceObject whose 1-based index lies in
// [failFrom, failTo] (failTo 0 means unbounded), counting attempts.
type failFromReplace struct {
	storage.ObjectStore
	failFrom int
	failTo   int
	err      error
	replaces int
}

func (s *failFromReplace) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	s.replaces++
	if s.replaces >= s.failFrom && (s.failTo == 0 || s.replaces <= s.failTo) {
		return "", s.err
	}
	return s.ObjectStore.ReplaceObject(ctx, key, etag, data)
}

// lastCommitFailEngine wraps a fake engine so the second read of the most
// recently created commit fails while armed. The commit flow reads the new
// commit once while exporting its increment pack; the checkpoint's pack
// export reads the exact same commit again, so the injected failure hits
// only checkpoint pack creation and the accepted commit result is
// unchanged.
type lastCommitFailEngine struct {
	inner       *fakeEngine
	armed       bool
	baseline    git.OID // lastCreated at arm time
	lastCreated git.OID
	reads       int // reads of lastCreated while armed
}

func (e *lastCommitFailEngine) arm() {
	e.armed = true
	e.baseline = e.lastCreated
	e.reads = 0
}
func (e *lastCommitFailEngine) disarm() { e.armed = false }

func (e *lastCommitFailEngine) CreateRepo(path string) (git.Repository, error) {
	r, err := e.inner.CreateRepo(path)
	if err != nil {
		return nil, err
	}
	return &lastCommitFailRepo{Repository: r, e: e}, nil
}

func (e *lastCommitFailEngine) OpenRepo(path string) (git.Repository, error) {
	r, err := e.inner.OpenRepo(path)
	if err != nil {
		return nil, err
	}
	return &lastCommitFailRepo{Repository: r, e: e}, nil
}

var _ workspace.Engine = (*lastCommitFailEngine)(nil)

type lastCommitFailRepo struct {
	git.Repository
	e *lastCommitFailEngine
}

func (r *lastCommitFailRepo) CreateCommit(spec git.CommitSpec) (git.OID, error) {
	id, err := r.Repository.CreateCommit(spec)
	if err == nil {
		r.e.lastCreated = id
	}
	return id, err
}

func (r *lastCommitFailRepo) ReadCommit(id git.OID) (git.Commit, error) {
	if r.e.armed && id == r.e.lastCreated && id != r.e.baseline {
		r.e.reads++
		if r.e.reads > 1 {
			return git.Commit{}, errors.New("injected: checkpoint pack creation failure")
		}
	}
	return r.Repository.ReadCommit(id)
}

// packBytes reads one stored object's complete bytes.
func packBytes(t *testing.T, store storage.ObjectStore, key string) []byte {
	t.Helper()
	rc, _, err := store.ReadObject(context.Background(), key)
	if err != nil {
		t.Fatalf("ReadObject(%q) = %v", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll(%q) = %v", key, err)
	}
	return data
}

// TestCheckpointCorruptPackRejected proves a checkpoint pack whose bytes
// contradict its descriptor is a storage-integrity failure and the old
// index stays authoritative (error coverage: corrupt-upload fixture). The
// check runs against a fresh reader whose cache cannot satisfy the
// descriptor, so the corrupted store bytes are actually imported.
func TestCheckpointCorruptPackRejected(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, checkpointPacks: 2})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second")
	writeLocal(t, w, map[string]string{"a.md": "v3"})
	commitOK(t, nb, "third")
	m := readManifest(t, store)
	if m.Generation != 4 {
		t.Fatalf("generation = %d, want the checkpointed state 4", m.Generation)
	}
	good := packBytes(t, store, m.Checkpoint.Key.String())

	// Corrupt the checkpoint pack in place: the descriptor no longer
	// matches the bytes.
	if err := store.PutObject(context.Background(), m.Checkpoint.Key.String(),
		bytes.NewReader(append(append([]byte(nil), good...), []byte("corruption")...)), storage.Metadata{}); err != nil {
		t.Fatalf("corrupt checkpoint pack: %v", err)
	}

	// A fresh reader with an empty cache must import the corrupted pack
	// and refuse it; the old index stays authoritative and L is untouched.
	c, cw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	baselineBefore := cw.Baseline()
	assertErrorCode(t, errOnly(c.Pull(context.Background())), CodeStorageIntegrity)
	if got := cw.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the corrupt pack: %+v -> %+v", baselineBefore, got)
	}
	if snap := localSnapshot(t, cw); len(snap) != 0 {
		t.Fatalf("L changed by the corrupt pack: %v", snap)
	}
	if m2 := readManifest(t, store); m2.Generation != 4 {
		t.Fatalf("manifest changed by the failed pull: gen %d, want 4", m2.Generation)
	}
}
