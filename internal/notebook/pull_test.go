package notebook

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestPullEmptyRemote proves the first pull against an absent current:
// valid local files become local additions merged from the canonical empty
// baseline, no remote state is created, and the pulled marker initializes
// P for a later commit.
func TestPullEmptyRemote(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "hello", "sub/b.md": "nested"})
	pullOK(t, nb)

	got := localSnapshot(t, w)
	if want := map[string]string{"a.md": "hello", "sub/b.md": "nested"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("L = %v, want %v", got, want)
	}
	if b := w.Baseline(); b.RemoteGeneration != 0 || !b.Head.IsZero() || b.Tree != workspace.EmptyTreeID {
		t.Fatalf("Baseline() = %+v, want generation 0 with the empty tree", b)
	}
	if !w.Pulled() {
		t.Fatal("Pull() did not mark P as pulled")
	}
	if store.ObjectCount() != 0 {
		t.Fatalf("pull created %d remote objects, want none", store.ObjectCount())
	}
}

// TestFirstPullMergesNonemptyLocal is the acceptance case for a nonempty L
// against an empty remote: the canonical empty tree is the merge base and
// the local files survive as additions.
func TestFirstPullMergesNonemptyLocal(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"local.md": "mine"})
	pullOK(t, nb)

	got := localSnapshot(t, w)
	if len(got) != 1 || got["local.md"] != "mine" {
		t.Fatalf("L = %v, want the local file only", got)
	}
	if !w.Pulled() {
		t.Fatal("Pull() did not mark P as pulled")
	}
}

// TestPullRebasesLocalChanges proves the pull rebase contract: local
// additions, modifications, and deletions merge onto the remote state and
// the baseline advances to R without discarding any mergeable local
// change.
func TestPullRebasesLocalChanges(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	// Shared baseline: a.md, b.md, and d.md at v1.
	writeLocal(t, aw, map[string]string{"a.md": "a1", "b.md": "b1", "d.md": "d1"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)

	// B edits locally: modify b.md, delete d.md, add c.md.
	writeLocal(t, bw, map[string]string{"b.md": "b-local"})
	removeLocal(t, bw, "d.md")
	writeLocal(t, bw, map[string]string{"c.md": "c-local"})

	// A publishes a.md -> a2 remotely.
	writeLocal(t, aw, map[string]string{"a.md": "a2"})
	commitOK(t, a, "remote change")

	// B pulls: the remote modification wins on a.md, B's local
	// modification and addition survive, and B's deletion sticks.
	pullOK(t, b)
	got := localSnapshot(t, bw)
	want := map[string]string{"a.md": "a2", "b.md": "b-local", "c.md": "c-local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("L after rebase = %v, want %v", got, want)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 2 {
		t.Fatalf("baseline generation = %d, want 2", gen)
	}
}

// TestPullDownloadsOnlyMissingPacks proves the pack-byte cache: a pull
// downloads only descriptors absent from the local cache, and a cache hit
// needs no pack GET at all.
func TestPullDownloadsOnlyMissingPacks(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")

	// First pull after the publication: current plus the checkpoint pack.
	getsAfterCommit := store.Calls(fake.OpGet)
	pullOK(t, nb)
	if got := store.Calls(fake.OpGet) - getsAfterCommit; got != 2 {
		t.Fatalf("first pull GET calls = %d, want current + pack", got)
	}
	// Second pull: the cached pack is verified and no pack GET happens.
	pullOK(t, nb)
	if got := store.Calls(fake.OpGet) - getsAfterCommit; got != 3 {
		t.Fatalf("second pull GET calls = %d, want only current", got)
	}
}

// TestPullCacheCorruptionForcesFreshDownload proves a corrupt cache entry
// is discarded and re-downloaded, never a false hit.
func TestPullCacheCorruptionForcesFreshDownload(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	pullOK(t, nb) // fills the cache
	m := readManifest(t, store)
	getsBefore := store.Calls(fake.OpGet)
	cachePath := filepath.Join(w.CacheDir(), m.Checkpoint.SHA256.String())
	if err := os.WriteFile(cachePath, []byte("corrupt cache bytes"), 0o600); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}

	pullOK(t, nb)
	if got := store.Calls(fake.OpGet) - getsBefore; got != 2 {
		t.Fatalf("pull after cache corruption GET calls = %d, want a fresh download of current + pack", got)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("repaired cache missing: %v", err)
	}
	if got := storage.SHA256(sha256.Sum256(data)); got != m.Checkpoint.SHA256 {
		t.Fatalf("repaired cache sha = %s, want %s", got, m.Checkpoint.SHA256)
	}
}

// TestPullReachabilityFailureLeavesLUntouched proves that a manifest whose
// accepted history cannot be validated fails with STORAGE_INTEGRITY before
// L changes, and the local state survives for caller recovery.
func TestPullReachabilityFailureLeavesLUntouched(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")

	// Corrupt current: a valid manifest whose head names a commit the
	// packs do not contain. The schema validates; the history walk fails.
	bogus, err := git.ParseOID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("ParseOID() = %v", err)
	}
	m := readManifest(t, store)
	m.Head = bogus
	m.Checkpoint.Head = bogus
	data, err := storage.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	rc, info, err := store.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		t.Fatalf("read current = %v", err)
	}
	rc.Close()
	if _, err := store.ReplaceObject(context.Background(), storage.CurrentKey, info.ETag, data); err != nil {
		t.Fatalf("replace current = %v", err)
	}

	writeLocal(t, w, map[string]string{"local.md": "mine"})
	baselineBefore := w.Baseline()
	before := localSnapshot(t, w)
	assertErrorCode(t, nb.Pull(context.Background()), CodeStorageIntegrity)
	if got := localSnapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed pull: %v -> %v", before, got)
	}
	if got := w.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed pull: %+v -> %+v", baselineBefore, got)
	}
	if got := readLocal(t, w, "local.md"); got != "mine" {
		t.Fatalf("local file lost: %q", got)
	}
}

// TestPullConflictWritesMarkersAndKeepsL proves a conflicting pull: L is
// rewritten with the full merge result (markers on the conflicted path,
// clean content elsewhere, local-only files preserved), the remote state
// becomes the baseline, and the pre-call bytes are not restored.
func TestPullConflictWritesMarkersAndKeepsL(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"a.md": "remote v1", "clean.md": "clean"})
	pullOK(t, a)
	commitOK(t, a, "base")

	writeLocal(t, bw, map[string]string{"a.md": "local v1", "local-only.md": "local only"})
	ne := assertErrorCode(t, b.Pull(context.Background()), CodeContentConflict)
	if len(ne.Files) != 1 || ne.Files[0].Path != "a.md" {
		t.Fatalf("conflict files = %+v, want a.md", ne.Files)
	}
	if want := []git.MarkerRange{{Start: 1, End: 5}}; !reflect.DeepEqual(ne.Files[0].Ranges, want) {
		t.Fatalf("conflict ranges = %+v, want %+v", ne.Files[0].Ranges, want)
	}

	got := localSnapshot(t, bw)
	for _, marker := range []string{"<<<<<<< local", "local v1", "=======", "remote v1", ">>>>>>> remote"} {
		if !strings.Contains(got["a.md"], marker) {
			t.Errorf("a.md after conflict missing %q: %q", marker, got["a.md"])
		}
	}
	if got["a.md"] == "local v1" {
		t.Fatal("pull restored the pre-call bytes instead of the merged result")
	}
	if got["clean.md"] != "clean" {
		t.Fatalf("clean.md = %q, want the clean merged content", got["clean.md"])
	}
	if got["local-only.md"] != "local only" {
		t.Fatalf("local-only.md = %q, want the local addition preserved", got["local-only.md"])
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation after conflict = %d, want the remote state 1", gen)
	}
	if !bw.Pulled() {
		t.Fatal("a conflicting pull must initialize P")
	}
}

// TestPullPackGetFailureLeavesBaselineUnchanged proves the pack GET
// failure path: a referenced pack that disappeared and a current that did
// not move is a storage-integrity error, and the baseline and L stay
// untouched.
func TestPullPackGetFailureLeavesBaselineUnchanged(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	m := readManifest(t, store)
	if err := store.DeleteObjects(context.Background(), []string{m.Checkpoint.Key.String()}); err != nil {
		t.Fatalf("DeleteObjects() = %v", err)
	}
	writeLocal(t, w, map[string]string{"local.md": "mine"})
	baselineBefore := w.Baseline()
	before := localSnapshot(t, w)

	assertErrorCode(t, nb.Pull(context.Background()), CodeStorageIntegrity)
	if got := localSnapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed pull: %v -> %v", before, got)
	}
	if got := w.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed pull: %+v -> %+v", baselineBefore, got)
	}
}

// TestPullCorruptPackRejected proves a pack whose bytes contradict its
// descriptor checksum and size is refused before import, and corrupt remote
// data never reaches visible files.
func TestPullCorruptPackRejected(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	m := readManifest(t, store)
	if err := store.PutObject(context.Background(), m.Checkpoint.Key.String(), strings.NewReader("garbage pack bytes"), storage.Metadata{}); err != nil {
		t.Fatalf("corrupt pack: %v", err)
	}
	baselineBefore := w.Baseline()

	assertErrorCode(t, nb.Pull(context.Background()), CodeStorageIntegrity)
	if got := w.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the corrupt pack: %+v -> %+v", baselineBefore, got)
	}
	if _, err := os.Stat(filepath.Join(w.Path(), "a.md")); err != nil {
		t.Fatalf("visible file missing after failed pull: %v", err)
	}
}

// TestPullStalePackRestartSucceeds proves the stale-observation restart of
// architecture section 10: a pack that disappeared during cleanup discards
// the observation; when current moved and the pack is back, the pull
// restarts and succeeds.
func TestPullStalePackRestartSucceeds(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")

	m := readManifest(t, store)
	rc, _, err := store.ReadObject(context.Background(), m.Checkpoint.Key.String())
	if err != nil {
		t.Fatalf("read pack = %v", err)
	}
	packBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read pack body = %v", err)
	}
	rc.Close()
	restart := &restartStore{Store: store, key: m.Checkpoint.Key.String(), data: packBytes}

	nb2, w2, _ := newNotebook(t, nbConfig{store: restart, ids: ids})
	pullOK(t, nb2)
	got := localSnapshot(t, w2)
	if len(got) != 1 || got["a.md"] != "v1" {
		t.Fatalf("L after stale restart = %v, want the published file", got)
	}
	if gen := w2.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}
}

// TestPullInvalidContentMapsToInvalidRequest proves invalid visible content
// is rejected before any S3 access.
func TestPullInvalidContentMapsToInvalidRequest(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	if err := os.WriteFile(filepath.Join(w.Path(), "bad.md"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}
	assertErrorCode(t, nb.Pull(context.Background()), CodeInvalidRequest)
	if got := store.Calls(fake.OpGet); got != 0 {
		t.Fatalf("pull with invalid content made %d GET calls, want none", got)
	}
}

// TestPullEntryRecoveryRunsBeforeWork proves the generic recovery entry:
// after a failed local mutation leaves P durably requiring recovery, the
// next pull performs an authoritative resynchronization before any normal
// work, and the recovered state is correct.
func TestPullEntryRecoveryRunsBeforeWork(t *testing.T) {
	engine := newFakeEngine()
	store := fake.New("")
	ids := &testIDSource{}
	triggered := errors.New("injected mutation failure")

	var replaceCalls, recoverCalls int
	wsFail := &workspace.Failpoints{
		Replace: func() error {
			replaceCalls++
			if replaceCalls == 2 {
				return triggered // fail the commit's local acceptance
			}
			return nil
		},
		Recover: func() error {
			recoverCalls++
			if recoverCalls == 1 {
				return triggered // fail the first recovery attempt
			}
			return nil
		},
	}
	nb, w, cfg := newNotebook(t, nbConfig{store: store, engine: engine, ids: ids, wsFail: wsFail})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	ne := assertErrorCode(t, nb.Commit(context.Background(), "first"), CodeRecoveryFailure)
	if ne.Recovery == nil {
		t.Fatal("RECOVERY_FAILURE carries no recovery report")
	}
	if ne.Recovery.Stage != stageCommit || ne.Recovery.RemoteAccepted != RemoteAcceptedYes || ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want commit.accept / yes / resynchronized=false", ne.Recovery)
	}
	if !w.RecoveryRequired() {
		t.Fatal("failed recovery must leave P durably requiring recovery")
	}

	// Reopen without failpoints: the next pull retries the authoritative
	// resynchronization before any normal work.
	reopenCfg := cfg
	reopenCfg.Failpoints = nil
	reopened, err := workspace.Open(context.Background(), reopenCfg)
	if err != nil {
		t.Fatalf("reopen Open() = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	nb2, err := New(Config{
		Workspace:           reopened,
		Store:               store,
		RetryLimit:          DefaultRetryLimit,
		CheckpointPacks:     DefaultCheckpointPacks,
		RetainedCheckpoints: DefaultRetainedCheckpoints,
		NewID:               ids.next,
		Now:                 func() time.Time { return testNow },
		Waiter:              noSleepWaiter(),
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	pullOK(t, nb2)
	if reopened.RecoveryRequired() {
		t.Fatal("entry recovery did not clear the recovery flag")
	}
	if gen := reopened.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation after entry recovery = %d, want 1", gen)
	}
	if got := readLocal(t, reopened, "a.md"); got != "v1" {
		t.Fatalf("L after entry recovery = %q, want the accepted content", got)
	}
}
