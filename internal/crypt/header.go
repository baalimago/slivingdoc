package crypt

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// Domain separation (phase-1-crypto-core.md, Envelope format). Derived keys
// and the header commitment bind the exact ASCII protocol object key, so an
// envelope sealed for one object never opens as another.
const (
	hkdfDomain     = "slivingdoc/envelope/v1/"
	headerDomain   = "slivingdoc/envelope/header/v1"
	wrapDomain     = "slivingdoc/envelope/wrap/v1"
	headerPurpose  = "header-mac"
	payloadPurpose = "payload-aead"
	keySize        = 32
)

// kdfIDArgon2id names Argon2id version 19, the only KDF this build writes.
const kdfIDArgon2id uint8 = 1

// DoorType names the kind of secret that wraps a notebook key.
type DoorType uint8

const (
	// DoorPassphrase marks a door opened by a human-chosen passphrase.
	DoorPassphrase DoorType = 1
	// DoorRecovery marks a door opened by a generated recovery code.
	DoorRecovery DoorType = 2
)

// Door is one wrapped copy of the notebook key: an Argon2id cost, a random
// salt, and the notebook key sealed under the derived KEK. WrappedKey is a
// 32-byte key and a 16-byte XChaCha20-Poly1305 tag.
type Door struct {
	Type       DoorType
	KDFID      uint8
	Salt       [16]byte
	Params     KDFParams
	Nonce      [24]byte
	WrappedKey [48]byte
}

// encodeObjectKey renders the ASCII protocol object key prefixed by its
// 16-bit byte length.
func encodeObjectKey(objectKey string) ([]byte, error) {
	if objectKey == "" {
		return nil, malformedf("empty object key")
	}
	if len(objectKey) > 0xFFFF {
		return nil, malformedf("object key length %d", len(objectKey))
	}
	for i := range len(objectKey) {
		if objectKey[i] >= 0x80 {
			return nil, malformedf("object key is not ASCII")
		}
	}
	out := make([]byte, 2, 2+len(objectKey))
	binary.BigEndian.PutUint16(out, uint16(len(objectKey)))
	return append(out, objectKey...), nil
}

// deriveKey expands the notebook key into a purpose- and object-bound key,
// HKDF-SHA-256 with an all-zero salt.
func deriveKey(key NotebookKey, purpose string, encodedKey []byte) ([32]byte, error) {
	var out [32]byte
	info := make([]byte, 0, len(hkdfDomain)+len(purpose)+1+len(encodedKey))
	info = append(info, hkdfDomain...)
	info = append(info, purpose...)
	info = append(info, 0)
	info = append(info, encodedKey...)
	raw, err := hkdf.Key(sha256.New, key[:], make([]byte, sha256.Size), string(info), keySize)
	if err != nil {
		return out, fmt.Errorf("crypt: derive %s key: %w", purpose, err)
	}
	copy(out[:], raw)
	return out, nil
}

// headerCommitment authenticates the format version, object kind, object
// key, immutable core, doors and extensions under the header-mac key. It
// excludes the commitment itself.
func headerCommitment(macKey [32]byte, kind ObjectKind, encodedKey, body []byte) [32]byte {
	transcript := make([]byte, 0, len(headerDomain)+1+2+1+len(encodedKey)+len(body))
	transcript = append(transcript, headerDomain...)
	transcript = append(transcript, 0)
	transcript = binary.BigEndian.AppendUint16(transcript, formatVersion)
	transcript = append(transcript, byte(kind))
	transcript = append(transcript, encodedKey...)
	transcript = append(transcript, body...)
	mac := hmac.New(sha256.New, macKey[:])
	mac.Write(transcript)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// wrapAD binds one wrapped notebook key to the version, object, door type,
// KDF and salt it was wrapped for.
func wrapAD(encodedKey []byte, door Door) []byte {
	ad := make([]byte, 0, len(wrapDomain)+1+2+len(encodedKey)+2+16+9)
	ad = append(ad, wrapDomain...)
	ad = append(ad, 0)
	ad = binary.BigEndian.AppendUint16(ad, formatVersion)
	ad = append(ad, encodedKey...)
	ad = append(ad, byte(door.Type), door.KDFID)
	ad = append(ad, door.Salt[:]...)
	ad = binary.BigEndian.AppendUint32(ad, door.Params.MemoryKiB)
	ad = binary.BigEndian.AppendUint32(ad, door.Params.Iterations)
	ad = append(ad, door.Params.Parallelism)
	return ad
}

// encodeDoorBody renders the fixed 97-byte body of a supported door.
func encodeDoorBody(door Door) []byte {
	body := make([]byte, 0, doorBodySize)
	body = append(body, door.Salt[:]...)
	body = binary.BigEndian.AppendUint32(body, door.Params.MemoryKiB)
	body = binary.BigEndian.AppendUint32(body, door.Params.Iterations)
	body = append(body, door.Params.Parallelism)
	body = append(body, door.Nonce[:]...)
	body = append(body, door.WrappedKey[:]...)
	return body
}

// decodeDoorBody reads a supported door's fixed body.
func decodeDoorBody(doorType DoorType, kdfID uint8, body []byte) Door {
	return Door{
		Type:  doorType,
		KDFID: kdfID,
		Salt:  [16]byte(body[0:16]),
		Params: KDFParams{
			MemoryKiB:   binary.BigEndian.Uint32(body[16:20]),
			Iterations:  binary.BigEndian.Uint32(body[20:24]),
			Parallelism: body[24],
		},
		Nonce:      [24]byte(body[25:49]),
		WrappedKey: [48]byte(body[49:doorBodySize]),
	}
}

// encodeDoorRecord renders one exact wire door record.
func encodeDoorRecord(door Door) ([]byte, error) {
	if door.Type != DoorPassphrase && door.Type != DoorRecovery {
		return nil, unsupportedf("door type %d", door.Type)
	}
	if door.KDFID != kdfIDArgon2id {
		return nil, unsupportedf("KDF id %d", door.KDFID)
	}
	body := encodeDoorBody(door)
	record := make([]byte, 0, 4+len(body))
	record = append(record, byte(door.Type), door.KDFID)
	record = binary.BigEndian.AppendUint16(record, uint16(len(body)))
	return append(record, body...), nil
}

// WrapKey seals the notebook key into one door under the secret, using the
// injected KDF gate. The KEK is Argon2id over the exact secret bytes.
func WrapKey(ctx context.Context, gate *KDFGate, key NotebookKey, secret []byte, objectKey string, kind DoorType, params KDFParams) (Door, error) {
	var door Door
	if kind != DoorPassphrase && kind != DoorRecovery {
		return door, unsupportedf("door type %d", kind)
	}
	encodedKey, err := encodeObjectKey(objectKey)
	if err != nil {
		return door, err
	}
	salt, err := randomBytes(16)
	if err != nil {
		return door, err
	}
	door = Door{Type: kind, KDFID: kdfIDArgon2id, Params: params}
	copy(door.Salt[:], salt)
	kek, err := DeriveKEK(ctx, gate, secret, door.Salt, params)
	if err != nil {
		return Door{}, err
	}
	nonce, err := randomBytes(24)
	if err != nil {
		return Door{}, err
	}
	copy(door.Nonce[:], nonce)
	aead, err := chacha20poly1305.NewX(kek[:])
	if err != nil {
		return Door{}, fmt.Errorf("crypt: wrap cipher: %w", err)
	}
	copy(door.WrappedKey[:], aead.Seal(nil, door.Nonce[:], key[:], wrapAD(encodedKey, door)))
	return door, nil
}

// UnwrapKey authenticates the door against the secret and returns the
// notebook key. A secret that does not open the door, or a wrap bound to
// another object key, reports ErrKeyRejected and no key.
func UnwrapKey(ctx context.Context, gate *KDFGate, door Door, secret []byte, objectKey string) (NotebookKey, error) {
	var key NotebookKey
	if door.Type != DoorPassphrase && door.Type != DoorRecovery {
		return key, unsupportedf("door type %d", door.Type)
	}
	if door.KDFID != kdfIDArgon2id {
		return key, unsupportedf("KDF id %d", door.KDFID)
	}
	encodedKey, err := encodeObjectKey(objectKey)
	if err != nil {
		return key, err
	}
	kek, err := DeriveKEK(ctx, gate, secret, door.Salt, door.Params)
	if err != nil {
		return key, err
	}
	aead, err := chacha20poly1305.NewX(kek[:])
	if err != nil {
		return key, fmt.Errorf("crypt: unwrap cipher: %w", err)
	}
	plaintext, err := aead.Open(nil, door.Nonce[:], door.WrappedKey[:], wrapAD(encodedKey, door))
	if err != nil {
		return key, fmt.Errorf("crypt: unwrap: %w", ErrKeyRejected)
	}
	copy(key[:], plaintext)
	return key, nil
}

// randomBytes returns n cryptographically secure random bytes.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("crypt: random bytes: %w", err)
	}
	return b, nil
}
