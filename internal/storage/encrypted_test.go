package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/baalimago/slivingdoc/internal/crypt"
)

// cheapParams is the narrowest accepted Argon2id cost, so a codec test does
// not pay the canonical 256 MiB per door.
func cheapParams() crypt.KDFParams {
	return crypt.KDFParams{MemoryKiB: 64 << 10, Iterations: 1, Parallelism: 1}
}

// codecKeyring builds a keyring that holds a fresh notebook key and one
// passphrase door wrapped for current.
func codecKeyring(t *testing.T) (*crypt.Keyring, crypt.Door) {
	t.Helper()
	gate, err := crypt.NewKDFGate(1)
	if err != nil {
		t.Fatalf("NewKDFGate: %v", err)
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey: %v", err)
	}
	door, err := crypt.WrapKey(context.Background(), gate, key, []byte("a passphrase"), CurrentKey, crypt.DoorPassphrase, cheapParams())
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	seen, err := crypt.NewUnlockedKeyring(key, nil, gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring: %v", err)
	}
	return seen, door
}

// TestEncryptedManifestCodec proves the version-two wrapper is strict and
// that its decrypted payload is the unchanged version-one manifest. A
// newer wrapper version is UpgradeRequiredError before any other field; a
// malformed or invalid wrapper is ErrIntegrity.
func TestEncryptedManifestCodec(t *testing.T) {
	keyring, door := codecKeyring(t)
	manifest, err := EncodeManifest(fixtureManifest())
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	envelope, err := keyring.Seal(CurrentKey, crypt.KindCurrent, manifest, []crypt.Door{door})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	wrapped, err := WrapCurrentEnvelope(envelope)
	if err != nil {
		t.Fatalf("WrapCurrentEnvelope: %v", err)
	}

	// The wrapper carries exactly the ordered fields version and envelope,
	// as compact JSON with no trailing newline.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(wrapped, &obj); err != nil {
		t.Fatalf("wrapper is not JSON: %v", err)
	}
	if len(obj) != 2 {
		t.Fatalf("wrapper fields = %v, want exactly version and envelope", obj)
	}
	if string(obj["version"]) != "2" {
		t.Fatalf("wrapper version = %s, want 2", obj["version"])
	}
	if bytes.Contains(wrapped, []byte("\n")) {
		t.Fatal("wrapper carries a trailing newline")
	}

	encoding, got, err := ClassifyCurrent(wrapped)
	if err != nil || encoding != CurrentEncrypted {
		t.Fatalf("ClassifyCurrent(wrapper) = %d, %v; want CurrentEncrypted", encoding, err)
	}
	if !bytes.Equal(got, envelope) {
		t.Fatal("ClassifyCurrent did not return the wrapped envelope")
	}
	plaintext, _, err := keyring.Open(context.Background(), CurrentKey, got)
	if err != nil {
		t.Fatalf("Open(wrapper envelope): %v", err)
	}
	if !bytes.Equal(plaintext, manifest) {
		t.Fatal("the decrypted payload is not the unchanged version-one manifest")
	}
	decoded, err := DecodeManifest(plaintext)
	if err != nil {
		t.Fatalf("DecodeManifest(decrypted): %v", err)
	}
	if !reflect.DeepEqual(decoded, fixtureManifest()) {
		t.Fatal("the decrypted manifest decoded to a different value")
	}

	cases := []struct {
		name    string
		data    string
		kind    CurrentEncoding
		wantErr error
		upgrade bool
	}{
		{name: "plaintext version one", data: `{"version":1,"generation":1}`, kind: CurrentPlaintext},
		{name: "not json", data: `not json`, kind: CurrentPlaintext},
		{name: "not an object", data: `[1,2,3]`, kind: CurrentPlaintext},
		{name: "no version", data: `{"envelope":"AAAA"}`, kind: CurrentPlaintext},
		{name: "duplicate version", data: `{"version":2,"version":2,"envelope":"AAAA"}`, kind: CurrentPlaintext},
		{name: "missing envelope", data: `{"version":2}`, wantErr: ErrIntegrity},
		{name: "envelope not a string", data: `{"version":2,"envelope":7}`, wantErr: ErrIntegrity},
		{name: "invalid base64", data: `{"version":2,"envelope":"!!"}`, wantErr: ErrIntegrity},
		{name: "unknown wrapper field", data: `{"version":2,"envelope":"AAAA","extra":1}`, wantErr: ErrIntegrity},
		{name: "null field", data: `{"version":2,"envelope":null}`, kind: CurrentPlaintext},
		{name: "newer version", data: `{"version":3,"generation":1}`, upgrade: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			encoding, _, err := ClassifyCurrent([]byte(tt.data))
			var upgrade *UpgradeRequiredError
			switch {
			case tt.upgrade:
				if !errors.As(err, &upgrade) || upgrade.Version != 3 {
					t.Fatalf("ClassifyCurrent(%s) = %v, want UpgradeRequiredError version 3", tt.data, err)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ClassifyCurrent(%s) = %v, want %v", tt.data, err, tt.wantErr)
				}
			default:
				if err != nil || encoding != tt.kind {
					t.Fatalf("ClassifyCurrent(%s) = %d, %v, want %d", tt.data, encoding, err, tt.kind)
				}
			}
		})
	}

	t.Run("oversized envelope is refused", func(t *testing.T) {
		if _, err := WrapCurrentEnvelope(make([]byte, crypt.CurrentEnvelopeLimit)); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("WrapCurrentEnvelope(oversized) = %v, want %v", err, ErrIntegrity)
		}
	})
}

// TestEncryptedManifestRollback proves a manifest below the workspace's
// recorded baseline is refused as rollback, while a cold workspace and an
// unchanged generation are accepted.
func TestEncryptedManifestRollback(t *testing.T) {
	cases := []struct {
		name     string
		manifest uint64
		baseline uint64
		wantErr  bool
	}{
		{name: "cold workspace", manifest: 1, baseline: 0},
		{name: "equal generation", manifest: 5, baseline: 5},
		{name: "ahead of the baseline", manifest: 6, baseline: 5},
		{name: "below the baseline", manifest: 1, baseline: 5, wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := Manifest{Generation: tt.manifest}
			err := CheckManifestRollback(m, tt.baseline)
			if tt.wantErr {
				if !errors.Is(err, ErrIntegrity) {
					t.Fatalf("CheckManifestRollback(%d, %d) = %v, want %v", tt.manifest, tt.baseline, err, ErrIntegrity)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckManifestRollback(%d, %d) = %v, want nil", tt.manifest, tt.baseline, err)
			}
		})
	}
}
