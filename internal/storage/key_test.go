package storage

import (
	"errors"
	"testing"
)

func TestParseKeyRoundTrip(t *testing.T) {
	id, err := ParseUUIDv7("01973e12-8b34-7b01-9e2f-000000000001")
	if err != nil {
		t.Fatalf("ParseUUIDv7: %v", err)
	}
	for _, k := range []Key{
		{Kind: KindCheckpoint, Generation: 8119, ID: id},
		{Kind: KindIncrement, Generation: 8120, ID: id},
	} {
		got, err := ParseKey(k.String())
		if err != nil {
			t.Fatalf("ParseKey(%q) = %v", k.String(), err)
		}
		if got != k {
			t.Fatalf("ParseKey(%q) = %+v, want %+v", k.String(), got, k)
		}
	}
}

func TestParseKeyRejectsInvalidForms(t *testing.T) {
	id := "01973e12-8b34-7b01-9e2f-000000000001"
	cases := []string{
		"",
		"packs",
		"packs/",
		"packs/checkpoints",
		"packs/checkpoints/",
		"packs/increments/8119-" + id, // missing .pack
		"packs/increments/8119-" + id + ".pack.x",                          // extra suffix
		"packs/increments/8119-" + id + ".pack/",                           // trailing segment
		"packs/checkpoints/8119-" + id + ".pack/",                          // trailing slash
		"packs/foo/8119-" + id + ".pack",                                   // unknown namespace
		"packs/checkpoints/x-" + id + ".pack",                              // non-numeric generation
		"packs/checkpoints/08119-" + id + ".pack",                          // leading zeros
		"packs/checkpoints/-" + id + ".pack",                               // empty generation
		"packs/checkpoints/8119-.pack",                                     // empty id
		"packs/checkpoints/8119-01973e12.pack",                             // truncated uuid
		"packs/checkpoints/8119-01973e12-8b34-7b01-9e2f-00000000000G.pack", // non-hex
		"packs/checkpoints/8119-01973e12-8b34-6b01-9e2f-000000000001.pack", // wrong version
		"packs/checkpoints/8119-" + id + ".pack/..",                        // .. segment
		"packs/../checkpoints/8119-" + id + ".pack",                        // .. segment
		"packs//checkpoints/8119-" + id + ".pack",                          // empty segment
		"packs/checkpoints\\8119-" + id + ".pack",                          // backslash
		"/packs/checkpoints/8119-" + id + ".pack",                          // absolute
		"PACKS/checkpoints/8119-" + id + ".pack",                           // uppercase namespace
	}
	for _, c := range cases {
		if _, err := ParseKey(c); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParseKey(%q) error = %v, want ErrInvalidKey", c, err)
		}
	}
}

func TestValidatePrefix(t *testing.T) {
	for _, ok := range []string{"", "a", "notebooks", "notebooks/alpha", "a/b/c"} {
		if err := ValidatePrefix(ok); err != nil {
			t.Errorf("ValidatePrefix(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"/", "/a", "a/", "a//b", "a/./b", "a/../b", ".", "..", "a\\b", "a\\", "a/..",
	} {
		if err := ValidatePrefix(bad); !errors.Is(err, ErrInvalidPrefix) {
			t.Errorf("ValidatePrefix(%q) error = %v, want ErrInvalidPrefix", bad, err)
		}
	}
}

func TestJoinKey(t *testing.T) {
	if got := JoinKey("", "current"); got != "current" {
		t.Fatalf("JoinKey(\"\", current) = %q", got)
	}
	if got := JoinKey("notebooks/alpha", "current"); got != "notebooks/alpha/current" {
		t.Fatalf("JoinKey = %q", got)
	}
}
