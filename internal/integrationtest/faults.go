package integrationtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// barrierTimeout bounds every barrier wait. A scenario that fails an
// assertion before releasing its barrier otherwise strands the blocked
// operation inside the store, deadlocking the harness cleanup and turning
// one localized failure into a package-wide timeout. Legitimate holds in
// this suite are milliseconds, so a small bound fails fast and keeps the
// diagnostic local.
const barrierTimeout = 5 * time.Second

// errBarrierTimeout is returned by an operation whose barrier was never
// released. It is deliberately not a storage sentinel: it must surface as
// an unexpected harness failure, never as a plausible protocol outcome.
var errBarrierTimeout = fake.ErrBarrierTimeout

// faultStore wraps any ObjectStore and injects the failures the real S3
// backend cannot reliably produce: precondition failures, accept-then-error
// writes, unprovable CAS reads, missing and corrupt objects, delete
// failures, and op+key barriers. Every injection is keyed by protocol key,
// so the harness can target one pack or the current manifest without
// touching unrelated traffic. The failure, ambiguity, and barrier engine is
// the shared fake.Injector; this wrapper adds what only a wrapping store
// can do:
// corrupting real reads, failing whole delete batches, and the
// unprovable-CAS read-after-write injection.
type faultStore struct {
	base   storage.ObjectStore
	faults *fake.Injector

	mu sync.Mutex
	// corruptRead keys whose reads return corrupted bytes.
	corruptRead map[string]bool
	// corruptPrefix key prefixes whose reads return corrupted bytes.
	corruptPrefix []string
	// failReadAfterWrite keys whose next read fails after an ambiguous
	// conditional write landed (the unprovable-CAS injection).
	failReadAfterWrite map[string]error
	// deleteErr makes every DeleteObjects call fail.
	deleteErr error
}

// NewFaultStore wraps base with no injections.
func NewFaultStore(base storage.ObjectStore) *faultStore {
	return &faultStore{
		base:               base,
		faults:             fake.NewInjector(barrierTimeout),
		corruptRead:        map[string]bool{},
		failReadAfterWrite: map[string]error{},
	}
}

var _ storage.ObjectStore = (*faultStore)(nil)

// FailNext makes the next operation of the given kind on key fail once
// with err.
func (f *faultStore) FailNext(op Op, key string, err error) { f.faults.FailNext(op, key, err) }

// FailNextPrefix makes the next operation of the given kind whose key
// starts with prefix fail once with err. Pack keys embed a publication UUID
// generated inside the notebook, so a scenario can only name their
// namespace in advance.
func (f *faultStore) FailNextPrefix(op Op, prefix string, err error) {
	f.faults.FailNextPrefix(op, prefix, err)
}

// FailAlways makes every operation of the given kind on key fail with err.
func (f *faultStore) FailAlways(op Op, key string, err error) { f.faults.FailAlways(op, key, err) }

// FailAlwaysPrefix makes every operation of the given kind whose key starts
// with prefix fail with err, until ClearFailures.
func (f *faultStore) FailAlwaysPrefix(op Op, prefix string, err error) {
	f.faults.FailAlwaysPrefix(op, prefix, err)
}

// ClearFailures removes every injected failure, ambiguity, corruption, and
// delete failure, so a scenario can prove that a bounded background effort
// succeeds on a later trigger. Barriers are not touched.
func (f *faultStore) ClearFailures() {
	f.faults.ClearFailures()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.corruptRead = map[string]bool{}
	f.corruptPrefix = nil
	f.failReadAfterWrite = map[string]error{}
	f.deleteErr = nil
}

// AmbiguousNext makes the next operation of the given kind on key land but
// report a transport error, as a response lost after acceptance does.
// For PutObject, CreateObject, and ReplaceObject the mutation lands and
// the caller must resolve the ambiguity by reading state back.
func (f *faultStore) AmbiguousNext(op Op, key string) { f.faults.AmbiguousNext(op, key) }

// AmbiguousNextOp makes the next operation of the given kind on any key
// land but report a transport error. It is the pack-upload ambiguity: the
// unique pack key is generated inside the notebook, so the scenario cannot
// name it in advance.
func (f *faultStore) AmbiguousNextOp(op Op) { f.faults.AmbiguousNextOp(op) }

// UnprovableNext makes the next ReplaceObject on key land but report a
// transport error, and makes the immediately following read of key fail
// too, so a publication lookup cannot prove acceptance (architecture
// section 11.3, L733).
func (f *faultStore) UnprovableNext(key string) {
	f.faults.AmbiguousNext(OpReplace, key)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failReadAfterWrite[key] = storage.ErrTransport
}

// CorruptRead makes every read of key return corrupted bytes: the original
// bytes with every bit flipped. The length is preserved, so the reader must
// reject the object on its recorded SHA-256 rather than on its size.
func (f *faultStore) CorruptRead(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.corruptRead[key] = true
}

// CorruptReadPrefix makes every read of a key starting with prefix return
// corrupted bytes. Pack keys embed a publication UUID generated inside the
// notebook, so a scenario can only name their namespace in advance.
func (f *faultStore) CorruptReadPrefix(prefix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.corruptPrefix = append(f.corruptPrefix, prefix)
}

// RestoreRead stops corrupting reads of key.
func (f *faultStore) RestoreRead(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.corruptRead, key)
}

// FailDeletes makes every DeleteObjects call fail with err.
func (f *faultStore) FailDeletes(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteErr = err
}

// BlockNext makes the next operation of the given kind on key block until
// Release. The barrier is one-shot: it applies to exactly one operation.
func (f *faultStore) BlockNext(op Op, key string) { f.faults.BlockNext(op, key) }

// BlockPrefix makes every operation of the given kind whose key starts
// with prefix block until ReleasePrefix. The barrier is multi-shot: it
// applies to every matching operation, so the scenario can interleave
// independent writers with a checkpoint build.
func (f *faultStore) BlockPrefix(op Op, prefix string) { f.faults.BlockPrefix(op, prefix) }

// ReleasePrefix releases every waiting and future operation of the kind on
// the prefix. Releasing before any operation starts lets the operations
// pass immediately.
func (f *faultStore) ReleasePrefix(op Op, prefix string) { f.faults.ReleasePrefix(op, prefix) }

// WaitingPrefix reports whether an operation is currently parked on the
// prefix barrier.
func (f *faultStore) WaitingPrefix(op Op, prefix string) bool {
	return f.faults.WaitingPrefix(op, prefix)
}

// Release unblocks the barrier for the operation on key, whether the
// operation already consumed the barrier and is waiting or has not started
// yet. A barrier re-armed while an operation is parked releases both, so a
// waiter can never be stranded.
func (f *faultStore) Release(op Op, key string) { f.faults.Release(op, key) }

// Waiting reports whether an operation on key has consumed a barrier and
// is waiting for Release.
func (f *faultStore) Waiting(op Op, key string) bool { return f.faults.Waiting(op, key) }

// moveReadFailure moves the read-after-write injection into a one-shot get
// failure after an ambiguous conditional write landed. The injected write
// returns only after this arms, so the publication lookup that follows it
// sequentially always observes the failure.
func (f *faultStore) moveReadFailure(key string) {
	f.mu.Lock()
	rerr, ok := f.failReadAfterWrite[key]
	if ok {
		delete(f.failReadAfterWrite, key)
	}
	f.mu.Unlock()
	if ok {
		f.faults.FailNext(OpGet, key, rerr)
	}
}

func (f *faultStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if err := f.faults.WaitForBarrier(ctx, OpGet, key); err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	if err := f.faults.ConsumeFail(OpGet, key); err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	f.mu.Lock()
	corrupt := f.corruptRead[key]
	for _, prefix := range f.corruptPrefix {
		if strings.HasPrefix(key, prefix) {
			corrupt = true
			break
		}
	}
	f.mu.Unlock()
	rc, info, err := f.base.ReadObject(ctx, key)
	if err != nil {
		return nil, info, err
	}
	if !corrupt {
		return rc, info, nil
	}
	data, rerr := io.ReadAll(rc)
	rc.Close()
	if rerr != nil {
		return nil, info, fmt.Errorf("integrationtest: read corrupt source: %w", rerr)
	}
	for i := range data {
		data[i] ^= 0xFF
	}
	return io.NopCloser(bytes.NewReader(data)), info, nil
}

func (f *faultStore) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	if err := f.faults.WaitForBarrier(ctx, OpPut, key); err != nil {
		return err
	}
	if err := f.faults.ConsumeFail(OpPut, key); err != nil {
		return err
	}
	err := f.base.PutObject(ctx, key, r, meta)
	if err == nil && f.faults.ConsumeAmbiguous(OpPut, key) {
		return fmt.Errorf("integrationtest: put %s: response lost: %w", key, storage.ErrTransport)
	}
	return err
}

func (f *faultStore) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	if err := f.faults.WaitForBarrier(ctx, OpCreate, key); err != nil {
		return "", err
	}
	if err := f.faults.ConsumeFail(OpCreate, key); err != nil {
		return "", err
	}
	etag, err := f.base.CreateObject(ctx, key, data)
	if err == nil && f.faults.ConsumeAmbiguous(OpCreate, key) {
		f.moveReadFailure(key)
		return lostResponse("create", key)
	}
	return etag, err
}

func (f *faultStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	if err := f.faults.WaitForBarrier(ctx, OpReplace, key); err != nil {
		return "", err
	}
	if err := f.faults.ConsumeFail(OpReplace, key); err != nil {
		return "", err
	}
	newETag, err := f.base.ReplaceObject(ctx, key, etag, data)
	if err == nil && f.faults.ConsumeAmbiguous(OpReplace, key) {
		f.moveReadFailure(key)
		return lostResponse("replace", key)
	}
	return newETag, err
}

// lostResponse is the accept-then-error result: the mutation landed but the
// caller learned nothing. A real adapter returns no ETag on any error
// (internal/s3store), so the injection must not hand back the new ETag —
// otherwise a publication path can resolve its own ambiguity under the
// harness and take the recovery path only in production.
func lostResponse(op, key string) (storage.ETag, error) {
	return "", fmt.Errorf("integrationtest: %s %s: response lost: %w", op, key, storage.ErrTransport)
}

func (f *faultStore) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	if err := f.faults.WaitForBarrier(ctx, OpList, prefix); err != nil {
		return err
	}
	if err := f.faults.ConsumeFail(OpList, prefix); err != nil {
		return err
	}
	return f.base.ListObjects(ctx, prefix, fn)
}

// DeleteObjects applies barriers and injections to the batch as a whole
// (the empty key) and to every key it names, so a scenario can make the
// deletion of one specific object fail.
func (f *faultStore) DeleteObjects(ctx context.Context, keys []string) error {
	if err := f.faults.WaitForBarrier(ctx, OpDelete, ""); err != nil {
		return err
	}
	for _, key := range keys {
		if err := f.faults.WaitForBarrier(ctx, OpDelete, key); err != nil {
			return err
		}
	}
	if err := f.faults.ConsumeFail(OpDelete, ""); err != nil {
		return err
	}
	for _, key := range keys {
		if err := f.faults.ConsumeFail(OpDelete, key); err != nil {
			return err
		}
	}
	f.mu.Lock()
	deleteErr := f.deleteErr
	f.mu.Unlock()
	if deleteErr != nil {
		return fmt.Errorf("integrationtest: delete: %w", deleteErr)
	}
	return f.base.DeleteObjects(ctx, keys)
}
