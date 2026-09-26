package notebook

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/baalimago/slivingdoc/internal/httpstore"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

const (
	hostedTestSpace = "notes"
	hostedTestToken = "sld_0123456789abcdef_bm90ZWJvb2staG9zdGVkLXRlc3Q"
)

// hostedStore starts a reference gateway with one space granted to the
// test token and returns a store over it.
func hostedStore(t *testing.T) (*httpstore.Store, *gatewaytest.Gateway) {
	t.Helper()
	g := gatewaytest.Start(t)
	g.AddSpace(hostedTestSpace, 1<<30)
	g.Grant(hostedTestToken, hostedTestSpace, false)
	s, err := httpstore.New(httpstore.Config{
		Endpoint: g.URL(), Space: hostedTestSpace, Prefix: "nb", Token: hostedTestToken, Retries: -1,
	})
	if err != nil {
		t.Fatalf("httpstore.New: %v", err)
	}
	return s, g
}

var errInjectedReplace = errors.New("injected replacement failure")

// unreachableCases make the space unreachable for the token the two ways
// the gateway answers 404 no_space: the grant moved to another space, and
// the space was deleted.
var unreachableCases = []struct {
	name string
	cut  func(g *gatewaytest.Gateway)
}{
	{"grant moved", func(g *gatewaytest.Gateway) {
		g.AddSpace("elsewhere", 1<<20)
		g.Grant(hostedTestToken, "elsewhere", false)
	}},
	{"space deleted", func(g *gatewaytest.Gateway) { g.DeleteSpace(hostedTestSpace) }},
}

// assertAccessDenied checks a pull refused because the space is
// unreachable: STORAGE_FAILURE/ACCESS_DENIED with action OPERATOR.
func assertAccessDenied(t *testing.T, err error) {
	t.Helper()
	ne := assertErrorCode(t, err, CodeStorageFailure)
	if ne.Reason != ReasonAccessDenied || ne.Action != ActionOperator {
		t.Fatalf("reason/action = %s/%s, want ACCESS_DENIED/OPERATOR (error: %v)", ne.Reason, ne.Action, err)
	}
}

// A pulled workspace whose space becomes unreachable is refused as
// ACCESS_DENIED. The 404 no_space is never read as an empty notebook, so
// the pull neither deletes the notes nor moves the baseline, with and
// without a local edit.
func TestHostedUnreachableSpacePullKeepsNotes(t *testing.T) {
	for _, tt := range unreachableCases {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local edit %v", tt.name, edit), func(t *testing.T) {
				store, g := hostedStore(t)
				nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
				pullOK(t, nb)
				writeLocal(t, w, map[string]string{"a.md": "kept\n"})
				commitOK(t, nb, "first")
				if edit {
					writeLocal(t, w, map[string]string{"b.md": "local edit\n"})
				}
				before := diskTree(t, w.Path())

				tt.cut(g)
				assertAccessDenied(t, errOnly(nb.Pull(context.Background())))
				assertTreeUnchanged(t, before, diskTree(t, w.Path()))
				if gen := w.Baseline().RemoteGeneration; gen != 1 {
					t.Fatalf("baseline generation = %d, want 1 unchanged", gen)
				}
				if w.RecoveryRequired() {
					t.Fatal("a refused read must not mark recovery")
				}
			})
		}
	}
}

// diskTree reads every regular file below dir straight from disk, so it
// works while the workspace refuses scans (recovery required).
func diskTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		tree[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return tree
}

func assertTreeUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("L = %v, want %v untouched", after, before)
	}
}

// A first pull into a directory holding files, from a space the token
// cannot reach, is refused as ACCESS_DENIED before anything changes: the
// files stay and the workspace is still unpulled.
func TestHostedUnreachableSpaceFirstPullKeepsFiles(t *testing.T) {
	for _, tt := range unreachableCases {
		t.Run(tt.name, func(t *testing.T) {
			store, g := hostedStore(t)
			nb, w, _ := newNotebook(t, nbConfig{store: store, ids: &testIDSource{}})
			writeLocal(t, w, map[string]string{"own.md": "mine\n"})

			tt.cut(g)
			assertAccessDenied(t, errOnly(nb.Pull(context.Background())))
			if got := localSnapshot(t, w); len(got) != 1 || got["own.md"] != "mine\n" {
				t.Fatalf("L after the refused first pull = %v, want own.md untouched", got)
			}
			ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "seed")), CodeInvalidRequest)
			if ne.Reason != ReasonPullRequired {
				t.Fatalf("commit after the refused first pull = %s, want PULL_REQUIRED", ne.Reason)
			}
		})
	}
}

// Entry recovery against an unreachable space does not resynchronize L to
// an empty notebook: the call is RECOVERY_FAILURE carrying ACCESS_DENIED,
// L keeps its files, and P keeps requiring recovery until the space is
// reachable again.
func TestHostedUnreachableSpaceEntryRecoveryKeepsNotes(t *testing.T) {
	store, g := hostedStore(t)
	var cut bool
	wsFail := &workspace.Failpoints{Replace: func() error {
		if !cut {
			return nil
		}
		cut = false
		unreachableCases[0].cut(g)
		return errInjectedReplace
	}}
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, wsFail: wsFail})
	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"a.md": "kept\n"})
	commitOK(t, nb, "first")

	other, ow, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	pullOK(t, other)
	writeLocal(t, ow, map[string]string{"b.md": "remote\n"})
	commitOK(t, other, "second")

	before := diskTree(t, w.Path())
	cut = true
	first := assertErrorCode(t, errOnly(nb.Pull(context.Background())), CodeRecoveryFailure)
	if first.Reason != ReasonAccessDenied || first.Recovery == nil || first.Recovery.Resynchronized {
		t.Fatalf("failed pull = %s %+v, want ACCESS_DENIED and not resynchronized", first.Reason, first.Recovery)
	}
	if !w.RecoveryRequired() {
		t.Fatal("the failed replacement must leave P requiring recovery")
	}
	assertTreeUnchanged(t, before, diskTree(t, w.Path()))

	entry := assertErrorCode(t, errOnly(nb.Pull(context.Background())), CodeRecoveryFailure)
	if entry.Reason != ReasonAccessDenied || entry.Action != ActionOperator ||
		entry.Recovery == nil || entry.Recovery.Stage != stageEntry || entry.Recovery.Resynchronized {
		t.Fatalf("entry recovery = %s/%s %+v, want ACCESS_DENIED/OPERATOR at entry, not resynchronized", entry.Reason, entry.Action, entry.Recovery)
	}
	assertTreeUnchanged(t, before, diskTree(t, w.Path()))
	if !w.RecoveryRequired() {
		t.Fatal("a refused entry recovery must keep P requiring recovery")
	}

	g.Grant(hostedTestToken, hostedTestSpace, false)
	assertEntryRecovered(t, errOnly(nb.Pull(context.Background())))
	if got := localSnapshot(t, w); got["a.md"] != "kept\n" || got["b.md"] != "remote\n" {
		t.Fatalf("L after the space is back = %v, want the accepted state", got)
	}
}
