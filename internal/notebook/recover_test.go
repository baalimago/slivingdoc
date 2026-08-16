package notebook

import (
	"context"
	"errors"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestCASFailpointTriggersRecovery proves the generic recovery path at the
// CAS boundary: after the manifest accepted the proposal, an injected
// failure reports RECOVERY_FAILURE with remote acceptance known, and the
// authoritative resynchronization reconstructs L and P.
func TestCASFailpointTriggersRecovery(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	triggered := errors.New("injected CAS failure")
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, nbFail: &Failpoints{
		CAS: func() error { return triggered },
	}})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "first")), CodeRecoveryFailure)
	if ne.Recovery == nil {
		t.Fatal("RECOVERY_FAILURE carries no recovery report")
	}
	if ne.Recovery.Stage != stageCAS || ne.Recovery.RemoteAccepted != RemoteAcceptedYes || !ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want commit.cas / yes / resynchronized=true", ne.Recovery)
	}
	if !errors.Is(ne, triggered) {
		t.Fatalf("RECOVERY_FAILURE cause = %v, want the injected failure", ne.Cause)
	}

	// The remote accepted and the resynchronization rebuilt L and P.
	m := readManifest(t, store)
	if m.Generation != 1 {
		t.Fatalf("current generation = %d, want the accepted 1", m.Generation)
	}
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}
	if got := readLocal(t, w, "a.md"); got != "v1" {
		t.Fatalf("L after recovery = %q, want the accepted content", got)
	}
	if w.RecoveryRequired() {
		t.Fatal("successful resynchronization must clear the recovery flag")
	}
}

// TestRecoverFailpointReportsFailedResync proves an immediate repair that
// cannot complete reports resynchronized=false and leaves P durably
// requiring recovery.
func TestRecoverFailpointReportsFailedResync(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	triggered := errors.New("injected")
	wsFail := &workspace.Failpoints{Recover: func() error { return triggered }}
	nb, w, _ := newNotebook(t, nbConfig{
		store: store, ids: ids,
		wsFail: wsFail,
		nbFail: &Failpoints{CAS: func() error { return errors.New("cas") }},
	})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "first")), CodeRecoveryFailure)
	if ne.Recovery == nil || ne.Recovery.Stage != stageCAS || ne.Recovery.RemoteAccepted != RemoteAcceptedYes || ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want commit.cas / yes / resynchronized=false", ne.Recovery)
	}
	if !w.RecoveryRequired() {
		t.Fatal("a failed resynchronization must leave P durably requiring recovery")
	}

	// A later call retries the resynchronization first: while the repair
	// still fails, every call keeps reporting RECOVERY_FAILURE and never
	// starts new work.
	again := assertErrorCode(t, errOnly(nb.Pull(context.Background())), CodeRecoveryFailure)
	if again.Recovery == nil || again.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want an unresolved resynchronization", again.Recovery)
	}
	if !w.RecoveryRequired() {
		t.Fatal("an unresolved resynchronization must keep the recovery flag")
	}

	// Once the condition passes, the next call self-heals from the
	// authoritative state because the remote accepted the proposal.
	wsFail.Recover = nil
	pullOK(t, nb)
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline after self-heal = %d, want 1", gen)
	}
	if w.RecoveryRequired() {
		t.Fatal("a successful resynchronization must clear the recovery flag")
	}
	if got := readLocal(t, w, "a.md"); got != "v1" {
		t.Fatalf("L after self-heal = %q, want the accepted content", got)
	}
}
