package crypt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestObjectKeyGrammar(t *testing.T) {
	encoded, err := encodeObjectKey("current")
	if err != nil {
		t.Fatalf("encodeObjectKey() = %v", err)
	}
	if !bytes.Equal(encoded, []byte{0, 7, 'c', 'u', 'r', 'r', 'e', 'n', 't'}) {
		t.Fatalf("encodeObjectKey() = %x", encoded)
	}

	longest := strings.Repeat("k", 0xFFFF)
	if _, err := encodeObjectKey(longest); err != nil {
		t.Fatalf("encodeObjectKey(65535) = %v", err)
	}

	for _, objectKey := range []string{"", strings.Repeat("k", 0x10000), "kéy", "packs/\xff.pack"} {
		if _, err := encodeObjectKey(objectKey); !errors.Is(err, ErrMalformedEnvelope) {
			t.Fatalf("encodeObjectKey(%d bytes) = %v, want %v", len(objectKey), err, ErrMalformedEnvelope)
		}
	}

	// The wrap associated data binds the object key, so a door wrapped for
	// one object never unwraps for another.
	gate := testGate(t, 1)
	door := mustDoor(t, gate, fixedKey(), testSecret, DoorPassphrase)
	otherKey := "packs/increments/2-id.pack"
	other := wrapAD(mustEncodedKey(t, otherKey), door)
	if bytes.Equal(other, wrapAD(encoded, door)) {
		t.Fatal("wrap associated data does not bind the object key")
	}
	keyAt := len(wrapDomain) + 1 + 2
	if got := binary.BigEndian.Uint16(other[keyAt : keyAt+2]); got != uint16(len(otherKey)) {
		t.Fatalf("wrap associated data object key length = %d, want %d", got, len(otherKey))
	}
	if !bytes.Equal(other[keyAt+2:keyAt+2+len(otherKey)], []byte(otherKey)) {
		t.Fatalf("wrap associated data object key = %q", other[keyAt+2:keyAt+2+len(otherKey)])
	}
}
