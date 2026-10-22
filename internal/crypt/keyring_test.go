package crypt

import (
	"bytes"
	"errors"
	"testing"
)

func TestGenerateNotebookKey(t *testing.T) {
	first, err := GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey() = %v", err)
	}
	second, err := GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey() = %v", err)
	}
	if first == second {
		t.Fatalf("GenerateNotebookKey() = %x twice", first)
	}
	if first == (NotebookKey{}) {
		t.Fatal("GenerateNotebookKey() = the zero key")
	}
}

func TestKeyringCandidateSearch(t *testing.T) {
	gate := testGate(t, 2)
	key := fixedKey()
	passphraseSecret := []byte("passphrase one")
	recoverySecret := []byte("recovery two")
	passphrase := mustDoor(t, gate, key, passphraseSecret, DoorPassphrase)
	recovery := mustDoor(t, gate, key, recoverySecret, DoorRecovery)
	envelope := mustSeal(t, mustUnlocked(t, key, gate), KindCurrent, []byte("content"), passphrase, recovery)

	// Every candidate is tried against every supported door; the first that
	// opens one resolves the keyring.
	keyring, err := NewKeyring([]Candidate{
		{Source: SourcePrompt, Secret: []byte("not it")},
		{Source: SourceEnvironmentFile, Secret: recoverySecret},
	}, gate)
	if err != nil {
		t.Fatalf("NewKeyring() = %v", err)
	}
	plaintext, header, err := keyring.Open(testContext, testObjectKey, envelope)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if string(plaintext) != "content" || header.Kind != KindCurrent {
		t.Fatalf("Open() = %q, kind %d", plaintext, header.Kind)
	}
	if keyring.ResolvedSource() != SourceEnvironmentFile {
		t.Fatalf("ResolvedSource() = %d, want %d", keyring.ResolvedSource(), SourceEnvironmentFile)
	}

	// A second envelope opens with the resolved key and needs no door.
	packEnvelope := mustSeal(t, keyring, KindPack, []byte("pack"), passphrase, recovery)
	if _, _, err := keyring.Open(testContext, testObjectKey, packEnvelope); err != nil {
		t.Fatalf("Open(pack) = %v", err)
	}

	t.Run("first door is reachable", func(t *testing.T) {
		ring, err := NewKeyring([]Candidate{{Source: SourceEnvironment, Secret: passphraseSecret}}, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); err != nil {
			t.Fatalf("Open() = %v", err)
		}
		if ring.ResolvedSource() != SourceEnvironment {
			t.Fatalf("ResolvedSource() = %d, want %d", ring.ResolvedSource(), SourceEnvironment)
		}
	})

	t.Run("no candidate opens a door", func(t *testing.T) {
		ring, err := NewKeyring([]Candidate{{Source: SourceKeyRecord, Secret: []byte("neither")}}, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("Open() = %v, want %v", err, ErrKeyRejected)
		}
		if ring.ResolvedSource() != SourceUnknown {
			t.Fatalf("ResolvedSource() = %d, want %d", ring.ResolvedSource(), SourceUnknown)
		}
	})

	t.Run("no candidate at all", func(t *testing.T) {
		ring, err := NewKeyring(nil, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("Open() = %v, want %v", err, ErrKeyRejected)
		}
	})

	t.Run("candidate added after the first refusal", func(t *testing.T) {
		ring, err := NewKeyring(nil, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if ring.HasCandidates() {
			t.Fatal("a keyring built without candidates reports one")
		}
		if err := ring.AddCandidate(Candidate{Source: SourcePrompt, Secret: []byte("not it")}); err != nil {
			t.Fatalf("AddCandidate() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("Open(wrong typed secret) = %v, want %v", err, ErrKeyRejected)
		}
		// The source rules are the construction rules: a second secret of a
		// source already present is the same invalid set.
		if err := ring.AddCandidate(Candidate{Source: SourcePrompt, Secret: passphraseSecret}); !errors.Is(err, ErrCandidateLimit) {
			t.Fatalf("AddCandidate(duplicate source) = %v, want %v", err, ErrCandidateLimit)
		}
		if err := ring.AddCandidate(Candidate{Source: SourceKeyRecord, Secret: passphraseSecret}); err != nil {
			t.Fatalf("AddCandidate() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); err != nil {
			t.Fatalf("Open(added candidate) = %v", err)
		}
		if ring.ResolvedSource() != SourceKeyRecord {
			t.Fatalf("ResolvedSource() = %d, want %d", ring.ResolvedSource(), SourceKeyRecord)
		}
	})

	t.Run("candidate added to a resolved keyring is a no-op", func(t *testing.T) {
		ring := mustUnlocked(t, key, gate)
		if err := ring.AddCandidate(Candidate{Source: SourceUnknown, Secret: passphraseSecret}); err != nil {
			t.Fatalf("AddCandidate(unknown source) = %v, want no error from a resolved keyring", err)
		}
	})

	t.Run("door is bound to its object key", func(t *testing.T) {
		ring, err := NewKeyring([]Candidate{{Source: SourcePrompt, Secret: passphraseSecret}}, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, "packs/increments/2-id.pack", envelope); !errors.Is(err, ErrKeyRejected) {
			t.Fatalf("Open(wrong object key) = %v, want %v", err, ErrKeyRejected)
		}
	})

	t.Run("candidate bounds", func(t *testing.T) {
		four := []Candidate{
			{Source: SourcePrompt, Secret: testSecret},
			{Source: SourceEnvironment, Secret: testSecret},
			{Source: SourceEnvironmentFile, Secret: testSecret},
			{Source: SourceKeyRecord, Secret: testSecret},
		}
		if _, err := NewKeyring(four, gate); err != nil {
			t.Fatalf("NewKeyring(four) = %v", err)
		}
		for _, candidates := range [][]Candidate{
			append(append([]Candidate{}, four...), Candidate{Source: SourcePrompt, Secret: testSecret}),
			{{Source: SourceUnknown, Secret: testSecret}},
			{
				{Source: SourcePrompt, Secret: testSecret},
				{Source: SourcePrompt, Secret: []byte("other")},
			},
			{{Source: SourcePrompt}},
		} {
			if _, err := NewKeyring(candidates, gate); !errors.Is(err, ErrCandidateLimit) && !errors.Is(err, ErrSecretBounds) {
				t.Fatalf("NewKeyring(%d candidates) = %v, want a refusal", len(candidates), err)
			}
		}
	})

	t.Run("known key ignores candidates", func(t *testing.T) {
		ring, err := NewUnlockedKeyring(key, []Candidate{{Source: SourcePrompt, Secret: []byte("unused")}}, gate)
		if err != nil {
			t.Fatalf("NewUnlockedKeyring() = %v", err)
		}
		if _, _, err := ring.Open(testContext, testObjectKey, envelope); err != nil {
			t.Fatalf("Open() = %v", err)
		}
		if ring.ResolvedSource() != SourceUnknown {
			t.Fatalf("ResolvedSource() = %d, want %d", ring.ResolvedSource(), SourceUnknown)
		}
	})

	t.Run("gate is required", func(t *testing.T) {
		if _, err := NewKeyring(nil, nil); !errors.Is(err, ErrKDFGate) {
			t.Fatalf("NewKeyring(nil gate) = %v, want %v", err, ErrKDFGate)
		}
		if _, err := NewUnlockedKeyring(key, nil, nil); !errors.Is(err, ErrKDFGate) {
			t.Fatalf("NewUnlockedKeyring(nil gate) = %v, want %v", err, ErrKDFGate)
		}
	})

	t.Run("locked keyring cannot seal", func(t *testing.T) {
		ring, err := NewKeyring([]Candidate{{Source: SourcePrompt, Secret: passphraseSecret}}, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		if _, err := ring.Seal(testObjectKey, KindCurrent, []byte("x"), []Door{passphrase}); !errors.Is(err, ErrKeyringLocked) {
			t.Fatalf("Seal() = %v, want %v", err, ErrKeyringLocked)
		}
		if _, err := ring.Rewrap(testObjectKey, envelope, []Door{passphrase}); !errors.Is(err, ErrKeyringLocked) {
			t.Fatalf("Rewrap() = %v, want %v", err, ErrKeyringLocked)
		}
	})
}

func TestDoorRewritePreservesPayload(t *testing.T) {
	gate := testGate(t, 2)
	key := fixedKey()
	keyring := mustUnlocked(t, key, gate)
	first := mustDoor(t, gate, key, []byte("first secret"), DoorPassphrase)
	second := mustDoor(t, gate, key, []byte("second secret"), DoorRecovery)
	envelope := mustSeal(t, keyring, KindCurrent, []byte("note: hello\n"), first)

	rewritten, err := keyring.Rewrap(testObjectKey, envelope, []Door{first, second})
	if err != nil {
		t.Fatalf("Rewrap() = %v", err)
	}
	if !bytes.Equal(payloadOf(envelope), payloadOf(rewritten)) {
		t.Fatal("Rewrap changed the payload ciphertext")
	}
	if bytes.Equal(commitmentOf(envelope), commitmentOf(rewritten)) {
		t.Fatal("Rewrap left the header commitment unchanged")
	}
	if headerLengthOf(rewritten) <= headerLengthOf(envelope) {
		t.Fatal("Rewrap did not add the new door")
	}
	plaintext, header, err := keyring.Open(testContext, testObjectKey, rewritten)
	if err != nil {
		t.Fatalf("Open(rewritten) = %v", err)
	}
	if string(plaintext) != "note: hello\n" || len(header.Doors) != 2 {
		t.Fatalf("Open(rewritten) = %q with %d doors", plaintext, len(header.Doors))
	}

	// The new door resolves the same notebook key.
	doorSearch, err := NewKeyring([]Candidate{{Source: SourcePrompt, Secret: []byte("second secret")}}, gate)
	if err != nil {
		t.Fatalf("NewKeyring() = %v", err)
	}
	if _, _, err := doorSearch.Open(testContext, testObjectKey, rewritten); err != nil {
		t.Fatalf("Open(second door) = %v", err)
	}

	// The commitment covers the door list, so a changed door record fails.
	wrappedKeyAt := preambleSize + coreSize + 2 + doorBodySize - 1
	tampered := bytes.Clone(rewritten)
	tampered[wrappedKeyAt] ^= 1
	if _, _, err := keyring.Open(testContext, testObjectKey, tampered); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Open(tampered door) = %v, want %v", err, ErrAuthentication)
	}
	if _, err := keyring.Rewrap(testObjectKey, tampered, []Door{first}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Rewrap(tampered) = %v, want %v", err, ErrAuthentication)
	}

	t.Run("extensions and opaque doors are preserved", func(t *testing.T) {
		withExtension := mustSealWithExtensions(t, keyring, []Door{first}, []Extension{{Type: 0x2a, Value: []byte{7, 8}}})
		unknown := make([]byte, 4+doorBodySize)
		unknown[0] = 9
		unknown[1] = 1
		unknown[2], unknown[3] = 0, doorBodySize
		parsed, err := decodeEnvelope(withExtension)
		if err != nil {
			t.Fatalf("decodeEnvelope() = %v", err)
		}
		macKey, err := deriveKey(key, headerPurpose, mustEncodedKey(t, testObjectKey))
		if err != nil {
			t.Fatalf("deriveKey() = %v", err)
		}
		doorSection, err := encodeDoorSection([][]byte{unknown})
		if err != nil {
			t.Fatalf("encodeDoorSection() = %v", err)
		}
		extensionSection, err := encodeExtensionSection([]Extension{{Type: 0x2a, Value: []byte{7, 8}}})
		if err != nil {
			t.Fatalf("encodeExtensionSection() = %v", err)
		}
		header, err := encodeHeader(KindCurrent, parsed.body[:coreSize], doorSection, extensionSection, macKey, mustEncodedKey(t, testObjectKey))
		if err != nil {
			t.Fatalf("encodeHeader() = %v", err)
		}
		opaque := assembleEnvelope(KindCurrent, header, parsed.payload)

		rewrittenOpaque, err := keyring.Rewrap(testObjectKey, opaque, []Door{first})
		if err != nil {
			t.Fatalf("Rewrap(opaque) = %v", err)
		}
		result, err := decodeEnvelope(rewrittenOpaque)
		if err != nil {
			t.Fatalf("decodeEnvelope(rewritten) = %v", err)
		}
		if len(result.knownDoors) != 1 || len(result.unknownDoors) != 1 {
			t.Fatalf("rewritten doors = %d known, %d opaque, want 1 and 1", len(result.knownDoors), len(result.unknownDoors))
		}
		if len(result.header.Extensions) != 1 || !bytes.Equal(result.header.Extensions[0].Value, []byte{7, 8}) {
			t.Fatalf("rewritten extensions = %+v", result.header.Extensions)
		}
	})
}

// mustSealWithExtensions seals a current envelope that carries an extension.
func mustSealWithExtensions(t *testing.T, keyring *Keyring, doors []Door, extensions []Extension) []byte {
	t.Helper()
	envelope, err := keyring.seal(testObjectKey, KindCurrent, []byte("note: hello\n"), doors, extensions)
	if err != nil {
		t.Fatalf("seal() = %v", err)
	}
	return envelope
}

func commitmentOf(envelope []byte) []byte {
	end := preambleSize + headerLengthOf(envelope)
	return envelope[end-commitmentSize : end]
}
