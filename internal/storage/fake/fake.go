// Package fake provides a concurrency-safe in-memory ObjectStore with real
// conditional-write semantics for deterministic protocol tests. It supports
// barriers, injected failures, ambiguous accepted writes, and ETag changes,
// so the storage contract suite and later fault-injection scenarios can
// exercise races and rare failure paths without Docker or network access.
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

// Store is an in-memory ObjectStore. All methods are safe for concurrent
// use. Prefix is the configured S3 prefix the store joins to protocol keys,
// exactly like the real adapter.
type Store struct {
	mu      sync.Mutex
	prefix  string
	objects map[string]*object
	seq     uint64

	failAny   map[Op]error
	failKey   map[opKey]error
	ambiguous map[opKey]bool
	block     map[opKey]chan struct{}
	waiting   map[opKey]chan struct{}
	calls     map[Op]int
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
		prefix:    prefix,
		objects:   map[string]*object{},
		failAny:   map[Op]error{},
		failKey:   map[opKey]error{},
		ambiguous: map[opKey]bool{},
		block:     map[opKey]chan struct{}{},
		waiting:   map[opKey]chan struct{}{},
		calls:     map[Op]int{},
	}
}

var _ storage.ObjectStore = (*Store)(nil)

// FailNext makes the next operation of the given kind fail with err,
// regardless of key.
func (s *Store) FailNext(op Op, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAny[op] = err
}

// FailNextKey makes the next operation of the given kind on key fail with
// err.
func (s *Store) FailNextKey(op Op, key string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failKey[opKey{op: op, key: key}] = err
}

// AmbiguousNext makes the next operation of the given kind on key store
// its bytes but report a transport error, as a response lost after
// acceptance would. For PutObject, CreateObject, and ReplaceObject the
// mutation lands and the caller must resolve the ambiguity by reading
// state back.
func (s *Store) AmbiguousNext(op Op, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ambiguous[opKey{op: op, key: key}] = true
}

// BlockNext makes the next operation of the given kind on key block until
// Unblock. The barrier is one-shot: it applies to exactly one operation.
func (s *Store) BlockNext(op Op, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.block[opKey{op: op, key: key}] = make(chan struct{})
}

// Unblock releases the barrier for the operation on key, if one is set.
// It works whether the operation already consumed the barrier and is
// waiting on it, or has not started yet.
func (s *Store) Unblock(op Op, key string) {
	k := opKey{op: op, key: key}
	s.mu.Lock()
	ch, ok := s.block[k]
	if ok {
		delete(s.block, k)
	} else {
		ch, ok = s.waiting[k]
		delete(s.waiting, k)
	}
	s.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Bump changes the ETag of an object without touching its bytes, as a
// concurrent accepted writer would after our read.
func (s *Store) Bump(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.objects[s.full(key)]; ok {
		s.seq++
		o.etag = storage.ETag(fmt.Sprintf(`"bump-%d"`, s.seq))
	}
}

// Waiting reports whether an operation on key has consumed a barrier and
// is waiting for Unblock. Tests use it to sequence deterministic races
// without sleeping.
func (s *Store) Waiting(op Op, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.waiting[opKey{op: op, key: key}]
	return ok
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

// waitForBarrier blocks until the matching barrier is released. When an
// operation consumes a pending barrier, the barrier moves to the waiting
// registry so Unblock can release it; after the release the operation
// counts and executes. Unblocking before the operation starts leaves a
// closed barrier that the operation passes immediately.
func (s *Store) waitForBarrier(op Op, key string) {
	k := opKey{op: op, key: key}
	s.mu.Lock()
	ch, ok := s.block[k]
	if ok {
		delete(s.block, k)
		s.waiting[k] = ch
	}
	s.mu.Unlock()
	if ok {
		<-ch
		s.mu.Lock()
		delete(s.waiting, k)
		s.mu.Unlock()
	}
}

// consumeFail reports and consumes a one-shot injected failure for the
// operation. It must be called with s.mu held.
func (s *Store) consumeFail(op Op, key string) error {
	if err, ok := s.failKey[opKey{op: op, key: key}]; ok {
		delete(s.failKey, opKey{op: op, key: key})
		return err
	}
	if err, ok := s.failAny[op]; ok {
		delete(s.failAny, op)
		return err
	}
	return nil
}

// consumeAmbiguous reports and consumes a one-shot response-loss injection
// for the operation. It must be called with s.mu held.
func (s *Store) consumeAmbiguous(op Op, key string) bool {
	k := opKey{op: op, key: key}
	if s.ambiguous[k] {
		delete(s.ambiguous, k)
		return true
	}
	return false
}

func (s *Store) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.waitForBarrier(OpGet, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpGet]++
	if err := s.consumeFail(OpGet, key); err != nil {
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
	s.waitForBarrier(OpPut, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpPut]++
	if err := s.consumeFail(OpPut, key); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("fake: put %s: source read failed: %w", key, storage.ErrTransport)
	}
	if s.consumeAmbiguous(OpPut, key) {
		s.objects[s.full(key)] = &object{data: data, etag: etagFor(data), meta: meta}
		return fmt.Errorf("fake: put %s: response lost: %w", key, storage.ErrTransport)
	}
	s.objects[s.full(key)] = &object{data: data, etag: etagFor(data), meta: meta}
	return nil
}

func (s *Store) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	s.waitForBarrier(OpCreate, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpCreate]++
	if err := s.consumeFail(OpCreate, key); err != nil {
		return "", err
	}
	if _, exists := s.objects[s.full(key)]; exists {
		return "", fmt.Errorf("fake: create %s: %w", key, storage.ErrPreconditionFailed)
	}
	o := &object{data: append([]byte(nil), data...), etag: etagFor(data)}
	s.objects[s.full(key)] = o
	if s.consumeAmbiguous(OpCreate, key) {
		return o.etag, fmt.Errorf("fake: create %s: response lost: %w", key, storage.ErrTransport)
	}
	return o.etag, nil
}

func (s *Store) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	s.waitForBarrier(OpReplace, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpReplace]++
	if err := s.consumeFail(OpReplace, key); err != nil {
		return "", err
	}
	o, ok := s.objects[s.full(key)]
	if !ok || o.etag != etag {
		return "", fmt.Errorf("fake: replace %s: %w", key, storage.ErrPreconditionFailed)
	}
	o.data = append([]byte(nil), data...)
	o.etag = etagFor(data)
	if s.consumeAmbiguous(OpReplace, key) {
		return o.etag, fmt.Errorf("fake: replace %s: response lost: %w", key, storage.ErrTransport)
	}
	return o.etag, nil
}

func (s *Store) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	s.waitForBarrier(OpList, prefix)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpList]++
	if err := s.consumeFail(OpList, prefix); err != nil {
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
	s.waitForBarrier(OpDelete, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[OpDelete]++
	if err := s.consumeFail(OpDelete, ""); err != nil {
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
