package crypt

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

const testObjectKey = "current"

var (
	testContext    = context.Background()
	testSecret     = []byte("correct horse battery staple")
	recoverySecret = []byte("recovery two")
)

// fixture is the shared test state: one notebook key, one KDF gate and two
// supported doors. Every door costs one Argon2id derivation, and -count=3
// repeats every test in one process, so the suite builds them once.
type fixture struct {
	key        NotebookKey
	gate       *KDFGate
	passphrase Door
	recovery   Door
}

var testFixture = sync.OnceValues(func() (fixture, error) {
	gate, err := NewKDFGate(1)
	if err != nil {
		return fixture{}, err
	}
	key := fixedKey()
	passphrase, err := WrapKey(testContext, gate, key, testSecret, testObjectKey, DoorPassphrase, DefaultKDFParams())
	if err != nil {
		return fixture{}, err
	}
	recovery, err := WrapKey(testContext, gate, key, recoverySecret, testObjectKey, DoorRecovery, cheapKDFParams())
	if err != nil {
		return fixture{}, err
	}
	return fixture{key: key, gate: gate, passphrase: passphrase, recovery: recovery}, nil
})

func testState(t *testing.T) fixture {
	t.Helper()
	state, err := testFixture()
	if err != nil {
		t.Fatalf("test fixture: %v", err)
	}
	return state
}

// unlocked returns a keyring that already holds the fixture key.
func (f fixture) unlocked(t *testing.T) *Keyring {
	t.Helper()
	keyring, err := NewUnlockedKeyring(f.key, nil, f.gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring() = %v", err)
	}
	return keyring
}

// fixedKey is a deterministic notebook key for the wire vectors.
func fixedKey() NotebookKey {
	var key NotebookKey
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func cheapKDFParams() KDFParams {
	return KDFParams{MemoryKiB: 64 << 10, Iterations: 1, Parallelism: 1}
}

func testGate(t *testing.T, jobs int) *KDFGate {
	t.Helper()
	gate, err := NewKDFGate(jobs)
	if err != nil {
		t.Fatalf("NewKDFGate(%d) = %v", jobs, err)
	}
	return gate
}

func mustSeal(t *testing.T, keyring *Keyring, kind ObjectKind, plaintext []byte, doors ...Door) []byte {
	t.Helper()
	envelope, err := keyring.Seal(testObjectKey, kind, plaintext, doors)
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	return envelope
}

func mustEncodedKey(t *testing.T, objectKey string) []byte {
	t.Helper()
	encoded, err := encodeObjectKey(objectKey)
	if err != nil {
		t.Fatalf("encodeObjectKey() = %v", err)
	}
	return encoded
}

// headerLengthOf returns the declared header length of a wire envelope.
func headerLengthOf(envelope []byte) int {
	return int(binary.BigEndian.Uint32(envelope[magicSize+3:]))
}

// payloadOf returns the payload bytes of a wire envelope.
func payloadOf(envelope []byte) []byte {
	return envelope[preambleSize+headerLengthOf(envelope):]
}

func TestEnvelopeVectors(t *testing.T) {
	key := fixedKey()
	encodedKey, err := encodeObjectKey(testObjectKey)
	if err != nil {
		t.Fatalf("encodeObjectKey() = %v", err)
	}
	macKey, err := deriveKey(key, headerPurpose, encodedKey)
	if err != nil {
		t.Fatalf("deriveKey(header) = %v", err)
	}
	aeadKey, err := deriveKey(key, payloadPurpose, encodedKey)
	if err != nil {
		t.Fatalf("deriveKey(payload) = %v", err)
	}

	var noncePrefix [16]byte
	for i := range noncePrefix {
		noncePrefix[i] = byte(0x10 + i)
	}
	header := Header{
		FormatVersion: formatVersion,
		Kind:          KindCurrent,
		ChunkSize:     DefaultChunkSize,
		PlaintextSize: 3,
		NoncePrefix:   noncePrefix,
	}
	core := header.core()

	door := Door{Type: DoorPassphrase, KDFID: kdfIDArgon2id, Params: DefaultKDFParams()}
	for i := range door.Salt {
		door.Salt[i] = byte(0x20 + i)
	}
	for i := range door.Nonce {
		door.Nonce[i] = byte(0x30 + i)
	}
	for i := range door.WrappedKey {
		door.WrappedKey[i] = byte(0x40 + i)
	}
	record, err := encodeDoorRecord(door)
	if err != nil {
		t.Fatalf("encodeDoorRecord() = %v", err)
	}
	if len(record) != 4+doorBodySize {
		t.Fatalf("door record length = %d, want %d", len(record), 4+doorBodySize)
	}
	doorSection, err := encodeDoorSection([][]byte{record})
	if err != nil {
		t.Fatalf("encodeDoorSection() = %v", err)
	}
	extensionSection, err := encodeExtensionSection([]Extension{{Type: 0x2a, Value: []byte{1, 2, 3}}})
	if err != nil {
		t.Fatalf("encodeExtensionSection() = %v", err)
	}
	encodedHeader, err := encodeHeader(KindCurrent, core[:], doorSection, extensionSection, macKey, encodedKey)
	if err != nil {
		t.Fatalf("encodeHeader() = %v", err)
	}
	payload, err := sealPayload(aeadKey, KindCurrent, encodedKey, core[:], noncePrefix[:], DefaultChunkSize, []byte("abc"))
	if err != nil {
		t.Fatalf("sealPayload() = %v", err)
	}
	envelope := assembleEnvelope(KindCurrent, encodedHeader, payload)

	// The preamble is exactly the declared magic, version, kind and length.
	if !bytes.Equal(envelope[:magicSize], []byte{'S', 'L', 'I', 'V', 'E', 'N', 'C', 0}) {
		t.Fatalf("magic = %x", envelope[:magicSize])
	}
	if got := binary.BigEndian.Uint16(envelope[magicSize:]); got != 1 {
		t.Fatalf("format version = %d, want 1", got)
	}
	if got := ObjectKind(envelope[magicSize+2]); got != KindCurrent {
		t.Fatalf("object kind = %d, want %d", got, KindCurrent)
	}
	if got := binary.BigEndian.Uint32(envelope[magicSize+3:]); got != uint32(len(encodedHeader)) {
		t.Fatalf("header length = %d, want %d", got, len(encodedHeader))
	}

	// The commitment is an independent HMAC over the documented transcript.
	transcript := append([]byte("slivingdoc/envelope/header/v1"), 0)
	transcript = append(transcript, 0, 1)
	transcript = append(transcript, byte(KindCurrent))
	transcript = append(transcript, encodedKey...)
	transcript = append(transcript, encodedHeader[:len(encodedHeader)-commitmentSize]...)
	mac := hmac.New(sha256.New, macKey[:])
	mac.Write(transcript)
	if !bytes.Equal(mac.Sum(nil), encodedHeader[len(encodedHeader)-commitmentSize:]) {
		t.Fatalf("commitment = %x, want %x", encodedHeader[len(encodedHeader)-commitmentSize:], mac.Sum(nil))
	}

	// The payload is exactly the length the core declares.
	if got, want := len(payload), 3+2*tagSize; got != want {
		t.Fatalf("payload length = %d, want %d", got, want)
	}

	if envelopeVectorHex == "" {
		t.Fatalf("envelope vector: %s", hex.EncodeToString(envelope))
	}
	if got := hex.EncodeToString(envelope); got != envelopeVectorHex {
		t.Fatalf("envelope = %s,\nwant      %s", got, envelopeVectorHex)
	}

	parsed, err := decodeEnvelope(envelope)
	if err != nil {
		t.Fatalf("decodeEnvelope() = %v", err)
	}
	if parsed.header.Kind != KindCurrent || parsed.header.PlaintextSize != 3 || parsed.header.ChunkSize != DefaultChunkSize {
		t.Fatalf("parsed header = %+v", parsed.header)
	}
	if len(parsed.header.Doors) != 1 || parsed.header.Doors[0] != door {
		t.Fatalf("parsed doors = %+v, want %+v", parsed.header.Doors, door)
	}
	if len(parsed.header.Extensions) != 1 || !bytes.Equal(parsed.header.Extensions[0].Value, []byte{1, 2, 3}) {
		t.Fatalf("parsed extensions = %+v", parsed.header.Extensions)
	}
}

// envelopeVectorHex pins the version-one wire bytes of TestEnvelopeVectors.
const envelopeVectorHex = "534c4956454e4300000101000000aa000100000000000000000003101112131415161718191a1b1c1d1e1f0101010061202122232425262728292a2b2c2d2e2f000400000000000304303132333435363738393a3b3c3d3e3f4041424344454647404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f606162636465666768696a6b6c6d6e6f01002a0003010203a82f67477bddeceb59a0627e3436863112864f72518e09d6b3b14a1139ee04a69e3e8e70fd447915b1cd91c83ba301fba8a1fca48a29f7783db2a9977eb567b1c05dff"

func TestEnvelopeRejectsMalformedHeaders(t *testing.T) {
	state := testState(t)
	envelope := mustSeal(t, state.unlocked(t), KindCurrent, []byte("abc"), state.passphrase)

	encodedKey := mustEncodedKey(t, testObjectKey)
	macKey, err := deriveKey(state.key, headerPurpose, encodedKey)
	if err != nil {
		t.Fatalf("deriveKey(header) = %v", err)
	}
	duplicateExtensions := []byte{2, 0, 7, 0, 1, 0xaa, 0, 7, 0, 1, 0xbb}

	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   error
	}{
		{
			name:   "short preamble",
			mutate: func(b []byte) []byte { return b[:5] },
			want:   ErrMalformedEnvelope,
		},
		{
			name:   "bad magic",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[0] ^= 0xff; return b },
			want:   ErrMalformedEnvelope,
		},
		{
			name:   "unsupported version",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[magicSize+1] = 2; return b },
			want:   ErrUnsupportedFormat,
		},
		{
			name:   "unknown object kind",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[magicSize+2] = 3; return b },
			want:   ErrUnsupportedFormat,
		},
		{
			name: "oversized header length",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint32(b[magicSize+3:], MaxHeaderLength+1)
				return b
			},
			want: ErrMalformedEnvelope,
		},
		{
			name: "header length beyond envelope",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint32(b[magicSize+3:], uint32(len(b)))
				return b
			},
			want: ErrMalformedEnvelope,
		},
		{
			name: "unsupported chunk size",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint32(b[preambleSize:], DefaultChunkSize/2)
				return b
			},
			want: ErrUnsupportedFormat,
		},
		{
			name: "inconsistent plaintext size",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint64(b[preambleSize+4:], 4)
				return b
			},
			want: ErrMalformedEnvelope,
		},
		{
			name: "implausible plaintext size",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint64(b[preambleSize+4:], 1<<40)
				return b
			},
			want: ErrMalformedEnvelope,
		},
		{
			name:   "truncated payload",
			mutate: func(b []byte) []byte { return b[:len(b)-1] },
			want:   ErrMalformedEnvelope,
		},
		{
			name:   "trailing payload byte",
			mutate: func(b []byte) []byte { return append(bytes.Clone(b), 0) },
			want:   ErrMalformedEnvelope,
		},
		{
			name:   "unsupported KDF id",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[preambleSize+coreSize+2] = 9; return b },
			want:   ErrUnsupportedFormat,
		},
		{
			name: "door body length beyond header",
			mutate: func(b []byte) []byte {
				b = bytes.Clone(b)
				binary.BigEndian.PutUint16(b[preambleSize+coreSize+3:], 0xFFFF)
				return b
			},
			want: ErrMalformedEnvelope,
		},
		{
			name: "header padding before the commitment",
			mutate: func(b []byte) []byte {
				commitmentAt := preambleSize + headerLengthOf(b) - commitmentSize
				out := append(bytes.Clone(b[:commitmentAt]), 0)
				out = append(out, b[commitmentAt:]...)
				binary.BigEndian.PutUint32(out[magicSize+3:], uint32(headerLengthOf(b)+1))
				return out
			},
			want: ErrMalformedEnvelope,
		},
		{
			name: "duplicate extension types",
			mutate: func(b []byte) []byte {
				core := b[preambleSize : preambleSize+coreSize]
				doorSection := b[preambleSize+coreSize : preambleSize+coreSize+102]
				header, err := encodeHeader(KindCurrent, core, doorSection, duplicateExtensions, macKey, encodedKey)
				if err != nil {
					t.Fatalf("encodeHeader() = %v", err)
				}
				return assembleEnvelope(KindCurrent, header, payloadOf(b))
			},
			want: ErrMalformedEnvelope,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeEnvelope(tt.mutate(envelope))
			if !errors.Is(err, tt.want) {
				t.Fatalf("decodeEnvelope() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestEnvelopeAuthenticatesPayload(t *testing.T) {
	state := testState(t)
	keyring := state.unlocked(t)
	envelope := mustSeal(t, keyring, KindCurrent, []byte("a note"), state.passphrase)
	payloadAt := preambleSize + headerLengthOf(envelope)
	commitmentAt := payloadAt - commitmentSize
	doorRecordAt := preambleSize + coreSize

	open := func(t *testing.T, raw []byte, objectKey string) error {
		t.Helper()
		_, _, err := keyring.Open(testContext, objectKey, raw)
		return err
	}

	if err := open(t, envelope, testObjectKey); err != nil {
		t.Fatalf("Open() = %v", err)
	}

	tests := []struct {
		name   string
		mutate func([]byte) []byte
		key    string
	}{
		{
			name:   "payload ciphertext",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[payloadAt] ^= 1; return b },
			key:    testObjectKey,
		},
		{
			name:   "final record tag",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[len(b)-1] ^= 1; return b },
			key:    testObjectKey,
		},
		{
			name:   "header commitment",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[commitmentAt] ^= 1; return b },
			key:    testObjectKey,
		},
		{
			name:   "immutable core",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[preambleSize+coreSize-1] ^= 1; return b },
			key:    testObjectKey,
		},
		{
			name:   "door record",
			mutate: func(b []byte) []byte { b = bytes.Clone(b); b[doorRecordAt+8] ^= 1; return b },
			key:    testObjectKey,
		},
		{
			name:   "wrong object key",
			mutate: func(b []byte) []byte { return b },
			key:    "packs/checkpoints/1-01920000-0000-7000-8000-000000000000.pack",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := open(t, tt.mutate(envelope), tt.key); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("Open() = %v, want %v", err, ErrAuthentication)
			}
		})
	}

	t.Run("wrong notebook key", func(t *testing.T) {
		other := fixedKey()
		other[0] ^= 1
		ring, err := NewUnlockedKeyring(other, nil, state.gate)
		if err != nil {
			t.Fatalf("NewUnlockedKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("Open() = %v, want %v", err, ErrAuthentication)
		}
	})

	t.Run("pack round trip", func(t *testing.T) {
		packKey := "packs/increments/2-01920000-0000-7000-8000-000000000001.pack"
		packEnvelope, err := keyring.Seal(packKey, KindPack, bytes.Repeat([]byte("x"), int(DefaultChunkSize)+1), []Door{state.passphrase})
		if err != nil {
			t.Fatalf("Seal(pack) = %v", err)
		}
		plaintext, header, err := keyring.Open(testContext, packKey, packEnvelope)
		if err != nil {
			t.Fatalf("Open(pack) = %v", err)
		}
		if len(plaintext) != int(DefaultChunkSize)+1 || header.Kind != KindPack {
			t.Fatalf("Open(pack) = %d bytes of kind %d", len(plaintext), header.Kind)
		}
	})
}

func TestEnvelopeEncoderBounds(t *testing.T) {
	keyring := testState(t).unlocked(t)

	if _, err := keyring.Seal(testObjectKey, ObjectKind(3), nil, nil); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Seal(unknown kind) = %v, want %v", err, ErrUnsupportedFormat)
	}
	if _, err := keyring.Rewrap(testObjectKey, []byte("not an envelope"), nil); !errors.Is(err, ErrMalformedEnvelope) {
		t.Fatalf("Rewrap(garbage) = %v, want %v", err, ErrMalformedEnvelope)
	}

	if _, err := encodeDoorSection(make([][]byte, 256)); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("encodeDoorSection(256) = %v, want %v", err, ErrUnsupportedFormat)
	}
	if _, err := encodeExtensionSection(make([]Extension, 256)); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("encodeExtensionSection(256) = %v, want %v", err, ErrUnsupportedFormat)
	}
	if _, err := encodeExtensionSection([]Extension{{Type: 1, Value: make([]byte, 1<<16)}}); !errors.Is(err, ErrMalformedEnvelope) {
		t.Fatalf("encodeExtensionSection(oversized value) = %v, want %v", err, ErrMalformedEnvelope)
	}

	// A header above MaxHeaderLength is refused before it is built.
	body := make([]byte, MaxHeaderLength)
	if _, err := encodeHeader(KindCurrent, body, []byte{0}, []byte{0}, [32]byte{}, nil); !errors.Is(err, ErrMalformedEnvelope) {
		t.Fatalf("encodeHeader(oversized) = %v, want %v", err, ErrMalformedEnvelope)
	}

	if _, err := encodeDoorRecords([]Door{{Type: DoorType(3)}}); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("encodeDoorRecords(unknown type) = %v, want %v", err, ErrUnsupportedFormat)
	}
}

func TestCurrentEnvelopeSizeLimit(t *testing.T) {
	state := testState(t)
	keyring := state.unlocked(t)

	small := mustSeal(t, keyring, KindCurrent, bytes.Repeat([]byte("a"), 1<<19), state.passphrase)
	if err := checkCurrentSize(len(small)); err != nil {
		t.Fatalf("checkCurrentSize(%d) = %v", len(small), err)
	}
	if _, err := keyring.Seal(testObjectKey, KindPack, small, []Door{state.passphrase}); err != nil {
		t.Fatalf("Seal(pack) = %v", err)
	}

	if _, err := keyring.Seal(testObjectKey, KindCurrent, bytes.Repeat([]byte("a"), 1<<20), []Door{state.passphrase}); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("Seal(oversized current) = %v, want %v", err, ErrEnvelopeTooLarge)
	}

	// The bound is the stored wrapper: the Base64 of the largest binary
	// envelope plus the empty version-two wrapper exactly fills the limit.
	if WrappedCurrentSize(786411) > CurrentEnvelopeLimit {
		t.Fatalf("WrappedCurrentSize(786411) = %d, want at most %d", WrappedCurrentSize(786411), CurrentEnvelopeLimit)
	}
	if WrappedCurrentSize(786412) <= CurrentEnvelopeLimit {
		t.Fatalf("WrappedCurrentSize(786412) = %d, want above %d", WrappedCurrentSize(786412), CurrentEnvelopeLimit)
	}
}

func TestEnvelopeFrameBoundaries(t *testing.T) {
	state := testState(t)
	keyring := state.unlocked(t)

	tests := []struct {
		name      string
		plaintext int
		records   int
	}{
		{"empty", 0, 1},
		{"one short record", 1, 2},
		{"one full record", int(DefaultChunkSize), 2},
		{"one full and one short record", int(DefaultChunkSize) + 1, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plaintext := bytes.Repeat([]byte("z"), tt.plaintext)
			envelope := mustSeal(t, keyring, KindPack, plaintext, state.passphrase)
			if got, want := len(payloadOf(envelope)), tt.plaintext+tt.records*tagSize; got != want {
				t.Fatalf("payload length = %d, want %d", got, want)
			}
			opened, header, err := keyring.Open(testContext, testObjectKey, envelope)
			if err != nil {
				t.Fatalf("Open() = %v", err)
			}
			if !bytes.Equal(opened, plaintext) || header.PlaintextSize != uint64(tt.plaintext) {
				t.Fatalf("Open() = %d bytes, want %d", len(opened), tt.plaintext)
			}
		})
	}
}
