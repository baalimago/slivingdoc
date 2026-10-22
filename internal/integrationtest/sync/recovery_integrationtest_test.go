package sync

import (
	"errors"
	"sync/atomic"
	"testing"

	. "github.com/baalimago/slivingdoc/internal/integrationtest/itest"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioRecoveryBoundaries drives failures at each mutation boundary
// that can leave L or P partially changed. Each result is observed only
// through the MCP envelope; the following pull proves that a completed
// authoritative resynchronization leaves the notebook usable
// (architecture/guarantees.md).
func TestScenarioRecoveryBoundaries(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name           string
		prepare        func(t *testing.T, h *Harness, path string)
		install        func(h *Harness)
		clear          func(h *Harness)
		tool           string
		message        string
		stage          string
		remoteAccepted string
	}{
		{
			name: "accepted CAS",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/a.md", "alpha")
			},
			install: func(h *Harness) {
				h.NotebookFailpoints().CAS = func() error { return errors.New("injected CAS boundary") }
			},
			clear: func(h *Harness) { h.NotebookFailpoints().CAS = nil },
			tool:  ToolCommit, message: "first", stage: "commit.cas", remoteAccepted: "yes",
		},
		{
			name: "pull replacement",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.WriteFile(path+"/local.md", "local")
			},
			install: FailOnce(ReplaceFailpoint, "injected replacement boundary"),
			clear:   ClearFailpoint(ReplaceFailpoint),
			tool:    ToolPull, stage: "pull.accept", remoteAccepted: "no",
		},
		{
			name: "pull baseline record",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.WriteFile(path+"/local.md", "local")
			},
			install: FailOnce(BaselineFailpoint, "injected baseline boundary"),
			clear:   ClearFailpoint(BaselineFailpoint),
			tool:    ToolPull, stage: "pull.accept", remoteAccepted: "no",
		},
		{
			name: "accepted commit baseline record",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/a.md", "alpha")
			},
			install: FailOnce(BaselineFailpoint, "injected accepted-commit baseline"),
			clear:   ClearFailpoint(BaselineFailpoint),
			tool:    ToolCommit, message: "first", stage: "commit.accept", remoteAccepted: "yes",
		},
		{
			// Staging fails before the workspace marks recovery, but the
			// remote already accepted, so the call is still a recovery.
			name: "accepted commit staging",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.AssertOK(t, h.Pull(""))
				h.WriteFile(path+"/a.md", "alpha")
			},
			install: FailOnce(StageFailpoint, "injected accepted-commit staging"),
			clear:   ClearFailpoint(StageFailpoint),
			tool:    ToolCommit, message: "first", stage: "commit.accept", remoteAccepted: "yes",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := NewRecoveryHarness(t)
			path := h.WorkspaceRoot()
			row.prepare(t, h, path)
			row.install(h)

			var res *sdk.CallToolResult
			if row.tool == ToolCommit {
				res = h.Commit("", row.message)
			} else {
				res = h.Pull("")
			}
			h.AssertEnvelope(t, ToolCall{
				Tool: row.tool, Path: path, Message: row.message,
				Expect: CallExpectation{
					ErrorCode: CodeRecoveryFailure,
					Retryable: new(true),
					Reason:    "LOCAL_MUTATION_FAILED",
					// Every row resynchronizes, so the action is PULL.
					Action: "PULL",
					Recovery: &RecoveryExpectation{
						Stage:          row.stage,
						RemoteAccepted: row.remoteAccepted,
						Resynchronized: new(true),
					},
				},
			}, res)

			row.clear(h)
			// A recovery error never becomes an OK result for the anomalous
			// call. The next public operation is allowed to proceed from the
			// reconstructed authoritative state.
			h.AssertOK(t, h.Pull(""))
			if h.StateRecord(t).RecoveryRequired {
				t.Fatal("state.json remains recovery-required after successful repair")
			}
		})
	}
}

// TestScenarioRecoveryConflictMaterialization proves recovery also protects
// the path that writes a merge result with conflict markers. The failed
// materialization is not reported as an ordinary conflict, because the
// server must first restore authoritative state (architecture/pull.md,
// conflicts.md, and guarantees.md).
func TestScenarioRecoveryConflictMaterialization(t *testing.T) {
	t.Parallel()
	a := NewFakeHarness(t, HarnessConfig{})
	b := NewRecoveryHarnessOnStore(t, a)
	pathA, pathB := a.WorkspaceRoot(), b.WorkspaceRoot()
	CommitFirst(t, a, "shared.md", "base\n", "first")
	b.AssertOK(t, b.Pull(""))

	a.WriteFile(pathA+"/shared.md", "remote\n")
	a.AssertOK(t, a.Commit("", "remote change"))
	b.WriteFile(pathB+"/shared.md", "local\n")
	var fired atomic.Bool
	b.WorkspaceFailpoints().Replace = func() error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		return errors.New("injected conflict replacement")
	}
	res := b.Commit("", "conflicting change")
	b.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: pathB, Message: "conflicting change",
		Expect: CallExpectation{
			ErrorCode: CodeRecoveryFailure,
			Retryable: new(true),
			Recovery: &RecoveryExpectation{
				Stage:          "merge.materialize",
				RemoteAccepted: "no",
				Resynchronized: new(true),
			},
		},
	}, res)
	if got := b.ReadFile(pathB + "/shared.md"); got != "remote\n" {
		t.Fatalf("recovery after failed conflict materialization = %q, want current remote", got)
	}
}

// TestScenarioRecoveryRepairImpossible proves the second failure guarantee:
// a failed immediate resynchronization is reported candidly and P remains in
// recovery-required mode. After removing the fault, the next MCP call runs
// entry recovery instead of its own work: it rewrites L to the accepted
// state, discarding an edit made meanwhile, and says so with
// RECOVERY_FAILURE at stage entry rather than OK. The call after that runs
// normally (architecture/guarantees.md).
func TestScenarioRecoveryRepairImpossible(t *testing.T) {
	t.Parallel()
	h := NewRecoveryHarness(t)
	path := h.WorkspaceRoot()
	h.AssertOK(t, h.Pull(""))
	h.WriteFile(path+"/a.md", "alpha")
	h.NotebookFailpoints().CAS = func() error { return errors.New("injected CAS boundary") }
	h.WorkspaceFailpoints().Recover = func() error { return errors.New("injected repair failure") }

	res := h.Commit("", "first")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolCommit, Path: path, Message: "first",
		Expect: CallExpectation{
			ErrorCode: CodeRecoveryFailure,
			Retryable: new(true),
			Reason:    "LOCAL_MUTATION_FAILED",
			// The resync failed, so the action falls back to RETRY.
			Action: "RETRY",
			Recovery: &RecoveryExpectation{
				Stage:          "commit.cas",
				RemoteAccepted: "yes",
				Resynchronized: new(false),
			},
		},
	}, res)
	if !h.StateRecord(t).RecoveryRequired {
		t.Fatal("failed repair did not persist recoveryRequired")
	}

	h.NotebookFailpoints().CAS = nil
	h.WorkspaceFailpoints().Recover = nil
	h.WriteFile(path+"/a.md", "edited while recovery was pending")
	res = h.Pull("")
	h.AssertEnvelope(t, ToolCall{
		Tool: ToolPull, Path: path,
		Expect: CallExpectation{
			ErrorCode: CodeRecoveryFailure,
			Retryable: new(true),
			Reason:    "LOCAL_MUTATION_FAILED",
			Action:    "PULL",
			Recovery: &RecoveryExpectation{
				Stage:          "entry",
				RemoteAccepted: "unknown",
				Resynchronized: new(true),
			},
		},
	}, res)
	if h.StateRecord(t).RecoveryRequired {
		t.Fatal("entry recovery did not clear recoveryRequired")
	}
	if got := h.ReadFile(path + "/a.md"); got != "alpha" {
		t.Fatalf("a.md after entry recovery = %q, want the accepted content", got)
	}
	AssertRemoteGeneration(t, h, 1)
	h.AssertOK(t, h.Pull(""))
}

// TestScenarioRecoveryNoMutationBoundaries proves the other half of the
// generic recovery contract: a failure at a boundary that has not begun
// mutating L or P is NOT reported as RECOVERY_FAILURE, leaves the visible
// directory untouched, and does not mark P recovery-required
// (architecture/guarantees.md).
//
// The scan and stage boundaries are documented as no-mutation points. If
// either starts reporting a recovery failure, callers are told to
// expect a rewritten directory where nothing was ever written.
func TestScenarioRecoveryNoMutationBoundaries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		install func(h *Harness, fail func() error)
		clear   func(h *Harness)
	}{
		{
			name:    "scan",
			install: func(h *Harness, fail func() error) { h.WorkspaceFailpoints().Scan = fail },
			clear:   func(h *Harness) { h.WorkspaceFailpoints().Scan = nil },
		},
		{
			name:    "stage",
			install: func(h *Harness, fail func() error) { h.WorkspaceFailpoints().Stage = fail },
			clear:   func(h *Harness) { h.WorkspaceFailpoints().Stage = nil },
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := NewRecoveryHarness(t)
			path := h.WorkspaceRoot()
			h.WriteFile(path+"/local.md", "local")

			var fired atomic.Bool
			row.install(h, func() error {
				if !fired.CompareAndSwap(false, true) {
					return nil
				}
				return errors.New("injected no-mutation boundary")
			})
			res := h.Pull("")

			env := DecodeEnvelope(t, ToolCall{Tool: ToolPull, Path: path}, res)
			if env.Code == CodeRecoveryFailure {
				t.Fatalf("a boundary before any mutation reported %s: %+v", env.Code, env.Recovery)
			}
			if env.Recovery != nil {
				t.Fatalf("non-recovery envelope carries a recovery report: %+v", env.Recovery)
			}
			if got := h.ReadFile(path + "/local.md"); got != "local" {
				t.Fatalf("L after the aborted call = %q, want the caller's untouched bytes", got)
			}
			if h.StateRecord(t).RecoveryRequired {
				t.Fatal("a boundary before any mutation marked P recovery-required")
			}

			// The call was refused, not half-applied: the retry succeeds.
			row.clear(h)
			h.AssertOK(t, h.Pull(""))
			AssertVisibleFiles(t, h, map[string]string{"local.md": "local"})
		})
	}
}
