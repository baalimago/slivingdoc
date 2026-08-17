package fake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/contract"
)

var prefixSeq atomic.Uint64

// TestContractSuite runs the shared object-store contract suite against the
// fake. Every subtest gets a store with a unique configured prefix, exactly
// like the real S3 backend does.
func TestContractSuite(t *testing.T) {
	contract.Run(t, func(t *testing.T) storage.ObjectStore {
		return New(fmt.Sprintf("fake-%d", prefixSeq.Add(1)))
	})
}

// TestStaleETagRaceBarrier uses the one-shot barrier to make the stale-ETag
// race deterministic: a replace holds on the barrier while another writer
// bumps the ETag, then loses the conditional write when the barrier opens.
func TestStaleETagRaceBarrier(t *testing.T) {
	s := New("barrier-race")
	ctx := context.Background()
	key := storage.CurrentKey
	etag1, err := s.CreateObject(ctx, key, []byte("v1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	s.BlockNext(OpReplace, key)
	done := make(chan error, 1)
	go func() {
		_, err := s.ReplaceObject(ctx, key, etag1, []byte("v2"))
		done <- err
	}()
	waitUntilBlocked(t, s, OpReplace, key)

	// A concurrent accepted writer replaces the object before the blocked
	// writer proceeds: the blocked writer's ETag is now stale.
	s.Bump(key)
	s.Unblock(OpReplace, key)
	if err := <-done; !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("raced replace error = %v, want ErrPreconditionFailed", err)
	}

	rc, info, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := readAll(t, rc); got != "v1" {
		t.Fatalf("bytes after lost race = %q, want %q", got, "v1")
	}
	if info.ETag == etag1 {
		t.Fatal("etag did not change after the concurrent bump")
	}
}

// TestConcurrentCASOneWinner proves real conditional-write semantics: many
// writers holding the same observed ETag race to replace current, and
// exactly one wins.
func TestConcurrentCASOneWinner(t *testing.T) {
	s := New("cas-race")
	ctx := context.Background()
	key := storage.CurrentKey
	// The seed bytes must differ from every race payload: a winning write
	// whose bytes hash to the seed ETag lets a second writer win too.
	if _, err := s.CreateObject(ctx, key, []byte("seed")); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, info, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	etag := info.ETag

	const n = 16
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := s.ReplaceObject(ctx, key, etag, fmt.Appendf(nil, "v%d", i))
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	wins := 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, storage.ErrPreconditionFailed):
		default:
			t.Fatalf("unexpected replace error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
}

// TestAmbiguousUploadRecovery proves the read-back verification of
// architecture section 15: the response was lost after the bytes were
// accepted, so UploadUnique succeeds without a second write.
func TestAmbiguousUploadRecovery(t *testing.T) {
	s := New("ambiguous")
	ctx := context.Background()
	data := []byte("immutable pack bytes")
	key := testKey(t, 1)
	meta := metaFor(t, data, key)

	s.AmbiguousNext(OpPut, key.String())
	if err := storage.UploadUnique(ctx, s, key, bytes.NewReader(data), meta); err != nil {
		t.Fatalf("UploadUnique after lost response = %v, want nil", err)
	}
	if got := s.Calls(OpPut); got != 1 {
		t.Fatalf("PutObject calls = %d, want 1 (no rewrite after read-back)", got)
	}
	rc, info, err := s.ReadObject(ctx, key.String())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got := readAll(t, rc); got != string(data) {
		t.Fatalf("stored bytes = %q, want %q", got, data)
	}
	if info.Meta != meta {
		t.Fatalf("stored metadata = %+v, want %+v", info.Meta, meta)
	}
}

// TestAmbiguousCreateLandsButReportsTransport proves the conditional-create
// response-loss injection: the object is created, the caller receives a
// transport error, and the stored bytes are the new value.
func TestAmbiguousCreateLandsButReportsTransport(t *testing.T) {
	s := New("ambiguous-create")
	ctx := context.Background()
	key := storage.CurrentKey

	s.AmbiguousNext(OpCreate, key)
	_, err := s.CreateObject(ctx, key, []byte("v1"))
	if !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("CreateObject() error = %v, want ErrTransport", err)
	}
	rc, _, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read back = %v", err)
	}
	if got := readAll(t, rc); got != "v1" {
		t.Fatalf("stored bytes = %q, want %q", got, "v1")
	}
}

// TestAmbiguousReplaceLandsButReportsTransport proves the conditional
// replace response-loss injection: the replacement lands, the caller
// receives a transport error, and the stored bytes are the new value.
func TestAmbiguousReplaceLandsButReportsTransport(t *testing.T) {
	s := New("ambiguous-replace")
	ctx := context.Background()
	key := storage.CurrentKey
	etag1, err := s.CreateObject(ctx, key, []byte("v1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	s.AmbiguousNext(OpReplace, key)
	_, err = s.ReplaceObject(ctx, key, etag1, []byte("v2"))
	if !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("ReplaceObject() error = %v, want ErrTransport", err)
	}
	rc, _, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read back = %v", err)
	}
	if got := readAll(t, rc); got != "v2" {
		t.Fatalf("stored bytes = %q, want %q", got, "v2")
	}
}

// TestAmbiguousInjectionIsOneShot proves the response-loss injection is
// consumed by exactly one operation and does not leak into later calls.
func TestAmbiguousInjectionIsOneShot(t *testing.T) {
	s := New("ambiguous-once")
	ctx := context.Background()
	key := storage.CurrentKey

	s.AmbiguousNext(OpPut, key)
	if err := s.PutObject(ctx, key, bytes.NewReader([]byte("a")), storage.Metadata{}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("first put error = %v, want ErrTransport", err)
	}
	if err := s.PutObject(ctx, key, bytes.NewReader([]byte("b")), storage.Metadata{}); err != nil {
		t.Fatalf("second put error = %v, want nil", err)
	}
	rc, _, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read back = %v", err)
	}
	if got := readAll(t, rc); got != "b" {
		t.Fatalf("stored bytes = %q, want %q", got, "b")
	}
}

// TestAmbiguousUploadNeverLanded proves the absent-key branch: the request
// never reached the store, so UploadUnique reports a transport failure and
// leaves no object behind.
func TestAmbiguousUploadNeverLanded(t *testing.T) {
	s := New("never-landed")
	ctx := context.Background()
	data := []byte("pack bytes")
	key := testKey(t, 2)
	meta := metaFor(t, data, key)

	s.FailNext(OpPut, storage.ErrTransport)
	if err := storage.UploadUnique(ctx, s, key, bytes.NewReader(data), meta); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("UploadUnique error = %v, want ErrTransport", err)
	}
	if n := s.ObjectCount(); n != 0 {
		t.Fatalf("objects after never-landed upload = %d, want 0", n)
	}
}

// TestAmbiguousUploadCollisionNoOverwrite proves the integrity branch: a
// unique key that already holds different bytes is never overwritten and
// the collision is reported as a storage-integrity error.
func TestAmbiguousUploadCollisionNoOverwrite(t *testing.T) {
	s := New("collision")
	ctx := context.Background()
	key := testKey(t, 3)
	other := []byte("old proposal bytes")
	if err := s.PutObject(ctx, key.String(), bytes.NewReader(other), storage.Metadata{Kind: key.Kind, Generation: key.Generation}); err != nil {
		t.Fatalf("seed collision: %v", err)
	}

	data := []byte("new pack bytes")
	meta := metaFor(t, data, key)
	s.FailNext(OpPut, storage.ErrTransport)
	if err := storage.UploadUnique(ctx, s, key, bytes.NewReader(data), meta); !errors.Is(err, storage.ErrIntegrity) {
		t.Fatalf("UploadUnique error = %v, want ErrIntegrity", err)
	}
	rc, _, err := s.ReadObject(ctx, key.String())
	if err != nil {
		t.Fatalf("read after collision: %v", err)
	}
	if got := readAll(t, rc); got != string(other) {
		t.Fatalf("collision overwrote bytes to %q, want %q", got, other)
	}
}

// errReader yields its bytes and then fails with a non-EOF error, like a
// transport that dies after accepting part of a body.
type errReader struct {
	remaining []byte
	err       error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.remaining) == 0 {
		return 0, r.err
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}

// TestUploadStreamFailure proves that a source read failure mid-stream
// reports a transport failure and stores no object: no partial pack can
// become visible state.
func TestUploadStreamFailure(t *testing.T) {
	s := New("stream-failure")
	ctx := context.Background()
	key := testKey(t, 4)
	meta := metaFor(t, make([]byte, 64), key)
	failing := &errReader{remaining: []byte("partial body bytes"), err: errors.New("source died mid-stream")}

	if err := storage.UploadUnique(ctx, s, key, failing, meta); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("UploadUnique error = %v, want ErrTransport", err)
	}
	if n := s.ObjectCount(); n != 0 {
		t.Fatalf("objects after failed stream = %d, want 0", n)
	}
}

// TestTruncatedObjectDetected proves that a download that ends before the
// declared size is a storage-integrity error, never silently accepted.
func TestTruncatedObjectDetected(t *testing.T) {
	s := New("truncated")
	ctx := context.Background()
	data := []byte("complete pack bytes")
	key := testKey(t, 5)
	meta := metaFor(t, data, key)
	if err := s.PutObject(ctx, key.String(), bytes.NewReader(data), meta); err != nil {
		t.Fatalf("put: %v", err)
	}

	s.mu.Lock()
	o := s.objects[s.full(key.String())]
	o.data = o.data[:5]
	s.mu.Unlock()

	if err := storage.VerifyObject(ctx, s, key.String(), meta.SHA256, meta.Size); !errors.Is(err, storage.ErrIntegrity) {
		t.Fatalf("VerifyObject error = %v, want ErrIntegrity", err)
	}
}

// TestBumpInvalidatesStaleETag proves that Bump changes the concurrency
// token without touching the bytes: the old ETag loses the race, the new
// one replaces normally.
func TestBumpInvalidatesStaleETag(t *testing.T) {
	s := New("bump")
	ctx := context.Background()
	key := storage.CurrentKey
	etag1, err := s.CreateObject(ctx, key, []byte("v1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	s.Bump(key)
	if _, err := s.ReplaceObject(ctx, key, etag1, []byte("v2")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale replace after bump error = %v, want ErrPreconditionFailed", err)
	}
	rc, info, err := s.ReadObject(ctx, key)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := readAll(t, rc); got != "v1" {
		t.Fatalf("bytes after bump = %q, want %q", got, "v1")
	}
	if info.ETag == etag1 {
		t.Fatal("Bump did not change the etag")
	}
	if _, err := s.ReplaceObject(ctx, key, info.ETag, []byte("v2")); err != nil {
		t.Fatalf("replace with bumped etag: %v", err)
	}
}

// TestFailNextKeyConsumed proves that a key-scoped injected failure is
// one-shot and does not leak into later operations.
func TestFailNextKeyConsumed(t *testing.T) {
	s := New("fail-key")
	ctx := context.Background()
	key := storage.CurrentKey

	s.FailNextKey(OpGet, key, errors.New("injected get failure"))
	if _, _, err := s.ReadObject(ctx, key); err == nil {
		t.Fatal("first read succeeded, want the injected failure")
	}
	if _, _, err := s.ReadObject(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("second read error = %v, want ErrNotFound", err)
	}
}

// TestDeleteFailureLeavesState proves the cleanup failure contract: a
// denied delete leaves every object untouched.
func TestDeleteFailureLeavesState(t *testing.T) {
	s := New("delete-denied")
	ctx := context.Background()
	key := testKey(t, 6)
	meta := metaFor(t, []byte("x"), key)
	if err := s.PutObject(ctx, key.String(), bytes.NewReader([]byte("x")), meta); err != nil {
		t.Fatalf("put: %v", err)
	}

	s.FailNext(OpDelete, errors.New("fake: delete denied"))
	if err := s.DeleteObjects(ctx, []string{key.String()}); err == nil {
		t.Fatal("delete succeeded, want the injected failure")
	}
	if _, _, err := s.ReadObject(ctx, key.String()); err != nil {
		t.Fatalf("object was removed despite the denied delete: %v", err)
	}
}

// TestCallsCountsOperations proves the operation counter, which other tests
// use to assert that no hidden second write happened.
func TestCallsCountsOperations(t *testing.T) {
	s := New("calls")
	ctx := context.Background()
	key := storage.CurrentKey

	if _, err := s.CreateObject(ctx, key, []byte("v1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.CreateObject(ctx, key, []byte("v2")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("second create error = %v, want ErrPreconditionFailed", err)
	}
	if got := s.Calls(OpCreate); got != 2 {
		t.Fatalf("create calls = %d, want 2 (failed operations count too)", got)
	}
}

// TestConcurrentMixedWorkload exercises the store under the race detector
// with concurrent writers, readers, lists, and deletes on disjoint keys.
func TestConcurrentMixedWorkload(t *testing.T) {
	s := New("mixed")
	ctx := context.Background()
	// ops is even so each writer deletes exactly half of its keys.
	const writers, ops = 8, 24

	// Every goroutine owns its keys; no key is ever written by two
	// goroutines, so the only shared state is the store's internal map.
	keys := make([][]storage.Key, writers)
	for w := range writers {
		keys[w] = make([]storage.Key, ops)
		for i := range ops {
			keys[w][i] = testKey(t, uint64(w*ops+i+1))
		}
	}

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i, key := range keys[w] {
				data := fmt.Appendf(nil, "writer-%d-%d", w, i)
				meta := metaFor(t, data, key)
				if err := s.PutObject(ctx, key.String(), bytes.NewReader(data), meta); err != nil {
					t.Errorf("put %s: %v", key, err)
					return
				}
				if _, _, err := s.ReadObject(ctx, key.String()); err != nil {
					t.Errorf("read %s: %v", key, err)
					return
				}
				if i%2 == 0 {
					if err := s.DeleteObjects(ctx, []string{key.String()}); err != nil {
						t.Errorf("delete %s: %v", key, err)
						return
					}
				}
			}
			if err := s.ListObjects(ctx, "packs/", func(string) error { return nil }); err != nil {
				t.Errorf("list: %v", err)
			}
		}(w)
	}
	wg.Wait()

	if want := writers * ops / 2; s.ObjectCount() != want {
		t.Fatalf("objects = %d, want %d", s.ObjectCount(), want)
	}
}

// testKey returns a protocol increment key with a fresh UUIDv7.
func testKey(t *testing.T, generation uint64) storage.Key {
	t.Helper()
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7: %v", err)
	}
	return storage.Key{Kind: storage.KindIncrement, Generation: generation, ID: id}
}

func metaFor(t *testing.T, data []byte, key storage.Key) storage.Metadata {
	t.Helper()
	sum := sha256.Sum256(data)
	return storage.Metadata{
		SHA256:     storage.SHA256(sum),
		Size:       uint64(len(data)),
		Kind:       key.Kind,
		Generation: key.Generation,
	}
}

// waitUntilBlocked waits until the operation has consumed its one-shot
// barrier and is now blocked on it (present in the waiting registry).
func waitUntilBlocked(t *testing.T, s *Store, op Op, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !s.Waiting(op, key) {
		if time.Now().After(deadline) {
			t.Fatal("operation never blocked on the barrier")
		}
		time.Sleep(time.Millisecond)
	}
}

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return string(got)
}
