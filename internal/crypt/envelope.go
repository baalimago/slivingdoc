// Package crypt is the client-side encryption boundary of a notebook: the
// version-one envelope wire format and the keyring that seals and opens it.
// Nothing outside this package touches a cryptographic primitive, a
// notebook key, or a secret (worklogs/26-10-05-encrypted-notebooks/phase-1-crypto-core.md).
package crypt

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// Envelope wire layout (phase-1-crypto-core.md, Envelope format):
//
//	preamble  magic[8] | formatVersion[2] | objectKind[1] | headerLength[4]
//	header    core[28] | doorSection | extensionSection | commitment[32]
//	core      chunkSize[4] | plaintextSize[8] | noncePrefix[16]
//	door      doorType[1] | kdfID[1] | bodyLength[2] | body[bodyLength]
//	door body salt[16] | memoryKiB[4] | iterations[4] | parallelism[1]
//	          | wrapNonce[24] | wrappedNotebookKey[48]
//	extension extensionType[2] | valueLength[2] | value[valueLength]
//	payload   payloadRecord*
//
// headerLength covers the core through the commitment, not the preamble or
// the payload.
const (
	magicSize      = 8
	preambleSize   = magicSize + 2 + 1 + 4
	coreSize       = 4 + 8 + 16
	commitmentSize = 32
	tagSize        = 16
	extensionHead  = 4

	// doorBodySize is the fixed body of a supported door: a 16-byte salt,
	// two 32-bit Argon2id costs, an 8-bit lane count, a 24-byte wrap nonce
	// and a 48-byte wrapped notebook key.
	doorBodySize = 16 + 4 + 4 + 1 + 24 + 48
)

// magic begins every envelope.
var magic = [magicSize]byte{'S', 'L', 'I', 'V', 'E', 'N', 'C', 0}

// formatVersion is the only envelope format this build reads and writes.
const formatVersion uint16 = 1

// DefaultChunkSize is the payload chunk size of a version-one envelope.
const DefaultChunkSize uint32 = 64 << 10

// MaxHeaderLength bounds a parsed or encoded header, the immutable core
// through the commitment.
const MaxHeaderLength = 1 << 20

// CurrentEnvelopeLimit bounds the stored encrypted `current` object: the
// version-two JSON wrapper, its Base64 encoding, the doors, the manifest
// payload and the framing. Phase 2 owns the wrapper encoding.
const CurrentEnvelopeLimit = 1 << 20

// currentWrapperOverhead is the byte length of the empty version-two
// wrapper `{"version":2,"envelope":""}` around the encoded envelope.
const currentWrapperOverhead = len(`{"version":2,"envelope":""}`)

// ObjectKind names the stored object an envelope belongs to.
type ObjectKind uint8

const (
	// KindCurrent marks the encrypted accepted-state manifest.
	KindCurrent ObjectKind = 1
	// KindPack marks an encrypted immutable pack.
	KindPack ObjectKind = 2
)

func (k ObjectKind) supported() bool { return k == KindCurrent || k == KindPack }

// Extension is one bounded opaque header extension.
type Extension struct {
	Type  uint16
	Value []byte
}

// Header is the authenticated immutable part of an envelope. Doors holds
// the supported door records in wire order; a door of an unknown type is
// opaque, so it is skipped here and preserved only by Rewrap.
type Header struct {
	FormatVersion uint16
	Kind          ObjectKind
	ChunkSize     uint32
	PlaintextSize uint64
	NoncePrefix   [16]byte
	Doors         []Door
	Extensions    []Extension
	Commitment    [32]byte
}

// core returns the immutable 28-byte core.
func (h Header) core() [coreSize]byte {
	var b [coreSize]byte
	binary.BigEndian.PutUint32(b[0:4], h.ChunkSize)
	binary.BigEndian.PutUint64(b[4:12], h.PlaintextSize)
	copy(b[12:coreSize], h.NoncePrefix[:])
	return b
}

// Errors every envelope decoder and keyring reports.
var (
	// ErrMalformedEnvelope reports a structurally invalid envelope: a
	// length that does not add up, a truncated record, or a trailing byte.
	ErrMalformedEnvelope = errors.New("crypt: malformed envelope")
	// ErrUnsupportedFormat reports a well-formed envelope of a format
	// version, object kind, or KDF this build does not support.
	ErrUnsupportedFormat = errors.New("crypt: unsupported envelope format")
	// ErrAuthentication reports that an envelope failed authentication
	// against its notebook key, object key, or payload.
	ErrAuthentication = errors.New("crypt: envelope authentication failed")
	// ErrEnvelopeTooLarge reports an envelope above the accepted bound.
	ErrEnvelopeTooLarge = errors.New("crypt: envelope too large")
)

func malformedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedEnvelope, fmt.Sprintf(format, args...))
}

func unsupportedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedFormat, fmt.Sprintf(format, args...))
}

// parsed is the structural view of an envelope. body is the authenticated
// header transcript without the commitment; unknownDoors keeps the exact
// records of unsupported door types for a later header rewrite.
type parsed struct {
	header       Header
	body         []byte
	knownDoors   []Door
	unknownDoors [][]byte
	payload      []byte
}

// decodeEnvelope parses and validates the whole wire structure without any
// key work, so a malformed or unsupported envelope is refused before a KDF
// runs.
func decodeEnvelope(raw []byte) (parsed, error) {
	var p parsed
	if len(raw) < preambleSize {
		return p, malformedf("short preamble")
	}
	if !bytes.Equal(raw[:magicSize], magic[:]) {
		return p, malformedf("bad magic")
	}
	if v := binary.BigEndian.Uint16(raw[magicSize:]); v != formatVersion {
		return p, unsupportedf("version %d", v)
	}
	kind := ObjectKind(raw[magicSize+2])
	if !kind.supported() {
		return p, unsupportedf("object kind %d", kind)
	}
	headerLength := int(binary.BigEndian.Uint32(raw[magicSize+3:]))
	if headerLength > MaxHeaderLength {
		return p, malformedf("header length %d", headerLength)
	}
	end := preambleSize + headerLength
	if end > len(raw) {
		return p, malformedf("header length %d exceeds envelope", headerLength)
	}
	if err := decodeHeader(raw[preambleSize:end], kind, &p); err != nil {
		return p, err
	}
	p.payload = raw[end:]
	if err := checkPayloadLength(p.header.PlaintextSize, p.header.ChunkSize, len(p.payload)); err != nil {
		return p, err
	}
	return p, nil
}

func decodeHeader(hdr []byte, kind ObjectKind, p *parsed) error {
	if len(hdr) < coreSize+2+commitmentSize {
		return malformedf("short header")
	}
	r := &reader{b: hdr}
	p.header.FormatVersion = formatVersion
	p.header.Kind = kind
	p.header.ChunkSize = r.u32()
	if p.header.ChunkSize != DefaultChunkSize {
		return unsupportedf("chunk size %d", p.header.ChunkSize)
	}
	p.header.PlaintextSize = r.u64()
	copy(p.header.NoncePrefix[:], r.take(16))

	for range int(r.u8()) {
		start := r.off
		doorType := DoorType(r.u8())
		kdfID := r.u8()
		body := r.take(int(r.u16()))
		if r.err != nil {
			return r.err
		}
		if doorType != DoorPassphrase && doorType != DoorRecovery {
			p.unknownDoors = append(p.unknownDoors, hdr[start:r.off])
			continue
		}
		if kdfID != kdfIDArgon2id {
			return unsupportedf("KDF id %d", kdfID)
		}
		if len(body) != doorBodySize {
			return malformedf("door body length %d", len(body))
		}
		door := decodeDoorBody(doorType, kdfID, body)
		p.knownDoors = append(p.knownDoors, door)
		p.header.Doors = append(p.header.Doors, door)
	}

	seen := make(map[uint16]struct{})
	for range int(r.u8()) {
		extensionType := r.u16()
		value := r.take(int(r.u16()))
		if r.err != nil {
			return r.err
		}
		if _, duplicate := seen[extensionType]; duplicate {
			return malformedf("duplicate extension type %d", extensionType)
		}
		seen[extensionType] = struct{}{}
		p.header.Extensions = append(p.header.Extensions, Extension{Type: extensionType, Value: bytes.Clone(value)})
	}
	if r.err != nil {
		return r.err
	}
	if r.off+commitmentSize != len(hdr) {
		return malformedf("header length %d", len(hdr))
	}
	copy(p.header.Commitment[:], hdr[r.off:])
	p.body = hdr[:r.off]
	return nil
}

// checkPayloadLength proves the declared plaintext size and chunk size
// account for exactly the payload bytes: every full data record, one short
// final data record, and exactly one empty final record.
func checkPayloadLength(plaintextSize uint64, chunkSize uint32, payloadLength int) error {
	if plaintextSize > uint64(payloadLength) {
		return malformedf("plaintext size %d exceeds payload", plaintextSize)
	}
	dataRecords := (plaintextSize + uint64(chunkSize) - 1) / uint64(chunkSize)
	want := plaintextSize + (dataRecords+1)*tagSize
	if want != uint64(payloadLength) {
		return malformedf("payload length %d, want %d", payloadLength, want)
	}
	return nil
}

// encodeDoorSection encodes the door count and the exact door records.
func encodeDoorSection(records [][]byte) ([]byte, error) {
	if len(records) > 0xFF {
		return nil, unsupportedf("door count %d", len(records))
	}
	out := make([]byte, 0, 1)
	out = append(out, byte(len(records)))
	for _, record := range records {
		out = append(out, record...)
	}
	return out, nil
}

// encodeExtensionSection encodes the extension count and the exact
// extension records.
func encodeExtensionSection(extensions []Extension) ([]byte, error) {
	if len(extensions) > 0xFF {
		return nil, unsupportedf("extension count %d", len(extensions))
	}
	out := make([]byte, 0, 1)
	out = append(out, byte(len(extensions)))
	for _, extension := range extensions {
		if len(extension.Value) > 0xFFFF {
			return nil, malformedf("extension value length %d", len(extension.Value))
		}
		out = binary.BigEndian.AppendUint16(out, extension.Type)
		out = binary.BigEndian.AppendUint16(out, uint16(len(extension.Value)))
		out = append(out, extension.Value...)
	}
	return out, nil
}

// encodeHeader assembles core, door section, extension section and
// commitment.
func encodeHeader(kind ObjectKind, core, doorSection, extensionSection []byte, macKey [32]byte, encodedKey []byte) ([]byte, error) {
	body := make([]byte, 0, len(core)+len(doorSection)+len(extensionSection))
	body = append(body, core...)
	body = append(body, doorSection...)
	body = append(body, extensionSection...)
	if len(body)+commitmentSize > MaxHeaderLength {
		return nil, malformedf("header length %d", len(body)+commitmentSize)
	}
	commitment := headerCommitment(macKey, kind, encodedKey, body)
	header := make([]byte, 0, len(body)+commitmentSize)
	header = append(header, body...)
	header = append(header, commitment[:]...)
	return header, nil
}

// assembleEnvelope joins the preamble, header and payload.
func assembleEnvelope(kind ObjectKind, header, payload []byte) []byte {
	out := make([]byte, preambleSize+len(header)+len(payload))
	copy(out[:magicSize], magic[:])
	binary.BigEndian.PutUint16(out[magicSize:], formatVersion)
	out[magicSize+2] = byte(kind)
	binary.BigEndian.PutUint32(out[magicSize+3:], uint32(len(header)))
	copy(out[preambleSize:], header)
	copy(out[preambleSize+len(header):], payload)
	return out
}

// WrappedCurrentSize returns the stored size of the version-two `current`
// object that wraps n binary envelope bytes.
func WrappedCurrentSize(n int) int {
	return currentWrapperOverhead + base64.StdEncoding.EncodedLen(n)
}

// checkCurrentSize refuses a `current` envelope whose stored wrapper would
// exceed CurrentEnvelopeLimit.
func checkCurrentSize(n int) error {
	if WrappedCurrentSize(n) > CurrentEnvelopeLimit {
		return fmt.Errorf("%w: %d-byte current envelope", ErrEnvelopeTooLarge, n)
	}
	return nil
}

// Payload record types: data records carry one plaintext chunk, the single
// final record carries an empty plaintext with the next counter.
const (
	recordData  uint8 = 1
	recordFinal uint8 = 2
)

// payloadDomain prefixes every payload record's associated data.
const payloadDomain = "slivingdoc/envelope/payload/v1"

// payloadAD binds one payload record to the notebook key's object, kind,
// core, record type, counter and plaintext length. It excludes the doors,
// the extensions and the header commitment so a header rewrite leaves the
// payload ciphertext unchanged.
func payloadAD(kind ObjectKind, encodedKey, core []byte, recordType uint8, counter uint64, plaintextLength uint32) []byte {
	ad := make([]byte, 0, len(payloadDomain)+1+2+1+len(encodedKey)+len(core)+1+8+4)
	ad = append(ad, payloadDomain...)
	ad = append(ad, 0)
	ad = binary.BigEndian.AppendUint16(ad, formatVersion)
	ad = append(ad, byte(kind))
	ad = append(ad, encodedKey...)
	ad = append(ad, core...)
	ad = append(ad, recordType)
	ad = binary.BigEndian.AppendUint64(ad, counter)
	ad = binary.BigEndian.AppendUint32(ad, plaintextLength)
	return ad
}

// sealPayload frames plaintext into one ciphertext record per full chunk, a
// short final data record, and one empty final record.
func sealPayload(aeadKey [32]byte, kind ObjectKind, encodedKey, core, noncePrefix []byte, chunkSize uint32, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(aeadKey[:])
	if err != nil {
		return nil, fmt.Errorf("crypt: payload cipher: %w", err)
	}
	dataRecords := (uint64(len(plaintext)) + uint64(chunkSize) - 1) / uint64(chunkSize)
	out := make([]byte, 0, len(plaintext)+int(dataRecords+1)*tagSize)
	var nonce [24]byte
	copy(nonce[:16], noncePrefix)
	for counter := range dataRecords {
		start := int(counter) * int(chunkSize)
		end := min(start+int(chunkSize), len(plaintext))
		binary.BigEndian.PutUint64(nonce[16:], counter)
		chunk := plaintext[start:end]
		out = aead.Seal(out, nonce[:], chunk, payloadAD(kind, encodedKey, core, recordData, counter, uint32(len(chunk))))
	}
	binary.BigEndian.PutUint64(nonce[16:], dataRecords)
	out = aead.Seal(out, nonce[:], nil, payloadAD(kind, encodedKey, core, recordFinal, dataRecords, 0))
	return out, nil
}

// openPayload authenticates and decrypts every declared record and refuses
// a record whose bytes or associated data were changed.
func openPayload(aeadKey [32]byte, kind ObjectKind, encodedKey, core, payload []byte, plaintextSize uint64, noncePrefix [16]byte, chunkSize uint32) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(aeadKey[:])
	if err != nil {
		return nil, fmt.Errorf("crypt: payload cipher: %w", err)
	}
	dataRecords := (plaintextSize + uint64(chunkSize) - 1) / uint64(chunkSize)
	plaintext := make([]byte, 0, int(plaintextSize))
	var nonce [24]byte
	copy(nonce[:16], noncePrefix[:])
	offset := 0
	for counter := range dataRecords {
		length := int(chunkSize)
		if counter == dataRecords-1 {
			length = int(plaintextSize - counter*uint64(chunkSize))
		}
		binary.BigEndian.PutUint64(nonce[16:], counter)
		// Decrypt into the preallocated plaintext, so a record neither
		// allocates nor copies on its own.
		plaintext, err = aead.Open(plaintext, nonce[:], payload[offset:offset+length+tagSize], payloadAD(kind, encodedKey, core, recordData, counter, uint32(length)))
		if err != nil {
			return nil, fmt.Errorf("crypt: payload record: %w", ErrAuthentication)
		}
		offset += length + tagSize
	}
	binary.BigEndian.PutUint64(nonce[16:], dataRecords)
	if _, err := aead.Open(nil, nonce[:], payload[offset:], payloadAD(kind, encodedKey, core, recordFinal, dataRecords, 0)); err != nil {
		return nil, fmt.Errorf("crypt: final payload record: %w", ErrAuthentication)
	}
	return plaintext, nil
}

// reader reads big-endian fields from a header, recording the first
// malformed length so a caller can check once.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) u8() uint8 {
	if r.err == nil && r.off+1 > len(r.b) {
		r.err = malformedf("truncated header")
	}
	if r.err != nil {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) u16() uint16 {
	b := r.take(2)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *reader) u32() uint32 {
	b := r.take(4)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *reader) u64() uint64 {
	b := r.take(8)
	if r.err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = malformedf("truncated header")
		return nil
	}
	b := r.b[r.off : r.off+n]
	r.off += n
	return b
}
