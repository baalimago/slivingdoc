// Package contract hosts the single storage contract suite that runs
// against both the in-memory fake and the MinIO-backed adapter. The suite
// is the behavioral contract of the ObjectStore boundary: the same
// assertions must pass on any compliant implementation.
package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
)

// Factory builds a fresh, isolated ObjectStore for one suite subtest.
// Isolation comes from a unique configured prefix, never from shared state.
type Factory func(t *testing.T) storage.ObjectStore

// Run executes the shared object-store contract suite.
func Run(t *testing.T, factory Factory) {
	t.Run("conditional create", func(t *testing.T) {
		s := factory(t)
		etag, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1"))
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		if etag == "" {
			t.Fatal("first create returned an empty etag")
		}
		if _, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v2")); !errors.Is(err, storage.ErrPreconditionFailed) {
			t.Fatalf("second create error = %v, want ErrPreconditionFailed", err)
		}
		rc, info, err := s.ReadObject(context.Background(), storage.CurrentKey)
		if err != nil {
			t.Fatalf("read after create: %v", err)
		}
		got := readAll(t, rc)
		if got != "v1" {
			t.Fatalf("read after create = %q, want %q", got, "v1")
		}
		if info.ETag != etag {
			t.Fatalf("read etag = %q, want %q", info.ETag, etag)
		}
	})

	t.Run("conditional replace", func(t *testing.T) {
		s := factory(t)
		etag1, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		etag2, err := s.ReplaceObject(context.Background(), storage.CurrentKey, etag1, []byte("v2"))
		if err != nil {
			t.Fatalf("matching replace: %v", err)
		}
		if etag2 == "" || etag2 == etag1 {
			t.Fatalf("replace etag = %q, want a new etag", etag2)
		}
		rc, info, err := s.ReadObject(context.Background(), storage.CurrentKey)
		if err != nil {
			t.Fatalf("read after replace: %v", err)
		}
		if got := readAll(t, rc); got != "v2" {
			t.Fatalf("read after replace = %q, want %q", got, "v2")
		}
		if info.ETag != etag2 {
			t.Fatalf("read etag = %q, want %q", info.ETag, etag2)
		}
	})

	t.Run("stale replace rejected without mutation", func(t *testing.T) {
		s := factory(t)
		etag1, err := s.CreateObject(context.Background(), storage.CurrentKey, []byte("v1"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := s.ReplaceObject(context.Background(), storage.CurrentKey, "stale-etag", []byte("v2")); !errors.Is(err, storage.ErrPreconditionFailed) {
			t.Fatalf("stale replace error = %v, want ErrPreconditionFailed", err)
		}
		rc, info, err := s.ReadObject(context.Background(), storage.CurrentKey)
		if err != nil {
			t.Fatalf("read after rejected replace: %v", err)
		}
		if got := readAll(t, rc); got != "v1" {
			t.Fatalf("rejected replace mutated bytes to %q, want %q", got, "v1")
		}
		if info.ETag != etag1 {
			t.Fatalf("rejected replace changed etag to %q, want %q", info.ETag, etag1)
		}
	})

	t.Run("replace missing key is a precondition failure", func(t *testing.T) {
		s := factory(t)
		if _, err := s.ReplaceObject(context.Background(), storage.CurrentKey, "etag", []byte("v1")); !errors.Is(err, storage.ErrPreconditionFailed) {
			t.Fatalf("replace missing error = %v, want ErrPreconditionFailed", err)
		}
	})

	t.Run("read missing is not found", func(t *testing.T) {
		s := factory(t)
		if _, _, err := s.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("read missing error = %v, want ErrNotFound", err)
		}
	})

	t.Run("put and read round trip", func(t *testing.T) {
		s := factory(t)
		data := []byte("pack bytes")
		meta := metadataFor(t, data, storage.KindIncrement, 7)
		key := incKey(meta.Meta.Generation, meta.PublicationID)
		if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta.Meta); err != nil {
			t.Fatalf("put: %v", err)
		}
		rc, info, err := s.ReadObject(context.Background(), key.String())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got := readAll(t, rc); got != string(data) {
			t.Fatalf("read bytes = %q, want %q", got, data)
		}
		if info.Size != int64(len(data)) {
			t.Fatalf("info size = %d, want %d", info.Size, len(data))
		}
		if info.ETag == "" {
			t.Fatal("read returned an empty etag")
		}
		if info.Meta != meta.Meta {
			t.Fatalf("info metadata = %+v, want %+v", info.Meta, meta.Meta)
		}
	})

	t.Run("verify object", func(t *testing.T) {
		s := factory(t)
		data := []byte("verified pack bytes")
		meta := metadataFor(t, data, storage.KindCheckpoint, 3)
		key := cpKey(meta.Meta.Generation, meta.PublicationID)
		if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), meta.Meta); err != nil {
			t.Fatalf("put: %v", err)
		}
		if err := storage.VerifyObject(context.Background(), s, key.String(), meta.Meta.SHA256, meta.Meta.Size); err != nil {
			t.Fatalf("verify matching object: %v", err)
		}
		other := sha256.Sum256([]byte("different bytes"))
		if err := storage.VerifyObject(context.Background(), s, key.String(), storage.SHA256(other), meta.Meta.Size); !errors.Is(err, storage.ErrIntegrity) {
			t.Fatalf("verify wrong digest error = %v, want ErrIntegrity", err)
		}
		if err := storage.VerifyObject(context.Background(), s, key.String(), meta.Meta.SHA256, meta.Meta.Size+1); !errors.Is(err, storage.ErrIntegrity) {
			t.Fatalf("verify wrong size error = %v, want ErrIntegrity", err)
		}
		if err := storage.VerifyObject(context.Background(), s, "packs/increments/9-missing.pack", meta.Meta.SHA256, meta.Meta.Size); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("verify missing error = %v, want ErrNotFound", err)
		}
	})

	t.Run("upload unique happy path", func(t *testing.T) {
		s := factory(t)
		data := []byte("immutable pack")
		meta := metadataFor(t, data, storage.KindIncrement, 1)
		key := incKey(meta.Meta.Generation, meta.PublicationID)
		if err := storage.UploadUnique(context.Background(), s, key, bytes.NewReader(data), meta.Meta); err != nil {
			t.Fatalf("UploadUnique: %v", err)
		}
		rc, _, err := s.ReadObject(context.Background(), key.String())
		if err != nil {
			t.Fatalf("read after upload: %v", err)
		}
		if got := readAll(t, rc); got != string(data) {
			t.Fatalf("uploaded bytes = %q, want %q", got, data)
		}
	})

	t.Run("list filters by prefix and returns protocol keys", func(t *testing.T) {
		s := factory(t)
		a := incKey(1, mustUUID(t, "01973e12-8b34-7b01-9e2f-000000000001"))
		b := incKey(2, mustUUID(t, "01973e12-8b34-7b01-9e2f-000000000002"))
		c := cpKey(2, mustUUID(t, "01973e12-8b34-7b01-9e2f-000000000003"))
		for _, key := range []storage.Key{a, b, c} {
			data := []byte("pack")
			if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), metadataFor(t, data, key.Kind, key.Generation).Meta); err != nil {
				t.Fatalf("put %s: %v", key, err)
			}
		}
		assertList(t, s, "packs/increments/", []string{a.String(), b.String()})
		assertList(t, s, "packs/checkpoints/", []string{c.String()})
		// S3 LIST returns keys in ascending UTF-8 binary order, so
		// "checkpoints" sorts before "increments".
		assertList(t, s, "packs/", []string{c.String(), a.String(), b.String()})
	})

	t.Run("delete removes and tolerates missing keys", func(t *testing.T) {
		s := factory(t)
		key := incKey(1, mustUUID(t, "01973e12-8b34-7b01-9e2f-000000000001"))
		data := []byte("pack")
		if err := s.PutObject(context.Background(), key.String(), bytes.NewReader(data), metadataFor(t, data, key.Kind, key.Generation).Meta); err != nil {
			t.Fatalf("put: %v", err)
		}
		if err := s.DeleteObjects(context.Background(), []string{key.String(), "packs/increments/9-missing.pack"}); err != nil {
			t.Fatalf("delete: %v", err)
		}
		assertList(t, s, "packs/", nil)
	})

	t.Run("probe passes and cleans its key", func(t *testing.T) {
		s := factory(t)
		if err := storage.Probe(context.Background(), s); err != nil {
			t.Fatalf("probe: %v", err)
		}
		assertList(t, s, "probe/", nil)
	})
}

// uploadMeta carries the metadata and the key ID for one fixture pack.
type uploadMeta struct {
	Meta          storage.Metadata
	PublicationID storage.UUID
}

func metadataFor(t *testing.T, data []byte, kind storage.PackKind, generation uint64) uploadMeta {
	t.Helper()
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7: %v", err)
	}
	sum := sha256.Sum256(data)
	return uploadMeta{
		Meta: storage.Metadata{
			SHA256:     storage.SHA256(sum),
			Size:       uint64(len(data)),
			Kind:       kind,
			Generation: generation,
		},
		PublicationID: id,
	}
}

func mustUUID(t *testing.T, s string) storage.UUID {
	t.Helper()
	id, err := storage.ParseUUIDv7(s)
	if err != nil {
		t.Fatalf("ParseUUIDv7(%q): %v", s, err)
	}
	return id
}

func incKey(generation uint64, id storage.UUID) storage.Key {
	return storage.Key{Kind: storage.KindIncrement, Generation: generation, ID: id}
}

func cpKey(through uint64, id storage.UUID) storage.Key {
	return storage.Key{Kind: storage.KindCheckpoint, Generation: through, ID: id}
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

func assertList(t *testing.T, s storage.ObjectStore, prefix string, want []string) {
	t.Helper()
	var got []string
	err := s.ListObjects(context.Background(), prefix, func(key string) error {
		got = append(got, key)
		return nil
	})
	if err != nil {
		t.Fatalf("list %q: %v", prefix, err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("list %q = %v, want %v", prefix, got, want)
	}
}
