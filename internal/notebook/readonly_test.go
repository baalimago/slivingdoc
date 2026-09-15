package notebook

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestNewRejectsInvalidReadOnlyPaths checks New validates and normalizes
// ReadOnlyPaths.
func TestNewRejectsInvalidReadOnlyPaths(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	_, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	base := Config{
		Workspace: w, Store: store, RetryLimit: DefaultRetryLimit,
		CheckpointPacks: DefaultCheckpointPacks, RetainedCheckpoints: DefaultRetainedCheckpoints,
		NewID: ids.next,
	}

	invalid := base
	invalid.ReadOnlyPaths = []string{"../escape"}
	if _, err := New(invalid); err == nil {
		t.Fatal("New(invalid read-only path) = nil, want error")
	}

	valid := base
	valid.ReadOnlyPaths = []string{"docs/", "docs/sub", "faq.md"}
	nb, err := New(valid)
	if err != nil {
		t.Fatalf("New(valid read-only paths) = %v", err)
	}
	if got, want := nb.ReadOnlyPaths(), []string{"docs", "faq.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOnlyPaths() = %v, want the normalized set %v", got, want)
	}

	empty := base
	nbEmpty, err := New(empty)
	if err != nil {
		t.Fatalf("New(no read-only paths) = %v", err)
	}
	if got := nbEmpty.ReadOnlyPaths(); got == nil || len(got) != 0 {
		t.Fatalf("ReadOnlyPaths() = %v, want an empty non-nil slice", got)
	}
}

// TestCommitMarkersBeforeReadOnly checks markers are rejected before the
// read-only check (architecture section 11.1).
func TestCommitMarkersBeforeReadOnly(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, readOnly: []string{"docs"}})
	writeLocal(t, w, map[string]string{"docs/a.md": "base", "notes/x.md": "base"})
	pullOK(t, nb)
	writeLocal(t, w, map[string]string{
		"docs/a.md":  "changed",                                        // a read-only violation
		"notes/x.md": "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n", // a marker block elsewhere
	})
	before := localSnapshot(t, w)

	ne := assertErrorCode(t, errOnly(nb.Commit(context.Background(), "msg")), CodeContentConflict)
	if ne.Reason != ReasonUnresolvedMarkers {
		t.Fatalf("reason = %s, want %s (markers must be checked before read-only)", ne.Reason, ReasonUnresolvedMarkers)
	}
	if got := localSnapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed before any reset should have run: %v -> %v", before, got)
	}
}

// dynamicReadFailRepo fails ReadTree for the tree *target names; the zero OID
// arms nothing.
type dynamicReadFailRepo struct {
	git.Repository
	target *git.OID
}

func (r *dynamicReadFailRepo) ReadTree(id git.OID) ([]git.TreeEntry, error) {
	if id == *r.target {
		return nil, errors.New("injected read-only baseline read failure")
	}
	return r.Repository.ReadTree(id)
}

// dynamicReadFailEngine wraps the fake engine's repositories in dynamicReadFailRepo.
type dynamicReadFailEngine struct {
	*fakeEngine
	target *git.OID
}

func (e *dynamicReadFailEngine) CreateRepo(path string) (git.Repository, error) {
	repo, err := e.fakeEngine.CreateRepo(path)
	if err != nil {
		return nil, err
	}
	return &dynamicReadFailRepo{Repository: repo, target: e.target}, nil
}

func (e *dynamicReadFailEngine) OpenRepo(path string) (git.Repository, error) {
	repo, err := e.fakeEngine.OpenRepo(path)
	if err != nil {
		return nil, err
	}
	return &dynamicReadFailRepo{Repository: repo, target: e.target}, nil
}

var _ workspace.Engine = (*dynamicReadFailEngine)(nil)

// TestCommitReadOnlyBaselineReadFailure checks a baseline read failure maps to
// STORAGE_INTEGRITY/ENGINE_FAILED without touching L.
func TestCommitReadOnlyBaselineReadFailure(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	target := new(git.OID) // armed after the baseline is established below
	eng := &dynamicReadFailEngine{fakeEngine: newFakeEngine(), target: target}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, engine: eng, readOnly: []string{"docs"}})
	writeLocal(t, w, map[string]string{"docs/a.md": "base"})
	pullOK(t, nb)
	*target = w.Baseline().Tree
	writeLocal(t, w, map[string]string{"docs/a.md": "changed"})
	before := localSnapshot(t, w)
	baselineBefore := w.Baseline()

	res, err := nb.Commit(context.Background(), "msg")
	ne := assertErrorCode(t, err, CodeStorageIntegrity)
	assertZeroResult(t, res)
	if ne.Reason != ReasonEngineFailed {
		t.Fatalf("reason = %s, want %s", ne.Reason, ReasonEngineFailed)
	}
	if got := localSnapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed baseline read: %v -> %v", before, got)
	}
	if got := w.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed baseline read: %+v -> %+v", baselineBefore, got)
	}
}

// toggleWriteTreeRepo fails WriteTree once *fail is set.
type toggleWriteTreeRepo struct {
	git.Repository
	fail *bool
}

func (r *toggleWriteTreeRepo) WriteTree(entries []git.TreeEntry) (git.OID, error) {
	if *r.fail {
		return git.OID{}, errors.New("injected pinned tree build failure")
	}
	return r.Repository.WriteTree(entries)
}

type toggleWriteTreeEngine struct {
	*fakeEngine
	fail *bool
}

func (e *toggleWriteTreeEngine) CreateRepo(path string) (git.Repository, error) {
	repo, err := e.fakeEngine.CreateRepo(path)
	if err != nil {
		return nil, err
	}
	return &toggleWriteTreeRepo{Repository: repo, fail: e.fail}, nil
}

func (e *toggleWriteTreeEngine) OpenRepo(path string) (git.Repository, error) {
	repo, err := e.fakeEngine.OpenRepo(path)
	if err != nil {
		return nil, err
	}
	return &toggleWriteTreeRepo{Repository: repo, fail: e.fail}, nil
}

var _ workspace.Engine = (*toggleWriteTreeEngine)(nil)

// TestCommitReadOnlyPinBuildFailure checks a pinned-tree build failure maps to
// INVALID_REQUEST/INVALID_CONTENT without touching L.
func TestCommitReadOnlyPinBuildFailure(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	fail := new(bool)
	eng := &toggleWriteTreeEngine{fakeEngine: newFakeEngine(), fail: fail}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids, engine: eng, readOnly: []string{"docs"}})
	writeLocal(t, w, map[string]string{"docs/a.md": "base"})
	pullOK(t, nb)
	writeLocal(t, w, map[string]string{"docs/a.md": "changed"})
	before := localSnapshot(t, w)
	baselineBefore := w.Baseline()
	*fail = true

	res, err := nb.Commit(context.Background(), "msg")
	ne := assertErrorCode(t, err, CodeInvalidRequest)
	assertZeroResult(t, res)
	if ne.Reason != ReasonInvalidContent {
		t.Fatalf("reason = %s, want %s", ne.Reason, ReasonInvalidContent)
	}
	if got := localSnapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Fatalf("L changed by the failed pin build: %v -> %v", before, got)
	}
	if got := w.Baseline(); got != baselineBefore {
		t.Fatalf("baseline changed by the failed pin build: %+v -> %+v", baselineBefore, got)
	}
}
