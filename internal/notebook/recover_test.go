package notebook

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/storage"
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

// TestAcceptFailureBeforeRecoveryMarkIsRecoveryFailure proves that a
// local acceptance failing after a proved CAS but before the workspace
// marks recovery (staging) still reports RECOVERY_FAILURE with remote
// acceptance known, not a plain local-state failure, and resynchronizes.
func TestAcceptFailureBeforeRecoveryMarkIsRecoveryFailure(t *testing.T) {
	store := fake.New("")
	triggered := errors.New("injected staging failure")
	var stageCalls int
	wsFail := &workspace.Failpoints{Stage: func() error {
		stageCalls++
		if stageCalls == 2 { // the pull stages first; the commit's acceptance second
			return triggered
		}
		return nil
	}}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}, wsFail: wsFail})

	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "first")), CodeRecoveryFailure)
	if ne.Recovery == nil || ne.Recovery.Stage != stageCommit || ne.Recovery.RemoteAccepted != RemoteAcceptedYes || !ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want commit.accept / yes / resynchronized=true", ne.Recovery)
	}
	if ne.Action != ActionPull {
		t.Fatalf("action = %s, want PULL", ne.Action)
	}
	if !errors.Is(ne, triggered) {
		t.Fatalf("RECOVERY_FAILURE cause = %v, want the injected failure", ne.Cause)
	}
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want the accepted 1", gen)
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
	// authoritative state because the remote accepted the proposal, and
	// reports the rewrite instead of returning OK.
	wsFail.Recover = nil
	assertEntryRecovered(t, errOnly(nb.Pull(context.Background())))
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline after self-heal = %d, want 1", gen)
	}
	if w.RecoveryRequired() {
		t.Fatal("a successful resynchronization must clear the recovery flag")
	}
	if got := readLocal(t, w, "a.md"); got != "v1" {
		t.Fatalf("L after self-heal = %q, want the accepted content", got)
	}
	pullOK(t, nb)
}

// recoveryRefusals are the store refusals a resynchronizing read can meet,
// with the reason, action and retryability the caller must see.
var recoveryRefusals = []struct {
	sentinel error
	reason   Reason
	action   Action
}{
	{storage.ErrAccessDenied, ReasonAccessDenied, ActionOperator},
	{storage.ErrQuotaExceeded, ReasonStorageFull, ActionOperator},
	{storage.ErrRequestLimit, ReasonRequestLimit, ActionOperator},
	{storage.ErrRateLimited, ReasonRateLimited, ActionRetry},
	{storage.ErrTooLarge, ReasonObjectTooLarge, ActionOperator},
}

// assertRecoveryRefused checks a RECOVERY_FAILURE whose resynchronization
// the store refused: the code stays, the refusal's reason, action and
// message surface, and the report says the directory was not repaired.
func assertRecoveryRefused(t *testing.T, err error, stage string, reason Reason, action Action, refusal error) {
	t.Helper()
	ne := assertErrorCode(t, err, CodeRecoveryFailure)
	if ne.Reason != reason || ne.Action != action {
		t.Fatalf("reason/action = %s/%s, want %s/%s", ne.Reason, ne.Action, reason, action)
	}
	if ne.Recovery == nil || ne.Recovery.Stage != stage || ne.Recovery.Resynchronized {
		t.Fatalf("recovery report = %+v, want %s / resynchronized=false", ne.Recovery, stage)
	}
	if !errors.Is(ne, refusal) {
		t.Fatalf("cause = %v, want the refusal", ne.Cause)
	}
	if !strings.Contains(ne.Message, "could not resynchronize") || !strings.Contains(ne.Message, "The storage says: slow down") {
		t.Fatalf("message = %q, want the recovery context and the store's own line", ne.Message)
	}
}

// A store refusal of the resynchronizing read surfaces with its own reason
// and action on every recovery path: after an accepted CAS
// (failAfterAccept), after a failed local mutation (applyLocal), and at
// the entry of the next call (entryRecovery). The local cause stays in the
// chain, and a P that required recovery keeps requiring it.
func TestRecoveryResyncRefusalSurfaces(t *testing.T) {
	for _, tt := range recoveryRefusals {
		t.Run(string(tt.reason), func(t *testing.T) {
			refusal := &storage.Refusal{Err: tt.sentinel, Detail: "read current", Message: "slow down"}

			t.Run("after an accepted CAS", func(t *testing.T) {
				store := fake.New("")
				triggered := errors.New("injected CAS failure")
				nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}, nbFail: &Failpoints{
					CAS: func() error {
						store.FailNextKey(fake.OpGet, storage.CurrentKey, refusal)
						return triggered
					},
				}})
				writeLocal(t, w, map[string]string{"a.md": "v1"})
				pullOK(t, nb)
				err := errOnly(nb.Commit(context.Background(), "first"))
				assertRecoveryRefused(t, err, stageCAS, tt.reason, tt.action, refusal)
				if !errors.Is(err, triggered) {
					t.Fatalf("cause = %v, want the injected failure kept", err)
				}
			})

			t.Run("after a failed local mutation, then at entry", func(t *testing.T) {
				store := fake.New("")
				triggered := errors.New("injected replace failure")
				var fail bool
				wsFail := &workspace.Failpoints{Replace: func() error {
					if !fail {
						return nil
					}
					fail = false
					store.FailNextKey(fake.OpGet, storage.CurrentKey, refusal)
					return triggered
				}}
				ids := &testIDSource{}
				nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, wsFail: wsFail})
				pullOK(t, nb)
				writeLocal(t, w, map[string]string{"a.md": "v1"})
				commitOK(t, nb, "first")

				other, ow, _ := newNotebook(t, nbConfig{store: store, ids: ids})
				pullOK(t, other)
				writeLocal(t, ow, map[string]string{"b.md": "v2"})
				commitOK(t, other, "second")

				fail = true
				err := errOnly(nb.Pull(context.Background()))
				assertRecoveryRefused(t, err, stagePull, tt.reason, tt.action, refusal)
				if !strings.Contains(err.Error(), triggered.Error()) {
					t.Fatalf("cause = %v, want the injected failure kept", err)
				}

				store.FailNextKey(fake.OpGet, storage.CurrentKey, refusal)
				assertRecoveryRefused(t, errOnly(nb.Pull(context.Background())), stageEntry, tt.reason, tt.action, refusal)
				if !w.RecoveryRequired() {
					t.Fatal("a refused resynchronization must keep the recovery flag")
				}

				assertEntryRecovered(t, errOnly(nb.Pull(context.Background())))
				pullOK(t, nb)
			})
		})
	}
}
