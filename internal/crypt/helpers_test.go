package crypt

import "testing"

// mustDoor wraps the notebook key into one supported door with the cheap
// test cost.
func mustDoor(t *testing.T, gate *KDFGate, key NotebookKey, secret []byte, kind DoorType) Door {
	t.Helper()
	door, err := WrapKey(testContext, gate, key, secret, testObjectKey, kind, cheapKDFParams())
	if err != nil {
		t.Fatalf("WrapKey() = %v", err)
	}
	return door
}

// mustUnlocked returns a keyring that already holds the given notebook key.
func mustUnlocked(t *testing.T, key NotebookKey, gate *KDFGate) *Keyring {
	t.Helper()
	keyring, err := NewUnlockedKeyring(key, nil, gate)
	if err != nil {
		t.Fatalf("NewUnlockedKeyring() = %v", err)
	}
	return keyring
}
