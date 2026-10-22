// Package keyrecord owns the stored key material of encrypted notebooks: the
// strict, versioned notebooks.json file of the user configuration directory
// and the candidate resolver that turns the environment, its envfile and that
// file into the secrets a keyring tries
// (worklogs/26-10-05-encrypted-notebooks/phase-3-key-material.md).
//
// It is deliberately separate from workspaces.json (internal/settings): a
// workspace record names where a notebook lives, never how to unlock it. The
// file carries a passphrase or the path of the envfile that holds it, never a
// notebook key, a wrapped key, or any other secret.
package keyrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/baalimago/slivingdoc/internal/pathutil"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// FileName is the key record file inside the configuration directory, beside
// the settings file.
const FileName = "notebooks.json"

// FormatVersion is the only key record file version this build reads.
const FormatVersion = 1

// The environment variables that name a passphrase and the file that holds
// it. The resolver reads them; no flag supplies a secret.
const (
	PassphraseEnv     = "SLIVINGDOC_PASSPHRASE"
	PassphraseFileEnv = "SLIVINGDOC_PASSPHRASE_FILE"
)

var (
	// ErrNoConfigDir reports that neither credentials.DirEnv nor the
	// platform's user configuration directory resolves from the injected
	// environment.
	ErrNoConfigDir = errors.New("keyrecord: no configuration directory")
	// ErrMalformed reports a key record file this build cannot read.
	ErrMalformed = errors.New("keyrecord: malformed key record file")
	// ErrUnsupportedVersion reports a key record file of a version this
	// build does not read. It is never rewritten: an older file is removed
	// by the person, and a newer build's is kept while slivingdoc is
	// updated instead.
	ErrUnsupportedVersion = errors.New("keyrecord: unsupported key record file version")
	// ErrExposed reports a key record file, or its directory, that other
	// users could read or change.
	ErrExposed = errors.New("keyrecord: the key record file is not private")
	// ErrTooLarge reports a key record file at or over the bound.
	ErrTooLarge = errors.New("keyrecord: the key record file is too large")
	// ErrNoMaterial reports that no key material is stored for a notebook
	// identity.
	ErrNoMaterial = errors.New("keyrecord: no key material stored for this notebook")
	// ErrEntry reports a record that cannot be built or stored: not a
	// canonical identity digest, or no key material.
	ErrEntry = errors.New("keyrecord: invalid entry")
	// ErrEnvfile reports that a passphrase file cannot supply a secret. It
	// never names the file's path.
	ErrEnvfile = errors.New("keyrecord: the passphrase file cannot supply a secret")
)

// digestLength is the hexadecimal length of the identity digest that keys the
// file: the full SHA-256 digest of a notebook storage identity
// (workspace.IdentityDigest).
const digestLength = 64

// fieldPassphraseFile is the record field, and the error label, of a stored
// envfile path.
const fieldPassphraseFile = "passphraseFile"

// Kind names the field that carries one entry's key material.
type Kind uint8

const (
	// KindEmpty is the zero entry: it names no key material.
	KindEmpty Kind = iota
	// KindPassphrase carries a plaintext passphrase.
	KindPassphrase
	// KindPassphraseFile carries the expanded path of the envfile that holds
	// the passphrase.
	KindPassphraseFile
)

// Entry is one stored key-material record: the digest of the notebook identity
// it belongs to and exactly one of a plaintext passphrase or the path of the
// envfile that holds it.
type Entry struct {
	digest string
	kind   Kind
	value  string
}

// Passphrase returns an entry that stores the passphrase for the identity
// digest, at rest and in the clear.
func Passphrase(digest, secret string) (Entry, error) {
	return newEntry(digest, KindPassphrase, secret)
}

// PassphraseFile returns an entry that stores the expanded path of the envfile
// that holds the passphrase for the identity digest.
func PassphraseFile(digest, path string) (Entry, error) {
	expanded, err := pathutil.ExpandHome(path)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: %s: %w", ErrEntry, fieldPassphraseFile, err)
	}
	return newEntry(digest, KindPassphraseFile, expanded)
}

func newEntry(digest string, kind Kind, value string) (Entry, error) {
	if !canonicalDigest(digest) {
		return Entry{}, fmt.Errorf("%w: the identity digest is not 64 lowercase hexadecimal characters", ErrEntry)
	}
	if value == "" {
		return Entry{}, fmt.Errorf("%w: empty key material", ErrEntry)
	}
	return Entry{digest: digest, kind: kind, value: value}, nil
}

// Digest returns the notebook identity digest the entry is stored under.
func (e Entry) Digest() string { return e.digest }

// Kind reports which key material the entry carries.
func (e Entry) Kind() Kind { return e.kind }

// Value returns the passphrase or the envfile path the entry carries; the
// empty string for the zero entry.
func (e Entry) Value() string { return e.value }

// canonicalDigest reports whether s is a lowercase hexadecimal SHA-256
// digest: the form workspace.IdentityDigest produces.
func canonicalDigest(s string) bool {
	if len(s) != digestLength {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Set is the content of the key record file: one entry per notebook identity
// digest, in the order loaded. The zero value is an empty file.
type Set struct {
	entries []Entry
}

// Entries returns a copy of the stored entries in load order.
func (s Set) Entries() []Entry { return slices.Clone(s.entries) }

// Lookup returns the key material stored for exactly digest, or ErrNoMaterial.
func (s Set) Lookup(digest string) (Entry, error) {
	for _, e := range s.entries {
		if e.digest == digest {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNoMaterial, digest)
}

// Put returns the set with e stored: an entry whose digest is identical is
// replaced where it stands, so a rewrite does not move it.
func (s Set) Put(e Entry) Set {
	out := Set{entries: slices.Clone(s.entries)}
	if i := slices.IndexFunc(out.entries, func(o Entry) bool { return o.digest == e.digest }); i >= 0 {
		out.entries[i] = e
		return out
	}
	out.entries = append(out.entries, e)
	return out
}

// Remove returns the set without the entry stored for digest.
func (s Set) Remove(digest string) Set {
	out := Set{entries: slices.Clone(s.entries)}
	out.entries = slices.DeleteFunc(out.entries, func(e Entry) bool { return e.digest == digest })
	return out
}

// The JSON field names of the file (phase-3-key-material.md, Record schema).
const (
	fieldVersion    = "version"
	fieldNotebooks  = "notebooks"
	fieldPassphrase = "passphrase"
)

type fileJSON struct {
	Version   int                     `json:"version"`
	Notebooks map[string]materialJSON `json:"notebooks"`
}

type materialJSON struct {
	Passphrase     string `json:"passphrase,omitempty"`
	PassphraseFile string `json:"passphraseFile,omitempty"`
}

// encode writes the set as compact JSON with the fields in the normative order
// and the entries in digest order, with HTML escaping disabled and no trailing
// newline, the same shape the settings and workspace records use.
func encode(s Set) ([]byte, error) {
	out := fileJSON{Version: FormatVersion, Notebooks: make(map[string]materialJSON, len(s.entries))}
	for _, e := range s.entries {
		switch e.kind {
		case KindPassphrase:
			out.Notebooks[e.digest] = materialJSON{Passphrase: e.value}
		case KindPassphraseFile:
			out.Notebooks[e.digest] = materialJSON{PassphraseFile: e.value}
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decode reads the file strictly, like every other slivingdoc protocol record:
// unknown, duplicate, missing and null fields are refused, an entry carries
// exactly one key-material field, and a version other than FormatVersion is a
// versionError that is never rewritten.
func decode(data []byte) (Set, error) {
	root, err := strictjson.Parse(data)
	if err != nil {
		return Set{}, err
	}
	if root.Kind != strictjson.Object {
		return Set{}, errors.New("the top level is not an object")
	}
	ver, ok := root.Field(fieldVersion)
	if !ok || ver.Kind != strictjson.Number {
		return Set{}, errors.New("missing numeric version")
	}
	if ver.Num != FormatVersion {
		return Set{}, &versionError{got: ver.Num}
	}
	if err := root.RejectUnknown(fieldVersion, fieldNotebooks); err != nil {
		return Set{}, err
	}
	list, ok := root.Field(fieldNotebooks)
	if !ok || list.Kind != strictjson.Object {
		return Set{}, errors.New("missing notebooks object")
	}
	var set Set
	for i, f := range list.Obj {
		if !canonicalDigest(f.Name) {
			return Set{}, fmt.Errorf("notebooks[%d]: the key is not a canonical identity digest", i)
		}
		entry, err := decodeEntry(f.Value, f.Name)
		if err != nil {
			return Set{}, fmt.Errorf("notebooks[%d]: %w", i, err)
		}
		set.entries = append(set.entries, entry)
	}
	return set, nil
}

func decodeEntry(v strictjson.Value, digest string) (Entry, error) {
	if v.Kind != strictjson.Object {
		return Entry{}, errors.New("not an object")
	}
	if err := v.RejectUnknown(fieldPassphrase, fieldPassphraseFile); err != nil {
		return Entry{}, err
	}
	secret, hasSecret := v.Field(fieldPassphrase)
	path, hasPath := v.Field(fieldPassphraseFile)
	switch {
	case hasSecret && hasPath:
		return Entry{}, errors.New("both passphrase and passphraseFile are set")
	case !hasSecret && !hasPath:
		return Entry{}, errors.New("neither passphrase nor passphraseFile is set")
	}
	field, name, kind := secret, fieldPassphrase, KindPassphrase
	if hasPath {
		field, name, kind = path, fieldPassphraseFile, KindPassphraseFile
	}
	if field.Kind != strictjson.String {
		return Entry{}, fmt.Errorf("field %q is not a string", name)
	}
	if field.Str == "" {
		return Entry{}, fmt.Errorf("field %q is empty", name)
	}
	return Entry{digest: digest, kind: kind, value: field.Str}, nil
}

// versionError is ErrUnsupportedVersion for a file of version got.
type versionError struct {
	got uint64
}

func (e *versionError) Error() string {
	return fmt.Sprintf("%s: it is version %d, and this slivingdoc reads version %d only", ErrUnsupportedVersion, e.got, FormatVersion)
}

func (e *versionError) Unwrap() error { return ErrUnsupportedVersion }

// withFix adds the fix for the file at path: a newer build's file is kept and
// slivingdoc updated; an older one is removed, and the key material stored
// again.
func (e *versionError) withFix(path string) error {
	if e.got > FormatVersion {
		return fmt.Errorf("%w; a newer slivingdoc wrote it (%s); update slivingdoc, and keep the file", e, path)
	}
	return fmt.Errorf("%w; remove %s and store the key material again", e, path)
}
