// The probe tests live in the external test package: they wrap the fake
// store, and the fake imports storage, so a same-package test file
// creates an import cycle. The tests exercise only the exported surface.
package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// probeStore wraps a fake store and breaks exactly one required probe
// condition. The probe is a storage-policy function over interface
// primitives, so a deliberately broken store proves that each deviation is
// detected (architecture section 9.4).
type probeStore struct {
	*fake.Store
	// breakCreate ignores If-None-Match: * and overwrites unconditionally.
	breakCreate bool
	// breakReplace ignores If-Match and overwrites unconditionally.
	breakReplace bool
	// breakReadAfterWrite serves the pre-replace bytes after a replace.
	breakReadAfterWrite bool

	shadow sync.Map // protocol key -> []byte as of the last create
}

func (s *probeStore) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	if s.breakCreate {
		// Ignore If-None-Match: *: the second create must also succeed.
		if err := s.DeleteObjects(ctx, []string{key}); err != nil {
			return "", err
		}
	}
	etag, err := s.Store.CreateObject(ctx, key, data)
	if err == nil {
		s.shadow.Store(key, append([]byte(nil), data...))
	}
	return etag, err
}

func (s *probeStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	if s.breakReplace {
		// Ignore If-Match: replace whatever is stored, even with a
		// wrong ETag.
		if err := s.DeleteObjects(ctx, []string{key}); err != nil {
			return "", err
		}
		return s.Store.CreateObject(ctx, key, data)
	}
	return s.Store.ReplaceObject(ctx, key, etag, data)
}

func (s *probeStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	rc, info, err := s.Store.ReadObject(ctx, key)
	if err != nil {
		return nil, storage.ObjectInfo{}, err
	}
	if !s.breakReadAfterWrite {
		return rc, info, nil
	}
	// Serve the last created bytes, not the replaced bytes: an
	// immediate read after a replace is stale.
	v, ok := s.shadow.Load(key)
	if !ok {
		return rc, info, nil
	}
	rc.Close()
	return io.NopCloser(bytes.NewReader(v.([]byte))), info, nil
}

func TestProbePassesOnFake(t *testing.T) {
	s := fake.New("probe-ok")
	if err := storage.Probe(context.Background(), s); err != nil {
		t.Fatalf("Probe() = %v, want nil", err)
	}
	assertNoProbeKeys(t, s)
}

func TestProbeDetectsMissingIfNoneMatch(t *testing.T) {
	s := &probeStore{Store: fake.New("probe-lax-create"), breakCreate: true}
	if err := storage.Probe(context.Background(), s); !errors.Is(err, storage.ErrIncompatible) {
		t.Fatalf("Probe() error = %v, want ErrIncompatible", err)
	}
	assertNoProbeKeys(t, s.Store)
}

func TestProbeDetectsMissingIfMatch(t *testing.T) {
	s := &probeStore{Store: fake.New("probe-lax-replace"), breakReplace: true}
	if err := storage.Probe(context.Background(), s); !errors.Is(err, storage.ErrIncompatible) {
		t.Fatalf("Probe() error = %v, want ErrIncompatible", err)
	}
	assertNoProbeKeys(t, s.Store)
}

func TestProbeDetectsBrokenReadAfterWrite(t *testing.T) {
	s := &probeStore{Store: fake.New("probe-stale-read"), breakReadAfterWrite: true}
	if err := storage.Probe(context.Background(), s); !errors.Is(err, storage.ErrIncompatible) {
		t.Fatalf("Probe() error = %v, want ErrIncompatible", err)
	}
	assertNoProbeKeys(t, s.Store)
}

func TestProbeCleansKeyAfterRecoverableFailure(t *testing.T) {
	// A denied delete during cleanup must not change the probe verdict;
	// the probe key stays behind but startup still reports the failure.
	s := fake.New("probe-delete-denied")
	s.FailNext(fake.OpCreate, errors.New("fake: create denied"))
	if err := storage.Probe(context.Background(), s); !errors.Is(err, storage.ErrIncompatible) {
		t.Fatalf("Probe() error = %v, want ErrIncompatible", err)
	}
}

// TestProbeSurfacesCreateFailureCause proves an operational create failure
// keeps its real reason in the probe error while ErrIncompatible still
// classifies it (so the startup diagnostic can name the cause).
func TestProbeSurfacesCreateFailureCause(t *testing.T) {
	s := fake.New("probe-create-cause")
	denied := errors.New("fake: AccessDenied: Access Denied.")
	s.FailNext(fake.OpCreate, denied)
	err := storage.Probe(context.Background(), s)
	if !errors.Is(err, storage.ErrIncompatible) {
		t.Fatalf("Probe() error = %v, want ErrIncompatible", err)
	}
	if !errors.Is(err, denied) {
		t.Fatalf("Probe() error = %v, want it to wrap the create cause %v", err, denied)
	}
	if !strings.Contains(err.Error(), "Access Denied") {
		t.Fatalf("Probe() error = %q, want the real create reason", err)
	}
}

// assertNoProbeKeys verifies the probe cleaned its disposable key.
func assertNoProbeKeys(t *testing.T, s *fake.Store) {
	t.Helper()
	var keys []string
	err := s.ListObjects(context.Background(), "probe/", func(key string) error {
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		t.Fatalf("list probe keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("probe left %d keys behind: %v", len(keys), keys)
	}
}
