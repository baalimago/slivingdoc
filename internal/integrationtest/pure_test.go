package integrationtest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// pureCtx is the background context of the wrapper tests; the barrier tests
// build their own cancellable contexts.
func pureCtx() context.Context { return context.Background() }

// newPureStore returns the concurrency-safe in-memory store used as the base
// of every wrapper test. It is the same fake the storage contract suite
// uses, so the wrappers are proven against real conditional-write semantics
// rather than against a second, weaker double.
func newPureStore() *fake.Store { return fake.New("") }

// pureRead reads one object and returns its bytes and info, failing the test
// on any error. It must only be called from the test goroutine.
func pureRead(t *testing.T, s storage.ObjectStore, key string) (string, storage.ObjectInfo) {
	t.Helper()
	rc, info, err := s.ReadObject(pureCtx(), key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s body: %v", key, err)
	}
	return string(data), info
}

// pureSeed creates an object directly on the base store, bypassing every
// wrapper, and returns its ETag.
func pureSeed(t *testing.T, s *fake.Store, key, data string) storage.ETag {
	t.Helper()
	etag, err := s.CreateObject(pureCtx(), key, []byte(data))
	if err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
	return etag
}

// pureExists reports whether the base store holds the key.
func pureExists(t *testing.T, s *fake.Store, key string) bool {
	t.Helper()
	rc, _, err := s.ReadObject(pureCtx(), key)
	if err == nil {
		rc.Close()
		return true
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("existence probe of %s: %v", key, err)
	}
	return false
}

func waitUntil(fn func() bool) bool {
	for range 200 {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// pureAwait requires done to close within d. Every "passes immediately"
// assertion uses a deadline below barrierTimeout, so a barrier that was not
// really released fails the test instead of slowly succeeding.
func pureAwait(t *testing.T, done <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not finish within %s", what, d)
	}
}

// pureStillBlocked requires done to stay open, proving the barrier holds.
func pureStillBlocked(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s completed while its barrier was armed", what)
	case <-time.After(25 * time.Millisecond):
	}
}

// TestRecorderCounts proves that the recorder counts every operation kind
// and per-key occurrence.
func TestRecorderCounts(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	rec := NewRecorder(base)
	ctx := pureCtx()

	etag, err := rec.CreateObject(ctx, "current", []byte("m1"))
	if err != nil {
		t.Fatalf("CreateObject() = %v", err)
	}
	if _, err := rec.ReplaceObject(ctx, "current", etag, []byte("m2")); err != nil {
		t.Fatalf("ReplaceObject() = %v", err)
	}
	rc, _, err := rec.ReadObject(ctx, "current")
	if err != nil {
		t.Fatalf("ReadObject() = %v", err)
	}
	rc.Close()
	if err := rec.PutObject(ctx, "packs/increments/2-x.pack", strings.NewReader("p"), storage.Metadata{}); err != nil {
		t.Fatalf("PutObject() = %v", err)
	}
	if err := rec.ListObjects(ctx, "packs/", func(string) error { return nil }); err != nil {
		t.Fatalf("ListObjects() = %v", err)
	}
	if err := rec.DeleteObjects(ctx, []string{"packs/increments/2-x.pack"}); err != nil {
		t.Fatalf("DeleteObjects() = %v", err)
	}

	for op, want := range map[Op]int{
		OpCreate: 1, OpReplace: 1, OpGet: 1, OpPut: 1, OpList: 1, OpDelete: 1,
	} {
		if got := rec.Count(op); got != want {
			t.Fatalf("%s count = %d, want %d", op, got, want)
		}
	}
	if got := rec.CountKey(OpReplace, "current"); got != 1 {
		t.Fatalf("replace-on-current count = %d, want 1", got)
	}
	if got := rec.CountKey(OpReplace, "packs/increments/2-x.pack"); got != 0 {
		t.Fatalf("replace on an untouched key = %d, want 0", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "packs/"); got != 1 {
		t.Fatalf("put prefix count = %d, want 1", got)
	}
	if got := rec.CountKeyPrefix(OpPut, "other/"); got != 0 {
		t.Fatalf("put count under an unrelated prefix = %d, want 0", got)
	}
	snap := rec.Snapshot()
	if snap[OpGet] != 1 {
		t.Fatalf("snapshot get = %d, want 1", snap[OpGet])
	}
	snap[OpGet] = 99
	if rec.Count(OpGet) != 1 {
		t.Fatal("Snapshot() aliases the live counters")
	}
}

// TestRecorderDeleteCounts proves the two delete counters answer different
// questions: Count is batches, CountKey is named keys.
func TestRecorderDeleteCounts(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	rec := NewRecorder(base)
	ctx := pureCtx()
	for _, key := range []string{"packs/a", "packs/b", "packs/c"} {
		pureSeed(t, base, key, "x")
	}

	if err := rec.DeleteObjects(ctx, []string{"packs/a", "packs/b"}); err != nil {
		t.Fatalf("first delete batch = %v", err)
	}
	if err := rec.DeleteObjects(ctx, []string{"packs/b", "packs/c"}); err != nil {
		t.Fatalf("second delete batch = %v", err)
	}

	if got := rec.Count(OpDelete); got != 2 {
		t.Fatalf("delete batches = %d, want 2", got)
	}
	for key, want := range map[string]int{"packs/a": 1, "packs/b": 2, "packs/c": 1} {
		if got := rec.CountKey(OpDelete, key); got != want {
			t.Fatalf("delete count for %s = %d, want %d", key, got, want)
		}
	}
	if got := rec.CountKeyPrefix(OpDelete, "packs/"); got != 4 {
		t.Fatalf("delete key count under packs/ = %d, want 4", got)
	}
	// The batch is counted only by Count: no synthetic empty-key entry.
	if got := rec.CountKey(OpDelete, ""); got != 0 {
		t.Fatalf("delete count for the empty key = %d, want 0", got)
	}
	if got := rec.CountKey(OpDelete, "packs/missing"); got != 0 {
		t.Fatalf("delete count for an unnamed key = %d, want 0", got)
	}
	if got := base.ObjectCount(); got != 0 {
		t.Fatalf("objects left after the batches = %d, want 0", got)
	}
}

// TestRecorderPreservesResults proves the recorder is a pure observer: bytes,
// ETags, and errors cross it unchanged, and failed operations still count.
func TestRecorderPreservesResults(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	rec := NewRecorder(base)
	ctx := pureCtx()

	etag, err := rec.CreateObject(ctx, "current", []byte("v1"))
	if err != nil {
		t.Fatalf("CreateObject() = %v", err)
	}
	data, info := pureRead(t, base, "current")
	if data != "v1" {
		t.Fatalf("stored bytes = %q, want %q", data, "v1")
	}
	if info.ETag != etag {
		t.Fatalf("returned ETag = %q, want the base store token %q", etag, info.ETag)
	}

	if _, err := rec.CreateObject(ctx, "current", []byte("v2")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("second create = %v, want ErrPreconditionFailed", err)
	}
	if _, err := rec.ReplaceObject(ctx, "current", storage.ETag("stale"), []byte("v3")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale replace = %v, want ErrPreconditionFailed", err)
	}
	if got, _ := pureRead(t, base, "current"); got != "v1" {
		t.Fatalf("bytes after the rejected writes = %q, want %q", got, "v1")
	}
	if got := rec.Count(OpCreate); got != 2 {
		t.Fatalf("create count = %d, want 2 (failed operations count too)", got)
	}

	// A read streams the base bytes, and a listing error propagates.
	if got, _ := pureRead(t, rec, "current"); got != "v1" {
		t.Fatalf("recorded read = %q, want %q", got, "v1")
	}
	sentinel := errors.New("stop listing")
	var seen []string
	err = rec.ListObjects(ctx, "", func(key string) error {
		seen = append(seen, key)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ListObjects() = %v, want the callback error", err)
	}
	if !reflect.DeepEqual(seen, []string{"current"}) {
		t.Fatalf("listed keys = %v, want [current]", seen)
	}
}

// TestFaultStoreFailNextIsOneShot proves the injection changes an outcome
// that would otherwise succeed, does not reach the base store, and applies to
// exactly one call.
func TestFaultStoreFailNextIsOneShot(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()
	etag := pureSeed(t, base, "current", "v1")

	// Baseline: the un-injected replace succeeds, so the assertions below
	// cannot be satisfied by the store's own precondition semantics.
	etag, err := f.ReplaceObject(ctx, "current", etag, []byte("v2"))
	if err != nil {
		t.Fatalf("baseline replace = %v, want success", err)
	}

	f.FailNext(OpReplace, "current", storage.ErrPreconditionFailed)
	if _, err := f.ReplaceObject(ctx, "current", etag, []byte("v3")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("injected replace = %v, want ErrPreconditionFailed", err)
	}
	if got, _ := pureRead(t, base, "current"); got != "v2" {
		t.Fatalf("bytes after the injected replace = %q, want the write to have been rejected", got)
	}

	// The identical second call is unaffected: the injection was one-shot.
	newETag, err := f.ReplaceObject(ctx, "current", etag, []byte("v3"))
	if err != nil {
		t.Fatalf("replace after the one-shot failure = %v, want success", err)
	}
	if newETag == "" {
		t.Fatal("successful replace returned an empty ETag")
	}
	if got, _ := pureRead(t, base, "current"); got != "v3" {
		t.Fatalf("bytes after the second replace = %q, want %q", got, "v3")
	}
}

// TestFaultStoreFailAlways proves a permanent failure survives repetition on
// its exact key, leaves other keys alone, and is lifted by ClearFailures.
func TestFaultStoreFailAlways(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "current", "v1")
	pureSeed(t, base, "other", "o1")

	if got, _ := pureRead(t, f, "current"); got != "v1" {
		t.Fatalf("baseline read = %q, want %q", got, "v1")
	}

	f.FailAlways(OpGet, "current", storage.ErrTransport)
	for i := range 3 {
		if _, _, err := f.ReadObject(pureCtx(), "current"); !errors.Is(err, storage.ErrTransport) {
			t.Fatalf("permanent get %d = %v, want ErrTransport", i, err)
		}
	}
	if got, _ := pureRead(t, f, "other"); got != "o1" {
		t.Fatalf("read of an untargeted key = %q, want %q", got, "o1")
	}

	f.ClearFailures()
	if got, _ := pureRead(t, f, "current"); got != "v1" {
		t.Fatalf("read after ClearFailures = %q, want %q", got, "v1")
	}
}

// TestFaultStoreFailNextPrefix proves the prefix injection fires once, on the
// first matching key only, and never on a non-matching key.
func TestFaultStoreFailNextPrefix(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "packs/increments/1-a.pack", "a")
	pureSeed(t, base, "packs/increments/1-b.pack", "b")
	pureSeed(t, base, "current", "c")

	if got, _ := pureRead(t, f, "packs/increments/1-a.pack"); got != "a" {
		t.Fatalf("baseline prefixed read = %q, want %q", got, "a")
	}

	f.FailNextPrefix(OpGet, "packs/", storage.ErrTransport)
	// A non-matching key must neither fail nor consume the injection.
	if got, _ := pureRead(t, f, "current"); got != "c" {
		t.Fatalf("read outside the prefix = %q, want %q", got, "c")
	}
	if _, _, err := f.ReadObject(pureCtx(), "packs/increments/1-a.pack"); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("first prefixed read = %v, want ErrTransport", err)
	}
	// One-shot across the whole prefix, not per key.
	if got, _ := pureRead(t, f, "packs/increments/1-b.pack"); got != "b" {
		t.Fatalf("second prefixed read = %q, want %q", got, "b")
	}
	if got, _ := pureRead(t, f, "packs/increments/1-a.pack"); got != "a" {
		t.Fatalf("repeat of the failed read = %q, want %q", got, "a")
	}
}

// TestFaultStoreFailAlwaysPrefix proves the permanent prefix injection covers
// every matching key until ClearFailures lifts it.
func TestFaultStoreFailAlwaysPrefix(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()

	put := func(key string) error {
		return f.PutObject(ctx, key, strings.NewReader("p"), storage.Metadata{})
	}
	if err := put("packs/baseline.pack"); err != nil {
		t.Fatalf("baseline put = %v, want success", err)
	}

	f.FailAlwaysPrefix(OpPut, "packs/", storage.ErrTransport)
	for _, key := range []string{"packs/x.pack", "packs/y.pack", "packs/x.pack"} {
		if err := put(key); !errors.Is(err, storage.ErrTransport) {
			t.Fatalf("put %s = %v, want ErrTransport", key, err)
		}
		if pureExists(t, base, key) {
			t.Fatalf("put %s reached the base store despite the injection", key)
		}
	}
	if err := put("current"); err != nil {
		t.Fatalf("put outside the prefix = %v, want success", err)
	}

	f.ClearFailures()
	if err := put("packs/x.pack"); err != nil {
		t.Fatalf("put after ClearFailures = %v, want success", err)
	}
	if !pureExists(t, base, "packs/x.pack") {
		t.Fatal("put after ClearFailures did not reach the base store")
	}
}

// TestFaultStoreClearFailures proves ClearFailures lifts every injection
// class at once — including the ones a scenario arms and never consumes —
// while leaving barriers armed.
func TestFaultStoreClearFailures(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()
	etag := pureSeed(t, base, "current", "v1")

	f.FailNext(OpGet, "current", storage.ErrTransport)
	f.FailNextPrefix(OpGet, "packs/", storage.ErrTransport)
	f.FailAlways(OpList, "packs/", storage.ErrTransport)
	f.FailAlwaysPrefix(OpPut, "packs/", storage.ErrTransport)
	f.AmbiguousNext(OpReplace, "current")
	f.AmbiguousNextOp(OpCreate)
	f.CorruptRead("current")
	f.UnprovableNext("current")
	f.FailDeletes(storage.ErrTransport)
	f.BlockNext(OpGet, "blocked")

	f.ClearFailures()

	if got, _ := pureRead(t, f, "current"); got != "v1" {
		t.Fatalf("read after ClearFailures = %q, want the uncorrupted bytes", got)
	}
	if err := f.ListObjects(ctx, "packs/", func(string) error { return nil }); err != nil {
		t.Fatalf("list after ClearFailures = %v", err)
	}
	if err := f.PutObject(ctx, "packs/x.pack", strings.NewReader("p"), storage.Metadata{}); err != nil {
		t.Fatalf("put after ClearFailures = %v", err)
	}
	if _, err := f.CreateObject(ctx, "fresh", []byte("n")); err != nil {
		t.Fatalf("create after ClearFailures = %v, want no lingering ambiguity", err)
	}
	if _, err := f.ReplaceObject(ctx, "current", etag, []byte("v2")); err != nil {
		t.Fatalf("replace after ClearFailures = %v, want no lingering ambiguity", err)
	}
	if err := f.DeleteObjects(ctx, []string{"fresh"}); err != nil {
		t.Fatalf("delete after ClearFailures = %v", err)
	}

	// Barriers are deliberately untouched, so a scenario can clear failures
	// while a coordinated operation is still parked.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if rc, _, err := f.ReadObject(ctx, "blocked"); err == nil {
			rc.Close()
		}
	}()
	if !waitUntil(func() bool { return f.Waiting(OpGet, "blocked") }) {
		t.Fatal("ClearFailures released an armed barrier")
	}
	f.Release(OpGet, "blocked")
	pureAwait(t, done, 2*time.Second, "released read")
}

// TestFaultStoreAmbiguousNext proves the accept-then-error write lands, hides
// its ETag, and is one-shot — on the exact key and on the key-agnostic form
// used for pack uploads.
func TestFaultStoreAmbiguousNext(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()
	etag := pureSeed(t, base, "current", "v1")

	f.AmbiguousNext(OpReplace, "current")
	newETag, err := f.ReplaceObject(ctx, "current", etag, []byte("landed"))
	if !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("ambiguous replace = %v, want ErrTransport", err)
	}
	// The caller must learn nothing it could use to resolve the ambiguity.
	if newETag != "" {
		t.Fatalf("ambiguous replace returned ETag %q, want the empty token", newETag)
	}
	landed, info := pureRead(t, base, "current")
	if landed != "landed" {
		t.Fatalf("bytes after the ambiguous replace = %q, want the mutation to have landed", landed)
	}

	// One-shot: the retry with the accepted ETag reports its success.
	if _, err := f.ReplaceObject(ctx, "current", info.ETag, []byte("clean")); err != nil {
		t.Fatalf("replace after the ambiguity = %v, want success", err)
	}

	// The exact-key injection must not fire on another key.
	f.AmbiguousNext(OpPut, "packs/named.pack")
	if err := f.PutObject(ctx, "packs/other.pack", strings.NewReader("p"), storage.Metadata{}); err != nil {
		t.Fatalf("put on an untargeted key = %v, want success", err)
	}
	if err := f.PutObject(ctx, "packs/named.pack", strings.NewReader("p"), storage.Metadata{}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("put on the targeted key = %v, want ErrTransport", err)
	}
}

// TestFaultStoreAmbiguousNextOp proves the key-agnostic ambiguity fires on
// the first operation of the kind whatever its key, exactly once. Pack keys
// embed a UUID generated inside the notebook, so a scenario cannot name them.
func TestFaultStoreAmbiguousNextOp(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()

	if err := f.PutObject(ctx, "packs/baseline.pack", strings.NewReader("b"), storage.Metadata{}); err != nil {
		t.Fatalf("baseline put = %v, want success", err)
	}

	f.AmbiguousNextOp(OpPut)
	unnamed := "packs/increments/3-9f2c.pack"
	if err := f.PutObject(ctx, unnamed, strings.NewReader("p"), storage.Metadata{}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("ambiguous put = %v, want ErrTransport", err)
	}
	if got, _ := pureRead(t, base, unnamed); got != "p" {
		t.Fatalf("bytes after the ambiguous put = %q, want the upload to have landed", got)
	}
	if err := f.PutObject(ctx, "packs/increments/4-1a7b.pack", strings.NewReader("q"), storage.Metadata{}); err != nil {
		t.Fatalf("put after the one-shot ambiguity = %v, want success", err)
	}

	// A create ambiguity hides the ETag of an object that does exist.
	f.AmbiguousNextOp(OpCreate)
	etag, err := f.CreateObject(ctx, "checkpoints/7.pack", []byte("c"))
	if !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("ambiguous create = %v, want ErrTransport", err)
	}
	if etag != "" {
		t.Fatalf("ambiguous create returned ETag %q, want the empty token", etag)
	}
	if !pureExists(t, base, "checkpoints/7.pack") {
		t.Fatal("ambiguous create did not land")
	}
}

// TestFaultStoreUnprovableNext proves the publication cannot prove its own
// acceptance: the replace lands but reports transport, and the read that would
// settle the question fails once (architecture section 11.3).
func TestFaultStoreUnprovableNext(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()
	etag := pureSeed(t, base, "current", "gen-1")

	f.UnprovableNext("current")
	if _, err := f.ReplaceObject(ctx, "current", etag, []byte("gen-2")); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("unprovable replace = %v, want ErrTransport", err)
	}
	// Read through the base store: the injected read failure is what makes
	// the acceptance unprovable to the caller, not to the assertion.
	if got, _ := pureRead(t, base, "current"); got != "gen-2" {
		t.Fatalf("bytes after the unprovable replace = %q, want the mutation to have landed", got)
	}
	if _, _, err := f.ReadObject(ctx, "current"); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("read after the unprovable replace = %v, want ErrTransport", err)
	}
	// Only the immediately following read is blinded.
	if got, _ := pureRead(t, f, "current"); got != "gen-2" {
		t.Fatalf("second read = %q, want %q", got, "gen-2")
	}
}

// TestFaultStoreCorruptRead proves reads return length-preserving garbage
// until restored, so a reader must reject the object on its recorded digest.
func TestFaultStoreCorruptRead(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "packs/1.pack", "payload")

	if got, _ := pureRead(t, f, "packs/1.pack"); got != "payload" {
		t.Fatalf("baseline read = %q, want %q", got, "payload")
	}

	f.CorruptRead("packs/1.pack")
	got, _ := pureRead(t, f, "packs/1.pack")
	if got == "payload" {
		t.Fatal("corrupt read returned the original bytes")
	}
	if len(got) != len("payload") {
		t.Fatalf("corrupt read length = %d, want %d", len(got), len("payload"))
	}

	f.RestoreRead("packs/1.pack")
	if got, _ := pureRead(t, f, "packs/1.pack"); got != "payload" {
		t.Fatalf("restored read = %q, want %q", got, "payload")
	}
}

// TestFaultStoreDeleteInjections proves both delete injections: the blanket
// FailDeletes and the keyed one-shot that targets a single object inside a
// batch. Neither may reach the base store.
func TestFaultStoreDeleteInjections(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()
	for _, key := range []string{"packs/a", "packs/b", "packs/c"} {
		pureSeed(t, base, key, "x")
	}

	f.FailDeletes(storage.ErrTransport)
	if err := f.DeleteObjects(ctx, []string{"packs/a"}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("blanket delete failure = %v, want ErrTransport", err)
	}
	if !pureExists(t, base, "packs/a") {
		t.Fatal("the failed delete still removed the object")
	}
	f.ClearFailures()

	// Keyed one-shot: the batch fails because of one named key.
	f.FailNext(OpDelete, "packs/b", storage.ErrTransport)
	if err := f.DeleteObjects(ctx, []string{"packs/a", "packs/b"}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("keyed delete failure = %v, want ErrTransport", err)
	}
	for _, key := range []string{"packs/a", "packs/b"} {
		if !pureExists(t, base, key) {
			t.Fatalf("%s was deleted despite the injected batch failure", key)
		}
	}
	// One-shot: the retry of the same batch succeeds.
	if err := f.DeleteObjects(ctx, []string{"packs/a", "packs/b"}); err != nil {
		t.Fatalf("retry of the batch = %v, want success", err)
	}
	if pureExists(t, base, "packs/a") || pureExists(t, base, "packs/b") {
		t.Fatal("the retried batch did not delete its keys")
	}

	// A batch that does not name the armed key passes untouched, and the
	// injection stays armed for the batch that does.
	f.FailNext(OpDelete, "packs/z", storage.ErrTransport)
	if err := f.DeleteObjects(ctx, []string{"packs/c"}); err != nil {
		t.Fatalf("unrelated batch = %v, want success", err)
	}
	if err := f.DeleteObjects(ctx, []string{"packs/z"}); !errors.Is(err, storage.ErrTransport) {
		t.Fatalf("batch naming the armed key = %v, want ErrTransport", err)
	}
}

// TestFaultStoreBlockNext proves the exact-key barrier really parks an
// operation and that Release lets it finish successfully.
func TestFaultStoreBlockNext(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "current", "v1")

	f.BlockNext(OpGet, "current")
	type result struct {
		data string
		err  error
	}
	res := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, _, err := f.ReadObject(pureCtx(), "current")
		if err != nil {
			res <- result{err: err}
			return
		}
		data, rerr := io.ReadAll(rc)
		rc.Close()
		res <- result{data: string(data), err: rerr}
	}()

	if !waitUntil(func() bool { return f.Waiting(OpGet, "current") }) {
		t.Fatal("the blocked get never reached the waiting state")
	}
	pureStillBlocked(t, done, "barriered get")

	f.Release(OpGet, "current")
	pureAwait(t, done, 2*time.Second, "released get")
	got := <-res
	if got.err != nil {
		t.Fatalf("released get = %v, want success", got.err)
	}
	if got.data != "v1" {
		t.Fatalf("released get bytes = %q, want %q", got.data, "v1")
	}
	if f.Waiting(OpGet, "current") {
		t.Fatal("the waiting registry still holds the released operation")
	}
	// The barrier was one-shot: the next get passes without a Release.
	if got, _ := pureRead(t, f, "current"); got != "v1" {
		t.Fatalf("get after the barrier = %q, want %q", got, "v1")
	}
}

// TestFaultStoreReleaseBeforeStart proves a barrier released before its
// operation starts lets that operation pass immediately, so a scenario cannot
// deadlock by winning a race with its own barrier.
func TestFaultStoreReleaseBeforeStart(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "current", "v1")

	f.BlockNext(OpGet, "current")
	f.Release(OpGet, "current")

	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, _, err := f.ReadObject(pureCtx(), "current")
		if err == nil {
			rc.Close()
		}
		errs <- err
	}()
	// Below barrierTimeout: a barrier left armed would fail this deadline
	// instead of quietly succeeding after the bound expires.
	pureAwait(t, done, 2*time.Second, "get after an early Release")
	if err := <-errs; err != nil {
		t.Fatalf("get after an early Release = %v, want success", err)
	}
}

// TestFaultStoreReleaseFreesReArmedWaiter proves Release frees an already
// parked operation even when the barrier was re-armed behind it. Without
// that, the re-arm would strand the waiter forever.
func TestFaultStoreReleaseFreesReArmedWaiter(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "current", "v1")

	f.BlockNext(OpGet, "current")
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, _, err := f.ReadObject(pureCtx(), "current")
		if err == nil {
			rc.Close()
		}
		errs <- err
	}()
	if !waitUntil(func() bool { return f.Waiting(OpGet, "current") }) {
		t.Fatal("the blocked get never reached the waiting state")
	}

	// Re-arm while the first operation is parked: the pending channel is now
	// a different one from the channel the waiter holds.
	f.BlockNext(OpGet, "current")
	pureStillBlocked(t, done, "re-armed barriered get")

	f.Release(OpGet, "current")
	pureAwait(t, done, 2*time.Second, "get released after a re-arm")
	if err := <-errs; err != nil {
		t.Fatalf("released get = %v, want success", err)
	}
}

// TestFaultStoreBlockPrefix proves the prefix barrier is multi-shot: it parks
// every matching operation at once, leaves non-matching keys free, and lets
// later matching operations pass immediately once released.
func TestFaultStoreBlockPrefix(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	ctx := pureCtx()

	f.BlockPrefix(OpPut, "packs/")

	var released atomic.Bool
	type outcome struct {
		key          string
		err          error
		afterRelease bool
	}
	results := make(chan outcome, 2)
	keys := []string{"packs/a.pack", "packs/b.pack"}
	var started atomic.Int32
	for _, key := range keys {
		go func() {
			started.Add(1)
			err := f.PutObject(ctx, key, strings.NewReader("p"), storage.Metadata{})
			results <- outcome{key: key, err: err, afterRelease: released.Load()}
		}()
	}

	if !waitUntil(func() bool { return started.Load() == 2 && f.WaitingPrefix(OpPut, "packs/") }) {
		t.Fatal("no put ever parked on the prefix barrier")
	}
	// A key outside the prefix must still flow while the barrier holds; this
	// also proves the barrier is not a store-wide lock.
	if err := f.PutObject(ctx, "current", strings.NewReader("c"), storage.Metadata{}); err != nil {
		t.Fatalf("put outside the prefix = %v, want success", err)
	}
	for _, key := range keys {
		if pureExists(t, base, key) {
			t.Fatalf("%s reached the base store while the prefix barrier was armed", key)
		}
	}

	released.Store(true)
	f.ReleasePrefix(OpPut, "packs/")
	for range keys {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatalf("released put %s = %v, want success", got.key, got.err)
			}
			// Both puts must have been parked, not just the one the poll saw.
			if !got.afterRelease {
				t.Fatalf("put %s completed before ReleasePrefix", got.key)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a released put never finished")
		}
	}
	for _, key := range keys {
		if !pureExists(t, base, key) {
			t.Fatalf("%s did not land after the release", key)
		}
	}
	if f.WaitingPrefix(OpPut, "packs/") {
		t.Fatal("the prefix waiter count survived the release")
	}

	// A later matching operation passes immediately: the released barrier is
	// gone, not merely open for the operations that were already parked.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = f.PutObject(ctx, "packs/c.pack", strings.NewReader("p"), storage.Metadata{})
	}()
	pureAwait(t, done, 2*time.Second, "put after ReleasePrefix")
}

// TestFaultStoreReleasePrefixBeforeStart proves a prefix barrier released
// before any matching operation starts never blocks one.
func TestFaultStoreReleasePrefixBeforeStart(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "packs/a.pack", "a")

	f.BlockPrefix(OpGet, "packs/")
	f.ReleasePrefix(OpGet, "packs/")
	if f.WaitingPrefix(OpGet, "packs/") {
		t.Fatal("WaitingPrefix reports a waiter with no operation running")
	}

	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, _, err := f.ReadObject(pureCtx(), "packs/a.pack")
		if err == nil {
			rc.Close()
		}
		errs <- err
	}()
	pureAwait(t, done, 2*time.Second, "get after an early ReleasePrefix")
	if err := <-errs; err != nil {
		t.Fatalf("get after an early ReleasePrefix = %v, want success", err)
	}
}

// TestFaultStoreDeleteBarrierOnKey proves the delete barrier can name one key
// inside a batch, which is how a scenario fences a cleanup on one object.
func TestFaultStoreDeleteBarrierOnKey(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "packs/a", "a")
	pureSeed(t, base, "packs/b", "b")

	f.BlockNext(OpDelete, "packs/b")
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		errs <- f.DeleteObjects(pureCtx(), []string{"packs/a", "packs/b"})
	}()
	if !waitUntil(func() bool { return f.Waiting(OpDelete, "packs/b") }) {
		t.Fatal("the delete batch never parked on the keyed barrier")
	}
	pureStillBlocked(t, done, "barriered delete batch")
	if !pureExists(t, base, "packs/a") {
		t.Fatal("the parked batch deleted a key before its barrier was released")
	}

	f.Release(OpDelete, "packs/b")
	pureAwait(t, done, 2*time.Second, "released delete batch")
	if err := <-errs; err != nil {
		t.Fatalf("released delete = %v, want success", err)
	}
	if pureExists(t, base, "packs/a") || pureExists(t, base, "packs/b") {
		t.Fatal("the released batch did not delete its keys")
	}
}

// TestFaultStoreBarrierFailsInsteadOfHanging proves an unreleased barrier
// surfaces as an error rather than a deadlock. The cancellation path is
// exercised directly because it is deterministic and instant; the wall-clock
// bound is asserted separately so it cannot silently grow into a hang.
func TestFaultStoreBarrierFailsInsteadOfHanging(t *testing.T) {
	t.Parallel()
	base := newPureStore()
	f := NewFaultStore(base)
	pureSeed(t, base, "current", "v1")

	f.BlockNext(OpGet, "current")
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, _, err := f.ReadObject(ctx, "current")
		if err == nil {
			rc.Close()
		}
		errs <- err
	}()
	if !waitUntil(func() bool { return f.Waiting(OpGet, "current") }) {
		t.Fatal("the blocked get never reached the waiting state")
	}
	cancel()
	pureAwait(t, done, 2*time.Second, "cancelled get")
	err := <-errs
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get = %v, want context.Canceled", err)
	}
	if f.Waiting(OpGet, "current") {
		t.Fatal("the cancelled operation stayed in the waiting registry")
	}

	// The bound itself must stay small enough to fail fast, and must never
	// masquerade as a protocol outcome: a harness barrier left armed is a
	// harness bug, not a plausible store error the code under test may retry.
	if barrierTimeout <= 0 || barrierTimeout > 10*time.Second {
		t.Fatalf("barrierTimeout = %s, want a small positive bound", barrierTimeout)
	}
	for _, sentinel := range []error{
		storage.ErrNotFound, storage.ErrPreconditionFailed,
		storage.ErrTransport, storage.ErrIncompatible, context.Canceled,
	} {
		if errors.Is(errBarrierTimeout, sentinel) {
			t.Fatalf("errBarrierTimeout matches the %v sentinel", sentinel)
		}
	}
}

// TestLogCaptureScoping proves the capture records, queries tool-call
// pairs, and keeps parallel captures disjoint — the mcpReqID scoping
// contract of the harness.
func TestLogCaptureScoping(t *testing.T) {
	t.Parallel()
	a := NewLogCapture()
	b := NewLogCapture()
	la := a.Logger()
	lb := b.Logger()

	la.Info("tool call started", "mcpReqID", "req-a-1", "tool", "notes_pull")
	la.Warn("tool call completed", "mcpReqID", "req-a-1", "tool", "notes_pull", "outcome", "error")
	la.Debug("unrelated record", "mcpReqID", "req-a-2")
	lb.Info("tool call started", "mcpReqID", "req-b-1", "tool", "notes_commit")

	if got := len(a.ToolCalls()); got != 2 {
		t.Fatalf("capture a tool-call records = %d, want 2", got)
	}
	if got := len(b.ToolCalls()); got != 1 {
		t.Fatalf("capture b tool-call records = %d, want 1", got)
	}
	if got := a.DistinctReqIDs(); !reflect.DeepEqual(got, []string{"req-a-1", "req-a-2"}) {
		t.Fatalf("distinct req ids = %v, want [req-a-1 req-a-2]", got)
	}
	if got := len(a.Warnings()); got != 1 {
		t.Fatalf("capture a warnings = %d, want 1", got)
	}
	if strings.Contains(a.String(), "req-b-1") {
		t.Fatal("capture a leaks capture b records")
	}
	if strings.Contains(b.String(), "req-a-1") {
		t.Fatal("capture b leaks capture a records")
	}
	// The warn record carries the correlated req id.
	if !strings.Contains(a.String(), "req-a-1") {
		t.Fatal("capture a lost its own req id")
	}
}

// pureOnlyRecord returns the single captured record.
func pureOnlyRecord(t *testing.T, c *LogCapture) Record {
	t.Helper()
	recs := c.Records()
	if len(recs) != 1 {
		t.Fatalf("captured %d records, want 1", len(recs))
	}
	return recs[0]
}

// pureWantAttrs compares the whole flattened attribute map, so a stray
// unprefixed or duplicated key fails the test.
func pureWantAttrs(t *testing.T, rec Record, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(rec.Attrs, want) {
		t.Fatalf("attrs = %#v, want %#v", rec.Attrs, want)
	}
}

// TestLogCaptureCallSiteAttrWins proves the slog precedence rule: an attr
// given at the call site overrides an attr of the same key bound by With.
func TestLogCaptureCallSiteAttrWins(t *testing.T) {
	t.Parallel()
	c := NewLogCapture()
	c.Logger().With("mcpReqID", "bound", "tool", "notes_pull").
		Info("tool call completed", "mcpReqID", "call-site")

	rec := pureOnlyRecord(t, c)
	pureWantAttrs(t, rec, map[string]any{
		"mcpReqID": "call-site",
		"tool":     "notes_pull",
	})
}

// TestLogCaptureGroups proves the group semantics the redaction and
// correlation scans depend on: bound attrs survive a later WithGroup with
// their unprefixed keys, call-site keys take the open group path, attrs bound
// inside a group keep the prefix they were bound with, and nested groups
// compose.
func TestLogCaptureGroups(t *testing.T) {
	t.Parallel()

	t.Run("bound attrs survive a later group", func(t *testing.T) {
		t.Parallel()
		c := NewLogCapture()
		c.Logger().With("mcpReqID", "req-1").WithGroup("commit").
			Info("stage", "stage", "publish")

		pureWantAttrs(t, pureOnlyRecord(t, c), map[string]any{
			"mcpReqID":     "req-1",
			"commit.stage": "publish",
		})
	})

	t.Run("attrs bound inside a group keep their prefix", func(t *testing.T) {
		t.Parallel()
		c := NewLogCapture()
		c.Logger().WithGroup("commit").With("generation", "7").
			Info("stage", "stage", "publish")

		pureWantAttrs(t, pureOnlyRecord(t, c), map[string]any{
			"commit.generation": "7",
			"commit.stage":      "publish",
		})
	})

	t.Run("nested groups compose", func(t *testing.T) {
		t.Parallel()
		c := NewLogCapture()
		c.Logger().With("mcpReqID", "req-1").
			WithGroup("commit").WithGroup("pack").
			Info("stage", "size", "42")

		pureWantAttrs(t, pureOnlyRecord(t, c), map[string]any{
			"mcpReqID":         "req-1",
			"commit.pack.size": "42",
		})
	})

	t.Run("an empty group name is a no-op", func(t *testing.T) {
		t.Parallel()
		c := NewLogCapture()
		c.Logger().WithGroup("").Info("stage", "stage", "publish")

		pureWantAttrs(t, pureOnlyRecord(t, c), map[string]any{"stage": "publish"})
	})
}

// TestLogCaptureGroupValues proves slog.Group values flatten to dotted keys,
// take the open group path, and that an anonymous group is inlined.
func TestLogCaptureGroupValues(t *testing.T) {
	t.Parallel()
	c := NewLogCapture()
	c.Logger().WithGroup("commit").Info("stage",
		slog.Group("pack", slog.String("kind", "increment"), slog.String("sha", "abc")),
		slog.Group("", slog.String("inlined", "yes")),
		slog.String("", "dropped"),
	)

	pureWantAttrs(t, pureOnlyRecord(t, c), map[string]any{
		"commit.pack.kind": "increment",
		"commit.pack.sha":  "abc",
		"commit.inlined":   "yes",
	})
}

// pureMasked hides its payload from every renderer except slog: only a
// handler that Resolves the LogValuer sees the secret. The redaction scans
// depend on that, because a leaked secret behind a LogValuer must fail the
// forbidden-substring assertions instead of hiding behind the mask.
type pureMasked struct{ secret string }

func (m pureMasked) String() string { return "***" }

func (m pureMasked) LogValue() slog.Value { return slog.StringValue(m.secret) }

// pureMaskedGroup resolves to a group, the other LogValuer shape a caller can
// hand to slog.
type pureMaskedGroup struct{ secret string }

func (m pureMaskedGroup) String() string { return "***" }

func (m pureMaskedGroup) LogValue() slog.Value {
	return slog.GroupValue(slog.String("token", m.secret))
}

// TestLogCaptureResolvesLogValuer proves values behind a LogValuer are
// resolved into the flat map and therefore visible to the redaction scans.
func TestLogCaptureResolvesLogValuer(t *testing.T) {
	t.Parallel()
	const secret = "AKIAsecret0123"
	c := NewLogCapture()
	c.Logger().WithGroup("aws").Info("probe",
		slog.Any("credential", pureMasked{secret: secret}),
		slog.Any("session", pureMaskedGroup{secret: secret}),
	)

	rec := pureOnlyRecord(t, c)
	pureWantAttrs(t, rec, map[string]any{
		"aws.credential":    secret,
		"aws.session.token": secret,
	})
	if got, ok := rec.Attrs["aws.credential"].(string); !ok || got != secret {
		t.Fatalf("credential attr = %#v, want the resolved string", rec.Attrs["aws.credential"])
	}
	// The rendered form is what the NoSubstring assertions scan.
	if !strings.Contains(c.String(), secret) {
		t.Fatalf("rendered capture = %q, want the resolved secret to be scannable", c.String())
	}
}
