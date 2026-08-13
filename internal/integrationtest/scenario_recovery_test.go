package integrationtest

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestScenarioRecoveryBoundaries drives failures at each mutation boundary
// that can leave L or P partially changed. Each result is observed only
// through the MCP envelope; the following pull proves that a completed
// authoritative resynchronization leaves the notebook usable (architecture
// sections 15 (L958) and 18 (L1115)).
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
				h.assertOK(t, h.Pull("", path))
				h.WriteFile(path+"/a.md", "alpha")
			},
			install: func(h *Harness) {
				h.NotebookFailpoints().CAS = func() error { return errors.New("injected CAS boundary") }
			},
			clear: func(h *Harness) { h.NotebookFailpoints().CAS = nil },
			tool:  toolCommit, message: "first", stage: "commit.cas", remoteAccepted: "yes",
		},
		{
			name: "pull replacement",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.WriteFile(path+"/local.md", "local")
			},
			install: failOnce(replaceFailpoint, "injected replacement boundary"),
			clear:   clearFailpoint(replaceFailpoint),
			tool:    toolPull, stage: "pull.accept", remoteAccepted: "no",
		},
		{
			name: "pull baseline record",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.WriteFile(path+"/local.md", "local")
			},
			install: failOnce(baselineFailpoint, "injected baseline boundary"),
			clear:   clearFailpoint(baselineFailpoint),
			tool:    toolPull, stage: "pull.accept", remoteAccepted: "no",
		},
		{
			name: "accepted commit baseline record",
			prepare: func(t *testing.T, h *Harness, path string) {
				h.assertOK(t, h.Pull("", path))
				h.WriteFile(path+"/a.md", "alpha")
			},
			install: failOnce(baselineFailpoint, "injected accepted-commit baseline"),
			clear:   clearFailpoint(baselineFailpoint),
			tool:    toolCommit, message: "first", stage: "commit.accept", remoteAccepted: "yes",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			h := newRecoveryHarness(t)
			path := h.Path("notes")
			row.prepare(t, h, path)
			row.install(h)

			var res *sdk.CallToolResult
			if row.tool == toolCommit {
				res = h.Commit("", path, row.message)
			} else {
				res = h.Pull("", path)
			}
			h.assertEnvelope(t, ToolCall{
				Tool: row.tool, Path: path, Message: row.message,
				Expect: CallExpectation{
					ErrorCode: codeRecoveryFailure,
					Retryable: new(true),
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
			h.assertOK(t, h.Pull("", path))
			if h.StateRecord(t, path).RecoveryRequired {
				t.Fatal("state.json remains recovery-required after successful repair")
			}
		})
	}
}

// TestScenarioRecoveryConflictMaterialization proves recovery also protects
// the path that writes a merge result with conflict markers. The failed
// materialization is not reported as an ordinary conflict, because the
// server must first restore authoritative state (architecture sections 10
// (L603), 12 (L763), and 15 (L958)).
func TestScenarioRecoveryConflictMaterialization(t *testing.T) {
	t.Parallel()
	a := newFakeHarness(t, HarnessConfig{})
	b := newRecoveryHarnessOnStore(t, a)
	pathA, pathB := a.Path("notes"), b.Path("notes")
	commitFirst(t, a, pathA, "shared.md", "base\n", "first")
	b.assertOK(t, b.Pull("", pathB))

	a.WriteFile(pathA+"/shared.md", "remote\n")
	a.assertOK(t, a.Commit("", pathA, "remote change"))
	b.WriteFile(pathB+"/shared.md", "local\n")
	var fired atomic.Bool
	b.WorkspaceFailpoints().Replace = func() error {
		if !fired.CompareAndSwap(false, true) {
			return nil
		}
		return errors.New("injected conflict replacement")
	}
	res := b.Commit("", pathB, "conflicting change")
	b.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: pathB, Message: "conflicting change",
		Expect: CallExpectation{
			ErrorCode: codeRecoveryFailure,
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
// entry recovery before normal work (architecture section 15, L958).
func TestScenarioRecoveryRepairImpossible(t *testing.T) {
	t.Parallel()
	h := newRecoveryHarness(t)
	path := h.Path("notes")
	h.assertOK(t, h.Pull("", path))
	h.WriteFile(path+"/a.md", "alpha")
	h.NotebookFailpoints().CAS = func() error { return errors.New("injected CAS boundary") }
	h.WorkspaceFailpoints().Recover = func() error { return errors.New("injected repair failure") }

	res := h.Commit("", path, "first")
	h.assertEnvelope(t, ToolCall{
		Tool: toolCommit, Path: path, Message: "first",
		Expect: CallExpectation{
			ErrorCode: codeRecoveryFailure,
			Retryable: new(true),
			Recovery: &RecoveryExpectation{
				Stage:          "commit.cas",
				RemoteAccepted: "yes",
				Resynchronized: new(false),
			},
		},
	}, res)
	if !h.StateRecord(t, path).RecoveryRequired {
		t.Fatal("failed repair did not persist recoveryRequired")
	}

	h.NotebookFailpoints().CAS = nil
	h.WorkspaceFailpoints().Recover = nil
	h.assertOK(t, h.Pull("", path))
	if h.StateRecord(t, path).RecoveryRequired {
		t.Fatal("entry recovery did not clear recoveryRequired")
	}
	assertRemoteGeneration(t, h, path, 1)
}

// TestScenarioRecoveryNoMutationBoundaries proves the other half of the
// generic recovery contract: a failure at a boundary that has not begun
// mutating L or P is NOT reported as RECOVERY_FAILURE, leaves the visible
// directory untouched, and does not mark P recovery-required (architecture
// section 15, L958).
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
			h := newRecoveryHarness(t)
			path := h.Path("notes")
			h.WriteFile(path+"/local.md", "local")

			var fired atomic.Bool
			row.install(h, func() error {
				if !fired.CompareAndSwap(false, true) {
					return nil
				}
				return errors.New("injected no-mutation boundary")
			})
			res := h.Pull("", path)

			env := decodeEnvelope(t, ToolCall{Tool: toolPull, Path: path}, res)
			if env.Code == codeRecoveryFailure {
				t.Fatalf("a boundary before any mutation reported %s: %+v", env.Code, env.Recovery)
			}
			if env.Recovery != nil {
				t.Fatalf("non-recovery envelope carries a recovery report: %+v", env.Recovery)
			}
			if got := h.ReadFile(path + "/local.md"); got != "local" {
				t.Fatalf("L after the aborted call = %q, want the caller's untouched bytes", got)
			}
			if h.StateRecord(t, path).RecoveryRequired {
				t.Fatal("a boundary before any mutation marked P recovery-required")
			}

			// The call was refused, not half-applied: the retry succeeds.
			row.clear(h)
			h.assertOK(t, h.Pull("", path))
			assertVisibleFiles(t, h, path, map[string]string{"local.md": "local"})
		})
	}
}

// failpointOf selects one mutable workspace failpoint slot of a harness.
// The rows install and clear through a selector so the only thing that
// varies between them is which boundary fires, not how the latch works.
type failpointOf func(h *Harness) *func() error

func replaceFailpoint(h *Harness) *func() error  { return &h.WorkspaceFailpoints().Replace }
func baselineFailpoint(h *Harness) *func() error { return &h.WorkspaceFailpoints().Baseline }

// failOnce installs a failpoint that fails its first call and then lets
// every later call through, so the recovery path itself is not blocked. The
// latch is atomic because the failpoint runs on the server's request
// goroutine while the test installs it from its own.
func failOnce(slot failpointOf, msg string) func(h *Harness) {
	return func(h *Harness) {
		var fired atomic.Bool
		*slot(h) = func() error {
			if !fired.CompareAndSwap(false, true) {
				return nil
			}
			return errors.New(msg)
		}
	}
}

// clearFailpoint removes the failpoint again.
func clearFailpoint(slot failpointOf) func(h *Harness) {
	return func(h *Harness) { *slot(h) = nil }
}

// newRecoveryHarness wires the mutable hook objects required by recovery
// scenarios. The application passes these objects to its workspaces and
// notebooks once; the test changes hook functions only between calls.
func newRecoveryHarness(t *testing.T) *Harness {
	t.Helper()
	return newFakeHarness(t, HarnessConfig{Hooks: &app.ServiceHooks{
		Workspace: &workspace.Failpoints{},
		Notebook:  &notebook.Failpoints{},
	}})
}

func newRecoveryHarnessOnStore(t *testing.T, shared *Harness) *Harness {
	t.Helper()
	return newSharedHarness(t, shared.Raw(), shared.cfg.Prefix, HarnessConfig{Hooks: &app.ServiceHooks{
		Workspace: &workspace.Failpoints{},
		Notebook:  &notebook.Failpoints{},
	}})
}
