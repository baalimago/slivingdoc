package integrationtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// barrierTimeout bounds every barrier wait. A scenario that fails an
// assertion before releasing its barrier would otherwise strand the blocked
// operation inside the store, deadlocking the harness cleanup and turning
// one localized failure into a package-wide timeout. Legitimate holds in
// this suite are milliseconds, so a small bound fails fast and keeps the
// diagnostic local.
const barrierTimeout = 5 * time.Second

// errBarrierTimeout is returned by an operation whose barrier was never
// released. It is deliberately not a storage sentinel: it must surface as
// an unexpected harness failure, never as a plausible protocol outcome.
var errBarrierTimeout = fmt.Errorf("integrationtest: barrier was never released")

// faultKey identifies one operation on one protocol key, key prefix, or
// LIST prefix.
type faultKey struct {
	op  Op
	key string
}

// faultStore wraps any ObjectStore and injects the failures MinIO cannot
// reliably produce: precondition failures, accept-then-error writes,
// unprovable CAS reads, missing and corrupt objects, delete failures, and
// op+key barriers. Every injection is keyed by protocol key, so the harness
// can target one pack or the current manifest without touching unrelated
// traffic.
type faultStore struct {
	base storage.ObjectStore

	mu sync.Mutex
	// failNext holds one-shot operation failures on an exact key.
	failNext map[faultKey]error
	// failNextPrefix holds one-shot operation failures on a key prefix, for
	// the pack keys whose publication UUID is generated inside the notebook.
	failNextPrefix map[faultKey]error
	// failAlways holds permanent operation failures on an exact key.
	failAlways map[faultKey]error
	// failAlwaysPrefix holds permanent operation failures on a key prefix.
	failAlwaysPrefix map[faultKey]error
	// ambiguous counts remaining accept-then-ErrTransport injections.
	ambiguous map[faultKey]int
	// ambiguousOp counts remaining accept-then-ErrTransport injections on
	// any key of the kind (the pack uploads whose unique key is generated
	// inside the notebook).
	ambiguousOp map[Op]int
	// corruptRead keys whose reads return corrupted bytes.
	corruptRead map[string]bool
	// corruptPrefix key prefixes whose reads return corrupted bytes.
	corruptPrefix []string
	// failReadAfterWrite keys whose next read fails after an ambiguous
	// conditional write landed (the unprovable-CAS injection).
	failReadAfterWrite map[string]error
	// deleteErr makes every DeleteObjects call fail.
	deleteErr error
	// block holds one-shot op+key barriers.
	block map[faultKey]chan struct{}
	// blockPrefix holds multi-shot op+prefix barriers: every operation of
	// the kind whose key starts with the prefix blocks until release.
	blockPrefix map[faultKey]chan struct{}
	// waiting tracks blocked exact-key operations for the poll helpers.
	waiting map[faultKey]chan struct{}
	// waitingPrefix counts operations currently parked on a prefix barrier.
	waitingPrefix map[faultKey]int
}

// NewFaultStore wraps base with no injections.
func NewFaultStore(base storage.ObjectStore) *faultStore {
	return &faultStore{
		base:               base,
		failNext:           map[faultKey]error{},
		failNextPrefix:     map[faultKey]error{},
		failAlways:         map[faultKey]error{},
		failAlwaysPrefix:   map[faultKey]error{},
		ambiguous:          map[faultKey]int{},
		ambiguousOp:        map[Op]int{},
		corruptRead:        map[string]bool{},
		failReadAfterWrite: map[string]error{},
		block:              map[faultKey]chan struct{}{},
		blockPrefix:        map[faultKey]chan struct{}{},
		waiting:            map[faultKey]chan struct{}{},
		waitingPrefix:      map[faultKey]int{},
	}
}

var _ storage.ObjectStore = (*faultStore)(nil)

// FailNext makes the next operation of the given kind on key fail once
// with err.
func (f *faultStore) FailNext(op Op, key string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext[faultKey{op: op, key: key}] = err
}

// FailNextPrefix makes the next operation of the given kind whose key
// starts with prefix fail once with err. Pack keys embed a publication UUID
// generated inside the notebook, so a scenario can only name their
// namespace in advance.
func (f *faultStore) FailNextPrefix(op Op, prefix string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNextPrefix[faultKey{op: op, key: prefix}] = err
}

// FailAlways makes every operation of the given kind on key fail with err.
func (f *faultStore) FailAlways(op Op, key string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAlways[faultKey{op: op, key: key}] = err
}

// FailAlwaysPrefix makes every operation of the given kind whose key starts
// with prefix fail with err, until ClearFailures.
func (f *faultStore) FailAlwaysPrefix(op Op, prefix string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAlwaysPrefix[faultKey{op: op, key: prefix}] = err
}

// ClearFailures removes every injected failure, ambiguity, corruption, and
// delete failure, so a scenario can prove that a bounded background effort
// succeeds on a later trigger. Barriers are not touched.
func (f *faultStore) ClearFailures() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = map[faultKey]error{}
	f.failNextPrefix = map[faultKey]error{}
	f.failAlways = map[faultKey]error{}
	f.failAlwaysPrefix = map[faultKey]error{}
	f.ambiguous = map[faultKey]int{}
	f.ambiguousOp = map[Op]int{}
	f.corruptRead = map[string]bool{}
	f.corruptPrefix = nil
	f.failReadAfterWrite = map[string]error{}
	f.deleteErr = nil
}

// AmbiguousNext makes the next operation of the given kind on key land but
// report a transport error, as a response lost after acceptance would.
// For PutObject, CreateObject, and ReplaceObject the mutation lands and
// the caller must resolve the ambiguity by reading state back.
func (f *faultStore) AmbiguousNext(op Op, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguous[faultKey{op: op, key: key}]++
}

// AmbiguousNextOp makes the next operation of the given kind on any key
// land but report a transport error. It is the pack-upload ambiguity: the
// unique pack key is generated inside the notebook, so the scenario cannot
// name it in advance.
func (f *faultStore) AmbiguousNextOp(op Op) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguousOp[op]++
}

// UnprovableNext makes the next ReplaceObject on key land but report a
// transport error, and makes the immediately following read of key fail
// too, so a publication lookup cannot prove acceptance (architecture
// section 11.3, L733).
func (f *faultStore) UnprovableNext(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguous[faultKey{op: OpReplace, key: key}]++
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
func (f *faultStore) BlockNext(op Op, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block[faultKey{op: op, key: key}] = make(chan struct{})
}

// BlockPrefix makes every operation of the given kind whose key starts
// with prefix block until ReleasePrefix. The barrier is multi-shot: it
// applies to every matching operation, so the scenario can interleave
// independent writers with a checkpoint build.
func (f *faultStore) BlockPrefix(op Op, prefix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockPrefix[faultKey{op: op, key: prefix}] = make(chan struct{})
}

// ReleasePrefix releases every waiting and future operation of the kind on
// the prefix. Releasing before any operation starts lets the operations
// pass immediately.
func (f *faultStore) ReleasePrefix(op Op, prefix string) {
	k := faultKey{op: op, key: prefix}
	f.mu.Lock()
	ch, ok := f.blockPrefix[k]
	delete(f.blockPrefix, k)
	f.mu.Unlock()
	if ok {
		close(ch)
	}
}

// WaitingPrefix reports whether an operation is currently parked on the
// prefix barrier.
func (f *faultStore) WaitingPrefix(op Op, prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitingPrefix[faultKey{op: op, key: prefix}] > 0
}

// Release unblocks the barrier for the operation on key, whether the
// operation already consumed the barrier and is waiting or has not started
// yet. A barrier re-armed while an operation is parked releases both, so a
// waiter can never be stranded.
func (f *faultStore) Release(op Op, key string) {
	k := faultKey{op: op, key: key}
	f.mu.Lock()
	pending, hasPending := f.block[k]
	delete(f.block, k)
	parked, hasParked := f.waiting[k]
	delete(f.waiting, k)
	f.mu.Unlock()
	if hasPending {
		close(pending)
	}
	if hasParked && parked != pending {
		close(parked)
	}
}

// Waiting reports whether an operation on key has consumed a barrier and
// is waiting for Release.
func (f *faultStore) Waiting(op Op, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.waiting[faultKey{op: op, key: key}]
	return ok
}

// waitForBarrier blocks until every matching barrier is released, the
// context is done, or the barrier bound expires. The exact-key barrier is
// consumed first and moves to the waiting registry so Release can release
// it; releasing before the operation starts leaves a closed barrier the
// operation passes immediately. Prefix barriers are then waited in sorted
// order, so overlapping prefixes are deterministic.
func (f *faultStore) waitForBarrier(ctx context.Context, op Op, key string) error {
	k := faultKey{op: op, key: key}
	f.mu.Lock()
	ch, exact := f.block[k]
	if exact {
		delete(f.block, k)
		f.waiting[k] = ch
	}
	f.mu.Unlock()
	if exact {
		err := awaitBarrier(ctx, ch)
		f.mu.Lock()
		delete(f.waiting, k)
		f.mu.Unlock()
		if err != nil {
			return err
		}
	}
	for _, pk := range f.matchingPrefixes(op, key) {
		f.mu.Lock()
		ch, ok := f.blockPrefix[pk]
		if ok {
			f.waitingPrefix[pk]++
		}
		f.mu.Unlock()
		if !ok {
			continue
		}
		err := awaitBarrier(ctx, ch)
		f.mu.Lock()
		if f.waitingPrefix[pk]--; f.waitingPrefix[pk] <= 0 {
			delete(f.waitingPrefix, pk)
		}
		f.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// matchingPrefixes returns every armed prefix barrier of the kind matching
// key, in sorted order.
func (f *faultStore) matchingPrefixes(op Op, key string) []faultKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []faultKey
	for pk := range f.blockPrefix {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			out = append(out, pk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// awaitBarrier waits for release, cancellation, or the barrier bound.
func awaitBarrier(ctx context.Context, ch chan struct{}) error {
	timer := time.NewTimer(barrierTimeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errBarrierTimeout
	}
}

// consumeFail reports and consumes a one-shot or permanent injected
// failure for the operation, matching exact keys before prefixes. It must
// be called with f.mu held.
func (f *faultStore) consumeFail(op Op, key string) error {
	k := faultKey{op: op, key: key}
	if err, ok := f.failNext[k]; ok {
		delete(f.failNext, k)
		return err
	}
	for _, pk := range sortedFaultKeys(f.failNextPrefix) {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			err := f.failNextPrefix[pk]
			delete(f.failNextPrefix, pk)
			return err
		}
	}
	if err, ok := f.failAlways[k]; ok {
		return err
	}
	for _, pk := range sortedFaultKeys(f.failAlwaysPrefix) {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			return f.failAlwaysPrefix[pk]
		}
	}
	return nil
}

// sortedFaultKeys returns the keys of a fault map in a deterministic order,
// so overlapping prefixes always resolve the same way.
func sortedFaultKeys(m map[faultKey]error) []faultKey {
	out := make([]faultKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].op != out[j].op {
			return out[i].op < out[j].op
		}
		return out[i].key < out[j].key
	})
	return out
}

// consumeAmbiguous reports whether the operation must land and then report
// a transport error. It must be called with f.mu held.
func (f *faultStore) consumeAmbiguous(op Op, key string) bool {
	k := faultKey{op: op, key: key}
	if f.ambiguous[k] > 0 {
		f.ambiguous[k]--
		return true
	}
	if f.ambiguousOp[op] > 0 {
		f.ambiguousOp[op]--
		return true
	}
	return false
}

// moveReadFailure moves the read-after-write injection into a one-shot get
// failure after an ambiguous conditional write landed. The caller holds
// f.mu.
func (f *faultStore) moveReadFailure(key string) {
	if rerr, ok := f.failReadAfterWrite[key]; ok {
		delete(f.failReadAfterWrite, key)
		f.failNext[faultKey{op: OpGet, key: key}] = rerr
	}
}

func (f *faultStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if err := f.waitForBarrier(ctx, OpGet, key); err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	f.mu.Lock()
	if err := f.consumeFail(OpGet, key); err != nil {
		f.mu.Unlock()
		return nil, storage.ObjectInfo{}, err
	}
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
	if err := f.waitForBarrier(ctx, OpPut, key); err != nil {
		return err
	}
	f.mu.Lock()
	if err := f.consumeFail(OpPut, key); err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	err := f.base.PutObject(ctx, key, r, meta)
	f.mu.Lock()
	amb := err == nil && f.consumeAmbiguous(OpPut, key)
	f.mu.Unlock()
	if amb {
		return fmt.Errorf("integrationtest: put %s: response lost: %w", key, storage.ErrTransport)
	}
	return err
}

func (f *faultStore) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	if err := f.waitForBarrier(ctx, OpCreate, key); err != nil {
		return "", err
	}
	f.mu.Lock()
	if err := f.consumeFail(OpCreate, key); err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.mu.Unlock()
	etag, err := f.base.CreateObject(ctx, key, data)
	f.mu.Lock()
	amb := err == nil && f.consumeAmbiguous(OpCreate, key)
	if amb {
		f.moveReadFailure(key)
	}
	f.mu.Unlock()
	if amb {
		return lostResponse("create", key)
	}
	return etag, err
}

func (f *faultStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	if err := f.waitForBarrier(ctx, OpReplace, key); err != nil {
		return "", err
	}
	f.mu.Lock()
	if err := f.consumeFail(OpReplace, key); err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.mu.Unlock()
	newETag, err := f.base.ReplaceObject(ctx, key, etag, data)
	f.mu.Lock()
	amb := err == nil && f.consumeAmbiguous(OpReplace, key)
	if amb {
		f.moveReadFailure(key)
	}
	f.mu.Unlock()
	if amb {
		return lostResponse("replace", key)
	}
	return newETag, err
}

// lostResponse is the accept-then-error result: the mutation landed but the
// caller learned nothing. A real adapter returns no ETag on any error
// (internal/s3store), so the injection must not hand back the new ETag —
// otherwise a publication path could resolve its own ambiguity under the
// harness and take the recovery path only in production.
func lostResponse(op, key string) (storage.ETag, error) {
	return "", fmt.Errorf("integrationtest: %s %s: response lost: %w", op, key, storage.ErrTransport)
}

func (f *faultStore) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	if err := f.waitForBarrier(ctx, OpList, prefix); err != nil {
		return err
	}
	f.mu.Lock()
	if err := f.consumeFail(OpList, prefix); err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	return f.base.ListObjects(ctx, prefix, fn)
}

// DeleteObjects applies barriers and injections to the batch as a whole
// (the empty key) and to every key it names, so a scenario can make the
// deletion of one specific object fail.
func (f *faultStore) DeleteObjects(ctx context.Context, keys []string) error {
	if err := f.waitForBarrier(ctx, OpDelete, ""); err != nil {
		return err
	}
	for _, key := range keys {
		if err := f.waitForBarrier(ctx, OpDelete, key); err != nil {
			return err
		}
	}
	f.mu.Lock()
	err := f.consumeFail(OpDelete, "")
	for _, key := range keys {
		if err != nil {
			break
		}
		err = f.consumeFail(OpDelete, key)
	}
	deleteErr := f.deleteErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if deleteErr != nil {
		return fmt.Errorf("integrationtest: delete: %w", deleteErr)
	}
	return f.base.DeleteObjects(ctx, keys)
}
