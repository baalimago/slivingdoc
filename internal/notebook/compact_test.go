package notebook

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// A full space refuses the increment of a commit that deletes notes. The
// commit then publishes as a checkpoint of the whole state, keeps no
// previous generation, and removes every pack the manifest no longer
// references, so the space shrinks. A new reader sees the accepted state.
func TestCommitCompactsWhenSpaceIsFull(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"c.md": big + "more\n"})
	commitOK(t, nb, "second")
	before := readManifest(t, store)
	if len(before.Increments) != 1 {
		t.Fatalf("increments before = %d, want 1", len(before.Increments))
	}

	removeLocal(t, w, "b.md")
	removeLocal(t, w, "c.md")
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	commitOK(t, nb, "free space")

	m := readManifest(t, store)
	if m.Generation != before.Generation+1 {
		t.Fatalf("generation = %d, want %d", m.Generation, before.Generation+1)
	}
	if len(m.Increments) != 0 || len(m.Retained) != 0 {
		t.Fatalf("manifest keeps %d increments and %d retained, want none", len(m.Increments), len(m.Retained))
	}
	cp := m.Checkpoint
	if cp.Head != m.Head || cp.ThroughGeneration != before.Increments[0].Generation+1 {
		t.Fatalf("checkpoint = %+v, want the commit through generation %d", cp, before.Increments[0].Generation+1)
	}
	var active uint64 = before.Checkpoint.Size + before.Increments[0].Size
	if cp.Size >= active {
		t.Fatalf("checkpoint size %d, want less than the %d it replaced", cp.Size, active)
	}
	if got := store.ObjectCount(); got != 2 {
		t.Fatalf("objects after compaction = %d, want current and the new checkpoint", got)
	}

	reader, rw, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	pullOK(t, reader)
	if got := localSnapshot(t, rw); len(got) != 1 || got["a.md"] != "keep\n" {
		t.Fatalf("reader state = %v, want only a.md", got)
	}

	writeLocal(t, w, map[string]string{"d.md": "after\n"})
	commitOK(t, nb, "extends the checkpoint")
	if m := readManifest(t, store); len(m.Increments) != 1 || m.Increments[0].Parent != cp.Head {
		t.Fatalf("next commit = %+v, want one increment on the checkpoint", m.Increments)
	}
}

// Compaction must never lift the limit: when the whole state is not smaller
// than the chain it would replace, the commit stays refused as STORAGE_FULL
// and nothing is uploaded or published.
func TestCommitOnFullSpaceThatGrowsIsRefused(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "small\n"})
	commitOK(t, nb, "first")
	before := readManifest(t, store)
	objects := store.ObjectCount()
	putsBefore := store.Calls(fake.OpPut)

	writeLocal(t, w, map[string]string{"b.md": strings.Repeat("new data\n", 400)})
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	_, err := nb.Commit(context.Background(), "grow")
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonStorageFull {
		t.Fatalf("commit on a full space = %v, want STORAGE_FULL", err)
	}
	if m := readManifest(t, store); m.Generation != before.Generation {
		t.Fatalf("generation = %d, want %d unchanged", m.Generation, before.Generation)
	}
	if got := store.ObjectCount(); got != objects {
		t.Fatalf("objects = %d, want %d: nothing uploaded", got, objects)
	}
	if got := store.Calls(fake.OpPut); got != putsBefore+1 {
		t.Fatalf("uploads = %d, want only the refused increment", got-putsBefore)
	}
}

// A checkpoint refused as well (the space is past its compaction margin)
// is STORAGE_FULL, with nothing published.
func TestCommitCompactionRefusedIsStorageFull(t *testing.T) {
	store := fake.New("")
	full := &fullStore{ObjectStore: store}
	nb, w, _ := newNotebook(t, nbConfig{store: full, ids: &testIDSource{}})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")
	before := readManifest(t, store)

	removeLocal(t, w, "b.md")
	full.refuse = true
	_, err := nb.Commit(context.Background(), "free space")
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonStorageFull {
		t.Fatalf("commit with both uploads refused = %v, want STORAGE_FULL", err)
	}
	if m := readManifest(t, store); m.Generation != before.Generation {
		t.Fatalf("generation = %d, want %d unchanged", m.Generation, before.Generation)
	}
	if full.refused != 2 {
		t.Fatalf("refused uploads = %d, want the increment and the checkpoint", full.refused)
	}
}

// fullStore refuses every pack upload as a full space once refuse is set.
type fullStore struct {
	storage.ObjectStore
	refuse  bool
	refused int
}

func (s *fullStore) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	if s.refuse {
		s.refused++
		return storage.ErrQuotaExceeded
	}
	return s.ObjectStore.PutObject(ctx, key, r, meta)
}

// A compaction the manifest definitely did not accept deletes its own
// checkpoint pack: on a full space that orphan could block the next
// compaction. The commit then retries as usual.
func TestCompactionLostCASDeletesItsCheckpoint(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")

	removeLocal(t, w, "b.md")
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	store.FailNext(fake.OpReplace, storage.ErrPreconditionFailed)
	commitOK(t, nb, "free space")

	m := readManifest(t, store)
	var checkpoints int
	if err := store.ListObjects(context.Background(), "packs/checkpoints/", func(key string) error {
		checkpoints++
		if key != m.Checkpoint.Key.String() {
			t.Errorf("checkpoint %s is not referenced by the manifest", key)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 1 {
		t.Fatalf("checkpoint packs = %d, want 1", checkpoints)
	}
}

// Compaction also frees retained generations: they count toward what the
// space holds, and the compacted manifest keeps none.
func TestCompactionDropsRetainedGenerations(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}, checkpointPacks: 2})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"c.md": "c\n"})
	commitOK(t, nb, "second")
	writeLocal(t, w, map[string]string{"d.md": "d\n"})
	commitOK(t, nb, "third")
	if m := readManifest(t, store); len(m.Retained) != 1 {
		t.Fatalf("retained before = %d, want 1 after an opportunistic checkpoint", len(m.Retained))
	}

	removeLocal(t, w, "b.md")
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	commitOK(t, nb, "free space")
	if m := readManifest(t, store); len(m.Retained) != 0 || len(m.Increments) != 0 {
		t.Fatalf("manifest keeps %d retained and %d increments, want none", len(m.Retained), len(m.Increments))
	}
	if got := store.ObjectCount(); got != 2 {
		t.Fatalf("objects after compaction = %d, want current and the new checkpoint", got)
	}
}

// A lost manifest response whose follow-up read also fails leaves
// acceptance unknown: the compaction checkpoint may be referenced, so it
// must stay.
func TestCompactionUnknownAcceptanceKeepsItsCheckpoint(t *testing.T) {
	store := fake.New("")
	lossy := &lossyStore{ObjectStore: store}
	nb, w, _ := newNotebook(t, nbConfig{store: lossy, ids: &testIDSource{}})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")

	removeLocal(t, w, "b.md")
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	lossy.loseReplace = true
	if _, err := nb.Commit(context.Background(), "free space"); err == nil {
		t.Fatal("commit with an unprovable manifest write succeeded")
	}
	m := readManifest(t, store)
	if len(m.Retained) != 0 || m.Checkpoint.ThroughGeneration == 1 {
		t.Fatalf("manifest = %+v, want the compacted checkpoint accepted", m.Checkpoint)
	}
	if rc, _, err := store.ReadObject(context.Background(), m.Checkpoint.Key.String()); err != nil {
		t.Fatalf("accepted manifest references %s, which is gone: %v", m.Checkpoint.Key, err)
	} else {
		rc.Close()
	}
}

// The size check counts retained generations: they are freed too, so a
// checkpoint larger than the active chain alone still compacts.
func TestCompactionCountsRetainedGenerations(t *testing.T) {
	store := fake.New("")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}, checkpointPacks: 1})
	big := strings.Repeat("large note body\n", 400)

	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
	commitOK(t, nb, "first")
	removeLocal(t, w, "b.md")
	writeLocal(t, w, map[string]string{"c.md": "c\n"})
	commitOK(t, nb, "second")
	before := readManifest(t, store)
	if len(before.Retained) != 1 || len(before.Increments) != 0 {
		t.Fatalf("before = %d retained, %d increments; want one retained and an empty tail", len(before.Retained), len(before.Increments))
	}

	writeLocal(t, w, map[string]string{"d.md": "d\n"})
	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	commitOK(t, nb, "grows a little")
	m := readManifest(t, store)
	if m.Checkpoint.Size <= before.Checkpoint.Size {
		t.Fatalf("new checkpoint %d is not larger than the active one %d; the case needs it to be", m.Checkpoint.Size, before.Checkpoint.Size)
	}
	if len(m.Retained) != 0 || store.ObjectCount() != 2 {
		t.Fatalf("after = %d retained, %d objects; want none retained and two objects", len(m.Retained), store.ObjectCount())
	}
}

// lossyStore applies the next manifest replace, then reports a lost
// response and fails the next read, so acceptance cannot be proved.
type lossyStore struct {
	storage.ObjectStore
	loseReplace bool
	failRead    bool
}

func (s *lossyStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	next, err := s.ObjectStore.ReplaceObject(ctx, key, etag, data)
	if err == nil && s.loseReplace {
		s.loseReplace, s.failRead = false, true
		return "", storage.ErrTransport
	}
	return next, err
}

func (s *lossyStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if s.failRead {
		s.failRead = false
		return nil, storage.ObjectInfo{}, storage.ErrTransport
	}
	return s.ObjectStore.ReadObject(ctx, key)
}

// checkpointKeys lists the stored checkpoint packs.
func checkpointKeys(t *testing.T, store storage.ObjectStore) []string {
	t.Helper()
	var keys []string
	if err := store.ListObjects(context.Background(), "packs/checkpoints/", func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return keys
}

// A manifest write the store refused was definitely not accepted, whatever
// reason the refusal gives the caller, so the compaction deletes its own
// checkpoint pack: in the full space it would otherwise keep blocking the
// next compaction.
func TestCompactionRefusedManifestWriteDeletesItsCheckpoint(t *testing.T) {
	for _, tt := range []struct {
		sentinel error
		reason   Reason
	}{
		{storage.ErrRateLimited, ReasonRateLimited},
		{storage.ErrQuotaExceeded, ReasonStorageFull},
		{storage.ErrRequestLimit, ReasonRequestLimit},
		{storage.ErrAccessDenied, ReasonAccessDenied},
		{storage.ErrTooLarge, ReasonObjectTooLarge},
		{errors.New("server error"), ReasonManifestWrite},
	} {
		t.Run(string(tt.reason), func(t *testing.T) {
			store := fake.New("")
			nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
			big := strings.Repeat("large note body\n", 400)

			pullOK(t, nb)
			writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
			commitOK(t, nb, "first")
			before := readManifest(t, store)

			removeLocal(t, w, "b.md")
			store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
			store.FailNext(fake.OpReplace, &storage.Refusal{Err: tt.sentinel, Detail: "replace current"})
			ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "free space")), CodeStorageFailure)
			if ne.Reason != tt.reason {
				t.Fatalf("reason = %s, want %s", ne.Reason, tt.reason)
			}
			if m := readManifest(t, store); m.Generation != before.Generation {
				t.Fatalf("generation = %d, want %d unchanged", m.Generation, before.Generation)
			}
			keys := checkpointKeys(t, store)
			if len(keys) != 1 || keys[0] != before.Checkpoint.Key.String() {
				t.Fatalf("checkpoint packs = %v, want only the accepted %s", keys, before.Checkpoint.Key)
			}
		})
	}
}

// A compacting commit whose local acceptance fails after the manifest
// accepted it still frees the space: the replaced chain is unreferenced
// whatever happens locally, so its cleanup runs, and the call reports
// RECOVERY_FAILURE with the remote acceptance known.
func TestCompactionCleansUpWhenLocalAcceptFails(t *testing.T) {
	for _, tt := range []struct {
		name  string
		stage string
		cas   bool
	}{
		{"accept fails", stageCommit, false},
		{"failure after the CAS", stageCAS, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := fake.New("")
			triggered := errors.New("injected failure after acceptance")
			var fail bool
			once := func() error {
				if !fail {
					return nil
				}
				fail = false
				return triggered
			}
			cfg := nbConfig{store: store, ids: &testIDSource{}}
			if tt.cas {
				cfg.nbFail = &Failpoints{CAS: once}
			} else {
				cfg.wsFail = &workspace.Failpoints{Stage: once}
			}
			nb, w, _ := newNotebook(t, cfg)
			big := strings.Repeat("large note body\n", 400)

			pullOK(t, nb)
			writeLocal(t, w, map[string]string{"a.md": "keep\n", "b.md": big})
			commitOK(t, nb, "first")
			writeLocal(t, w, map[string]string{"c.md": big + "more\n"})
			commitOK(t, nb, "second")

			removeLocal(t, w, "b.md")
			removeLocal(t, w, "c.md")
			store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
			fail = true
			ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "free space")), CodeRecoveryFailure)
			if ne.Recovery == nil || ne.Recovery.Stage != tt.stage || ne.Recovery.RemoteAccepted != RemoteAcceptedYes {
				t.Fatalf("recovery report = %+v, want %s / yes", ne.Recovery, tt.stage)
			}

			m := readManifest(t, store)
			if len(m.Increments) != 0 || len(m.Retained) != 0 {
				t.Fatalf("manifest keeps %d increments and %d retained, want the compaction accepted", len(m.Increments), len(m.Retained))
			}
			if got := store.ObjectCount(); got != 2 {
				t.Fatalf("objects = %d, want current and the new checkpoint: the replaced chain must be cleaned up", got)
			}
			if got := localSnapshot(t, w); len(got) != 1 || got["a.md"] != "keep\n" {
				t.Fatalf("L after recovery = %v, want only a.md", got)
			}
		})
	}
}
