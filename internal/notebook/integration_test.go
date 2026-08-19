package notebook

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/tests3"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// The real-S3 notebook suite proves the complete publication order over
// real HTTP conditional writes with real libgit2: first publication,
// independent pull, and concurrent writers converging on one accepted
// state. It runs against the shared pinned S3-compatible backend supplied by
// make test (or a per-process fallback for direct go test); a missing Docker
// daemon fails the run with a diagnostic that names the unavailable
// dependency, and the CI integration job treats any skip as failure.

// TestMain terminates the shared S3-compatible container after the whole
// suite.
func TestMain(m *testing.M) {
	code := m.Run()
	tests3.Terminate()
	os.Exit(code)
}

// newTestStore builds one real s3store adapter on a fresh per-test prefix;
// tests share one store across every notebook so writers observe each
// other's publications.
func newTestStore(t *testing.T) *s3store.Store {
	t.Helper()
	suite := tests3.Ensure(t)
	prefix := suite.FreshPrefix("notebook")
	mc := suite.StoreConfig()
	st, err := s3store.New(context.Background(), s3store.Config{
		Bucket: tests3.Bucket, Prefix: prefix, Region: mc.Region,
		Endpoint: mc.Endpoint, AccessKey: mc.AccessKey, SecretKey: mc.SecretKey,
	})
	if err != nil {
		t.Fatalf("s3store.New(%q) = %v", prefix, err)
	}
	return st
}

// newTestNotebook builds a real workspace over real libgit2 and the given
// store, with any additional harness configuration applied.
func newTestNotebook(t *testing.T, store storage.ObjectStore, ids *testIDSource, cfg nbConfig) (*Notebook, *workspace.Workspace, workspace.Config) {
	t.Helper()
	cfg.store = store
	cfg.engine = openNativeEngine(t)
	cfg.ids = ids
	return newNotebook(t, cfg)
}

// TestNotebookPullSeesPublishedChange proves an independent pull over the
// real S3 backend sees every accepted commit result.
func TestNotebookPullSeesPublishedChange(t *testing.T) {
	store := newTestStore(t)
	ids := &testIDSource{}
	a, aw, _ := newTestNotebook(t, store, ids, nbConfig{})
	b, bw, _ := newTestNotebook(t, store, ids, nbConfig{})

	writeLocal(t, aw, map[string]string{"a.md": "s3 v1"})
	pullOK(t, a)
	commitOK(t, a, "first")

	pullOK(t, b)
	got := localSnapshot(t, bw)
	if got["a.md"] != "s3 v1" {
		t.Fatalf("L after pull = %v, want the published file", got)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}

	writeLocal(t, bw, map[string]string{"b.md": "from b"})
	commitOK(t, b, "second")
	pullOK(t, a)
	got = localSnapshot(t, aw)
	if got["a.md"] != "s3 v1" || got["b.md"] != "from b" {
		t.Fatalf("L after second publication = %v, want both files", got)
	}
}

// TestNotebookTwoWriterRace runs concurrent commits from one shared
// baseline over real HTTP CAS: both writers succeed and the final state
// contains both changes in one linear accepted history.
func TestNotebookTwoWriterRace(t *testing.T) {
	store := newTestStore(t)
	ids := &testIDSource{}
	a, aw, _ := newTestNotebook(t, store, ids, nbConfig{})
	b, bw, _ := newTestNotebook(t, store, ids, nbConfig{})

	writeLocal(t, aw, map[string]string{"shared.md": "base", "a.md": "a"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)

	writeLocal(t, aw, map[string]string{"a.md": "A"})
	writeLocal(t, bw, map[string]string{"b.md": "B"})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, commit := range []func() error{
		func() error { return errOnly(a.Commit(context.Background(), "A change")) },
		func() error { return errOnly(b.Commit(context.Background(), "B change")) },
	} {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			<-start
			errs <- fn()
		}(commit)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Commit() = %v", err)
		}
	}

	// Both writers converge on one linear accepted state; a pull after the
	// race materializes the final state with both changes.
	pullOK(t, a)
	got := localSnapshot(t, aw)
	if got["shared.md"] != "base" || got["a.md"] != "A" || got["b.md"] != "B" {
		t.Fatalf("L after the race = %v, want both changes", got)
	}
	if gen := aw.Baseline().RemoteGeneration; gen != 3 {
		t.Fatalf("baseline generation after the race = %d, want 3", gen)
	}
}

// gatedStore blocks one ReadObject of a key until release, so a test can
// interleave a checkpoint and its cleanup between the reader's manifest
// read and its pack download over real HTTP.
type gatedStore struct {
	storage.ObjectStore
	key     string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if key == s.key {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.ObjectStore.ReadObject(ctx, key)
}

// TestNotebookCheckpointCleansAndReaderRestarts proves checkpoint
// publication, cleanup, and the stale-reader restart over real HTTP
// conditional writes and real libgit2: a checkpoint compacts the accepted
// tail, cleanup deletes the replaced generations, and a reader blocked on a
// pack that cleanup deletes discards its stale observation, rereads
// current, and reconstructs the final head.
func TestNotebookCheckpointCleansAndReaderRestarts(t *testing.T) {
	store := newTestStore(t)
	ids := &testIDSource{}
	// Writer A with retention 0: the next checkpoint may delete the
	// previous generation's physical packs.
	a, aw, _ := newTestNotebook(t, store, ids, nbConfig{checkpointPacks: 2, retained: 0, retainedSet: true})

	writeLocal(t, aw, map[string]string{"a.md": "v1"})
	pullOK(t, a)
	commitOK(t, a, "first") // gen 1: checkpoint pack (ids 1 and 2)
	writeLocal(t, aw, map[string]string{"a.md": "v2"})
	commitOK(t, a, "second") // gen 2: increment pack id 3

	// Reader C starts a pull through a gate on the generation-2 increment
	// pack: it observes the pre-checkpoint manifest and blocks on the pack
	// that the upcoming cleanup deletes.
	inc2 := "packs/increments/2-" + testUUIDv7(3).String() + ".pack"
	gate := &gatedStore{ObjectStore: store, key: inc2, entered: make(chan struct{}), release: make(chan struct{})}
	c, cw, _ := newTestNotebook(t, gate, ids, nbConfig{})
	pullDone := make(chan error, 1)
	go func() { pullDone <- errOnly(c.Pull(context.Background())) }()
	<-gate.entered

	// A's third commit publishes gen 3, compacts the two increments into a
	// checkpoint (gen 4), and cleanup (retention 0) deletes the old packs —
	// including the one C is blocked on.
	writeLocal(t, aw, map[string]string{"a.md": "v3"})
	commitOK(t, a, "third")

	for _, gone := range []string{
		"packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack",
		inc2,
		"packs/increments/3-" + testUUIDv7(4).String() + ".pack",
	} {
		if _, _, err := store.ReadObject(context.Background(), gone); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("old pack %s survived cleanup (err %v), want deleted", gone, err)
		}
	}
	cp4 := "packs/checkpoints/3-" + testUUIDv7(5).String() + ".pack"
	if _, _, err := store.ReadObject(context.Background(), cp4); err != nil {
		t.Fatalf("checkpoint pack %s missing: %v", cp4, err)
	}

	// Release C's blocked download: the pack is gone, so C discards its
	// stale observation, rereads current (the manifest moved), and
	// reconstructs the final head.
	close(gate.release)
	if err := <-pullDone; err != nil {
		t.Fatalf("C Pull() = %v", err)
	}

	m := readManifest(t, store)
	if m.Generation != 4 || m.Checkpoint.ThroughGeneration != 3 || len(m.Increments) != 0 {
		t.Fatalf("manifest = gen %d checkpoint through %d increments %d, want 4, 3, and an empty tail",
			m.Generation, m.Checkpoint.ThroughGeneration, len(m.Increments))
	}
	if got := localSnapshot(t, cw); got["a.md"] != "v3" {
		t.Fatalf("L after the stale restart = %v, want the final head v3", got)
	}
	if gen := cw.Baseline().RemoteGeneration; gen != 4 {
		t.Fatalf("C baseline generation = %d, want 4", gen)
	}
}
