package notebook

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
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

// TestPullDownloadsOnlyMissingPacks proves the pack-byte cache and the
// import skip: a reader's first pull downloads current plus the one pack it
// lacks, and its next pull of the unchanged tail reads only current, since
// the repository already holds the pack's objects.
func TestPullDownloadsOnlyMissingPacks(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	reader, _, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})

	// First pull of a cold reader: current plus the checkpoint pack.
	getsAfterCommit := store.Calls(fake.OpGet)
	pullOK(t, reader)
	if got := store.Calls(fake.OpGet) - getsAfterCommit; got != 2 {
		t.Fatalf("first pull GET calls = %d, want current + pack", got)
	}
	// Second pull: the pack is already imported and no pack GET happens.
	pullOK(t, reader)
	if got := store.Calls(fake.OpGet) - getsAfterCommit; got != 3 {
		t.Fatalf("second pull GET calls = %d, want only current", got)
	}
	// The publisher never needs its own pack: one GET, for current.
	getsBefore := store.Calls(fake.OpGet)
	pullOK(t, nb)
	if got := store.Calls(fake.OpGet) - getsBefore; got != 1 {
		t.Fatalf("publisher pull GET calls = %d, want only current", got)
	}
}

// TestPullCacheCorruptionForcesFreshDownload proves a corrupt cache entry
// is discarded and re-downloaded, never a false hit: a cold reader whose
// cache already holds wrong bytes under the pack's SHA-256 downloads the
// pack and heals the entry.
func TestPullCacheCorruptionForcesFreshDownload(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	m := readManifest(t, store)
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	cachePath := filepath.Join(rw.CacheDir(), m.Checkpoint.SHA256.String())
	if err := os.MkdirAll(rw.CacheDir(), 0o700); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("corrupt cache bytes"), 0o600); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}
	getsBefore := store.Calls(fake.OpGet)

	pullOK(t, reader)
	if got := store.Calls(fake.OpGet) - getsBefore; got != 2 {
		t.Fatalf("pull over a corrupt cache entry GET calls = %d, want current + pack", got)
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
	assertErrorCode(t, errOnly(nb.Pull(context.Background())), CodeStorageIntegrity)
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

	writeLocal(t, aw, map[string]string{"a.md": "base v0"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b) // B at gen 1; a first pull must start from a copy of the notebook

	writeLocal(t, aw, map[string]string{"a.md": "remote v1", "clean.md": "clean"})
	commitOK(t, a, "remote change")

	writeLocal(t, bw, map[string]string{"a.md": "local v1", "local-only.md": "local only"})
	res, err := b.Pull(context.Background())
	ne := assertErrorCode(t, err, CodeContentConflict)
	assertZeroResult(t, res)
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
	if gen := bw.Baseline().RemoteGeneration; gen != 2 {
		t.Fatalf("baseline generation after conflict = %d, want the remote state 2", gen)
	}
}

// TestFileDirectoryConflictKeepsLocalSide proves a file-versus-directory
// conflict leaves the local side in L in both directions, through both
// operations: the local file when R made the path a directory, and every
// file of the local directory when R made the path a file. The fake engine
// stages the directory side's entries as libgit2 does (stage 0 below the
// path), so only the local tree tells the sides apart.
func TestFileDirectoryConflictKeepsLocalSide(t *testing.T) {
	tests := []struct {
		name   string
		base   map[string]string
		remote map[string]string
		local  map[string]string
		remove []string
	}{
		{
			name:   "local directory remote file",
			remote: map[string]string{"p": "remote file"},
			local:  map[string]string{"p/q.md": "local q", "p/sub/r.md": "local r"},
		},
		{
			name:   "local file remote directory",
			remote: map[string]string{"p/q.md": "remote q"},
			local:  map[string]string{"p": "local file"},
		},
		{
			// The remote edit below p conflicts with the local deletion,
			// but only the conflict at p is reported and L keeps the file.
			name:   "local file replaces a directory the remote side changed",
			base:   map[string]string{"p/q.md": "base q"},
			remote: map[string]string{"p/q.md": "remote q"},
			local:  map[string]string{"p": "local file"},
			remove: []string{"p/q.md", "p"},
		},
	}
	ops := []struct {
		name string
		run  func(nb *Notebook) error
	}{
		{name: "pull", run: func(nb *Notebook) error { return errOnly(nb.Pull(context.Background())) }},
		{name: "commit", run: func(nb *Notebook) error { return errOnly(nb.Commit(context.Background(), "mine")) }},
	}
	for _, tt := range tests {
		for _, op := range ops {
			t.Run(tt.name+"/"+op.name, func(t *testing.T) {
				store := fake.New("")
				ids := &testIDSource{}
				a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
				b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

				base := map[string]string{"base.md": "base"}
				maps.Copy(base, tt.base)
				writeLocal(t, aw, base)
				pullOK(t, a)
				commitOK(t, a, "base")
				pullOK(t, b)

				writeLocal(t, aw, tt.remote)
				commitOK(t, a, "remote side")
				for _, path := range tt.remove {
					removeLocal(t, bw, path)
				}
				writeLocal(t, bw, tt.local)

				ne := assertErrorCode(t, op.run(b), CodeContentConflict)
				if len(ne.Files) != 1 || ne.Files[0].Path != "p" || ne.Files[0].Reason != FileReasonPathConflict {
					t.Fatalf("conflict files = %+v, want one PATH_CONFLICT at p", ne.Files)
				}
				want := map[string]string{"base.md": "base"}
				maps.Copy(want, tt.local)
				if got := localSnapshot(t, bw); !reflect.DeepEqual(got, want) {
					t.Fatalf("L after the conflict = %v, want the local side %v", got, want)
				}
			})
		}
	}
}

// TestPullPackGetFailureLeavesBaselineUnchanged proves the stale-manifest
// failure path: a referenced pack that disappeared and a current that did
// not move is a storage-integrity error for a reader that needs the pack,
// and the reader's baseline and L stay untouched.
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
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	writeLocal(t, rw, map[string]string{"local.md": "mine"})
	baselineBefore := rw.Baseline()
	before := localSnapshot(t, rw)

	assertErrorCode(t, errOnly(reader.Pull(context.Background())), CodeStorageIntegrity)
	if got := localSnapshot(t, rw); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed pull: %v -> %v", before, got)
	}
	if got := rw.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed pull: %+v -> %+v", baselineBefore, got)
	}
}

// TestPullCorruptPackRejected proves a pack whose bytes contradict its
// descriptor checksum and size is refused before import, and corrupt remote
// data never reaches visible files. The reader is a second workspace: the
// publisher already holds every object it published, so it would never
// need the pack again.
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
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	baselineBefore := rw.Baseline()

	assertErrorCode(t, errOnly(reader.Pull(context.Background())), CodeStorageIntegrity)
	if got := rw.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the corrupt pack: %+v -> %+v", baselineBefore, got)
	}
	if _, err := os.Stat(filepath.Join(rw.Path(), "a.md")); err == nil {
		t.Fatal("corrupt remote data reached the visible files")
	}
}

// TestPullStalePackRestartSucceeds proves the stale-observation restart in
// architecture/pull.md: a pack that disappeared during cleanup discards
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
	ne := assertErrorCode(t, errOnly(nb.Pull(context.Background())), CodeInvalidRequest)
	if ne.Reason != ReasonInvalidContent || ne.Action != ActionEditFiles {
		t.Fatalf("reason/action = %s/%s, want %s/%s", ne.Reason, ne.Action, ReasonInvalidContent, ActionEditFiles)
	}
	if len(ne.Files) != 1 || ne.Files[0].Path != "bad.md" || ne.Files[0].Reason != FileReasonInvalidContent {
		t.Fatalf("files = %+v, want exactly [{bad.md INVALID_CONTENT}]", ne.Files)
	}
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
	ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "first")), CodeRecoveryFailure)
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
	// An edit made while P required recovery is discarded by the repair,
	// so the recovering call reports it instead of returning OK.
	writeLocal(t, reopened, map[string]string{"a.md": "edited while broken"})
	assertEntryRecovered(t, errOnly(nb2.Pull(context.Background())))
	if reopened.RecoveryRequired() {
		t.Fatal("entry recovery did not clear the recovery flag")
	}
	if gen := reopened.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation after entry recovery = %d, want 1", gen)
	}
	if got := readLocal(t, reopened, "a.md"); got != "v1" {
		t.Fatalf("L after entry recovery = %q, want the accepted content", got)
	}
	// The flag is clear, so the next call runs its own work.
	pullOK(t, nb2)
}

// assertEntryRecovered asserts the RECOVERY_FAILURE of a successful entry
// recovery: stage entry, remote acceptance unknown, resynchronized, PULL.
func assertEntryRecovered(t *testing.T, err error) {
	t.Helper()
	ne := assertErrorCode(t, err, CodeRecoveryFailure)
	if ne.Recovery == nil || ne.Recovery.Stage != stageEntry || ne.Recovery.RemoteAccepted != RemoteAcceptedUnknown || !ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want entry / unknown / resynchronized=true", ne.Recovery)
	}
	if ne.Action != ActionPull {
		t.Fatalf("action = %s, want PULL", ne.Action)
	}
	if !errors.Is(ne, errEntryRecovered) {
		t.Fatalf("cause = %v, want errEntryRecovered", ne.Cause)
	}
}

// TestPullResultStat proves the pull result: the accepted remote
// generation and the diffstat of the on-disk delta between the visible
// state the pull observed and the materialized result.
func TestPullResultStat(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"a.md": "a1\n", "b.md": "b1\n", "d.md": "d1\n"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b) // B at gen 1: L = {a1,b1,d1}

	writeLocal(t, aw, map[string]string{"a.md": "a2\n", "c.md": "c1\n"})
	removeLocal(t, aw, "d.md")
	commitOK(t, a, "remote advance")

	// B's pull of the advanced remote with no local edits: L before is
	// {a1,b1,d1}, L after is {a2,b1,c1}.
	res, err := b.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull() = %v", err)
	}
	if res.Generation != 2 {
		t.Fatalf("result generation = %d, want 2", res.Generation)
	}
	want := git.DiffStat{
		Files: []git.FileStat{
			{Path: "a.md", Insertions: 1, Deletions: 1},
			{Path: "c.md", Insertions: 1, Deletions: 0},
			{Path: "d.md", Insertions: 0, Deletions: 1},
		},
		Insertions: 2,
		Deletions:  2,
	}
	if !reflect.DeepEqual(res.Stat, want) {
		t.Fatalf("result stat = %+v, want %+v", res.Stat, want)
	}
	got := localSnapshot(t, bw)
	if wantL := map[string]string{"a.md": "a2\n", "b.md": "b1\n", "c.md": "c1\n"}; !reflect.DeepEqual(got, wantL) {
		t.Fatalf("L after pull = %v, want the materialized result %v", got, wantL)
	}
}

// TestPullDiffStatReadFailureMapsToIntegrity proves that a snapshot read
// failure while computing the pull change summary returns the zero result
// with the existing STORAGE_INTEGRITY path, before L or P changes: the
// summary is presentation-only and never mutates state.
func TestPullDiffStatReadFailureMapsToIntegrity(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"a.md": "a1\n", "b.md": "b1\n", "d.md": "d1\n"})
	pullOK(t, a)
	commitOK(t, a, "base")
	writeLocal(t, aw, map[string]string{"a.md": "a2\n", "c.md": "c1\n"})
	removeLocal(t, aw, "d.md")
	commitOK(t, a, "remote advance")

	// The pull's merge of base {a1,b1,d1}, local {a1,b-local,d1}, and
	// remote {a2,b1,c1} produces the brand-new tree {a2,b-local,c1}, which
	// nothing reads before the change summary. Failing its read therefore
	// fails exactly the summary read.
	merged := mergedTreeOf(t, map[string]string{"a.md": "a2\n", "b.md": "b-local\n", "c.md": "c1\n"})
	fail := &readFailEngine{fakeEngine: newFakeEngine(), failTree: merged}
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids, engine: fail})
	pullOK(t, b) // gen 1: the merged tree equals the remote tree, so the fail target is never read
	writeLocal(t, bw, map[string]string{"b.md": "b-local\n"})

	baselineBefore := bw.Baseline()
	before := localSnapshot(t, bw)
	res, err := b.Pull(context.Background())
	assertErrorCode(t, err, CodeStorageIntegrity)
	assertZeroResult(t, res)
	if got := localSnapshot(t, bw); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed pull: %v -> %v", before, got)
	}
	if got := bw.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed pull: %+v -> %+v", baselineBefore, got)
	}
}

// rendezvousStore proves download overlap: a pack read parks until a
// second pack read is concurrently active, then every read proceeds. The
// park has a short grace period, so a serial fetcher — which never has
// two reads in flight — records no overlap and fails the assertion
// instead of hanging the suite. Non-pack reads pass straight through.
type rendezvousStore struct {
	storage.ObjectStore
	mu      sync.Mutex
	active  int
	ready   chan struct{}
	overlap bool
}

func (s *rendezvousStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if !strings.HasPrefix(key, "packs/") {
		return s.ObjectStore.ReadObject(ctx, key)
	}
	s.mu.Lock()
	s.active++
	if s.active >= 2 && !s.overlap {
		s.overlap = true
		close(s.ready)
	}
	s.mu.Unlock()
	select {
	case <-s.ready:
	case <-time.After(500 * time.Millisecond):
	}
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return s.ObjectStore.ReadObject(ctx, key)
}

// TestPullDownloadsPacksConcurrently proves the import path's fetch
// fan-out: a manifest referencing several packs must hold more than one
// download in flight at once. Fetched serially, a long increment tail
// costs one storage round trip per generation, which dominates a cold
// pull's wall clock; the overlap is the contract that prevents it.
func TestPullDownloadsPacksConcurrently(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	writer, ww, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, ww, map[string]string{"a.md": "v1"})
	pullOK(t, writer)
	commitOK(t, writer, "first")
	writeLocal(t, ww, map[string]string{"a.md": "v2"})
	commitOK(t, writer, "second")
	writeLocal(t, ww, map[string]string{"a.md": "v3"})
	commitOK(t, writer, "third")

	// The manifest now references three packs: the generation-1 checkpoint
	// and two increments. A fresh reader has an empty cache, so its pull
	// downloads all three.
	gate := &rendezvousStore{ObjectStore: store, ready: make(chan struct{})}
	reader, rw, _ := newNotebook(t, nbConfig{store: gate, ids: ids})
	pullOK(t, reader)

	gate.mu.Lock()
	overlap := gate.overlap
	gate.mu.Unlock()
	if !overlap {
		t.Fatal("pack downloads never overlapped; the import path fetches serially")
	}
	if got := localSnapshot(t, rw); got["a.md"] != "v3" {
		t.Fatalf("L = %v, want the accepted head content", got)
	}
}

// TestPullCleanPullPolicyAddsNoBlobRead proves a pull that changed no
// protected path opens exactly the blobs the same pull opens with no policy
// configured. The merge takes the baseline tree as its base side and opens
// baseline blobs of its own accord, so the oracle is the difference the
// policy makes, not an absolute.
func TestPullCleanPullPolicyAddsNoBlobRead(t *testing.T) {
	pull := func(nb *Notebook) error { return errOnly(nb.Pull(context.Background())) }
	configured := policyAccess(t, []string{"docs"}, []string{"notes"}, pull)
	unconfigured := policyAccess(t, nil, nil, pull)
	if configured.blobReads != unconfigured.blobReads {
		t.Fatalf("configured pull read %d blobs, want the unconfigured pull's %d", configured.blobReads, unconfigured.blobReads)
	}
}

// TestPullCleanPullBuildsNoExtraTree proves the pin is skipped when nothing
// protected changed: the merge takes the local tree, so the configured pull
// writes no tree the unconfigured pull does not.
func TestPullCleanPullBuildsNoExtraTree(t *testing.T) {
	pull := func(nb *Notebook) error { return errOnly(nb.Pull(context.Background())) }
	configured := policyAccess(t, []string{"docs"}, []string{"notes"}, pull)
	unconfigured := policyAccess(t, nil, nil, pull)
	if configured.treeWrites != unconfigured.treeWrites {
		t.Fatalf("configured pull wrote %d trees, want the unconfigured pull's %d", configured.treeWrites, unconfigured.treeWrites)
	}
}

// TestPullFirstPullGuard proves the first-pull guard: against a non-empty
// remote a first pull proceeds only when every visible file is already in
// R with identical bytes, and otherwise refuses before any local change;
// a later pull is never guarded.
func TestPullFirstPullGuard(t *testing.T) {
	for _, tt := range []struct {
		name     string
		local    map[string]string
		readOnly []string
		writable []string
		want     map[string]string // the L a passing pull leaves
		files    []ErrorFile       // non-nil: the pull is refused naming these
		message  string
	}{
		{name: "empty", local: map[string]string{}},
		{name: "identical subset", local: map[string]string{"a.md": "alpha"}},
		{
			name: "unrelated file", local: map[string]string{"x.md": "mine"},
			files:   []ErrorFile{{Path: "x.md", Reason: FileReasonNotInNotebook}},
			message: "found files that are not in the notebook;",
		},
		{
			name: "same path other bytes", local: map[string]string{"a.md": "ALPHA"},
			files:   []ErrorFile{{Path: "a.md", Reason: FileReasonDiffersFromNotebook}},
			message: "found files that differ from the notebook;",
		},
		{
			name: "both kinds", local: map[string]string{"a.md": "ALPHA", "b.md": "beta", "x.md": "mine"},
			files: []ErrorFile{
				{Path: "a.md", Reason: FileReasonDiffersFromNotebook},
				{Path: "x.md", Reason: FileReasonNotInNotebook},
			},
			message: "found files that are not in the notebook or differ from it;",
		},
		{
			// A protected file R holds is restored from R, so it is not
			// compared.
			name: "protected file in the notebook with other bytes", readOnly: []string{"a.md"},
			local: map[string]string{"a.md": "ALPHA"},
		},
		{
			// A protected file R lacks would be deleted by the pull.
			name: "protected file not in the notebook", readOnly: []string{"ro"},
			local:   map[string]string{"a.md": "alpha", "ro/x.md": "local only"},
			files:   []ErrorFile{{Path: "ro/x.md", Reason: FileReasonNotInNotebook}},
			message: "found files that are not in the notebook;",
		},
		{
			name: "file outside the writable set not in the notebook", writable: []string{"w"},
			local:   map[string]string{"todo.md": "mine"},
			files:   []ErrorFile{{Path: "todo.md", Reason: FileReasonNotInNotebook}},
			message: "found files that are not in the notebook;",
		},
		{
			name: "unprotected file inside the writable set", writable: []string{"w"},
			local:   map[string]string{"w/x.md": "mine"},
			files:   []ErrorFile{{Path: "w/x.md", Reason: FileReasonNotInNotebook}},
			message: "found files that are not in the notebook;",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := fake.New("")
			ids := &testIDSource{}
			a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
			writeLocal(t, aw, map[string]string{"a.md": "alpha", "b.md": "beta"})
			pullOK(t, a)
			commitOK(t, a, "seed")

			b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids, readOnly: tt.readOnly, writable: tt.writable})
			writeLocal(t, bw, tt.local)
			res, err := b.Pull(context.Background())
			if tt.files == nil {
				if err != nil {
					t.Fatalf("Pull() = %v", err)
				}
				if got := localSnapshot(t, bw); got["a.md"] != "alpha" || got["b.md"] != "beta" || len(got) != 2 {
					t.Fatalf("L after the first pull = %v, want the notebook", got)
				}
				return
			}
			ne := assertErrorCode(t, err, CodeInvalidRequest)
			assertZeroResult(t, res)
			if ne.Reason != ReasonDirectoryNotEmpty || ne.Action != ActionFixInput {
				t.Fatalf("reason/action = %s/%s, want DIRECTORY_NOT_EMPTY/FIX_INPUT", ne.Reason, ne.Action)
			}
			if !reflect.DeepEqual(ne.Files, tt.files) {
				t.Fatalf("files = %+v, want %+v", ne.Files, tt.files)
			}
			if !strings.Contains(ne.Message, tt.message) {
				t.Fatalf("message = %q, want it to contain %q", ne.Message, tt.message)
			}
			if bw.Pulled() || bw.Baseline().RemoteGeneration != 0 {
				t.Fatal("a refused first pull changed P")
			}
			if got := localSnapshot(t, bw); !reflect.DeepEqual(got, tt.local) {
				t.Fatalf("L after the refusal = %v, want %v untouched", got, tt.local)
			}
		})
	}

	t.Run("seeding an empty remote refuses a protected file", func(t *testing.T) {
		b, bw, _ := newNotebook(t, nbConfig{store: fake.New(""), ids: &testIDSource{}, writable: []string{"notes"}})
		local := map[string]string{"notes/a.md": "seed", "todo.md": "mine"}
		writeLocal(t, bw, local)
		ne := assertErrorCode(t, errOnly(b.Pull(context.Background())), CodeInvalidRequest)
		if want := []ErrorFile{{Path: "todo.md", Reason: FileReasonNotInNotebook}}; ne.Reason != ReasonDirectoryNotEmpty || !reflect.DeepEqual(ne.Files, want) {
			t.Fatalf("refusal = %s %+v, want DIRECTORY_NOT_EMPTY naming %+v", ne.Reason, ne.Files, want)
		}
		if got := localSnapshot(t, bw); !reflect.DeepEqual(got, local) {
			t.Fatalf("L after the refusal = %v, want %v untouched", got, local)
		}
	})

	t.Run("seeding an empty remote and later pulls are not guarded", func(t *testing.T) {
		store := fake.New("")
		ids := &testIDSource{}
		a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
		writeLocal(t, aw, map[string]string{"a.md": "alpha"})
		pullOK(t, a) // the remote is empty: the directory seeds it
		commitOK(t, a, "seed")
		writeLocal(t, aw, map[string]string{"local.md": "not yet published"})
		pullOK(t, a) // not a first pull: local additions merge as usual
		if got := readLocal(t, aw, "local.md"); got != "not yet published" {
			t.Fatalf("local.md after a later pull = %q, want it kept", got)
		}
	})
}

// TestPullRepairsValidationFailureByFullReimport proves the repair inside
// loadRemote: when the head moved and an object below it has vanished from
// the private repository, importing the one missing pack leaves validation
// failing, so every pack is re-imported from verified bytes once and the
// pull succeeds with the object restored. The reader has deleted the file
// locally, so the scan never rewrites the blob from L.
func TestPullRepairsValidationFailureByFullReimport(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	pullOK(t, reader)

	repo, ok := rw.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("reader repository is %T, want the fake", rw.Repo())
	}
	blob, err := repo.WriteBlob([]byte("v1"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	removeLocal(t, rw, "a.md")
	delete(repo.data.blobs, blob)
	delete(repo.data.raw, blob)

	writeLocal(t, w, map[string]string{"b.md": "v2"})
	commitOK(t, nb, "second")
	getsBefore := store.Calls(fake.OpGet)

	pullOK(t, reader)
	if present, err := repo.HasObject(blob); err != nil || !present {
		t.Fatalf("blob after the repair: present=%v err=%v; want it re-imported", present, err)
	}
	if got, want := localSnapshot(t, rw), map[string]string{"b.md": "v2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("L after the repair = %v, want %v", got, want)
	}
	// Only the new increment was downloaded; the checkpoint came from the
	// reader's byte cache.
	if got := store.Calls(fake.OpGet) - getsBefore; got != 2 {
		t.Fatalf("GET calls = %d, want current + the new increment", got)
	}
}

// TestPullRepairsAncestorLossBelowReusedHead proves the reuse path cannot
// hide damage below the accepted head: with the checkpoint's head commit
// gone from the reader's repository while the accepted head and its tree
// survive, the next pull re-imports the checkpoint pack and the following
// commit, whose export walks that history, succeeds. Without the presence
// sweep every pull would report OK while every commit failed for good.
func TestPullRepairsAncestorLossBelowReusedHead(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"b.md": "v2"})
	commitOK(t, nb, "second")
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	pullOK(t, reader)

	m := readManifest(t, store)
	repo, ok := rw.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("reader repository is %T, want the fake", rw.Repo())
	}
	delete(repo.data.commits, m.Checkpoint.Head)
	delete(repo.data.raw, m.Checkpoint.Head)

	pullOK(t, reader)
	if present, err := repo.HasObject(m.Checkpoint.Head); err != nil || !present {
		t.Fatalf("checkpoint head after the pull: present=%v err=%v; want it re-imported", present, err)
	}
	writeLocal(t, rw, map[string]string{"c.md": "v3"})
	commitOK(t, reader, "third")
}

// readerIDBase starts a second notebook's deterministic publication IDs
// well past the writer's, so a reader that publishes can never reuse an ID
// the manifest already binds to another commit.
const readerIDBase = 1000

// breakBlobRead makes one blob of the reader's repository present but
// unreadable: HasObject answers from the raw map and still says yes, while
// ReadBlob fails. It is the damage class a presence check cannot see, and
// the one an engine reports for a corrupt object inside an intact pack.
func breakBlobRead(t *testing.T, w *workspace.Workspace, content string) (*fakeRepository, git.OID) {
	t.Helper()
	repo, ok := w.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("repository is %T, want the fake", w.Repo())
	}
	blob, err := repo.WriteBlob([]byte(content))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	delete(repo.data.blobs, blob)
	if present, err := repo.HasObject(blob); err != nil || !present {
		t.Fatalf("broken blob presence = %v, %v; want it still present", present, err)
	}
	return repo, blob
}

// TestCommitRepairsUnreadableObject proves the repair covers an object the
// store still lists but can no longer supply, which a presence check cannot
// see: the history walk proves blobs by presence, so only the snapshot read
// meets it. Deleting the file locally is what stops the scan from rewriting
// the object, and the commit's change summary still reads the accepted tree
// that holds it. Classifying that failure as content damage instead would
// leave every later commit broken for good (architecture/pull.md).
func TestCommitRepairsUnreadableObject(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "v1", "b.md": "v2"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	pullOK(t, reader)

	repo, blob := breakBlobRead(t, rw, "v1")
	removeLocal(t, rw, "a.md")

	res := commitOK(t, reader, "drop a")
	if res.Generation != 2 {
		t.Fatalf("generation = %d, want 2", res.Generation)
	}
	if data, err := repo.ReadBlob(blob); err != nil || string(data) != "v1" {
		t.Fatalf("blob after the commit = %q, %v; want it re-imported", data, err)
	}
	if got, want := localSnapshot(t, rw), map[string]string{"b.md": "v2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("L after the commit = %v, want %v", got, want)
	}
}

// TestCommitStrictRetryRepairsReusedState proves the commit retry. The
// accepted-head fast path checks every descriptor head and reads the head
// tree, so the damage it cannot see is an ancestor object no longer
// reachable from the head tree — here the root tree of the first generation,
// after the file it held was deleted. The export walks that history and
// fails; the commit then reloads strictly once, which validates, repairs,
// and publishes, without spending a CAS retry (architecture/pull.md).
func TestCommitStrictRetryRepairsReusedState(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	firstTree := readManifest(t, store).Checkpoint.Head

	writeLocal(t, w, map[string]string{"b.md": "v2"}) // a.md is gone from L
	commitOK(t, nb, "second")
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	pullOK(t, reader)

	repo, ok := rw.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("repository is %T, want the fake", rw.Repo())
	}
	root, err := repo.ReadCommit(firstTree)
	if err != nil {
		t.Fatalf("ReadCommit(first) = %v", err)
	}
	// The first generation's tree is an ancestor object the head tree does
	// not reach, so the fast path cannot see it go.
	delete(repo.data.trees, root.Tree)
	delete(repo.data.raw, root.Tree)

	writeLocal(t, rw, map[string]string{"b.md": "v2", "c.md": "v3"})
	res := commitOK(t, reader, "third")

	if res.Generation != 3 {
		t.Fatalf("generation = %d, want 3", res.Generation)
	}
	if present, err := repo.HasObject(root.Tree); err != nil || !present {
		t.Fatalf("ancestor tree after the commit: present=%v err=%v; want it re-imported", present, err)
	}
	// One strict reload, not a CAS retry: exactly one increment per commit.
	if m := readManifest(t, store); len(m.Increments) != 2 {
		t.Fatalf("increments = %d, want one per publication", len(m.Increments))
	}
}

// TestPullStrictRetryRepairsReusedState proves the pull retry, the mirror of
// the commit one: the merge needs the accepted tree object itself, which the
// scan cannot rewrite once the visible directory differs from it, so a tree
// the store can no longer supply fails the merge. One strict reload
// validates, re-imports and repairs, and the pull reports the accepted state
// (architecture/pull.md).
func TestPullStrictRetryRepairsReusedState(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": "v1", "b.md": "v2"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{n: readerIDBase}})
	pullOK(t, reader)

	repo, ok := rw.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("repository is %T, want the fake", rw.Repo())
	}
	head := rw.Baseline().Tree
	delete(repo.data.trees, head)
	if present, err := repo.HasObject(head); err != nil || !present {
		t.Fatalf("broken tree presence = %v, %v; want it still present", present, err)
	}
	// L now differs from the accepted tree, so the scan rebuilds another
	// tree and the merge has to read the accepted one.
	removeLocal(t, rw, "a.md")

	pullOK(t, reader)
	if _, err := repo.ReadTree(head); err != nil {
		t.Fatalf("accepted tree after the pull = %v; want it re-imported", err)
	}
	// The local deletion is an unpublished local change, so the merge keeps
	// it: the repair restores the object, never the caller's edit.
	if got, want := localSnapshot(t, rw), map[string]string{"b.md": "v2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("L after the pull = %v, want %v", got, want)
	}
}
