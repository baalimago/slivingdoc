package storage

import (
	"crypto/sha256"
	"testing"
)

func TestMetadataRoundTrip(t *testing.T) {
	meta := Metadata{
		SHA256:     SHA256(sha256.Sum256([]byte("pack bytes"))),
		Size:       12345,
		Kind:       KindCheckpoint,
		Generation: 7,
	}
	got, err := ParseMetadata(meta.Fields())
	if err != nil {
		t.Fatalf("ParseMetadata(meta.Fields()): %v", err)
	}
	if got != meta {
		t.Fatalf("metadata round trip = %+v, want %+v", got, meta)
	}

	// Absent metadata decodes to the zero value: the fields are diagnostic,
	// the manifest descriptor is authoritative.
	got, err = ParseMetadata(nil)
	if err != nil {
		t.Fatalf("ParseMetadata(nil): %v", err)
	}
	if got != (Metadata{}) {
		t.Fatalf("ParseMetadata(nil) = %+v, want the zero value", got)
	}

	malformed := []map[string]string{
		{MetaSHA256: "not-hex"},
		{MetaSHA256: "ABCDEF"},
		{MetaSize: "big"},
		{MetaSize: "-1"},
		{MetaKind: "bogus"},
		{MetaGeneration: "one"},
	}
	for _, md := range malformed {
		if _, err := ParseMetadata(md); err == nil {
			t.Fatalf("ParseMetadata(%v) succeeded, want an error", md)
		}
	}
}
