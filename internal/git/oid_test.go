package git

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOID(t *testing.T) {
	hex := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	oid, err := ParseOID(hex)
	if err != nil {
		t.Fatalf("ParseOID(%q) = %v", hex, err)
	}
	if got := oid.String(); got != hex {
		t.Fatalf("round trip = %q, want %q", got, hex)
	}
	if oid.IsZero() {
		t.Fatal("parsed OID must not be zero")
	}
}

func TestParseOIDRejectsInvalidForms(t *testing.T) {
	cases := []string{
		"",
		"4b825dc642cb6eb9a060e54bf8d69288fbee490",   // 39 chars
		"4b825dc642cb6eb9a060e54bf8d69288fbee49041", // 41 chars
		"4B825DC642CB6EB9A060E54BF8D69288FBEE4904",  // uppercase hex
		"g4b825dc642cb6eb9a060e54bf8d69288fbee490",  // non-hex character
		"4b825dc642cb6eb9a060e54bf8d69288fbee490-",  // trailing dash
		"4b825dc642cb6eb9a060e54bf8d69288fbee49 0",  // space
	}
	for _, c := range cases {
		if _, err := ParseOID(c); !errors.Is(err, ErrInvalidOID) {
			t.Errorf("ParseOID(%q) error = %v, want ErrInvalidOID", c, err)
		}
	}
}

func TestOIDZero(t *testing.T) {
	var zero OID
	if !zero.IsZero() {
		t.Fatal("zero OID must report IsZero")
	}
	if got, want := zero.String(), strings.Repeat("0", 40); got != want {
		t.Fatalf("zero OID String() = %q, want %q", got, want)
	}
}

func TestOIDMarshalJSON(t *testing.T) {
	hex := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	oid, err := ParseOID(hex)
	if err != nil {
		t.Fatalf("ParseOID(%q) = %v", hex, err)
	}
	got, err := oid.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() = %v", err)
	}
	want := `"` + hex + `"`
	if string(got) != want {
		t.Fatalf("MarshalJSON() = %s, want %s", got, want)
	}
}
