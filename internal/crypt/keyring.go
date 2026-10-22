package crypt

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Errors the keyring reports.
var (
	// ErrKeyRejected reports that no candidate secret opened a door.
	ErrKeyRejected = errors.New("crypt: no candidate secret opened the notebook")
	// ErrKeyringLocked reports a seal or rewrite without a notebook key.
	ErrKeyringLocked = errors.New("crypt: keyring holds no notebook key")
	// ErrCandidateLimit reports more candidates than the supported sources,
	// a repeated source, or an unknown source.
	ErrCandidateLimit = errors.New("crypt: invalid candidate set")
)

// maxCandidateSources is the number of sources a caller may draw a secret
// from: the prompted secret, the environment value, its envfile value, and
// the key-record value.
const maxCandidateSources = 4

// NotebookKey is the random symmetric key that encrypts one notebook's
// stored objects. It exists in process memory only.
type NotebookKey [32]byte

// GenerateNotebookKey returns a new random notebook key.
func GenerateNotebookKey() (NotebookKey, error) {
	var key NotebookKey
	b, err := randomBytes(len(key))
	if err != nil {
		return key, err
	}
	copy(key[:], b)
	return key, nil
}

// CandidateSource names where a candidate secret came from. The source
// labels a secret for reporting; its value and path never leave the
// candidate.
type CandidateSource uint8

const (
	// SourceUnknown marks a candidate without a supported source.
	SourceUnknown CandidateSource = iota
	// SourcePrompt marks a secret typed by the caller.
	SourcePrompt
	// SourceEnvironment marks the environment value.
	SourceEnvironment
	// SourceEnvironmentFile marks the value read from the environment file.
	SourceEnvironmentFile
	// SourceKeyRecord marks the value in the stored key record.
	SourceKeyRecord
)

// Candidate is one secret to try against every supported door.
type Candidate struct {
	Source CandidateSource
	Secret []byte
}

// Keyring seals and opens envelopes with one notebook key. A keyring built
// from candidates resolves the key on its first open; a keyring built from
// a known key never searches doors. It is safe for concurrent use.
type Keyring struct {
	mu         sync.Mutex
	key        NotebookKey
	unlocked   bool
	source     CandidateSource
	candidates []Candidate
	gate       *KDFGate
}

// NewKeyring returns a locked keyring that resolves the notebook key from
// the first candidate that opens a door.
func NewKeyring(candidates []Candidate, gate *KDFGate) (*Keyring, error) {
	if err := validateCandidates(candidates); err != nil {
		return nil, err
	}
	if gate == nil {
		return nil, fmt.Errorf("crypt: keyring: %w", ErrKDFGate)
	}
	return &Keyring{candidates: candidates, gate: gate}, nil
}

// NewUnlockedKeyring returns a keyring that already holds the notebook key
// and never searches doors. The candidates are validated for the same bound
// as NewKeyring, so one caller cannot pass an out-of-bound set unnoticed.
func NewUnlockedKeyring(key NotebookKey, candidates []Candidate, gate *KDFGate) (*Keyring, error) {
	if err := validateCandidates(candidates); err != nil {
		return nil, err
	}
	if gate == nil {
		return nil, fmt.Errorf("crypt: keyring: %w", ErrKDFGate)
	}
	return &Keyring{key: key, unlocked: true, gate: gate}, nil
}

// ResolvedSource returns the source of the candidate that resolved the
// notebook key, or SourceUnknown before an open and for a known key.
func (k *Keyring) ResolvedSource() CandidateSource {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.source
}

// ResolvedSecret returns a copy of the candidate secret that resolved the
// notebook key, or nil before an open and for a keyring built from a known
// key. Its source is ResolvedSource.
func (k *Keyring) ResolvedSecret() []byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.unlocked {
		return nil
	}
	for _, c := range k.candidates {
		if c.Source == k.source {
			return slices.Clone(c.Secret)
		}
	}
	return nil
}

// HasCandidates reports whether the keyring holds a secret to try. A caller
// that opens a door of an encrypted notebook uses it to tell the two
// refusals apart: no secret configured at all, or a secret that opened no
// door.
func (k *Keyring) HasCandidates() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.candidates) > 0
}

// AddCandidate adds one candidate secret to a keyring that has not resolved
// its key yet, so a secret typed after the first refusal is tried exactly like
// a configured one. The bound and the one-secret-per-source rule apply as at
// construction. A keyring that already holds the key never searches doors, so
// the candidate would never be tried and the call is a no-op.
func (k *Keyring) AddCandidate(c Candidate) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.unlocked {
		return nil
	}
	if err := validateCandidates(append(slices.Clone(k.candidates), c)); err != nil {
		return err
	}
	k.candidates = append(k.candidates, c)
	return nil
}

// Unlocked reports whether the keyring already holds the notebook key: at
// construction for a known key, or after the first open that resolved a
// door from the candidates.
func (k *Keyring) Unlocked() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.unlocked
}

// Open authenticates and decrypts an envelope. A keyring without a key
// searches every candidate against every supported door and stops at the
// first that opens one. It returns the plaintext and the authenticated
// header.
func (k *Keyring) Open(ctx context.Context, objectKey string, envelope []byte) ([]byte, Header, error) {
	parsed, err := decodeEnvelope(envelope)
	if err != nil {
		return nil, Header{}, err
	}
	encodedKey, err := encodeObjectKey(objectKey)
	if err != nil {
		return nil, Header{}, err
	}
	key, err := k.resolve(ctx, objectKey, parsed.knownDoors)
	if err != nil {
		return nil, Header{}, err
	}
	macKey, err := deriveKey(key, headerPurpose, encodedKey)
	if err != nil {
		return nil, Header{}, err
	}
	want := headerCommitment(macKey, parsed.header.Kind, encodedKey, parsed.body)
	if !hmac.Equal(parsed.header.Commitment[:], want[:]) {
		return nil, Header{}, fmt.Errorf("crypt: header: %w", ErrAuthentication)
	}
	aeadKey, err := deriveKey(key, payloadPurpose, encodedKey)
	if err != nil {
		return nil, Header{}, err
	}
	plaintext, err := openPayload(aeadKey, parsed.header.Kind, encodedKey, parsed.body[:coreSize], parsed.payload, parsed.header.PlaintextSize, parsed.header.NoncePrefix, parsed.header.ChunkSize)
	if err != nil {
		return nil, Header{}, err
	}
	return plaintext, parsed.header, nil
}

// Seal encrypts plaintext into a new envelope of the given kind with the
// given doors. It requires a resolved notebook key.
func (k *Keyring) Seal(objectKey string, kind ObjectKind, plaintext []byte, doors []Door) ([]byte, error) {
	return k.seal(objectKey, kind, plaintext, doors, nil)
}

// seal encrypts plaintext into a new envelope with the given doors and
// extensions. Only Seal and the tests build an envelope with an extension.
func (k *Keyring) seal(objectKey string, kind ObjectKind, plaintext []byte, doors []Door, extensions []Extension) ([]byte, error) {
	if !kind.supported() {
		return nil, unsupportedf("object kind %d", kind)
	}
	encodedKey, err := encodeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	key, err := k.unlockedKey()
	if err != nil {
		return nil, err
	}
	macKey, err := deriveKey(key, headerPurpose, encodedKey)
	if err != nil {
		return nil, err
	}
	aeadKey, err := deriveKey(key, payloadPurpose, encodedKey)
	if err != nil {
		return nil, err
	}
	noncePrefix, err := randomBytes(16)
	if err != nil {
		return nil, err
	}
	header := Header{FormatVersion: formatVersion, Kind: kind, ChunkSize: DefaultChunkSize, PlaintextSize: uint64(len(plaintext))}
	copy(header.NoncePrefix[:], noncePrefix)
	core := header.core()
	records, err := encodeDoorRecords(doors)
	if err != nil {
		return nil, err
	}
	doorSection, err := encodeDoorSection(records)
	if err != nil {
		return nil, err
	}
	extensionSection, err := encodeExtensionSection(extensions)
	if err != nil {
		return nil, err
	}
	encodedHeader, err := encodeHeader(kind, core[:], doorSection, extensionSection, macKey, encodedKey)
	if err != nil {
		return nil, err
	}
	payload, err := sealPayload(aeadKey, kind, encodedKey, core[:], header.NoncePrefix[:], header.ChunkSize, plaintext)
	if err != nil {
		return nil, err
	}
	envelope := assembleEnvelope(kind, encodedHeader, payload)
	if kind == KindCurrent {
		if err := checkCurrentSize(len(envelope)); err != nil {
			return nil, err
		}
	}
	return envelope, nil
}

// WrapDoor wraps the keyring's held notebook key under secret as a new door
// of the given type, with the given Argon2id cost. It requires a resolved
// key and exists so a door change never moves the key out of this package.
func (k *Keyring) WrapDoor(ctx context.Context, objectKey string, secret []byte, kind DoorType, params KDFParams) (Door, error) {
	key, err := k.unlockedKey()
	if err != nil {
		return Door{}, err
	}
	return WrapKey(ctx, k.gate, key, secret, objectKey, kind, params)
}

// Rewrap replaces the door list of an envelope with the supported doors
// given, keeps any opaque door of an unknown type and every extension, and
// leaves the payload ciphertext untouched. It requires the notebook key of
// the envelope.
func (k *Keyring) Rewrap(objectKey string, envelope []byte, doors []Door) ([]byte, error) {
	parsed, err := decodeEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	encodedKey, err := encodeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	key, err := k.unlockedKey()
	if err != nil {
		return nil, err
	}
	macKey, err := deriveKey(key, headerPurpose, encodedKey)
	if err != nil {
		return nil, err
	}
	want := headerCommitment(macKey, parsed.header.Kind, encodedKey, parsed.body)
	if !hmac.Equal(parsed.header.Commitment[:], want[:]) {
		return nil, fmt.Errorf("crypt: header: %w", ErrAuthentication)
	}
	records, err := encodeDoorRecords(doors)
	if err != nil {
		return nil, err
	}
	records = append(records, parsed.unknownDoors...)
	doorSection, err := encodeDoorSection(records)
	if err != nil {
		return nil, err
	}
	extensionSection, err := encodeExtensionSection(parsed.header.Extensions)
	if err != nil {
		return nil, err
	}
	header, err := encodeHeader(parsed.header.Kind, parsed.body[:coreSize], doorSection, extensionSection, macKey, encodedKey)
	if err != nil {
		return nil, err
	}
	rewritten := assembleEnvelope(parsed.header.Kind, header, parsed.payload)
	if parsed.header.Kind == KindCurrent {
		if err := checkCurrentSize(len(rewritten)); err != nil {
			return nil, err
		}
	}
	return rewritten, nil
}

// resolve returns the keyring's key, searching the candidates against the
// supported doors when the keyring holds none.
func (k *Keyring) resolve(ctx context.Context, objectKey string, doors []Door) (NotebookKey, error) {
	k.mu.Lock()
	if k.unlocked {
		key := k.key
		k.mu.Unlock()
		return key, nil
	}
	candidates := k.candidates
	gate := k.gate
	k.mu.Unlock()
	for _, candidate := range candidates {
		for _, door := range doors {
			key, err := UnwrapKey(ctx, gate, door, candidate.Secret, objectKey)
			if err == nil {
				k.mu.Lock()
				k.key, k.unlocked, k.source = key, true, candidate.Source
				k.mu.Unlock()
				return key, nil
			}
			if !errors.Is(err, ErrKeyRejected) {
				return NotebookKey{}, err
			}
		}
	}
	return NotebookKey{}, fmt.Errorf("crypt: open: %w", ErrKeyRejected)
}

// unlockedKey returns the keyring's notebook key or reports that it holds
// none.
func (k *Keyring) unlockedKey() (NotebookKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.unlocked {
		return NotebookKey{}, fmt.Errorf("crypt: seal: %w", ErrKeyringLocked)
	}
	return k.key, nil
}

// encodeDoorRecords encodes every door in order.
func encodeDoorRecords(doors []Door) ([][]byte, error) {
	records := make([][]byte, 0, len(doors))
	for _, door := range doors {
		record, err := encodeDoorRecord(door)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// validateCandidates enforces the candidate bound: at most one candidate
// per source, at most maxCandidateSources in all, every source known and
// every secret inside the secret bounds.
func validateCandidates(candidates []Candidate) error {
	if len(candidates) > maxCandidateSources {
		return fmt.Errorf("%w: %d candidates", ErrCandidateLimit, len(candidates))
	}
	seen := make(map[CandidateSource]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Source < SourcePrompt || candidate.Source > SourceKeyRecord {
			return fmt.Errorf("%w: unknown source %d", ErrCandidateLimit, candidate.Source)
		}
		if _, duplicate := seen[candidate.Source]; duplicate {
			return fmt.Errorf("%w: duplicate source %d", ErrCandidateLimit, candidate.Source)
		}
		seen[candidate.Source] = struct{}{}
		if err := validateSecret(candidate.Secret); err != nil {
			return err
		}
	}
	return nil
}
