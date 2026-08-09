package storage

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestSHA256RoundTrip(t *testing.T) {
	sum := sha256.Sum256([]byte("pack bytes"))
	h := SHA256(sum)
	s := h.String()
	if len(s) != 64 {
		t.Fatalf("String() length = %d, want 64", len(s))
	}
	back, err := ParseSHA256(s)
	if err != nil {
		t.Fatalf("ParseSHA256(String()) = %v", err)
	}
	if back != h {
		t.Fatal("parse round trip changed the value")
	}
}

func TestParseSHA256RejectsInvalidForms(t *testing.T) {
	valid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []string{
		"",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("A", 64), // uppercase
		strings.Repeat("g", 64), // non-hex
		valid + " ",             // trailing space
		" " + valid,             // leading space
	}
	for _, c := range cases {
		if _, err := ParseSHA256(c); !errors.Is(err, ErrInvalidSHA256) {
			t.Errorf("ParseSHA256(%q) error = %v, want ErrInvalidSHA256", c, err)
		}
	}
}

func TestSHA256MarshalJSON(t *testing.T) {
	sum := sha256.Sum256([]byte("pack bytes"))
	h := SHA256(sum)
	got, err := h.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() = %v", err)
	}
	if string(got) != `"`+h.String()+`"` {
		t.Fatalf("MarshalJSON() = %s", got)
	}
}
