package fake

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrBarrierTimeout is returned by an operation whose barrier was never
// released. It is deliberately not a storage sentinel: it must surface
// as an unexpected harness failure, never as a plausible protocol
// outcome.
var ErrBarrierTimeout = errors.New("fake: barrier was never released")

// Injector is the fault-injection engine shared by the in-memory Store
// and the integration harness's wrapping store: one-shot and permanent
// failures on exact keys or key prefixes, accept-then-error ambiguity,
// and op+key barriers with a bounded wait. All methods are safe for
// concurrent use; the consume and wait methods take no caller lock.
type Injector struct {
	// timeout bounds every barrier wait. A test that fails an assertion
	// before releasing its barrier would otherwise strand the blocked
	// operation, deadlocking cleanup and turning one localized failure
	// into a package-wide timeout.
	timeout time.Duration

	mu               sync.Mutex
	failNext         map[opKey]error
	failNextPrefix   map[opKey]error
	failAlways       map[opKey]error
	failAlwaysPrefix map[opKey]error
	ambiguous        map[opKey]int
	ambiguousOp      map[Op]int
	block            map[opKey]chan struct{}
	blockPrefix      map[opKey]chan struct{}
	waiting          map[opKey]chan struct{}
	waitingPrefix    map[opKey]int
}

// NewInjector returns an engine with no armed injections. The timeout
// bounds every barrier wait and must be positive.
func NewInjector(timeout time.Duration) *Injector {
	return &Injector{
		timeout:          timeout,
		failNext:         map[opKey]error{},
		failNextPrefix:   map[opKey]error{},
		failAlways:       map[opKey]error{},
		failAlwaysPrefix: map[opKey]error{},
		ambiguous:        map[opKey]int{},
		ambiguousOp:      map[Op]int{},
		block:            map[opKey]chan struct{}{},
		blockPrefix:      map[opKey]chan struct{}{},
		waiting:          map[opKey]chan struct{}{},
		waitingPrefix:    map[opKey]int{},
	}
}

// FailNext makes the next operation of the given kind on key fail once
// with err.
func (in *Injector) FailNext(op Op, key string, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.failNext[opKey{op: op, key: key}] = err
}

// FailNextPrefix makes the next operation of the given kind whose key
// starts with prefix fail once with err. The empty prefix matches every
// key.
func (in *Injector) FailNextPrefix(op Op, prefix string, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.failNextPrefix[opKey{op: op, key: prefix}] = err
}

// FailAlways makes every operation of the given kind on key fail with
// err, until ClearFailures.
func (in *Injector) FailAlways(op Op, key string, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.failAlways[opKey{op: op, key: key}] = err
}

// FailAlwaysPrefix makes every operation of the given kind whose key
// starts with prefix fail with err, until ClearFailures.
func (in *Injector) FailAlwaysPrefix(op Op, prefix string, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.failAlwaysPrefix[opKey{op: op, key: prefix}] = err
}

// ClearFailures removes every injected failure and ambiguity, so a test
// can prove that a bounded background effort succeeds on a later
// trigger. Barriers are not touched.
func (in *Injector) ClearFailures() {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.failNext = map[opKey]error{}
	in.failNextPrefix = map[opKey]error{}
	in.failAlways = map[opKey]error{}
	in.failAlwaysPrefix = map[opKey]error{}
	in.ambiguous = map[opKey]int{}
	in.ambiguousOp = map[Op]int{}
}

// AmbiguousNext makes the next operation of the given kind on key land
// but report a transport error, as a response lost after acceptance
// would. The store decides what "landing" means for each operation.
func (in *Injector) AmbiguousNext(op Op, key string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.ambiguous[opKey{op: op, key: key}]++
}

// AmbiguousNextOp makes the next operation of the given kind on any key
// land but report a transport error, for keys generated inside the
// system under test that a test cannot name in advance.
func (in *Injector) AmbiguousNextOp(op Op) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.ambiguousOp[op]++
}

// BlockNext makes the next operation of the given kind on key block
// until Release. The barrier is one-shot: it applies to exactly one
// operation.
func (in *Injector) BlockNext(op Op, key string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.block[opKey{op: op, key: key}] = make(chan struct{})
}

// BlockPrefix makes every operation of the given kind whose key starts
// with prefix block until ReleasePrefix. The barrier is multi-shot: it
// applies to every matching operation.
func (in *Injector) BlockPrefix(op Op, prefix string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.blockPrefix[opKey{op: op, key: prefix}] = make(chan struct{})
}

// Release unblocks the barrier for the operation on key, whether the
// operation already consumed the barrier and is waiting or has not
// started yet. A barrier re-armed while an operation is parked releases
// both, so a waiter can never be stranded.
func (in *Injector) Release(op Op, key string) {
	k := opKey{op: op, key: key}
	in.mu.Lock()
	pending, hasPending := in.block[k]
	delete(in.block, k)
	parked, hasParked := in.waiting[k]
	delete(in.waiting, k)
	in.mu.Unlock()
	if hasPending {
		close(pending)
	}
	if hasParked && parked != pending {
		close(parked)
	}
}

// ReleasePrefix releases every waiting and future operation of the kind
// on the prefix. Releasing before any operation starts lets the
// operations pass immediately.
func (in *Injector) ReleasePrefix(op Op, prefix string) {
	k := opKey{op: op, key: prefix}
	in.mu.Lock()
	ch, ok := in.blockPrefix[k]
	delete(in.blockPrefix, k)
	in.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Waiting reports whether an operation on key has consumed a barrier
// and is waiting for Release. Tests use it to sequence deterministic
// races without sleeping.
func (in *Injector) Waiting(op Op, key string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	_, ok := in.waiting[opKey{op: op, key: key}]
	return ok
}

// WaitingPrefix reports whether an operation is currently parked on the
// prefix barrier.
func (in *Injector) WaitingPrefix(op Op, prefix string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.waitingPrefix[opKey{op: op, key: prefix}] > 0
}

// WaitForBarrier blocks until every matching barrier is released, the
// context is done, or the barrier bound expires. The exact-key barrier
// is consumed first and moves to the waiting registry so Release can
// release it; releasing before the operation starts leaves a closed
// barrier the operation passes immediately. Prefix barriers are then
// waited in sorted order, so overlapping prefixes are deterministic.
func (in *Injector) WaitForBarrier(ctx context.Context, op Op, key string) error {
	k := opKey{op: op, key: key}
	in.mu.Lock()
	ch, exact := in.block[k]
	if exact {
		delete(in.block, k)
		in.waiting[k] = ch
	}
	in.mu.Unlock()
	if exact {
		err := in.awaitBarrier(ctx, ch)
		in.mu.Lock()
		delete(in.waiting, k)
		in.mu.Unlock()
		if err != nil {
			return err
		}
	}
	for _, pk := range in.matchingPrefixes(op, key) {
		in.mu.Lock()
		ch, ok := in.blockPrefix[pk]
		if ok {
			in.waitingPrefix[pk]++
		}
		in.mu.Unlock()
		if !ok {
			continue
		}
		err := in.awaitBarrier(ctx, ch)
		in.mu.Lock()
		if in.waitingPrefix[pk]--; in.waitingPrefix[pk] <= 0 {
			delete(in.waitingPrefix, pk)
		}
		in.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// matchingPrefixes returns every armed prefix barrier of the kind
// matching key, in sorted order.
func (in *Injector) matchingPrefixes(op Op, key string) []opKey {
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []opKey
	for pk := range in.blockPrefix {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			out = append(out, pk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// awaitBarrier waits for release, cancellation, or the barrier bound.
func (in *Injector) awaitBarrier(ctx context.Context, ch chan struct{}) error {
	timer := time.NewTimer(in.timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrBarrierTimeout
	}
}

// ConsumeFail reports and consumes an injected failure for the
// operation: one-shot exact key, then one-shot prefix, then permanent
// exact key, then permanent prefix. Prefixes resolve in a deterministic
// sorted order.
func (in *Injector) ConsumeFail(op Op, key string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	k := opKey{op: op, key: key}
	if err, ok := in.failNext[k]; ok {
		delete(in.failNext, k)
		return err
	}
	for _, pk := range sortedOpKeys(in.failNextPrefix) {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			err := in.failNextPrefix[pk]
			delete(in.failNextPrefix, pk)
			return err
		}
	}
	if err, ok := in.failAlways[k]; ok {
		return err
	}
	for _, pk := range sortedOpKeys(in.failAlwaysPrefix) {
		if pk.op == op && strings.HasPrefix(key, pk.key) {
			return in.failAlwaysPrefix[pk]
		}
	}
	return nil
}

// sortedOpKeys returns the keys of a fault map in a deterministic
// order, so overlapping prefixes always resolve the same way.
func sortedOpKeys(m map[opKey]error) []opKey {
	out := make([]opKey, 0, len(m))
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

// ConsumeAmbiguous reports whether the operation must land and then
// report a transport error, consuming one exact-key injection first and
// one op-wide injection second.
func (in *Injector) ConsumeAmbiguous(op Op, key string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	k := opKey{op: op, key: key}
	if in.ambiguous[k] > 0 {
		in.ambiguous[k]--
		return true
	}
	if in.ambiguousOp[op] > 0 {
		in.ambiguousOp[op]--
		return true
	}
	return false
}
