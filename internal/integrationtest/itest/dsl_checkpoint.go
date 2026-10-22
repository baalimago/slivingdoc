package itest

import (
	"fmt"
	"testing"
)

// assertWarning polls until a warning record with the message and the exact
// attribute value appears in the harness capture.
func AssertWarning(t *testing.T, h *Harness, msg, attr, value string) {
	t.Helper()
	h.Eventually(t, SettleTimeout, func() error {
		for _, rec := range h.Logs().Warnings() {
			if rec.Msg == msg && fmt.Sprint(rec.Attrs[attr]) == value {
				return nil
			}
		}
		return fmt.Errorf("no warning %q with %s=%q in:\n%s", msg, attr, value, h.Logs())
	})
}

// compactedCaller publishes three commits at a threshold of two, leaving the
// notebook compacted, and returns a second caller whose first pull imports the
// checkpoint pack alone. That caller's repository is shallow at the checkpoint
// commit, whose recorded parent the pack omits.
func CompactedCaller(t *testing.T, a *Harness, pathA string) (*Harness, string) {
	t.Helper()
	CommitFirst(t, a, "f1.md", "v1", "c1")
	CommitNext(t, a, "f2.md", "v2", "c2")
	CommitNext(t, a, "f3.md", "v3", "c3")
	if m := a.Manifest(); len(m.Increments) != 0 {
		t.Fatalf("manifest increments = %d, want the compacted empty tail", len(m.Increments))
	}
	b := NewSharedHarness(t, a.Raw(), a.Cfg.Prefix, HarnessConfig{CheckpointPacks: new(1024)})
	pathB := b.WorkspaceRoot()
	b.AssertOK(t, b.Pull(""))
	return b, pathB
}
