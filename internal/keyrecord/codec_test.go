package keyrecord

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestKeyRecordRoundTrip proves a valid record round-trips with the entries
// written in deterministic identity-digest order, whatever order they were
// stored in.
func TestKeyRecordRoundTrip(t *testing.T) {
	f := testFile(t)
	one := workspace.IdentityDigest(testIdentity("one"))
	two := workspace.IdentityDigest(testIdentity("two"))
	first, err := Passphrase(one, "correct horse")
	if err != nil {
		t.Fatalf("Passphrase() = %v", err)
	}
	second, err := PassphraseFile(two, "/keys/two.env")
	if err != nil {
		t.Fatalf("PassphraseFile() = %v", err)
	}
	// Put in reverse order: the encoder owes the file digest order.
	if err := f.Save(Set{}.Put(second).Put(first)); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if entries := got.Entries(); len(entries) != 2 {
		t.Fatalf("Entries() = %d entries, want 2", len(entries))
	}
	for _, want := range []Entry{first, second} {
		entry, err := got.Lookup(want.Digest())
		if err != nil {
			t.Fatalf("Lookup(%s) = %v", want.Digest(), err)
		}
		if entry.Kind() != want.Kind() || entry.Value() != want.Value() {
			t.Fatalf("Lookup(%s) = kind %d value %q, want kind %d value %q", want.Digest(), entry.Kind(), entry.Value(), want.Kind(), want.Value())
		}
	}
	if _, err := got.Lookup("0000000000000000000000000000000000000000000000000000000000000000"); !errors.Is(err, ErrNoMaterial) {
		t.Fatalf("Lookup() of an unrecorded digest = %v, want ErrNoMaterial", err)
	}
	raw, err := os.ReadFile(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	order := []string{one, two}
	sort.Strings(order)
	material := map[string]string{one: `{"passphrase":"correct horse"}`, two: `{"passphraseFile":"/keys/two.env"}`}
	want := `{"version":1,"notebooks":{"` + order[0] + `":` + material[order[0]] + `,"` + order[1] + `":` + material[order[1]] + `}}`
	if string(raw) != want {
		t.Fatalf("file = %s, want %s", raw, want)
	}
}

// TestKeyRecordStrictCodec proves every strict shape violation is refused and
// that a refusal never echoes a stored secret.
func TestKeyRecordStrictCodec(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const secret = "s3cret-value"
	valid := `{"version":1,"notebooks":{"` + digest + `":{"passphrase":"` + secret + `"}}}`
	with := func(old, new string) string { return strings.Replace(valid, old, new, 1) }
	tests := []struct {
		name string
		data string
	}{
		{"not json", `{`},
		{"array", `[]`},
		{"unknown top field", with(`"notebooks"`, `"extra":1,"notebooks"`)},
		{"missing version", `{"notebooks":{}}`},
		{"version as string", `{"version":"1","notebooks":{}}`},
		{"missing notebooks", `{"version":1}`},
		{"notebooks not an object", `{"version":1,"notebooks":[]}`},
		{"duplicate version", `{"version":1,"version":1,"notebooks":{}}`},
		{"null notebooks", `{"version":1,"notebooks":null}`},
		{"null field", with(`"passphrase"`, `"passphrase":null`)},
		{"short digest", `{"version":1,"notebooks":{"abc":{"passphrase":"x"}}}`},
		{"upper digest", with(digest, strings.ToUpper(digest))},
		{"entry not an object", with(`{"passphrase":"`+secret+`"}`, `"nope"`)},
		{"unknown entry field", with(`"passphrase"`, `"token":"`+secret+`","passphrase"`)},
		{"conflicting fields", with(`{"passphrase":"`+secret+`"}`, `{"passphrase":"`+secret+`","passphraseFile":"/k"}`)},
		{"no field", with(`{"passphrase":"`+secret+`"}`, `{}`)},
		{"empty passphrase", with(`"`+secret+`"`, `""`)},
		{"numeric passphrase", with(`"`+secret+`"`, `3`)},
		{"duplicate digest", with(`}}`, `},"`+digest+`":{"passphrase":"other"}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := testFile(t)
			writeFile(t, f.Path(), tt.data, 0o600)
			_, err := f.Load()
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("Load(%s) = %v, want ErrMalformed", tt.data, err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("Load(%s) = %q echoes the secret", tt.data, err)
			}
			raw, readErr := os.ReadFile(f.Path())
			if readErr != nil || string(raw) != tt.data {
				t.Fatalf("the refusal rewrote the file to %q, %v", raw, readErr)
			}
		})
	}
	f := testFile(t)
	writeFile(t, f.Path(), valid, 0o600)
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load() of the valid record = %v", err)
	}
	if entry, err := got.Lookup(digest); err != nil || entry.Kind() != KindPassphrase || entry.Value() != secret {
		t.Fatalf("Lookup() = kind %d value %q, %v; want the stored passphrase", entry.Kind(), entry.Value(), err)
	}
}

// TestKeyRecordUnsupportedVersion proves a file of another version is refused
// with its direction's fix and is never rewritten.
func TestKeyRecordUnsupportedVersion(t *testing.T) {
	for _, row := range []struct {
		data, fix, not string
	}{
		{`{"version":0,"notebooks":{}}`, "store the key material again", "update slivingdoc"},
		{`{"version":2,"notebooks":{},"extra":"s3cret"}`, "update slivingdoc", "remove"},
	} {
		f := testFile(t)
		writeFile(t, f.Path(), row.data, 0o600)
		_, err := f.Load()
		if !errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrMalformed) ||
			!strings.Contains(err.Error(), row.fix) || !strings.Contains(err.Error(), f.Path()) ||
			strings.Contains(err.Error(), row.not) || !strings.Contains(err.Error(), "reads version 1 only") ||
			strings.Contains(err.Error(), "s3cret") {
			t.Fatalf("Load(%s) = %v, want ErrUnsupportedVersion with %q", row.data, err, row.fix)
		}
		if raw, _ := os.ReadFile(f.Path()); string(raw) != row.data {
			t.Fatalf("the refusal rewrote the file to %s", raw)
		}
	}
}

// TestEntryRejectsBadValues proves an entry is refused before it can be stored:
// a non-canonical digest or empty key material is a caller error.
func TestEntryRejectsBadValues(t *testing.T) {
	digest := workspace.IdentityDigest(testIdentity("bad"))
	for _, tt := range []struct {
		name string
		call func() (Entry, error)
	}{
		{"short digest", func() (Entry, error) { return Passphrase("abc", "x") }},
		{"upper digest", func() (Entry, error) { return Passphrase(strings.ToUpper(digest), "x") }},
		{"empty passphrase", func() (Entry, error) { return Passphrase(digest, "") }},
		{"empty path", func() (Entry, error) { return PassphraseFile(digest, "") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.call(); !errors.Is(err, ErrEntry) {
				t.Fatalf("entry = %v, want ErrEntry", err)
			}
		})
	}
}
