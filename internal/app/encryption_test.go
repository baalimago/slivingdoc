package app

import (
	"context"
	"errors"
	"testing"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git2"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
)

// TestEncryptDoorLimit proves the four-door bound and the empty-door
// refusal of the door-set rewrite. A notebook cannot reach the bound through
// this build's two door types, so the rewrite is the only place the cap can
// be exercised (worklogs/26-10-05-encrypted-notebooks/
// phase-4-command-surface.md, Options and command parameters).
func TestEncryptDoorLimit(t *testing.T) {
	t.Parallel()
	doors := []crypt.Door{
		{Type: crypt.DoorPassphrase},
		{Type: crypt.DoorRecovery},
		{Type: crypt.DoorType(10)},
		{Type: crypt.DoorType(11)},
	}

	added := crypt.Door{Type: crypt.DoorType(20)}
	_, err := replaceDoor(doors, added.Type, &added)
	var refusal *notebook.Error
	if !errors.As(err, &refusal) || refusal.Code != notebook.CodeInvalidRequest ||
		refusal.Reason != notebook.ReasonDoorLimit || refusal.Action != notebook.ActionFixInput {
		t.Fatalf("replaceDoor at the cap = %v, want INVALID_REQUEST/DOOR_LIMIT", err)
	}

	_, err = replaceDoor([]crypt.Door{{Type: crypt.DoorPassphrase}}, crypt.DoorPassphrase, nil)
	if !errors.As(err, &refusal) || refusal.Reason != notebook.ReasonMalformedInput {
		t.Fatalf("replaceDoor removing the last door = %v, want a malformed-input refusal", err)
	}

	replaced, err := replaceDoor(doors, crypt.DoorRecovery, &crypt.Door{Type: crypt.DoorRecovery})
	if err != nil {
		t.Fatalf("replaceDoor at the cap replacing an existing door = %v", err)
	}
	if len(replaced) != maxDoors {
		t.Fatalf("replaceDoor at the cap = %d doors, want %d", len(replaced), maxDoors)
	}
}

// cheapParamKDF is the Argon2id cost the secret-prompt tests derive doors
// with: the canonical cost is deliberately expensive and this unit covers no
// KDF decision.
func cheapParamKDF() crypt.KDFParams {
	return crypt.KDFParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}
}

// encryptedCurrentStore returns a fake store that holds one encrypted current
// wrapper, and the envelope the assertions open.
func encryptedCurrentStore(t *testing.T, prefix string, secret []byte) (*fake.Store, []byte) {
	t.Helper()
	ctx := context.Background()
	params := cheapParamKDF()
	gate, err := crypt.NewKDFGateForCost(1, params)
	if err != nil {
		t.Fatalf("NewKDFGateForCost() = %v", err)
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		t.Fatalf("GenerateNotebookKey() = %v", err)
	}
	door, err := crypt.WrapKey(ctx, gate, key, secret, storage.CurrentKey, crypt.DoorPassphrase, params)
	if err != nil {
		t.Fatalf("WrapKey() = %v", err)
	}
	seal, err := crypt.NewUnlockedKeyring(key, nil, gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring() = %v", err)
	}
	envelope, err := seal.Seal(storage.CurrentKey, crypt.KindCurrent, []byte(`{"version":1,"generation":1}`), []crypt.Door{door})
	if err != nil {
		t.Fatalf("Seal() = %v", err)
	}
	wrapped, err := storage.WrapCurrentEnvelope(envelope)
	if err != nil {
		t.Fatalf("WrapCurrentEnvelope() = %v", err)
	}
	store := fake.New(prefix)
	if _, err := store.CreateObject(ctx, storage.CurrentKey, wrapped); err != nil {
		t.Fatalf("CreateObject() = %v", err)
	}
	return store, envelope
}

// newSecretService wires a service over one store and keyring, which is all
// the secret prompt needs: no notebook operation runs.
func newSecretService(t *testing.T, store storage.ObjectStore, ring *crypt.Keyring) *Service {
	t.Helper()
	eng := git2.New()
	if err := eng.Open(); err != nil {
		t.Fatalf("git2.Open() = %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	cfg := testServiceConfig(t)
	svc, err := NewService(eng, store, cfg.serviceConfig(), nil, ring)
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// TestServiceStoredCurrent proves the classification the secret prompt asks
// before a pull or commit: an absent current, a plaintext one, an encrypted
// wrapper, and a store that cannot answer.
func TestServiceStoredCurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		svc := newSecretService(t, fake.New("absent"), nil)
		if got := svc.storedCurrent(ctx); got != storedCurrentAbsent {
			t.Fatalf("storedCurrent() = %d, want %d", got, storedCurrentAbsent)
		}
	})

	t.Run("plaintext", func(t *testing.T) {
		t.Parallel()
		store := fake.New("plaintext")
		if _, err := store.CreateObject(ctx, storage.CurrentKey, []byte(`{"version":1}`)); err != nil {
			t.Fatalf("CreateObject() = %v", err)
		}
		svc := newSecretService(t, store, nil)
		if got := svc.storedCurrent(ctx); got != storedCurrentPlaintext {
			t.Fatalf("storedCurrent() = %d, want %d", got, storedCurrentPlaintext)
		}
	})

	t.Run("encrypted", func(t *testing.T) {
		t.Parallel()
		store, _ := encryptedCurrentStore(t, "encrypted", []byte("secret"))
		svc := newSecretService(t, store, nil)
		if got := svc.storedCurrent(ctx); got != storedCurrentEncrypted {
			t.Fatalf("storedCurrent() = %d, want %d", got, storedCurrentEncrypted)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		t.Parallel()
		store, _ := encryptedCurrentStore(t, "unreadable", []byte("secret"))
		store.FailNextKey(fake.OpGet, storage.CurrentKey, storage.ErrTransport)
		svc := newSecretService(t, store, nil)
		if got := svc.storedCurrent(ctx); got != storedCurrentUnknown {
			t.Fatalf("storedCurrent() = %d, want %d", got, storedCurrentUnknown)
		}
	})
}

// TestServiceAskForSecret proves the prompt seam: an encrypted notebook with
// no configured secret asks once and adopts the answer for this run, while a
// plaintext notebook, a configured secret, and an unreadable store ask
// nothing, and an empty answer is the KEY_REQUIRED refusal.
func TestServiceAskForSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	secret := []byte("typed passphrase 5c1")

	lockedRing := func(t *testing.T, candidates ...crypt.Candidate) *crypt.Keyring {
		t.Helper()
		gate, err := crypt.NewKDFGateForCost(1, cheapParamKDF())
		if err != nil {
			t.Fatalf("NewKDFGateForCost() = %v", err)
		}
		ring, err := crypt.NewKeyring(candidates, gate)
		if err != nil {
			t.Fatalf("NewKeyring() = %v", err)
		}
		return ring
	}

	t.Run("encrypted notebook without a candidate asks once", func(t *testing.T) {
		t.Parallel()
		store, envelope := encryptedCurrentStore(t, "ask", secret)
		ring := lockedRing(t)
		svc := newSecretService(t, store, ring)
		asked := 0
		if err := svc.AskForSecret(ctx, func() ([]byte, error) {
			asked++
			return secret, nil
		}); err != nil {
			t.Fatalf("AskForSecret() = %v", err)
		}
		if asked != 1 {
			t.Fatalf("the prompt ran %d times, want 1", asked)
		}
		if _, _, err := ring.Open(ctx, storage.CurrentKey, envelope); err != nil {
			t.Fatalf("Open() with the typed secret = %v", err)
		}
		if ring.ResolvedSource() != crypt.SourcePrompt {
			t.Fatalf("ResolvedSource() = %d, want %d", ring.ResolvedSource(), crypt.SourcePrompt)
		}
	})

	t.Run("plaintext notebook asks nothing", func(t *testing.T) {
		t.Parallel()
		store := fake.New("ask-plaintext")
		if _, err := store.CreateObject(ctx, storage.CurrentKey, []byte(`{"version":1}`)); err != nil {
			t.Fatalf("CreateObject() = %v", err)
		}
		svc := newSecretService(t, store, lockedRing(t))
		if err := svc.AskForSecret(ctx, func() ([]byte, error) {
			t.Fatal("a plaintext notebook asked for a secret")
			return nil, nil
		}); err != nil {
			t.Fatalf("AskForSecret() = %v", err)
		}
	})

	t.Run("configured candidate asks nothing", func(t *testing.T) {
		t.Parallel()
		store, _ := encryptedCurrentStore(t, "ask-configured", secret)
		ring := lockedRing(t, crypt.Candidate{Source: crypt.SourceEnvironment, Secret: []byte("wrong")})
		svc := newSecretService(t, store, ring)
		if err := svc.AskForSecret(ctx, func() ([]byte, error) {
			t.Fatal("a process with a configured secret asked again")
			return nil, nil
		}); err != nil {
			t.Fatalf("AskForSecret() = %v", err)
		}
	})

	t.Run("unreadable current asks nothing", func(t *testing.T) {
		t.Parallel()
		store, _ := encryptedCurrentStore(t, "ask-unreadable", secret)
		store.FailNextKey(fake.OpGet, storage.CurrentKey, storage.ErrTransport)
		svc := newSecretService(t, store, lockedRing(t))
		if err := svc.AskForSecret(ctx, func() ([]byte, error) {
			t.Fatal("a store that could not be read asked for a secret")
			return nil, nil
		}); err != nil {
			t.Fatalf("AskForSecret() = %v", err)
		}
	})

	t.Run("empty answer is KEY_REQUIRED", func(t *testing.T) {
		t.Parallel()
		store, _ := encryptedCurrentStore(t, "ask-empty", secret)
		ring := lockedRing(t)
		svc := newSecretService(t, store, ring)
		err := svc.AskForSecret(ctx, func() ([]byte, error) { return nil, errNoSecret })
		var refusal *notebook.Error
		if !errors.As(err, &refusal) || refusal.Code != notebook.CodeStorageFailure || refusal.Reason != notebook.ReasonKeyRequired {
			t.Fatalf("AskForSecret(empty answer) = %v, want STORAGE_FAILURE/KEY_REQUIRED", err)
		}
		if ring.HasCandidates() {
			t.Fatal("an empty answer was adopted as a candidate")
		}
	})
}
