package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/s3store"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/contract"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/tests3"
)

// encryptedContractSeq isolates the decorated contract stores of the
// cross-backend suite below.
var encryptedContractSeq atomic.Uint64

// The reference hosted space and its write token of the cross-backend
// suite.
const (
	contractSpace = "enc-contract-space"
	contractToken = "sld_0123456789abcdef_c2VjcmV0LXRva2VuLXZhbHVlLWZvci10ZXN0cy0wMDAwMA"
)

// testKDFParams is the narrowest accepted Argon2id cost, so a decorator
// test does not pay the canonical 256 MiB per door.
func testKDFParams() crypt.KDFParams {
	return crypt.KDFParams{MemoryKiB: 64 << 10, Iterations: 1, Parallelism: 1}
}

// ringFixture mints one notebook key with one passphrase door and exposes
// the locked and unlocked keyring views a decorator test needs.
type ringFixture struct {
	gate   *crypt.KDFGate
	key    crypt.NotebookKey
	door   crypt.Door
	secret []byte
}

func newRingFixture(t *testing.T) *ringFixture {
	t.Helper()
	gate, err := crypt.NewKDFGate(1)
	if err != nil {
		t.Fatalf("NewKDFGate: %v", err)
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey: %v", err)
	}
	secret := []byte("a passphrase")
	door, err := crypt.WrapKey(context.Background(), gate, key, secret, storage.CurrentKey, crypt.DoorPassphrase, testKDFParams())
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	return &ringFixture{gate: gate, key: key, door: door, secret: secret}
}

func (f *ringFixture) unlocked(t *testing.T) *crypt.Keyring {
	t.Helper()
	ring, err := crypt.NewUnlockedKeyring(f.key, nil, f.gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring: %v", err)
	}
	return ring
}

func (f *ringFixture) locked(t *testing.T, secret []byte) *crypt.Keyring {
	t.Helper()
	ring, err := crypt.NewKeyring([]crypt.Candidate{{Source: crypt.SourcePrompt, Secret: secret}}, f.gate)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return ring
}

// seedEncryptedCurrent writes a version-two current object whose envelope
// opens with the fixture's door, so a decorator over the raw store resolves
// encrypted mode.
func seedEncryptedCurrent(t *testing.T, raw storage.ObjectStore, f *ringFixture, manifest []byte) {
	t.Helper()
	envelope, err := f.unlocked(t).Seal(storage.CurrentKey, crypt.KindCurrent, manifest, []crypt.Door{f.door})
	if err != nil {
		t.Fatalf("Seal current: %v", err)
	}
	wrapped, err := storage.WrapCurrentEnvelope(envelope)
	if err != nil {
		t.Fatalf("WrapCurrentEnvelope: %v", err)
	}
	if _, err := raw.CreateObject(context.Background(), storage.CurrentKey, wrapped); err != nil {
		t.Fatalf("seed current: %v", err)
	}
}

// packFixture is one protocol pack key with matching plaintext metadata.
func packFixture(t *testing.T) (storage.Key, []byte, storage.Metadata) {
	t.Helper()
	id, err := storage.NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7: %v", err)
	}
	key := storage.Key{Kind: storage.KindIncrement, Generation: 1, ID: id}
	plaintext := []byte("immutable pack bytes")
	sum := sha256.Sum256(plaintext)
	return key, plaintext, storage.Metadata{
		SHA256:     storage.SHA256(sum),
		Size:       uint64(len(plaintext)),
		Kind:       storage.KindIncrement,
		Generation: 1,
	}
}

func readAll(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return data
}

// TestEncryptedStoreContract proves every decorated method round-trips a
// plaintext notebook unchanged, and that an encrypted notebook stores
// ciphertext while presenting plaintext bytes, sizes, and digests. The raw
// store's metadata describes the stored ciphertext; the notebook sees the
// plaintext it wrote.
func TestEncryptedStoreContract(t *testing.T) {
	f := newRingFixture(t)

	t.Run("plaintext notebook passes through", func(t *testing.T) {
		raw := fake.New("plaintext")
		store := storage.Encrypted(raw, f.locked(t, f.secret))
		ctx := context.Background()

		if _, err := store.CreateObject(ctx, storage.CurrentKey, []byte("v1")); err != nil {
			t.Fatalf("create current: %v", err)
		}
		rc, _, err := store.ReadObject(ctx, storage.CurrentKey)
		if err != nil {
			t.Fatalf("read current: %v", err)
		}
		if got := readAll(t, rc); string(got) != "v1" {
			t.Fatalf("current = %q, want %q", got, "v1")
		}
		rawBytes, err := rawObject(t, raw, storage.CurrentKey)
		if err != nil {
			t.Fatalf("raw current: %v", err)
		}
		if string(rawBytes) != "v1" {
			t.Fatalf("stored current = %q, want the plaintext bytes", rawBytes)
		}
	})

	t.Run("encrypted notebook seals and opens", func(t *testing.T) {
		raw := fake.New("encrypted")
		seedEncryptedCurrent(t, raw, f, []byte("manifest-one"))
		store := storage.Encrypted(raw, f.locked(t, f.secret))
		ctx := context.Background()

		rc, _, err := store.ReadObject(ctx, storage.CurrentKey)
		if err != nil {
			t.Fatalf("read current: %v", err)
		}
		if got := readAll(t, rc); string(got) != "manifest-one" {
			t.Fatalf("current = %q, want %q", got, "manifest-one")
		}

		key, plaintext, meta := packFixture(t)
		if err := store.PutObject(ctx, key.String(), bytes.NewReader(plaintext), meta); err != nil {
			t.Fatalf("put pack: %v", err)
		}
		rawBytes, err := rawObject(t, raw, key.String())
		if err != nil {
			t.Fatalf("raw pack: %v", err)
		}
		if bytes.Contains(rawBytes, plaintext) || !bytes.HasPrefix(rawBytes, []byte("SLIVENC\x00")) {
			t.Fatalf("stored pack is not an envelope: %q", rawBytes)
		}
		_, rawInfo, err := raw.ReadObject(ctx, key.String())
		if err != nil {
			t.Fatalf("raw read pack: %v", err)
		}
		rawSum := sha256.Sum256(rawBytes)
		if rawInfo.Size != int64(len(rawBytes)) || rawInfo.Meta.SHA256 != storage.SHA256(rawSum) {
			t.Fatalf("raw metadata %+v does not describe the stored ciphertext", rawInfo.Meta)
		}
		if rawInfo.Meta.Kind != storage.KindIncrement || rawInfo.Meta.Generation != 1 {
			t.Fatalf("raw metadata kind/generation = %v/%d, want the descriptor values", rawInfo.Meta.Kind, rawInfo.Meta.Generation)
		}

		rc, info, err := store.ReadObject(ctx, key.String())
		if err != nil {
			t.Fatalf("read pack: %v", err)
		}
		if got := readAll(t, rc); !bytes.Equal(got, plaintext) {
			t.Fatalf("pack = %q, want %q", got, plaintext)
		}
		if info.Size != int64(len(plaintext)) || info.Meta.SHA256 != meta.SHA256 || info.Meta.Size != meta.Size {
			t.Fatalf("notebook metadata %+v/%d does not describe the plaintext pack", info.Meta, info.Size)
		}

		listed := false
		if err := store.ListObjects(ctx, "packs/", func(string) error { listed = true; return nil }); err != nil {
			t.Fatalf("list packs: %v", err)
		}
		if !listed {
			t.Fatal("listing through the decorator hid the stored pack")
		}

		_, etag, err := readCurrentWithETag(t, store)
		if err != nil {
			t.Fatalf("read current etag: %v", err)
		}
		if _, err := store.ReplaceObject(ctx, storage.CurrentKey, etag, []byte("manifest-two")); err != nil {
			t.Fatalf("replace current: %v", err)
		}
		rawBytes, err = rawObject(t, raw, storage.CurrentKey)
		if err != nil {
			t.Fatalf("raw current: %v", err)
		}
		if !bytes.HasPrefix(rawBytes, []byte(`{"version":2,"envelope":"`)) {
			t.Fatalf("stored current is not the version-two wrapper: %q", rawBytes)
		}

		rc, _, err = store.ReadObject(ctx, storage.CurrentKey)
		if err != nil {
			t.Fatalf("read current: %v", err)
		}
		if got := readAll(t, rc); string(got) != "manifest-two" {
			t.Fatalf("current = %q, want %q", got, "manifest-two")
		}

		_, etag, err = readCurrentWithETag(t, store)
		if err != nil {
			t.Fatalf("read current etag: %v", err)
		}
		if _, err := store.ReplaceObject(ctx, storage.CurrentKey, etag, []byte("manifest-three")); err != nil {
			t.Fatalf("replace current: %v", err)
		}
		rc, _, err = store.ReadObject(ctx, storage.CurrentKey)
		if err != nil {
			t.Fatalf("read replaced current: %v", err)
		}
		if got := readAll(t, rc); string(got) != "manifest-three" {
			t.Fatalf("current = %q, want %q", got, "manifest-three")
		}

		if err := store.DeleteObjects(ctx, []string{key.String()}); err != nil {
			t.Fatalf("delete pack: %v", err)
		}
		if _, _, err := raw.ReadObject(ctx, key.String()); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("raw read after delete = %v, want ErrNotFound", err)
		}
	})
}

// TestEncryptedConditionalWrite proves a conditional-write loss is returned
// unchanged and the decorator retries nothing: the raw store is written
// exactly once.
func TestEncryptedConditionalWrite(t *testing.T) {
	f := newRingFixture(t)
	raw := fake.New("conditional")
	seedEncryptedCurrent(t, raw, f, []byte("manifest-one"))
	store := storage.Encrypted(raw, f.locked(t, f.secret))
	ctx := context.Background()

	rawCreate := raw.Calls(fake.OpCreate)
	if _, err := store.CreateObject(ctx, storage.CurrentKey, []byte("manifest-two")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("create on an existing current = %v, want ErrPreconditionFailed", err)
	}
	if _, err := store.ReplaceObject(ctx, storage.CurrentKey, "stale-etag", []byte("manifest-two")); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Fatalf("stale replace = %v, want ErrPreconditionFailed", err)
	}
	if got := raw.Calls(fake.OpCreate); got != rawCreate+1 {
		t.Fatalf("raw create calls = %d, want %d (no decorator retry)", got, rawCreate+1)
	}
	if got := raw.Calls(fake.OpReplace); got != 1 {
		t.Fatalf("raw replace calls = %d, want 1 (no decorator retry)", got)
	}
	rawBytes, err := rawObject(t, raw, storage.CurrentKey)
	if err != nil {
		t.Fatalf("raw current: %v", err)
	}
	rc, _, err := store.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	if !bytes.Equal(readAll(t, rc), []byte("manifest-one")) || bytes.Contains(rawBytes, []byte("manifest-two")) {
		t.Fatal("a lost conditional write mutated the accepted current")
	}
}

// TestEncryptedStoreRefusesLockedNotebook proves a keyring no door opens
// refuses a pack read and every object write before the raw store is
// touched beyond the one current read that resolves the mode.
func TestEncryptedStoreRefusesLockedNotebook(t *testing.T) {
	f := newRingFixture(t)
	raw := fake.New("locked")
	seedEncryptedCurrent(t, raw, f, []byte("manifest-one"))
	store := storage.Encrypted(raw, f.locked(t, []byte("wrong passphrase")))
	ctx := context.Background()
	key, plaintext, meta := packFixture(t)

	rc, _, err := store.ReadObject(ctx, key.String())
	if rc != nil {
		rc.Close()
	}
	if !errors.Is(err, crypt.ErrKeyRejected) {
		t.Fatalf("pack read = %v, want ErrKeyRejected", err)
	}
	if got := raw.Calls(fake.OpGet); got != 1 {
		t.Fatalf("raw get calls = %d, want 1 (current only, no pack read)", got)
	}
	if err := store.PutObject(ctx, key.String(), bytes.NewReader(plaintext), meta); !errors.Is(err, crypt.ErrKeyRejected) {
		t.Fatalf("pack write = %v, want ErrKeyRejected", err)
	}
	if _, err := store.CreateObject(ctx, storage.CurrentKey, []byte("manifest-two")); !errors.Is(err, crypt.ErrKeyRejected) {
		t.Fatalf("current create = %v, want ErrKeyRejected", err)
	}
	if _, err := store.ReplaceObject(ctx, storage.CurrentKey, "etag", []byte("manifest-two")); !errors.Is(err, crypt.ErrKeyRejected) {
		t.Fatalf("current replace = %v, want ErrKeyRejected", err)
	}
	if got := raw.Calls(fake.OpPut); got != 0 {
		t.Fatalf("raw put calls = %d, want 0", got)
	}
	if got := raw.Calls(fake.OpCreate); got != 1 {
		t.Fatalf("raw create calls = %d, want 1 (the seed only)", got)
	}
	if got := raw.Calls(fake.OpReplace); got != 0 {
		t.Fatalf("raw replace calls = %d, want 0", got)
	}
}

// TestEncryptedStoreRejectsTampering proves a corrupted envelope is refused
// as ErrIntegrity for a pack and for current alike.
func TestEncryptedStoreRejectsTampering(t *testing.T) {
	f := newRingFixture(t)
	raw := fake.New("tampering")
	seedEncryptedCurrent(t, raw, f, []byte("manifest-one"))
	store := storage.Encrypted(raw, f.locked(t, f.secret))
	ctx := context.Background()

	key, _, meta := packFixture(t)
	if err := raw.PutObject(ctx, key.String(), bytes.NewReader([]byte("not a real envelope")), meta); err != nil {
		t.Fatalf("seed tampered pack: %v", err)
	}
	rc, _, err := store.ReadObject(ctx, key.String())
	if rc != nil {
		rc.Close()
	}
	if !errors.Is(err, storage.ErrIntegrity) {
		t.Fatalf("tampered pack read = %v, want ErrIntegrity", err)
	}

	rc, info, err := raw.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("raw read current: %v", err)
	}
	rc.Close()
	if _, err := raw.ReplaceObject(ctx, storage.CurrentKey, info.ETag, []byte(`{"version":2,"envelope":"AAAA"}`)); err != nil {
		t.Fatalf("install tampered wrapper: %v", err)
	}
	rc, _, err = store.ReadObject(ctx, storage.CurrentKey)
	if rc != nil {
		rc.Close()
	}
	if !errors.Is(err, storage.ErrIntegrity) {
		t.Fatalf("tampered current read = %v, want ErrIntegrity", err)
	}
}

// rawObject reads one stored object's physical bytes straight from the raw
// store, bypassing the decorator.
func rawObject(t *testing.T, raw storage.ObjectStore, key string) ([]byte, error) {
	t.Helper()
	rc, _, err := raw.ReadObject(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return readAll(t, rc), nil
}

// readCurrentWithETag reads current through the decorator and returns its
// bytes and the raw ETag.
func readCurrentWithETag(t *testing.T, store storage.ObjectStore) ([]byte, storage.ETag, error) {
	t.Helper()
	rc, info, err := store.ReadObject(context.Background(), storage.CurrentKey)
	if err != nil {
		return nil, "", err
	}
	return readAll(t, rc), info.ETag, nil
}

// TestEncryptedStorePreservesBackendContracts runs the shared ObjectStore
// contract suite through the decorator for the fake, the real S3 adapter and
// the hosted adapter: encryption must preserve every backend's conditional
// write, listing and metadata semantics.
func TestEncryptedStorePreservesBackendContracts(t *testing.T) {
	f := newRingFixture(t)

	t.Run("fake", func(t *testing.T) {
		contract.Run(t, func(t *testing.T) storage.ObjectStore {
			prefix := fmt.Sprintf("enc-contract-%d", encryptedContractSeq.Add(1))
			return storage.Encrypted(fake.New(prefix), f.locked(t, f.secret))
		})
	})

	t.Run("s3", func(t *testing.T) {
		suite := tests3.Ensure(t)
		contract.Run(t, func(t *testing.T) storage.ObjectStore {
			mc := suite.StoreConfig()
			st, err := s3store.New(context.Background(), s3store.Config{
				Bucket: tests3.Bucket, Prefix: suite.FreshPrefix("enc-contract"), Region: mc.Region,
				Endpoint: mc.Endpoint, AccessKey: mc.AccessKey, SecretKey: mc.SecretKey,
			})
			if err != nil {
				t.Fatalf("s3store.New: %v", err)
			}
			return storage.Encrypted(st, f.locked(t, f.secret))
		})
	})

	t.Run("hosted", func(t *testing.T) {
		contract.Run(t, func(t *testing.T) storage.ObjectStore {
			g := gatewaytest.Start(t)
			g.AddSpace(contractSpace, 1<<30)
			g.Grant(contractToken, contractSpace, false)
			st, err := httpstore.New(httpstore.Config{
				Endpoint: g.URL(), Space: contractSpace, Prefix: fmt.Sprintf("enc-%d", encryptedContractSeq.Add(1)),
				Token: contractToken, Retries: -1,
			})
			if err != nil {
				t.Fatalf("httpstore.New: %v", err)
			}
			return storage.Encrypted(st, f.locked(t, f.secret))
		})
	})
}
