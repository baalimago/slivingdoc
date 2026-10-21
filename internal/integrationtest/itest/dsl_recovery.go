package itest

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// failpointOf selects one mutable workspace failpoint slot of a harness.
// The rows install and clear through a selector so the only thing that
// varies between them is which boundary fires, not how the latch works.
type FailpointOf func(h *Harness) *func() error

func ReplaceFailpoint(h *Harness) *func() error  { return &h.WorkspaceFailpoints().Replace }
func BaselineFailpoint(h *Harness) *func() error { return &h.WorkspaceFailpoints().Baseline }
func StageFailpoint(h *Harness) *func() error    { return &h.WorkspaceFailpoints().Stage }

// failOnce installs a failpoint that fails its first call and then lets
// every later call through, so the recovery path itself is not blocked. The
// latch is atomic because the failpoint runs on the server's request
// goroutine while the test installs it from its own.
func FailOnce(slot FailpointOf, msg string) func(h *Harness) {
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
func ClearFailpoint(slot FailpointOf) func(h *Harness) {
	return func(h *Harness) { *slot(h) = nil }
}

// newRecoveryHarness wires the mutable hook objects required by recovery
// scenarios. The application passes these objects to its workspaces and
// notebooks once; the test changes hook functions only between calls.
func NewRecoveryHarness(t *testing.T) *Harness {
	t.Helper()
	return NewFakeHarness(t, HarnessConfig{Hooks: &app.ServiceHooks{
		Workspace: &workspace.Failpoints{},
		Notebook:  &notebook.Failpoints{},
	}})
}

func NewRecoveryHarnessOnStore(t *testing.T, shared *Harness) *Harness {
	t.Helper()
	return NewSharedHarness(t, shared.Raw(), shared.Cfg.Prefix, HarnessConfig{Hooks: &app.ServiceHooks{
		Workspace: &workspace.Failpoints{},
		Notebook:  &notebook.Failpoints{},
	}})
}
