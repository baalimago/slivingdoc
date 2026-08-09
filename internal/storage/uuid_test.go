package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestNewUUIDv7(t *testing.T) {
	id, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7() = %v", err)
	}
	s := id.String()
	if len(s) != 36 {
		t.Fatalf("String() length = %d, want 36", len(s))
	}
	back, err := ParseUUIDv7(s)
	if err != nil {
		t.Fatalf("ParseUUIDv7(String()) = %v", err)
	}
	if back != id {
		t.Fatal("parse round trip changed the value")
	}
	if !strings.Contains(s, "-") {
		t.Fatal("String() has no dashes")
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef-", c) {
			t.Fatalf("String() contains non-canonical character %q", c)
		}
	}
}

func TestParseUUIDv7RejectsInvalidForms(t *testing.T) {
	cases := []string{
		"",
		"01973e12-8b34-7b01-9e2f-00000000000",           // 35 chars
		"01973e12-8b34-7b01-9e2f-0000000000000",         // 37 chars
		"01973E12-8B34-7B01-9E2F-000000000001",          // uppercase
		"01973e12-8b34-6b01-9e2f-000000000001",          // version 6, not 7
		"01973e12-8b34-7b01-4e2f-000000000001",          // variant 0100, not 10xx
		"01973e12-8b34-7b01-9e2f-0000000000011",         // 13 chars in last group
		"01973e128b347b019e2f000000000001",              // no dashes
		"01973e12-8b34-7b01-9e2f-00000000000g",          // non-hex
		"01973e12-8b34-7b01-9e2f-00000000000-",          // dash in wrong place
		" 01973e12-8b34-7b01-9e2f-000000000001",         // leading space
		"{01973e12-8b34-7b01-9e2f-000000000001}",        // braces
		"urn:uuid:01973e12-8b34-7b01-9e2f-000000000001", // urn prefix
	}
	for _, c := range cases {
		if _, err := ParseUUIDv7(c); !errors.Is(err, ErrInvalidUUID) {
			t.Errorf("ParseUUIDv7(%q) error = %v, want ErrInvalidUUID", c, err)
		}
	}
}

func TestUUIDMarshalJSON(t *testing.T) {
	s := "01973e12-8b34-7b01-9e2f-000000000001"
	id, err := ParseUUIDv7(s)
	if err != nil {
		t.Fatalf("ParseUUIDv7: %v", err)
	}
	got, err := id.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() = %v", err)
	}
	if string(got) != `"`+s+`"` {
		t.Fatalf("MarshalJSON() = %s, want %q", got, `"`+s+`"`)
	}
}

func TestUUIDZero(t *testing.T) {
	var zero UUID
	if !zero.IsZero() {
		t.Fatal("zero UUID must report IsZero")
	}
	if _, err := ParseUUIDv7(zero.String()); !errors.Is(err, ErrInvalidUUID) {
		t.Fatalf("zero UUID must not parse as v7: %v", err)
	}
}
