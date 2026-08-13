package integrationtest

import (
	"context"
	"io"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// Op identifies one ObjectStore operation kind, mirroring the storage
// boundary methods. It aliases the fake store's enum so the recorder, the
// fault wrappers, and the fake share one definition and scenarios can
// assert and inject on the same names.
type Op = fake.Op

const (
	OpPut     = fake.OpPut
	OpGet     = fake.OpGet
	OpCreate  = fake.OpCreate
	OpReplace = fake.OpReplace
	OpList    = fake.OpList
	OpDelete  = fake.OpDelete
)

// AllOps lists every operation kind in a stable order, for zero-count
// assertions.
var AllOps = []Op{OpPut, OpGet, OpCreate, OpReplace, OpList, OpDelete}

// Recorder counts every operation crossing the harness store seam, by kind
// and by kind+protocol-key. Scenarios assert the counts to prove the
// zero-mutation rules and the bounded retry cycles; the recorder itself
// never changes the operation result.
//
// The base store is an explicit field rather than an embedded interface, so
// a new ObjectStore method fails to compile here instead of being forwarded
// silently and counted nowhere.
type Recorder struct {
	base   storage.ObjectStore
	mu     sync.Mutex
	counts map[Op]int
	keys   map[Op]map[string]int
}

// NewRecorder wraps base and starts counting.
func NewRecorder(base storage.ObjectStore) *Recorder {
	return &Recorder{
		base:   base,
		counts: map[Op]int{},
		keys:   map[Op]map[string]int{},
	}
}

var _ storage.ObjectStore = (*Recorder)(nil)

// Count returns how many operations of the given kind were started,
// including the ones that then failed or were injected. Deletes count one
// per call, not one per key.
func (r *Recorder) Count(op Op) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[op]
}

// CountKey returns how many operations of the given kind touched exactly
// the given protocol key.
func (r *Recorder) CountKey(op Op, key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.keys[op][key]
}

// CountKeyPrefix returns how many operations of the given kind touched a
// key with the given protocol-key prefix.
func (r *Recorder) CountKeyPrefix(op Op, prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for key, n := range r.keys[op] {
		if strings.HasPrefix(key, prefix) {
			total += n
		}
	}
	return total
}

// KeysWithPrefix returns the distinct protocol keys of the given kind that
// start with prefix, sorted. Scenarios use it where the contract is about
// key identity rather than call volume: a retried publication must use a
// NEW key, which a call count alone cannot distinguish from one key written
// twice.
func (r *Recorder) KeysWithPrefix(op Op, prefix string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for key := range r.keys[op] {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// Snapshot returns a copy of the per-kind counters.
func (r *Recorder) Snapshot() map[Op]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[Op]int, len(r.counts))
	maps.Copy(out, r.counts)
	return out
}

func (r *Recorder) record(op Op, key string) {
	r.mu.Lock()
	r.counts[op]++
	if r.keys[op] == nil {
		r.keys[op] = map[string]int{}
	}
	r.keys[op][key]++
	r.mu.Unlock()
}

func (r *Recorder) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	r.record(OpGet, key)
	return r.base.ReadObject(ctx, key)
}

func (r *Recorder) PutObject(ctx context.Context, key string, body io.Reader, meta storage.Metadata) error {
	r.record(OpPut, key)
	return r.base.PutObject(ctx, key, body, meta)
}

func (r *Recorder) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	r.record(OpCreate, key)
	return r.base.CreateObject(ctx, key, data)
}

func (r *Recorder) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	r.record(OpReplace, key)
	return r.base.ReplaceObject(ctx, key, etag, data)
}

func (r *Recorder) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	r.record(OpList, prefix)
	return r.base.ListObjects(ctx, prefix, fn)
}

// DeleteObjects counts one delete operation for the batch and one key
// count for every key it names, so a scenario can assert both "how many
// delete batches ran" and "this object was never handed to a delete".
func (r *Recorder) DeleteObjects(ctx context.Context, keys []string) error {
	r.mu.Lock()
	r.counts[OpDelete]++
	if r.keys[OpDelete] == nil {
		r.keys[OpDelete] = map[string]int{}
	}
	for _, key := range keys {
		r.keys[OpDelete][key]++
	}
	r.mu.Unlock()
	return r.base.DeleteObjects(ctx, keys)
}
