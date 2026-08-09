package integrationtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// stateRecord is the minimal strict view of the private state record
// (architecture section 7.2), read by the harness for baseline
// assertions. The workspace package owns the strict decode; the harness
// reads the fields the black-box contract observes: the remote generation
// and the recovery-required flag.
type stateRecord struct {
	Version          int    `json:"version"`
	Identity         string `json:"identity"`
	RemoteGeneration uint64 `json:"remoteGeneration"`
	BaselineHead     string `json:"baselineHead"`
	BaselineTree     string `json:"baselineTree"`
	RecoveryRequired bool   `json:"recoveryRequired"`
}

// StateRecord reads and decodes the private state record of a request
// path, failing the test when it is absent or invalid.
func (h *Harness) StateRecord(t *testing.T, path string) stateRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.PrivateDir(path), "state.json"))
	if err != nil {
		t.Fatalf("read state.json for %s: %v", path, err)
	}
	var rec stateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode state.json for %s: %v", path, err)
	}
	return rec
}

// PackCacheDir returns the private pack-byte cache directory of a request
// path.
func (h *Harness) PackCacheDir(path string) string {
	return filepath.Join(h.PrivateDir(path), "pack-cache")
}

// jsonValid reports whether s is a single valid JSON value.
func jsonValid(s string) bool {
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}
