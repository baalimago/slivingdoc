// Package fake provides a concurrency-safe in-memory ObjectStore with real
// conditional-write semantics for deterministic protocol tests, plus the
// Injector fault engine it shares with the integration harness. It supports
// barriers, injected failures, ambiguous accepted writes, and ETag changes,
// so the storage contract suite and fault-injection scenarios can exercise
// races and rare failure paths without Docker or network access.
package fake

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// Op identifies one ObjectStore operation for failure injection and
// barrier coordination.
type Op string

const (
	OpPut     Op = "put"
	OpGet     Op = "get"
	OpCreate  Op = "create"
	OpReplace Op = "replace"
	OpList    Op = "list"
	OpDelete  Op = "delete"
)

// barrierTimeout bounds every barrier wait in the in-memory store. A test
// that fails before Unblock would otherwise strand the blocked operation
// until the package deadline.
const barrierTimeout = 10 * time.Second

// Store is an in-memory ObjectStore. All methods are safe for concurrent
// use. Prefix is the configured S3 prefix the store joins to protocol keys,
// exactly like the real adapter.
type Store struct {
	faults *Injector

	mu      sync.Mutex
	prefix  string
	objects map[string]*object
	seq     uint64
	calls   map[Op]int
}

type object struct {
	data []byte
	etag storage.ETag
	meta storage.Metadata
}

type opKey struct {
	op  Op
	key string
}

// New returns an empty store that joins the given prefix. The prefix must
// satisfy storage.ValidatePrefix.
func New(prefix string) *Store {
	return &Store{
		faults:  NewInjector(barrierTimeout),
		prefix:  prefix,
		objects: map[string]*object{},
		calls:   map[Op]int{},
	}
}

var _ storage.ObjectStore = (*Store)(nil)

// FailNext makes the next operation of the given kind fail with err,
// regardless of key.
func (s *Store) FailNext(op Op, err error) { s.faults.FailNextPrefix(op, "", err) }

// FailNextKey makes the next operation of the given kind on key fail with
// err.
func (s *Store) FailNextKey(op Op, key string, err error) { s.faults.FailNext(op, key, err) }

// AmbiguousNext makes the next operation of the given kind on key store
// its bytes but report a transport error, as a response lost after
// acceptance does. For PutObject, CreateObject, and ReplaceObject the
// mutation lands and the caller must resolve the ambiguity by reading
// state back.
func (s *Store) AmbiguousNext(op Op, key string) { s.faults.AmbiguousNext(op, key) }

// BlockNext makes the next operation of the given kind on key block until
// Unblock. The barrier is one-shot: it applies to exactly one operation.
func (s *Store) BlockNext(op Op, key string) { s.faults.BlockNext(op, key) }

// Unblock releases the barrier for the operation on key, if one is set.
// It works whether the operation already consumed the barrier and is
// waiting on it, or has not started yet.
func (s *Store) Unblock(op Op, key string) { s.faults.Release(op, key) }

// Waiting reports whether an operation on key has consumed a barrier and
// is waiting for Unblock. Tests use it to sequence deterministic races
// without sleeping.
func (s *Store) Waiting(op Op, key string) bool { return s.faults.Waiting(op, key) }

// Bump changes the ETag of an object without touching its bytes, as a
// concurrent accepted writer does after our read.
func (s *Store) Bump(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.objects[s.full(key)]; ok {
		s.seq++
		o.etag = storage.ETag(fmt.Sprintf(`"bump-%d"`, s.seq))
	}
}

// Calls returns how many times operations of the given kind have completed
// (including failed ones).
func (s *Store) Calls(op Op) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[op]
}

// ObjectCount returns the number of stored objects, for state assertions.
func (s *Store) ObjectCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects)
}

func (s *Store) full(key string) string { return storage.JoinKey(s.prefix, key) }

func (s *Store) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if err := s.faults.WaitForBarrier(ctx, OpGet, key); err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpGet]++
	if err := s.faults.ConsumeFail(OpGet, key); err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	o, ok := s.objects[s.full(key)]
	if !ok {
		return nil, storage.ObjectInfo{}, fmt.Errorf("fake: get %s: %w", key, storage.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), o.data...))),
		storage.ObjectInfo{Size: int64(len(o.data)), ETag: o.etag, Meta: o.meta}, nil
}

func (s *Store) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	if err := s.faults.WaitForBarrier(ctx, OpPut, key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpPut]++
	if err := s.faults.ConsumeFail(OpPut, key); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("fake: put %s: source read failed: %w", key, storage.ErrTransport)
	}
	s.objects[s.full(key)] = &object{data: data, etag: etagFor(data), meta: meta}
	if s.faults.ConsumeAmbiguous(OpPut, key) {
		return fmt.Errorf("fake: put %s: response lost: %w", key, storage.ErrTransport)
	}
	return nil
}

func (s *Store) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	if err := s.faults.WaitForBarrier(ctx, OpCreate, key); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpCreate]++
	if err := s.faults.ConsumeFail(OpCreate, key); err != nil {
		return "", err
	}
	if _, exists := s.objects[s.full(key)]; exists {
		return "", fmt.Errorf("fake: create %s: %w", key, storage.ErrPreconditionFailed)
	}
	o := &object{data: append([]byte(nil), data...), etag: etagFor(data)}
	s.objects[s.full(key)] = o
	if s.faults.ConsumeAmbiguous(OpCreate, key) {
		return o.etag, fmt.Errorf("fake: create %s: response lost: %w", key, storage.ErrTransport)
	}
	return o.etag, nil
}

func (s *Store) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	if err := s.faults.WaitForBarrier(ctx, OpReplace, key); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpReplace]++
	if err := s.faults.ConsumeFail(OpReplace, key); err != nil {
		return "", err
	}
	o, ok := s.objects[s.full(key)]
	if !ok || o.etag != etag {
		return "", fmt.Errorf("fake: replace %s: %w", key, storage.ErrPreconditionFailed)
	}
	o.data = append([]byte(nil), data...)
	o.etag = etagFor(data)
	if s.faults.ConsumeAmbiguous(OpReplace, key) {
		return o.etag, fmt.Errorf("fake: replace %s: response lost: %w", key, storage.ErrTransport)
	}
	return o.etag, nil
}

func (s *Store) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	if err := s.faults.WaitForBarrier(ctx, OpList, prefix); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpList]++
	if err := s.faults.ConsumeFail(OpList, prefix); err != nil {
		return err
	}
	full := s.full(prefix)
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		if len(k) >= len(full) && k[:len(full)] == full {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		proto := k
		if s.prefix != "" {
			proto = k[len(s.prefix)+1:]
		}
		if err := fn(proto); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteObjects(ctx context.Context, keys []string) error {
	if err := s.faults.WaitForBarrier(ctx, OpDelete, ""); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpDelete]++
	if err := s.faults.ConsumeFail(OpDelete, ""); err != nil {
		return err
	}
	for _, key := range keys {
		delete(s.objects, s.full(key))
	}
	return nil
}

// etagFor mimics the S3 single-PUT ETag: the MD5 of the object bytes in
// quotes. It is a concurrency token only, never a content digest.
func etagFor(data []byte) storage.ETag {
	sum := md5.Sum(data)
	return storage.ETag(`"` + hex.EncodeToString(sum[:]) + `"`)
}
