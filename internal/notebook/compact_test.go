package notebook

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
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
