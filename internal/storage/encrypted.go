package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/baalimago/slivingdoc/internal/crypt"
)

// Encrypted decorates a raw ObjectStore so the notebook reads and writes
// plaintext while the store holds ciphertext (architecture/storage.md,
// Encrypted store). It is the only place a notebook body meets the
// envelope: PutObject, CreateObject, and ReplaceObject seal their body
// before the raw store sees it, and ReadObject decrypts the body it reads.
// Listing and deletion pass through unchanged, and so does a plaintext
// version-one notebook or an absent current.
//
// The decorator decides the notebook mode lazily. A keyring that already
// holds the notebook key (NewUnlockedKeyring) seals at once, which is how
// encrypt init and rekey publish their first encrypted objects. Otherwise
// the first operation reads current: a version-two wrapper is opened, its
// door list is remembered for later current writes, and every later read
// and write is encrypted; a version-one manifest or an absent current
// leaves the notebook plaintext.
//
// A current body the caller already wrapped (encrypt init's, or a door
// rewrite's) is written unchanged, so the two never double-wrap; every
// other current body is sealed with the doors of the encrypted current
// this store opened last. Packs never carry doors: a pack is always opened
// through the key current resolved. Object metadata describes the stored
// ciphertext, and the decorator translates the sizes and digests back to
// plaintext for the notebook. A conditional-write loss and every other
// raw store result are returned unchanged: the decorator never retries.
func Encrypted(store ObjectStore, keyring *crypt.Keyring) ObjectStore {
	return &encryptedStore{store: store, keyring: keyring}
}

// storeMode is the resolved encryption mode of a decorated store.
type storeMode uint8

const (
	// modeUnresolved means the first operation has not read current yet.
	modeUnresolved storeMode = iota
	// modePlaintext means the notebook is not encrypted: bodies pass
	// through unchanged.
	modePlaintext
	// modeEncrypted means the notebook is encrypted: bodies are sealed and
	// opened.
	modeEncrypted
)

type encryptedStore struct {
	store   ObjectStore
	keyring *crypt.Keyring

	mu    sync.Mutex
	mode  storeMode
	doors []crypt.Door
}

var _ ObjectStore = (*encryptedStore)(nil)

func (e *encryptedStore) ReadObject(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if key == CurrentKey {
		return e.readCurrent(ctx, key)
	}
	mode, err := e.ensureMode(ctx)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	rc, info, err := e.store.ReadObject(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	if mode != modeEncrypted {
		return rc, info, nil
	}
	return e.open(ctx, key, rc, info)
}

func (e *encryptedStore) PutObject(ctx context.Context, key string, r io.Reader, meta Metadata) error {
	if key == CurrentKey {
		return fmt.Errorf("storage: encrypt %s: %w: use CreateObject or ReplaceObject", key, ErrIntegrity)
	}
	mode, err := e.ensureMode(ctx)
	if err != nil {
		return err
	}
	if mode != modeEncrypted {
		return e.store.PutObject(ctx, key, r, meta)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("storage: read upload body: %w", ErrTransport)
	}
	envelope, err := e.keyring.Seal(key, crypt.KindPack, body, nil)
	if err != nil {
		return e.envelopeError(key, err)
	}
	return e.store.PutObject(ctx, key, bytes.NewReader(envelope), ciphertextMeta(meta, envelope))
}

func (e *encryptedStore) CreateObject(ctx context.Context, key string, data []byte) (ETag, error) {
	return e.writeObject(ctx, key, data, func(body []byte) (ETag, error) {
		return e.store.CreateObject(ctx, key, body)
	})
}

func (e *encryptedStore) ReplaceObject(ctx context.Context, key string, etag ETag, data []byte) (ETag, error) {
	return e.writeObject(ctx, key, data, func(body []byte) (ETag, error) {
		return e.store.ReplaceObject(ctx, key, etag, body)
	})
}

// ListObjects and DeleteObjects are transparent: cleanup lists and deletes
// stored keys, which the decorator never changes.
func (e *encryptedStore) ListObjects(ctx context.Context, prefix string, fn func(key string) error) error {
	return e.store.ListObjects(ctx, prefix, fn)
}

func (e *encryptedStore) DeleteObjects(ctx context.Context, keys []string) error {
	return e.store.DeleteObjects(ctx, keys)
}

// readCurrent reads the authoritative current object, refreshes the cached
// mode from it, and returns the plaintext version-one manifest. The mode
// is refreshed on every current read, so a notebook another process
// converted between two operations is picked up.
func (e *encryptedStore) readCurrent(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	rc, info, err := e.store.ReadObject(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	data, err := readAllClose(rc)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	encoding, envelope, err := ClassifyCurrent(data)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	if encoding == CurrentPlaintext {
		e.cacheMode(modePlaintext, nil)
		return io.NopCloser(bytes.NewReader(data)), info, nil
	}
	plaintext, header, err := e.keyring.Open(ctx, key, envelope)
	if err != nil {
		return nil, ObjectInfo{}, e.envelopeError(key, err)
	}
	e.cacheMode(modeEncrypted, header.Doors)
	info.Size = int64(len(plaintext))
	return io.NopCloser(bytes.NewReader(plaintext)), info, nil
}

// writeObject renders the bytes to store for one conditional write and
// forwards the raw store result unchanged.
func (e *encryptedStore) writeObject(ctx context.Context, key string, data []byte, write func([]byte) (ETag, error)) (ETag, error) {
	if key == CurrentKey {
		return e.writeCurrent(ctx, data, write)
	}
	mode, err := e.ensureMode(ctx)
	if err != nil {
		return "", err
	}
	if mode != modeEncrypted {
		return write(data)
	}
	envelope, err := e.keyring.Seal(key, crypt.KindPack, data, nil)
	if err != nil {
		return "", e.envelopeError(key, err)
	}
	return write(envelope)
}

// writeCurrent seals a plaintext manifest into a wrapped envelope, or
// passes an already-wrapped body through. A passthrough invalidates the
// cached mode, so the next operation re-reads current and learns any new
// door set.
func (e *encryptedStore) writeCurrent(ctx context.Context, data []byte, write func([]byte) (ETag, error)) (ETag, error) {
	encoding, _, err := ClassifyCurrent(data)
	if err != nil {
		return "", err
	}
	if encoding == CurrentEncrypted {
		etag, err := write(data)
		if err == nil {
			e.invalidateMode()
		}
		return etag, err
	}
	mode, err := e.ensureMode(ctx)
	if err != nil {
		return "", err
	}
	if mode != modeEncrypted {
		return write(data)
	}
	doors := e.sealSet()
	if len(doors) == 0 {
		return "", fmt.Errorf("storage: encrypt %s: %w: no door set", CurrentKey, ErrIntegrity)
	}
	envelope, err := e.keyring.Seal(CurrentKey, crypt.KindCurrent, data, doors)
	if err != nil {
		return "", e.envelopeError(CurrentKey, err)
	}
	wrapped, err := WrapCurrentEnvelope(envelope)
	if err != nil {
		return "", err
	}
	return write(wrapped)
}

// open decrypts one stored body and translates its metadata to the
// plaintext the notebook sees.
func (e *encryptedStore) open(ctx context.Context, key string, rc io.ReadCloser, info ObjectInfo) (io.ReadCloser, ObjectInfo, error) {
	data, err := readAllClose(rc)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	plaintext, _, err := e.keyring.Open(ctx, key, data)
	if err != nil {
		return nil, ObjectInfo{}, e.envelopeError(key, err)
	}
	sum := sha256.Sum256(plaintext)
	info.Size = int64(len(plaintext))
	info.Meta.SHA256 = SHA256(sum)
	info.Meta.Size = uint64(len(plaintext))
	return io.NopCloser(bytes.NewReader(plaintext)), info, nil
}

// ensureMode returns the cached mode or resolves it from current. A
// keyring that already holds the notebook key resolves without a store
// read.
func (e *encryptedStore) ensureMode(ctx context.Context) (storeMode, error) {
	e.mu.Lock()
	mode := e.mode
	e.mu.Unlock()
	if mode != modeUnresolved {
		return mode, nil
	}
	if e.keyring.Unlocked() {
		e.cacheMode(modeEncrypted, nil)
		return modeEncrypted, nil
	}
	rc, _, err := e.store.ReadObject(ctx, CurrentKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			e.cacheMode(modePlaintext, nil)
			return modePlaintext, nil
		}
		return modeUnresolved, err
	}
	data, err := readAllClose(rc)
	if err != nil {
		return modeUnresolved, err
	}
	encoding, envelope, err := ClassifyCurrent(data)
	if err != nil {
		return modeUnresolved, err
	}
	if encoding == CurrentPlaintext {
		e.cacheMode(modePlaintext, nil)
		return modePlaintext, nil
	}
	_, header, err := e.keyring.Open(ctx, CurrentKey, envelope)
	if err != nil {
		return modeUnresolved, e.envelopeError(CurrentKey, err)
	}
	e.cacheMode(modeEncrypted, header.Doors)
	return modeEncrypted, nil
}

// cacheMode records the resolved mode and the doors of the encrypted
// current it resolved from; a plaintext resolution clears the doors.
func (e *encryptedStore) cacheMode(mode storeMode, doors []crypt.Door) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = mode
	e.doors = doors
}

// invalidateMode forgets the resolved mode, so the next operation reads
// current again.
func (e *encryptedStore) invalidateMode() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = modeUnresolved
	e.doors = nil
}

// sealSet returns the doors of the encrypted current this store opened
// last, which every later sealed current write uses.
func (e *encryptedStore) sealSet() []crypt.Door {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.doors
}

// ciphertextMeta replaces the plaintext digest and size with the values of
// the stored ciphertext, so the existing protocol metadata always
// describes the body that is actually stored. Kind and generation are
// unchanged.
func ciphertextMeta(meta Metadata, envelope []byte) Metadata {
	sum := sha256.Sum256(envelope)
	meta.SHA256 = SHA256(sum)
	meta.Size = uint64(len(envelope))
	return meta
}

// readAllClose reads a streamed body completely and closes it.
func readAllClose(rc io.ReadCloser) ([]byte, error) {
	data, err := io.ReadAll(rc)
	closeErr := rc.Close()
	if err != nil {
		return nil, fmt.Errorf("storage: read body: %w", ErrTransport)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("storage: close body: %w", ErrTransport)
	}
	return data, nil
}

// ErrKeyRequired reports an encrypted notebook this process holds no secret
// for. It is the refusal of a process configured with no candidate at all,
// which the caller resolves by supplying one; a candidate that opens no
// door is crypt.ErrKeyRejected instead.
var ErrKeyRequired = errors.New("storage: encrypted notebook and no key material")

// envelopeError classifies a crypt failure at the decorator boundary. A
// secret that opens no door is a key-material failure, not corruption; an
// unsupported envelope is this build's UPGRADE_REQUIRED; every other
// framing or authentication failure is stored state that failed
// validation.
func (e *encryptedStore) envelopeError(key string, err error) error {
	switch {
	case errors.Is(err, crypt.ErrKeyRejected) && !e.keyring.HasCandidates():
		return fmt.Errorf("storage: open %s: %w", key, ErrKeyRequired)
	case errors.Is(err, crypt.ErrKeyRejected):
		return fmt.Errorf("storage: open %s: %w", key, err)
	case errors.Is(err, crypt.ErrUnsupportedFormat):
		return fmt.Errorf("storage: open %s: %w: %w", key, ErrUpgradeRequired, err)
	default:
		return fmt.Errorf("storage: open %s: %w: %w", key, ErrIntegrity, err)
	}
}
