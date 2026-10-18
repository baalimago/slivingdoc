package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// encryptedCanary is note text that must never appear in a stored object of
// an encrypted notebook: its presence would prove a plaintext leak.
const encryptedCanary = "SECRET-NOTE-CANARY-8f31"

func encryptedKDFParams() crypt.KDFParams {
	return crypt.KDFParams{MemoryKiB: 64 << 10, Iterations: 1, Parallelism: 1}
}

// encryptedKeys mints the notebook key and passphrase door a converted
// notebook understands, and rebuilds the locked keyring a reader holds.
type encryptedKeys struct {
	gate   *crypt.KDFGate
	key    crypt.NotebookKey
	door   crypt.Door
	secret []byte
}

func newEncryptedKeys(t *testing.T) *encryptedKeys {
	t.Helper()
	gate, err := crypt.NewKDFGate(1)
	if err != nil {
		t.Fatalf("NewKDFGate: %v", err)
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey: %v", err)
	}
	secret := []byte("a passphrase for the notebook")
	door, err := crypt.WrapKey(context.Background(), gate, key, secret, storage.CurrentKey, crypt.DoorPassphrase, encryptedKDFParams())
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	return &encryptedKeys{gate: gate, key: key, door: door, secret: secret}
}

// reader is the keyring a process holds when it knows only the passphrase.
func (k *encryptedKeys) reader(t *testing.T) *crypt.Keyring {
	t.Helper()
	ring, err := crypt.NewKeyring([]crypt.Candidate{{Source: crypt.SourcePrompt, Secret: k.secret}}, k.gate)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return ring
}

// wrongRing is the keyring of a process whose secret opens no door: it
// holds a candidate, so the refusal is KEY_REJECTED rather than
// KEY_REQUIRED.
func wrongRing(t *testing.T) *crypt.Keyring {
	t.Helper()
	return ringFor(t, crypt.Candidate{Source: crypt.SourcePrompt, Secret: []byte("a secret that opens no door")})
}

// emptyRing is the keyring of a process configured with no secret at all.
func emptyRing(t *testing.T) *crypt.Keyring {
	t.Helper()
	return ringFor(t)
}

// ringFor returns the keyring of a process holding the given candidate
// secrets.
func ringFor(t *testing.T, candidates ...crypt.Candidate) *crypt.Keyring {
	t.Helper()
	gate, err := crypt.NewKDFGate(1)
	if err != nil {
		t.Fatalf("NewKDFGate: %v", err)
	}
	ring, err := crypt.NewKeyring(candidates, gate)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return ring
}

// sealer is the keyring that converts a plaintext notebook at rest.
func (k *encryptedKeys) sealer(t *testing.T) *crypt.Keyring {
	t.Helper()
	ring, err := crypt.NewUnlockedKeyring(k.key, nil, k.gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring: %v", err)
	}
	return ring
}

// newEncryptedHarness publishes one plaintext generation over a fresh fake,
// converts every stored object to ciphertext exactly as encrypt init will,
// and returns a harness whose process knows only the passphrase. The
// harness holds the raw store and the service decorates it, so the
// scenarios exercise the wiring a released process has
// (architecture/encryption.md, Process keyring).
func newEncryptedHarness(t *testing.T, k *encryptedKeys) (*Harness, *fake.Store) {
	t.Helper()
	const prefix = "encrypted"
	base := fake.New(prefix)
	seed := NewHarness(t, HarnessConfig{Store: base, Prefix: prefix, Bucket: "test-bucket"})
	CommitFirst(t, seed, "a.md", "alpha\n", "first")
	encryptNotebookAtRest(t, base, k)
	h := NewHarness(t, HarnessConfig{Store: base, Prefix: prefix, Bucket: "test-bucket", Keyring: k.reader(t)})
	return h, base
}

// encryptNotebookAtRest rewrites every pack and current of a published
// plaintext notebook as ciphertext, so the decorated store reads it as an
// encrypted notebook.
func encryptNotebookAtRest(t *testing.T, raw storage.ObjectStore, k *encryptedKeys) {
	t.Helper()
	ctx := context.Background()
	rc, info, err := raw.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read plaintext current: %v", err)
	}
	current := readBody(t, rc)
	m, err := storage.DecodeManifest(current)
	if err != nil {
		t.Fatalf("decode plaintext manifest: %v", err)
	}
	ring := k.sealer(t)
	keys := []string{m.Checkpoint.Key.String()}
	for _, inc := range m.Increments {
		keys = append(keys, inc.Key.String())
	}
	for _, key := range keys {
		rc, _, err := raw.ReadObject(ctx, key)
		if err != nil {
			t.Fatalf("read plaintext pack %s: %v", key, err)
		}
		envelope, err := ring.Seal(key, crypt.KindPack, readBody(t, rc), nil)
		if err != nil {
			t.Fatalf("seal pack %s: %v", key, err)
		}
		if err := raw.PutObject(ctx, key, bytes.NewReader(envelope), storage.Metadata{}); err != nil {
			t.Fatalf("store encrypted pack %s: %v", key, err)
		}
	}
	envelope, err := ring.Seal(storage.CurrentKey, crypt.KindCurrent, current, []crypt.Door{k.door})
	if err != nil {
		t.Fatalf("seal current: %v", err)
	}
	wrapped, err := storage.WrapCurrentEnvelope(envelope)
	if err != nil {
		t.Fatalf("WrapCurrentEnvelope: %v", err)
	}
	if _, err := raw.ReplaceObject(ctx, storage.CurrentKey, info.ETag, wrapped); err != nil {
		t.Fatalf("store encrypted current: %v", err)
	}
}

// TestScenarioEncryptedPullCommit proves the two-machine contract: one
// process converts and publishes, and a second process with only the
// passphrase reads the accepted state. The visible workspace stays
// plaintext; the store holds ciphertext.
func TestScenarioEncryptedPullCommit(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	writer, base := newEncryptedHarness(t, k)
	path := writer.WorkspaceRoot()

	writer.AssertOK(t, writer.Pull(""))
	if got := writer.ReadFile(path + "/a.md"); got != "alpha\n" {
		t.Fatalf("pulled note = %q, want %q", got, "alpha\n")
	}
	writer.WriteFile(path+"/a.md", "alpha\n"+encryptedCanary+"\n")
	writer.AssertOK(t, writer.Commit("", "second"))

	reader := NewHarness(t, HarnessConfig{Store: base, Prefix: "encrypted", Bucket: "test-bucket", Keyring: k.reader(t)})
	rpath := reader.WorkspaceRoot()
	reader.AssertOK(t, reader.Pull(""))
	if got := reader.ReadFile(rpath + "/a.md"); !strings.Contains(got, encryptedCanary) {
		t.Fatalf("second machine pulled %q, want the committed note", got)
	}
	if got := writer.ReadFile(path + "/a.md"); !strings.Contains(got, encryptedCanary) {
		t.Fatalf("writer workspace = %q, want plaintext", got)
	}
}

// TestScenarioPlaintextNotebookIgnoresKeyMaterial proves a process that
// holds a secret of another notebook still pulls and commits a plaintext
// notebook: the decorator resolves the plaintext mode from current, so
// every stored object stays readable without any key.
func TestScenarioPlaintextNotebookIgnoresKeyMaterial(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	const prefix = "plaintext-with-key-material"
	base := fake.New(prefix)
	h := NewHarness(t, HarnessConfig{Store: base, Prefix: prefix, Bucket: "test-bucket", Keyring: k.reader(t)})
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))
	h.WriteFile(path+"/a.md", "alpha\n"+encryptedCanary+"\n")
	h.AssertOK(t, h.Commit("", "first"))
	if got := h.ReadFile(path + "/a.md"); !strings.Contains(got, encryptedCanary) {
		t.Fatalf("workspace = %q, want the committed note", got)
	}

	ctx := context.Background()
	rc, _, err := base.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read stored current: %v", err)
	}
	if current := readBody(t, rc); !bytes.HasPrefix(current, []byte(`{"version":1,`)) {
		t.Fatalf("stored current is not a plaintext version-one manifest: %s", current)
	}
	checkpoint := manifestThrough(t, storage.Encrypted(base, k.reader(t))).Checkpoint.Key.String()
	rc, _, err = base.ReadObject(ctx, checkpoint)
	if err != nil {
		t.Fatalf("read stored pack: %v", err)
	}
	if pack := readBody(t, rc); bytes.HasPrefix(pack, []byte("SLIVENC\x00")) {
		t.Fatal("the plaintext notebook stored an encrypted pack")
	}
}

// TestScenarioEncryptedCommitContainsCiphertext proves no plaintext note
// content reaches the store and that the decrypted manifest descriptor
// still binds the plaintext pack the notebook uploaded.
func TestScenarioEncryptedCommitContainsCiphertext(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	h, base := newEncryptedHarness(t, k)
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))
	h.WriteFile(path+"/b.md", encryptedCanary+"\n")
	h.AssertOK(t, h.Commit("", "second"))

	ctx := context.Background()
	m := manifestThrough(t, storage.Encrypted(base, k.reader(t)))
	type descriptor struct {
		key  storage.Key
		sha  storage.SHA256
		size uint64
	}
	descriptors := []descriptor{{m.Checkpoint.Key, m.Checkpoint.SHA256, m.Checkpoint.Size}}
	for _, inc := range m.Increments {
		descriptors = append(descriptors, descriptor{inc.Key, inc.SHA256, inc.Size})
	}
	for _, d := range descriptors {
		rc, _, err := base.ReadObject(ctx, d.key.String())
		if err != nil {
			t.Fatalf("read stored pack %s: %v", d.key, err)
		}
		ciphertext := readBody(t, rc)
		if bytes.Contains(ciphertext, []byte(encryptedCanary)) || !bytes.HasPrefix(ciphertext, []byte("SLIVENC\x00")) {
			t.Fatalf("stored pack %s is not ciphertext", d.key)
		}
		plaintext := readBody(t, mustRead(t, storage.Encrypted(base, k.reader(t)), d.key.String()))
		if uint64(len(plaintext)) != d.size || storage.SHA256(sha256.Sum256(plaintext)) != d.sha {
			t.Fatalf("descriptor for %s does not bind the plaintext pack", d.key)
		}
	}
	rc, _, err := base.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read stored current: %v", err)
	}
	current := readBody(t, rc)
	if bytes.Contains(current, []byte(encryptedCanary)) || !bytes.HasPrefix(current, []byte(`{"version":2,"envelope":"`)) {
		t.Fatalf("stored current is not the encrypted wrapper: %s", current)
	}
}

// TestScenarioEncryptedCommitUploadBeforeManifest proves the pack upload
// precedes the manifest publication: when the manifest write is refused,
// the new ciphertext pack is already stored and the accepted current is
// unchanged.
func TestScenarioEncryptedCommitUploadBeforeManifest(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	h, base := newEncryptedHarness(t, k)
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))

	ctx := context.Background()
	before := base.ObjectCount()
	rc, info, err := base.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read current before commit: %v", err)
	}
	beforeCurrent := readBody(t, rc)

	h.WriteFile(path+"/c.md", encryptedCanary+"\n")
	h.Faults().FailAlways(OpReplace, storage.CurrentKey, storage.ErrAccessDenied)
	res := h.Commit("", "second")
	env := DecodeEnvelope(t, ToolCall{Tool: ToolCommit}, res)
	if env.Code != "STORAGE_FAILURE" {
		t.Fatalf("commit = %s/%s, want a storage refusal", env.Code, env.Reason)
	}

	if got := base.ObjectCount(); got != before+1 {
		t.Fatalf("store objects = %d, want %d (the pack upload must precede the manifest)", got, before+1)
	}
	rc, afterInfo, err := base.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read current after commit: %v", err)
	}
	if after := readBody(t, rc); !bytes.Equal(after, beforeCurrent) || afterInfo.ETag != info.ETag {
		t.Fatal("the refused manifest write changed the accepted current")
	}
}

// TestScenarioEncryptedPackLimitPreservesCurrent proves an encrypted pack
// that the store refuses as too large leaves the accepted current
// unchanged, exactly as a plaintext too-large pack does.
func TestScenarioEncryptedPackLimitPreservesCurrent(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	const prefix = "encrypted-limit"
	base := fake.New(prefix)
	seed := NewHarness(t, HarnessConfig{Store: base, Prefix: prefix, Bucket: "test-bucket"})
	CommitFirst(t, seed, "a.md", "alpha\n", "first")
	encryptNotebookAtRest(t, base, k)

	h := NewHarness(t, HarnessConfig{
		Store: bodyLimitStore{ObjectStore: base, limit: 1}, Prefix: prefix, Bucket: "test-bucket",
		Keyring: k.reader(t),
	})
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))
	h.WriteFile(path+"/b.md", encryptedCanary+"\n")

	res := h.Commit("", "second")
	env := DecodeEnvelope(t, ToolCall{Tool: ToolCommit}, res)
	if env.Code != "STORAGE_FAILURE" || env.Reason != "OBJECT_TOO_LARGE" {
		t.Fatalf("commit = %s/%s, want STORAGE_FAILURE/OBJECT_TOO_LARGE", env.Code, env.Reason)
	}
	if _, _, err := base.ReadObject(context.Background(), storage.CurrentKey); err != nil {
		t.Fatalf("current was lost: %v", err)
	}
	if gen := manifestThrough(t, storage.Encrypted(base, k.reader(t))).Generation; gen != 1 {
		t.Fatalf("accepted generation = %d, want 1 (unchanged)", gen)
	}
}

// TestScenarioEncryptedManifestRollback proves the notebook refuses a
// stored current below the workspace's recorded baseline as HISTORY_INVALID
// and keeps the local baseline, even when the older manifest is a valid
// encrypted one.
func TestScenarioEncryptedManifestRollback(t *testing.T) {
	t.Parallel()
	k := newEncryptedKeys(t)
	h, base := newEncryptedHarness(t, k)
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))

	ctx := context.Background()
	gen1 := readBody(t, mustRead(t, base, storage.CurrentKey))
	h.WriteFile(path+"/a.md", "second\n")
	h.AssertOK(t, h.Commit("", "second"))
	AssertRemoteGeneration(t, h, 2)

	rc, info, err := base.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		t.Fatalf("read the accepted current: %v", err)
	}
	readBody(t, rc)
	if _, err := base.ReplaceObject(ctx, storage.CurrentKey, info.ETag, gen1); err != nil {
		t.Fatalf("install the older current: %v", err)
	}

	res := h.Pull("")
	env := DecodeEnvelope(t, ToolCall{Tool: ToolPull}, res)
	if env.Code != "STORAGE_INTEGRITY" || env.Reason != "HISTORY_INVALID" {
		t.Fatalf("pull = %s/%s, want STORAGE_INTEGRITY/HISTORY_INVALID", env.Code, env.Reason)
	}
	AssertRemoteGeneration(t, h, 2)
}

// bodyLimitStore refuses a stored body above limit, like the hosted
// gateway's ciphertext object limit, while every other method passes
// through.
type bodyLimitStore struct {
	storage.ObjectStore
	limit int
}

func (s bodyLimitStore) PutObject(ctx context.Context, key string, r io.Reader, meta storage.Metadata) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(data) > s.limit {
		return fmt.Errorf("store: body of %d bytes: %w", len(data), storage.ErrTooLarge)
	}
	return s.ObjectStore.PutObject(ctx, key, bytes.NewReader(data), meta)
}

// manifestThrough decodes the accepted manifest through a store view that
// opens the stored current, for a notebook whose objects are ciphertext.
func manifestThrough(t *testing.T, store storage.ObjectStore) storage.Manifest {
	t.Helper()
	data := readBody(t, mustRead(t, store, storage.CurrentKey))
	m, err := storage.DecodeManifest(data)
	if err != nil {
		t.Fatalf("decode accepted manifest: %v", err)
	}
	return m
}

// mustRead reads one object through a store, failing the test on any error.
func mustRead(t *testing.T, store storage.ObjectStore, key string) io.ReadCloser {
	t.Helper()
	rc, _, err := store.ReadObject(context.Background(), key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return rc
}

// readBody drains and closes a streamed body, failing the test on any error.
func readBody(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(rc)
	if err != nil {
		rc.Close()
		t.Fatalf("read body: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return data
}
