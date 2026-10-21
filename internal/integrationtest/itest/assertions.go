package itest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/baalimago/slivingdoc/internal/workspace"
)

// stateRecord is the minimal strict view of the private state record
// (architecture/workspace.md), read by the harness for baseline
// assertions. The workspace package owns the strict decode; the harness
// reads the fields the black-box contract observes: the remote generation
// and the recovery-required flag.
type StateRecord struct {
	Version          int    `json:"version"`
	Identity         string `json:"identity"`
	RemoteGeneration uint64 `json:"remoteGeneration"`
	BaselineHead     string `json:"baselineHead"`
	BaselineTree     string `json:"baselineTree"`
	RecoveryRequired bool   `json:"recoveryRequired"`
}

// StateRecord reads and decodes the private state record of the server's
// notebook directory, failing the test when it is absent or invalid.
func (h *Harness) StateRecord(t *testing.T) StateRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.PrivateDir(), "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var rec StateRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode state.json: %v", err)
	}
	return rec
}

// PackCacheDir returns the private pack-byte cache directory of the
// server's notebook directory.
func (h *Harness) PackCacheDir() string {
	return filepath.Join(h.PrivateDir(), "pack-cache")
}

// SharedPackCacheDir returns the identity-selected shared pack-cache
// directory of the harness. It fails the test when the harness was wired
// without a pack cache root, so an assertion can never silently point at
// the private location instead.
func (h *Harness) SharedPackCacheDir(t *testing.T) string {
	t.Helper()
	if h.Cfg.PackCacheRoot == "" {
		t.Fatal("harness has no pack cache root")
	}
	return filepath.Join(h.Cfg.PackCacheRoot, workspace.SharedCacheDirName(h.Identity()))
}

// jsonValid reports whether s is a single valid JSON value.
func JsonValid(s string) bool {
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}
