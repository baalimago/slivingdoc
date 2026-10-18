package notebook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// conversionCanary is note text that must never appear in a stored object of a
// converted notebook (worklogs/26-10-05-encrypted-notebooks/phase-5-conversion.md).
const conversionCanary = "SECRET-CONVERSION-CANARY-5a17"

// conversionKDF is the cheapest accepted Argon2id cost, so the conversion
// scenarios derive doors without the canonical 256 MiB cost.
func conversionKDF() crypt.KDFParams {
	return crypt.KDFParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}
}

// conversionCipher mints the notebook key and one passphrase door a root
// publication writes, and returns the cipher and the secret a reader holds.
func conversionCipher(tb testing.TB, store storage.ObjectStore) (Cipher, []byte) {
	tb.Helper()
	gate, err := crypt.NewKDFGateForCost(1, conversionKDF())
	if err != nil {
		tb.Fatalf("NewKDFGateForCost() = %v", err)
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		tb.Fatalf("GenerateNotebookKey() = %v", err)
	}
	secret := []byte("conversion passphrase 9c2f")
	door, err := crypt.WrapKey(context.Background(), gate, key, secret, storage.CurrentKey, crypt.DoorPassphrase, conversionKDF())
	if err != nil {
		tb.Fatalf("WrapKey() = %v", err)
	}
	seal, err := crypt.NewUnlockedKeyring(key, nil, gate)
	if err != nil {
		tb.Fatalf("NewUnlockedKeyring() = %v", err)
	}
	return Cipher{Store: store, Seal: seal, Doors: []crypt.Door{door}}, secret
}

// conversionReader opens the encrypted current of a converted notebook.
func conversionReader(tb testing.TB, secret []byte) *crypt.Keyring {
	tb.Helper()
	gate, err := crypt.NewKDFGateForCost(1, conversionKDF())
	if err != nil {
		tb.Fatalf("NewKDFGateForCost() = %v", err)
	}
	ring, err := crypt.NewKeyring([]crypt.Candidate{{Source: crypt.SourceEnvironment, Secret: secret}}, gate)
	if err != nil {
		tb.Fatalf("NewKeyring() = %v", err)
	}
	return ring
}

// readStoreObject reads one protocol object body.
func readStoreObject(tb testing.TB, store storage.ObjectStore, key string) []byte {
	tb.Helper()
	rc, _, err := store.ReadObject(context.Background(), key)
	if err != nil {
		tb.Fatalf("ReadObject(%s) = %v", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		tb.Fatalf("read %s body = %v", key, err)
	}
	return data
}

// readEncryptedManifest decrypts and decodes the stored current of a converted
// notebook, proving it is the version-two wrapper and not plaintext.
func readEncryptedManifest(tb testing.TB, store storage.ObjectStore, secret []byte) storage.Manifest {
	tb.Helper()
	encoding, envelope, err := storage.ClassifyCurrent(readStoreObject(tb, store, storage.CurrentKey))
	if err != nil {
		tb.Fatalf("ClassifyCurrent() = %v", err)
	}
	if encoding != storage.CurrentEncrypted {
		tb.Fatalf("stored current encoding = %v, want the encrypted wrapper", encoding)
	}
	plaintext, _, err := conversionReader(tb, secret).Open(context.Background(), storage.CurrentKey, envelope)
	if err != nil {
		tb.Fatalf("Open(current) = %v", err)
	}
	m, err := storage.DecodeManifest(plaintext)
	if err != nil {
		tb.Fatalf("DecodeManifest() = %v", err)
	}
	return m
}

// convertedSnapshot reads the accepted tree of a root manifest as notebook
// files, so a test can prove a conversion preserved the merged visible state.
func convertedSnapshot(t *testing.T, nb *Notebook, m storage.Manifest) map[string]string {
	t.Helper()
	commit, err := nb.ws.Repo().ReadCommit(m.Head)
	if err != nil {
		t.Fatalf("ReadCommit(%s) = %v", m.Head, err)
	}
	snap, err := git.ReadSnapshot(nb.ws.Repo(), commit.Tree)
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	out := make(map[string]string, len(snap.Files))
	for _, f := range snap.Files {
		out[f.Path] = string(f.Data)
	}
	return out
}

// storedKeys lists every protocol key of a store. The list callback must not
// read objects: the fake store holds its lock for the whole callback.
func storedKeys(t *testing.T, store storage.ObjectStore) []string {
	t.Helper()
	var keys []string
	if err := store.ListObjects(context.Background(), "", func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("ListObjects() = %v", err)
	}
	return keys
}

// assertNoPlaintextAtRest fails when any stored object body carries a
// forbidden plaintext fragment.
func assertNoPlaintextAtRest(t *testing.T, store storage.ObjectStore, forbidden ...string) {
	t.Helper()
	for _, key := range storedKeys(t, store) {
		data := readStoreObject(t, store, key)
		for _, want := range forbidden {
			if bytes.Contains(data, []byte(want)) {
				t.Fatalf("stored object %s contains plaintext %q", key, want)
			}
		}
	}
}

// assertEncryptedPack proves a stored pack is an envelope, never raw note
// bytes.
func assertEncryptedPack(t *testing.T, store storage.ObjectStore, key string) {
	t.Helper()
	if data := readStoreObject(t, store, key); !bytes.HasPrefix(data, []byte("SLIVENC\x00")) {
		t.Fatalf("stored pack %s is not an envelope", key)
	}
}

// conversionStore records the order of pack uploads and current publications
// and can refuse an oversized body or every conditional current replacement.
type conversionStore struct {
	storage.ObjectStore
	mu           sync.Mutex
	sequence     []string
	limit        int
	failReplaces bool
}

func (s *conversionStore) record(entry string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence = append(s.sequence, entry)
}

func (s *conversionStore) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.record("put " + key)
	s.mu.Lock()
	limit := s.limit
	s.mu.Unlock()
	if limit > 0 && len(data) > limit {
		return fmt.Errorf("conversion store: body of %d bytes: %w", len(data), storage.ErrTooLarge)
	}
	return s.ObjectStore.PutObject(ctx, key, bytes.NewReader(data), meta)
}

func (s *conversionStore) CreateObject(ctx context.Context, key string, data []byte) (storage.ETag, error) {
	s.record("create " + key)
	return s.ObjectStore.CreateObject(ctx, key, data)
}

func (s *conversionStore) ReplaceObject(ctx context.Context, key string, etag storage.ETag, data []byte) (storage.ETag, error) {
	s.record("replace " + key)
	s.mu.Lock()
	fail := s.failReplaces
	s.mu.Unlock()
	if fail {
		return "", fmt.Errorf("conversion store: replace %s: %w", key, storage.ErrPreconditionFailed)
	}
	return s.ObjectStore.ReplaceObject(ctx, key, etag, data)
}

func (s *conversionStore) setLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = limit
}

func (s *conversionStore) setFailReplaces(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failReplaces = fail
}

func (s *conversionStore) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sequence...)
}

// TestEncryptInitEmptyNotebook proves conversion of an empty remote publishes
// the encrypted root checkpoint before the encrypted current.
func TestEncryptInitEmptyNotebook(t *testing.T) {
	store := &conversionStore{ObjectStore: fake.New("")}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
	writeLocal(t, w, map[string]string{"a.md": conversionCanary + "\n"})
	cipher, secret := conversionCipher(t, store)

	res, err := nb.EncryptInit(context.Background(), cipher, "encrypt init")
	if err != nil {
		t.Fatalf("EncryptInit() = %v", err)
	}
	if res.Generation != 1 {
		t.Fatalf("generation = %d, want 1", res.Generation)
	}

	m := readEncryptedManifest(t, store, secret)
	if m.Generation != 1 || len(m.Increments) != 0 || len(m.Retained) != 0 {
		t.Fatalf("root manifest = generation %d, %d increments, %d retained; want one root checkpoint",
			m.Generation, len(m.Increments), len(m.Retained))
	}
	assertEncryptedPack(t, store, m.Checkpoint.Key.String())

	upload, create := -1, -1
	for i, entry := range store.recorded() {
		switch entry {
		case "put " + m.Checkpoint.Key.String():
			upload = i
		case "create " + storage.CurrentKey:
			create = i
		}
	}
	if upload < 0 || create < 0 || upload > create {
		t.Fatalf("operation order = %v, want the checkpoint upload before the current creation", store.recorded())
	}
	assertNoPlaintextAtRest(t, store, conversionCanary)
}

// TestEncryptInitConvertsHistory proves a plaintext notebook with accepted
// history converts to its current tree with ciphertext at rest and no
// plaintext in the active objects.
func TestEncryptInitConvertsHistory(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, w, map[string]string{"a.md": conversionCanary + " one\n"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	writeLocal(t, w, map[string]string{"b.md": conversionCanary + " two\n"})
	cipher, secret := conversionCipher(t, store)

	res, err := nb.EncryptInit(context.Background(), cipher, "encrypt init")
	if err != nil {
		t.Fatalf("EncryptInit() = %v", err)
	}
	if res.Generation != 2 {
		t.Fatalf("generation = %d, want 2", res.Generation)
	}

	m := readEncryptedManifest(t, store, secret)
	if len(m.Increments) != 0 || len(m.Retained) != 0 {
		t.Fatalf("root manifest kept %d increments and %d retained generations, want none",
			len(m.Increments), len(m.Retained))
	}
	got := convertedSnapshot(t, nb, m)
	if got["a.md"] != conversionCanary+" one\n" || got["b.md"] != conversionCanary+" two\n" {
		t.Fatalf("accepted tree = %v, want both visible files", got)
	}
	assertEncryptedPack(t, store, m.Checkpoint.Key.String())
	assertNoPlaintextAtRest(t, store, conversionCanary)
	// The superseded plaintext history is unreferenced, so cleanup removed it:
	// only the new checkpoint pack and the encrypted current remain.
	if keys := storedKeys(t, store); len(keys) != 2 {
		t.Fatalf("stored objects = %v, want the encrypted current and one checkpoint pack", keys)
	}
}

// TestEncryptInitRequiresCurrentBaseline proves a non-empty remote without an
// established workspace baseline is refused before any write.
func TestEncryptInitRequiresCurrentBaseline(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	seed, sw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, sw, map[string]string{"a.md": conversionCanary + "\n"})
	pullOK(t, seed)
	commitOK(t, seed, "first")
	before := readStoreObject(t, store, storage.CurrentKey)
	count := store.ObjectCount()

	converter, _, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	cipher, _ := conversionCipher(t, store)
	ne := assertErrorCode(t, errOnly(converter.EncryptInit(context.Background(), cipher, "encrypt init")), CodeInvalidRequest)
	if ne.Reason != ReasonPullRequired {
		t.Fatalf("reason = %s, want PULL_REQUIRED (error: %v)", ne.Reason, ne)
	}
	if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
		t.Fatal("the refused conversion changed the accepted current")
	}
	if got := store.ObjectCount(); got != count {
		t.Fatalf("stored objects = %d, want %d unchanged", got, count)
	}
}

// TestEncryptInitFailurePreservesCurrent proves a refused checkpoint upload
// leaves the accepted current and the stored objects unchanged.
func TestEncryptInitFailurePreservesCurrent(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": conversionCanary + "\n"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	before := readStoreObject(t, store, storage.CurrentKey)
	count := store.ObjectCount()

	store.FailNext(fake.OpPut, storage.ErrQuotaExceeded)
	cipher, _ := conversionCipher(t, store)
	ne := assertErrorCode(t, errOnly(nb.EncryptInit(context.Background(), cipher, "encrypt init")), CodeStorageFailure)
	if ne.Reason != ReasonStorageFull {
		t.Fatalf("reason = %s, want STORAGE_FULL (error: %v)", ne.Reason, ne)
	}
	if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
		t.Fatal("the refused conversion changed the accepted current")
	}
	if got := store.ObjectCount(); got != count {
		t.Fatalf("stored objects = %d, want %d unchanged", got, count)
	}
}

// TestEncryptInitPackLimit proves ciphertext above the store's object limit is
// refused as OBJECT_TOO_LARGE before any manifest is published.
func TestEncryptInitPackLimit(t *testing.T) {
	store := &conversionStore{ObjectStore: fake.New("")}
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": conversionCanary + "\n"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	before := readStoreObject(t, store, storage.CurrentKey)

	store.setLimit(1)
	cipher, _ := conversionCipher(t, store)
	ne := assertErrorCode(t, errOnly(nb.EncryptInit(context.Background(), cipher, "encrypt init")), CodeStorageFailure)
	if ne.Reason != ReasonObjectTooLarge {
		t.Fatalf("reason = %s, want OBJECT_TOO_LARGE (error: %v)", ne.Reason, ne)
	}
	if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
		t.Fatal("the oversized checkpoint changed the accepted current")
	}
}

// TestEncryptInitCleanupFailure proves a cleanup failure after an accepted
// conversion never changes the successful result.
func TestEncryptInitCleanupFailure(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": conversionCanary + "\n"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	cipher, secret := conversionCipher(t, store)

	store.FailNext(fake.OpDelete, errors.New("injected cleanup delete failure"))
	res, err := nb.EncryptInit(context.Background(), cipher, "encrypt init")
	if err != nil {
		t.Fatalf("EncryptInit() with a failing cleanup = %v, want the accepted result", err)
	}
	if res.Generation != 2 {
		t.Fatalf("generation = %d, want 2", res.Generation)
	}
	if nb.Metrics().CleanupErrors.Load() == 0 {
		t.Fatal("the failed cleanup was not recorded in metrics")
	}
	m := readEncryptedManifest(t, store, secret)
	if m.Generation != 2 {
		t.Fatalf("accepted manifest generation = %d, want 2", m.Generation)
	}
	if keys := storedKeys(t, store); len(keys) < 3 {
		t.Fatalf("stored objects = %v, want the aborted cleanup to leave the old pack", keys)
	}
}

// TestEncryptInitContention proves a lost conditional publication re-reads and
// merges, and an exhausted retry budget returns REMOTE_BUSY without replacing
// the newer accepted state.
func TestEncryptInitContention(t *testing.T) {
	t.Run("retry merges the winner", func(t *testing.T) {
		store := fake.New("")
		ids := &testIDSource{}
		nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
		writeLocal(t, w, map[string]string{"a.md": conversionCanary + "\n"})
		pullOK(t, nb)
		commitOK(t, nb, "first")
		writeLocal(t, w, map[string]string{"b.md": conversionCanary + " two\n"})

		store.FailNextKey(fake.OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
		cipher, secret := conversionCipher(t, store)
		res, err := nb.EncryptInit(context.Background(), cipher, "encrypt init")
		if err != nil {
			t.Fatalf("EncryptInit() under one lost CAS = %v", err)
		}
		if res.Generation != 2 {
			t.Fatalf("generation = %d, want 2", res.Generation)
		}
		m := readEncryptedManifest(t, store, secret)
		if got := convertedSnapshot(t, nb, m); got["b.md"] != conversionCanary+" two\n" {
			t.Fatalf("accepted tree = %v, want the retried visible change", got)
		}
	})

	t.Run("exhaustion keeps the accepted state", func(t *testing.T) {
		store := &conversionStore{ObjectStore: fake.New("")}
		ids := &testIDSource{}
		seed, sw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
		writeLocal(t, sw, map[string]string{"a.md": conversionCanary + "\n"})
		pullOK(t, seed)
		commitOK(t, seed, "first")
		before := readStoreObject(t, store, storage.CurrentKey)

		converter, _, _ := newNotebook(t, nbConfig{store: store, ids: ids})
		pullOK(t, converter)
		store.setFailReplaces(true)
		cipher, _ := conversionCipher(t, store)
		ne := assertErrorCode(t, errOnly(converter.EncryptInit(context.Background(), cipher, "encrypt init")), CodeRemoteBusy)
		if ne.Reason != ReasonRetriesExhausted {
			t.Fatalf("reason = %s, want RETRIES_EXHAUSTED (error: %v)", ne.Reason, ne)
		}
		if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
			t.Fatal("the exhausted conversion replaced the accepted current")
		}
	})
}

// TestEncryptInitMergeConflict proves a local and remote change conflict
// materializes markers and publishes no checkpoint.
func TestEncryptInitMergeConflict(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	writer, ww, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, ww, map[string]string{"a.md": "base\n"})
	pullOK(t, writer)
	commitOK(t, writer, "base")

	converter, cw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	pullOK(t, converter)
	writeLocal(t, cw, map[string]string{"a.md": "local\n"})
	writeLocal(t, ww, map[string]string{"a.md": "remote\n"})
	commitOK(t, writer, "remote")
	before := readStoreObject(t, store, storage.CurrentKey)
	count := store.ObjectCount()

	cipher, _ := conversionCipher(t, store)
	ne := assertErrorCode(t, errOnly(converter.EncryptInit(context.Background(), cipher, "encrypt init")), CodeContentConflict)
	if ne.Reason != ReasonMergeConflict {
		t.Fatalf("reason = %s, want MERGE_CONFLICT (error: %v)", ne.Reason, ne)
	}
	if got := readLocal(t, cw, "a.md"); !bytes.Contains([]byte(got), []byte("<<<<<<<")) {
		t.Fatalf("a.md = %q, want the materialized conflict markers", got)
	}
	if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
		t.Fatal("the conflicted conversion published a checkpoint")
	}
	if got := store.ObjectCount(); got != count {
		t.Fatalf("stored objects = %d, want %d (no checkpoint pack)", got, count)
	}
}

// TestEncryptInitRejectsMarkersAndProtectedChanges proves conversion refuses
// unresolved markers and a protected-path edit before publishing.
func TestEncryptInitRejectsMarkersAndProtectedChanges(t *testing.T) {
	t.Run("unresolved markers", func(t *testing.T) {
		store := fake.New("")
		nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
		writeLocal(t, w, map[string]string{"a.md": "<<<<<<< local\nours\n=======\ntheirs\n>>>>>>> remote\n"})
		cipher, _ := conversionCipher(t, store)
		ne := assertErrorCode(t, errOnly(nb.EncryptInit(context.Background(), cipher, "encrypt init")), CodeContentConflict)
		if ne.Reason != ReasonUnresolvedMarkers {
			t.Fatalf("reason = %s, want UNRESOLVED_MARKERS (error: %v)", ne.Reason, ne)
		}
		if _, ok := readCurrentOrAbsent(t, store); ok {
			t.Fatal("the refused conversion published a current object")
		}
		if got := store.ObjectCount(); got != 0 {
			t.Fatalf("stored objects = %d, want none", got)
		}
	})

	t.Run("protected path", func(t *testing.T) {
		store := fake.New("")
		ids := &testIDSource{}
		seed, sw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
		writeLocal(t, sw, map[string]string{"protected.md": "original\n"})
		pullOK(t, seed)
		commitOK(t, seed, "first")
		before := readStoreObject(t, store, storage.CurrentKey)

		converter, cw, _ := newNotebook(t, nbConfig{store: store, ids: ids, readOnly: []string{"protected.md"}})
		pullOK(t, converter)
		writeLocal(t, cw, map[string]string{"protected.md": "changed\n"})
		cipher, _ := conversionCipher(t, store)
		ne := assertErrorCode(t, errOnly(converter.EncryptInit(context.Background(), cipher, "encrypt init")), CodeInvalidRequest)
		if ne.Reason != ReasonReadOnlyPath {
			t.Fatalf("reason = %s, want READ_ONLY_PATH (error: %v)", ne.Reason, ne)
		}
		if got := readLocal(t, cw, "protected.md"); got != "original\n" {
			t.Fatalf("protected.md = %q, want it restored to the baseline", got)
		}
		if got := readStoreObject(t, store, storage.CurrentKey); !bytes.Equal(got, before) {
			t.Fatal("the refused conversion changed the accepted current")
		}
	})
}

// readCurrentOrAbsent reads the raw current object, reporting absence.
func readCurrentOrAbsent(t *testing.T, store storage.ObjectStore) ([]byte, bool) {
	t.Helper()
	rc, _, err := store.ReadObject(context.Background(), storage.CurrentKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("ReadObject(current) = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read current body = %v", err)
	}
	return data, true
}
