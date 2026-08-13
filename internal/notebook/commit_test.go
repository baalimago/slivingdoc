package notebook

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// TestNewValidatesConfig proves the notebook refuses a missing workspace,
// a missing store, a retry limit outside 0..100, a checkpoint packs
// threshold below 1, and a retained-checkpoints count outside 0..64.
func TestNewValidatesConfig(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	_, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	base := Config{
		Workspace:           w,
		Store:               store,
		RetryLimit:          DefaultRetryLimit,
		CheckpointPacks:     DefaultCheckpointPacks,
		RetainedCheckpoints: DefaultRetainedCheckpoints,
		NewID:               ids.next,
	}

	if _, err := New(base); err != nil {
		t.Fatalf("New(valid) = %v", err)
	}
	noWorkspace := base
	noWorkspace.Workspace = nil
	if _, err := New(noWorkspace); err == nil {
		t.Fatal("New(nil workspace) = nil, want error")
	}
	noStore := base
	noStore.Store = nil
	if _, err := New(noStore); err == nil {
		t.Fatal("New(nil store) = nil, want error")
	}
	for _, limit := range []int{-1, 101} {
		bad := base
		bad.RetryLimit = limit
		if _, err := New(bad); err == nil {
			t.Fatalf("New(retry limit %d) = nil, want error", limit)
		}
	}
	for _, threshold := range []int{-4, 0} {
		bad := base
		bad.CheckpointPacks = threshold
		if _, err := New(bad); err == nil {
			t.Fatalf("New(checkpoint packs %d) = nil, want error", threshold)
		}
	}
	for _, retained := range []int{-1, 65} {
		bad := base
		bad.RetainedCheckpoints = retained
		if _, err := New(bad); err == nil {
			t.Fatalf("New(retained checkpoints %d) = nil, want error", retained)
		}
	}
}

// TestCommitWithoutPull proves a commit before any successful or
// conflicting pull is rejected before any S3 access.
func TestCommitWithoutPull(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "x"})

	assertErrorCode(t, nb.Commit(context.Background(), "msg"), CodeInvalidRequest)
	for _, op := range []fake.Op{fake.OpGet, fake.OpPut, fake.OpCreate, fake.OpReplace} {
		if got := store.Calls(op); got != 0 {
			t.Fatalf("commit without pull made %d %s calls, want none", got, op)
		}
	}
}

// TestCommitBlankMessage proves the message contract runs before any scan
// or S3 access.
func TestCommitBlankMessage(t *testing.T) {
	long := strings.Repeat("m", 16385)
	cases := map[string]string{
		"empty":        "",
		"whitespace":   "   \t\n",
		"too long":     long,
		"invalid utf8": string([]byte{0xff, 0xfe}),
		"nul":          "a\x00b",
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			store := fake.New("")
			ids := &testIDSource{}
			nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
			writeLocal(t, w, map[string]string{"a.md": "x"})
			pullOK(t, nb)
			getsBefore := store.Calls(fake.OpGet)
			assertErrorCode(t, nb.Commit(context.Background(), message), CodeInvalidRequest)
			if got := store.Calls(fake.OpGet) - getsBefore; got != 0 {
				t.Fatalf("invalid message made %d GET calls, want none", got)
			}
		})
	}
}

// TestRejectMarkersTable covers the marker scanner contract: LF, CRLF,
// multiple blocks, near matches, literal examples, and blocks at the end
// of a file without a trailing newline.
func TestRejectMarkersTable(t *testing.T) {
	block := "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n"
	cases := []struct {
		name string
		data string
		want []git.MarkerRange
	}{
		{"lf block", block, []git.MarkerRange{{Start: 1, End: 5}}},
		{"crlf block", "<<<<<<< local\r\na\r\n=======\r\nb\r\n>>>>>>> remote\r\n", []git.MarkerRange{{Start: 1, End: 5}}},
		{"multiple blocks", block + "clean\n" + block, []git.MarkerRange{{Start: 1, End: 5}, {Start: 7, End: 11}}},
		{"block at eof", "<<<<<<< local\n=======\n>>>>>>> remote", []git.MarkerRange{{Start: 1, End: 3}}},
		{"literal example", "<<<<<<< local\nthe caller's text\n=======\nthe accepted remote text\n>>>>>>> remote\n", []git.MarkerRange{{Start: 1, End: 5}}},
		{"near match label", "<<<<<<< mine\n=======\n>>>>>>> remote\n", nil},
		{"near match missing closer", "<<<<<<< local\n=======\n", nil},
		{"near match missing separator", "<<<<<<< local\ntext\n>>>>>>> remote\n", nil},
		{"nested opener is content", "<<<<<<< local\n<<<<<<< local\n=======\ntext\n>>>>>>> remote\n", []git.MarkerRange{{Start: 1, End: 5}}},
		{"indented is text", "  <<<<<<< local\n  =======\n  >>>>>>> remote\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := git.Snapshot{Files: []git.File{{Path: "notes/a.md", Data: []byte(c.data)}}}
			files := rejectMarkers(snap)
			if c.want == nil {
				if len(files) != 0 {
					t.Fatalf("rejectMarkers() = %+v, want no conflict", files)
				}
				return
			}
			if len(files) != 1 || files[0].Path != "notes/a.md" || !reflect.DeepEqual(files[0].Ranges, c.want) {
				t.Fatalf("rejectMarkers() = %+v, want notes/a.md with ranges %+v", files, c.want)
			}
		})
	}
}

// TestCommitRejectsMarkerBlockBeforeS3 proves a complete marker block in
// any candidate file is a CONTENT_CONFLICT with the exact path and ranges,
// before any Git or S3 mutation.
func TestCommitRejectsMarkerBlockBeforeS3(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "clean"})
	pullOK(t, nb)
	writeLocal(t, w, map[string]string{
		"conflict.md": "<<<<<<< local\na\n=======\nb\n>>>>>>> remote\n",
		"other.md":    "fine",
	})

	ne := assertErrorCode(t, nb.Commit(context.Background(), "msg"), CodeContentConflict)
	if len(ne.Files) != 1 || ne.Files[0].Path != "conflict.md" {
		t.Fatalf("conflict files = %+v, want conflict.md", ne.Files)
	}
	if want := []git.MarkerRange{{Start: 1, End: 5}}; !reflect.DeepEqual(ne.Files[0].Ranges, want) {
		t.Fatalf("conflict ranges = %+v, want %+v", ne.Files[0].Ranges, want)
	}
	if got := store.Calls(fake.OpGet); got != 1 {
		t.Fatalf("marker rejection made %d GET calls, want only the pull's read", got)
	}
}

// TestCommitAcceptsNearMatchMarkerText proves a marker signature with any
// changed character is ordinary text and commits normally.
func TestCommitAcceptsNearMatchMarkerText(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"literal.md": "<<<<<<< mine\n=======\n>>>>>>> remote\n"})
	pullOK(t, nb)
	commitOK(t, nb, "near match is text")

	m := readManifest(t, store)
	if m.Generation != 1 {
		t.Fatalf("generation = %d, want the near-match file published at 1", m.Generation)
	}
}

// TestCommitRejectsInvalidContent proves invalid UTF-8 and U+0000 content
// is rejected before any Git or S3 mutation.
func TestCommitRejectsInvalidContent(t *testing.T) {
	cases := map[string][]byte{
		"invalid utf8": {0xff, 0xfe, 0x01},
		"nul byte":     []byte("a\x00b"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			store := fake.New("")
			ids := &testIDSource{}
			nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
			writeLocal(t, w, map[string]string{"ok.md": "fine"})
			pullOK(t, nb)
			if err := os.WriteFile(filepath.Join(w.Path(), "bad.md"), data, 0o644); err != nil {
				t.Fatalf("write invalid file: %v", err)
			}
			assertErrorCode(t, nb.Commit(context.Background(), "msg"), CodeInvalidRequest)
			if got := store.Calls(fake.OpGet); got != 1 {
				t.Fatalf("invalid content made %d GET calls, want only the pull's read", got)
			}
		})
	}
}

// TestFirstCommitCreatesCheckpointPublication proves the first publication:
// a root commit, a state-complete checkpoint pack, a shallow boundary, and
// current created with If-None-Match: *.
func TestFirstCommitCreatesCheckpointPublication(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "hello"})
	pullOK(t, nb)
	commitOK(t, nb, "first")

	m := readManifest(t, store)
	if m.Generation != 1 || len(m.Increments) != 0 {
		t.Fatalf("manifest = generation %d with %d increments, want 1 with none", m.Generation, len(m.Increments))
	}
	if m.Checkpoint.ThroughGeneration != 1 || m.Checkpoint.Head != m.Head {
		t.Fatalf("checkpoint = %+v, want through 1 and head equal to manifest head", m.Checkpoint)
	}
	if m.Checkpoint.Publication != testUUIDv7(1) || m.Checkpoint.ID != testUUIDv7(2) {
		t.Fatalf("checkpoint ids = pub %s id %s, want deterministic 1 and 2", m.Checkpoint.Publication, m.Checkpoint.ID)
	}
	if got := store.Calls(fake.OpCreate); got != 1 {
		t.Fatalf("CreateObject calls = %d, want exactly one If-None-Match creation", got)
	}
	if got := store.ObjectCount(); got != 2 {
		t.Fatalf("object count = %d, want current + checkpoint pack", got)
	}
	rc, _, err := store.ReadObject(context.Background(), m.Checkpoint.Key.String())
	if err != nil {
		t.Fatalf("read checkpoint pack = %v", err)
	}
	packBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read pack body = %v", err)
	}
	rc.Close()
	if len(packBytes) != int(m.Checkpoint.Size) {
		t.Fatalf("pack size = %d, want %d", len(packBytes), m.Checkpoint.Size)
	}
	if sum := storage.SHA256(sha256.Sum256(packBytes)); sum != m.Checkpoint.SHA256 {
		t.Fatalf("pack sha = %s, want %s", sum, m.Checkpoint.SHA256)
	}

	commit, err := w.Repo().ReadCommit(m.Head)
	if err != nil {
		t.Fatalf("ReadCommit(head) = %v", err)
	}
	if len(commit.Parents) != 0 {
		t.Fatalf("first commit parents = %v, want a root commit", commit.Parents)
	}
	fakeRepo, ok := w.Repo().(*fakeRepository)
	if !ok {
		t.Fatalf("repo is %T, want the fake repository", w.Repo())
	}
	if len(fakeRepo.data.shallow) != 1 || fakeRepo.data.shallow[0] != m.Head {
		t.Fatalf("shallow boundary = %v, want [%s]", fakeRepo.data.shallow, m.Head)
	}
	if got := readLocal(t, w, "a.md"); got != "hello" {
		t.Fatalf("L = %q, want the published content", got)
	}
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}
}

// TestCommitNoChangeSynchronizes proves a clean no-change commit returns
// OK without a remote mutation and synchronizes L and P to R.
func TestCommitNoChangeSynchronizes(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	pullOK(t, nb)

	// No local files at all: the merged result equals R, so no publication.
	commitOK(t, nb, "no change")
	if store.ObjectCount() != 0 {
		t.Fatalf("no-change commit created %d remote objects, want none", store.ObjectCount())
	}
	if gen := w.Baseline().RemoteGeneration; gen != 0 {
		t.Fatalf("baseline generation = %d, want 0", gen)
	}

	// After a real publication, a second no-change commit also stays local.
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	commitOK(t, nb, "first")
	afterFirst := store.ObjectCount()
	commitOK(t, nb, "no change again")
	if got := store.ObjectCount(); got != afterFirst {
		t.Fatalf("no-change commit after a publication created objects: %d -> %d", afterFirst, got)
	}
	if gen := w.Baseline().RemoteGeneration; gen != 1 {
		t.Fatalf("baseline generation = %d, want 1", gen)
	}
	if got := readLocal(t, w, "a.md"); got != "v1" {
		t.Fatalf("L = %q, want the accepted content", got)
	}
}

// TestCommitAppendsIncrementPack proves a normal commit publishes exactly
// one incremental pack and no new checkpoint.
func TestCommitAppendsIncrementPack(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)
	commitOK(t, nb, "first")
	m1 := readManifest(t, store)

	writeLocal(t, w, map[string]string{"a.md": "v2"})
	commitOK(t, nb, "second")
	m2 := readManifest(t, store)

	if m2.Generation != 2 {
		t.Fatalf("generation = %d, want 2", m2.Generation)
	}
	if len(m2.Increments) != 1 {
		t.Fatalf("increments = %d, want one", len(m2.Increments))
	}
	inc := m2.Increments[0]
	if inc.Publication != testUUIDv7(3) || inc.Generation != 2 || inc.Parent != m1.Head || inc.Head != m2.Head {
		t.Fatalf("increment = %+v, want publication 3, generation 2, parent gen-1 head", inc)
	}
	if got := store.ObjectCount(); got != 3 {
		t.Fatalf("object count = %d, want current + checkpoint + increment", got)
	}
	commit, err := w.Repo().ReadCommit(m2.Head)
	if err != nil {
		t.Fatalf("ReadCommit(head) = %v", err)
	}
	if len(commit.Parents) != 1 || commit.Parents[0] != m1.Head {
		t.Fatalf("second commit parents = %v, want [gen-1 head]", commit.Parents)
	}
}

// TestCommitUploadsPackBeforeManifestCAS proves the publication order: the
// immutable pack is stored before the manifest CAS accepts the proposal.
func TestCommitUploadsPackBeforeManifestCAS(t *testing.T) {
	store := fake.New("")
	gate := &casGateStore{ObjectStore: store, entered: make(chan struct{}), release: make(chan struct{})}
	ids := &testIDSource{}
	nb, w, _ := newNotebook(t, nbConfig{store: gate, ids: ids})
	writeLocal(t, w, map[string]string{"a.md": "v1"})
	pullOK(t, nb)

	done := make(chan error, 1)
	go func() { done <- nb.Commit(context.Background(), "first") }()
	<-gate.entered // the manifest CAS is in flight and blocked

	cpKey := "packs/checkpoints/1-" + testUUIDv7(2).String() + ".pack"
	rc, _, err := gate.ReadObject(context.Background(), cpKey)
	if err != nil {
		t.Fatalf("checkpoint pack absent while the CAS is blocked: %v", err)
	}
	rc.Close()
	if _, _, err := gate.ReadObject(context.Background(), storage.CurrentKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("current exists before the CAS ran: %v", err)
	}
	if got := store.Calls(fake.OpReplace); got != 0 {
		t.Fatalf("ReplaceObject ran while blocked: %d calls", got)
	}

	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	m := readManifest(t, gate)
	if m.Checkpoint.Key.String() != cpKey {
		t.Fatalf("manifest references %q, want %q", m.Checkpoint.Key, cpKey)
	}
}

// TestCommitCASRaceOneWinnerOneRetry proves a deterministic two-writer
// race: one winner and exactly one retry, where the loser builds a new
// generation, publication ID, key, commit, and pack, and never publishes
// the losing attempt's pack at a later generation.
func TestCommitCASRaceOneWinnerOneRetry(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base", "a.md": "a"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)

	writeLocal(t, aw, map[string]string{"a.md": "A"})
	commitOK(t, a, "A change")

	writeLocal(t, bw, map[string]string{"b.md": "B"})
	store.FailNextKey(fake.OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	commitOK(t, b, "B change")

	m := readManifest(t, store)
	if m.Generation != 3 {
		t.Fatalf("generation = %d, want 3", m.Generation)
	}
	if len(m.Increments) != 2 {
		t.Fatalf("increments = %d, want 2", len(m.Increments))
	}
	if m.Increments[0].Publication != testUUIDv7(3) {
		t.Fatalf("gen-2 publication = %s, want A's id 3", m.Increments[0].Publication)
	}
	if m.Increments[1].Publication != testUUIDv7(5) {
		t.Fatalf("gen-3 publication = %s, want B's retry id 5", m.Increments[1].Publication)
	}
	// The losing attempt's pack is an orphan at gen 3 with B's first id;
	// the manifest references only the retry's pack.
	orphanKey := "packs/increments/3-" + testUUIDv7(4).String() + ".pack"
	if _, _, err := store.ReadObject(context.Background(), orphanKey); err != nil {
		t.Fatalf("losing attempt pack missing: %v", err)
	}
	if m.Increments[1].Key.String() == orphanKey {
		t.Fatal("manifest references the losing attempt's pack")
	}
	if got := store.Calls(fake.OpReplace); got != 3 {
		t.Fatalf("ReplaceObject calls = %d, want A's win + B's loss + B's retry", got)
	}

	got := localSnapshot(t, bw)
	want := map[string]string{"shared.md": "base", "a.md": "A", "b.md": "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("L after the race = %v, want both changes %v", got, want)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 3 {
		t.Fatalf("B baseline generation = %d, want 3", gen)
	}
}

// TestCommitOverlappingConflictWritesMarkers proves two overlapping
// commits race into a content conflict: the accepted state stays valid and
// the loser materializes markers without a false success.
func TestCommitOverlappingConflictWritesMarkers(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"f.txt": "base\n"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)

	writeLocal(t, aw, map[string]string{"f.txt": "A\n"})
	commitOK(t, a, "A")
	putsBefore := store.Calls(fake.OpPut)

	writeLocal(t, bw, map[string]string{"f.txt": "B\n"})
	store.FailNextKey(fake.OpReplace, storage.CurrentKey, storage.ErrPreconditionFailed)
	ne := assertErrorCode(t, b.Commit(context.Background(), "B"), CodeContentConflict)
	if len(ne.Files) != 1 || ne.Files[0].Path != "f.txt" {
		t.Fatalf("conflict files = %+v, want f.txt", ne.Files)
	}
	if want := []git.MarkerRange{{Start: 1, End: 5}}; !reflect.DeepEqual(ne.Files[0].Ranges, want) {
		t.Fatalf("conflict ranges = %+v, want %+v", ne.Files[0].Ranges, want)
	}
	got := readLocal(t, bw, "f.txt")
	for _, marker := range []string{"<<<<<<< local", "B", "=======", "A", ">>>>>>> remote"} {
		if !strings.Contains(got, marker) {
			t.Errorf("f.txt missing %q: %q", marker, got)
		}
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 2 {
		t.Fatalf("B baseline generation = %d, want the remote state 2", gen)
	}
	m := readManifest(t, store)
	if m.Generation != 2 {
		t.Fatalf("current generation = %d, want the accepted state 2 unchanged", m.Generation)
	}
	if got := store.Calls(fake.OpPut); got != putsBefore {
		t.Fatalf("loser uploaded %d packs, want none", got-putsBefore)
	}
}

// TestCommitRemoteBusyPreservesFiles proves retry exhaustion: the exact
// bound returns REMOTE_BUSY, visible files stay available, and current is
// not mutated.
func TestCommitRemoteBusyPreservesFiles(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")

	// B pulls through the flaky wrapper (pulls never replace current) so
	// the commit's CAS bound is the only failure.
	flaky := &flakyStore{Store: store, failReplace: storage.ErrPreconditionFailed}
	nbB, bw, _ := newNotebook(t, nbConfig{store: flaky, ids: ids, retryLimit: 2})
	pullOK(t, nbB)
	writeLocal(t, bw, map[string]string{"b.md": "B"})
	assertErrorCode(t, nbB.Commit(context.Background(), "B"), CodeRemoteBusy)
	if got := flaky.replaceCalls; got != 3 {
		t.Fatalf("ReplaceObject attempts = %d, want retryLimit+1 = 3", got)
	}
	got := localSnapshot(t, bw)
	if len(got) != 2 || got["b.md"] != "B" {
		t.Fatalf("L after exhaustion = %v, want the caller's files preserved", got)
	}
	m := readManifest(t, store)
	if m.Generation != 1 {
		t.Fatalf("current generation = %d, want the accepted state 1 unchanged", m.Generation)
	}
}

// TestCommitAmbiguousAcceptedCAS proves a CAS whose response was lost after
// acceptance resolves by reading current and finding the publication ID.
func TestCommitAmbiguousAcceptedCAS(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)
	writeLocal(t, bw, map[string]string{"b.md": "B"})

	store.AmbiguousNext(fake.OpReplace, storage.CurrentKey)
	commitOK(t, b, "B")

	m := readManifest(t, store)
	if m.Generation != 2 || len(m.Increments) != 1 || m.Increments[0].Publication != testUUIDv7(3) {
		t.Fatalf("manifest = generation %d increments %+v, want B's publication at 2", m.Generation, m.Increments)
	}
	if gen := bw.Baseline().RemoteGeneration; gen != 2 {
		t.Fatalf("B baseline generation = %d, want 2", gen)
	}
	got := localSnapshot(t, bw)
	if got["shared.md"] != "base" || got["b.md"] != "B" {
		t.Fatalf("L = %v, want both changes", got)
	}
}

// TestCommitAmbiguousRejectedCAS proves a CAS whose response was lost
// before acceptance is a STORAGE_FAILURE, never an OK, and the visible
// workspace is preserved without automatic republication.
func TestCommitAmbiguousRejectedCAS(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)
	writeLocal(t, bw, map[string]string{"b.md": "B"})

	store.FailNext(fake.OpReplace, storage.ErrTransport)
	assertErrorCode(t, b.Commit(context.Background(), "B"), CodeStorageFailure)

	m := readManifest(t, store)
	if m.Generation != 1 {
		t.Fatalf("current generation = %d, want the accepted state 1 unchanged", m.Generation)
	}
	got := localSnapshot(t, bw)
	if len(got) != 2 || got["b.md"] != "B" {
		t.Fatalf("L after unprovable CAS = %v, want the caller's files preserved", got)
	}
	if got := store.Calls(fake.OpReplace); got != 1 {
		t.Fatalf("ReplaceObject calls = %d, want exactly one without republication", got)
	}
}

// TestCommitPackUploadFailure proves a failed pack upload leaves current
// and the visible workspace unchanged.
func TestCommitPackUploadFailure(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)
	writeLocal(t, bw, map[string]string{"b.md": "B"})

	store.FailNext(fake.OpPut, storage.ErrTransport)
	assertErrorCode(t, b.Commit(context.Background(), "B"), CodeStorageFailure)

	m := readManifest(t, store)
	if m.Generation != 1 {
		t.Fatalf("current generation = %d, want the accepted state 1 unchanged", m.Generation)
	}
	if got := store.Calls(fake.OpReplace); got != 0 {
		t.Fatalf("ReplaceObject calls = %d, want none after a failed upload", got)
	}
	got := localSnapshot(t, bw)
	if len(got) != 2 || got["b.md"] != "B" {
		t.Fatalf("L after upload failure = %v, want the caller's files preserved", got)
	}
}

// TestCommitPackUploadAmbiguous proves a pack upload whose response was
// lost resolves by read-back verification and continues to the CAS.
func TestCommitPackUploadAmbiguous(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	a, aw, _ := newNotebook(t, nbConfig{store: store, ids: ids})
	b, bw, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	writeLocal(t, aw, map[string]string{"shared.md": "base"})
	pullOK(t, a)
	commitOK(t, a, "base")
	pullOK(t, b)
	writeLocal(t, bw, map[string]string{"b.md": "B"})

	packKey := "packs/increments/2-" + testUUIDv7(3).String() + ".pack"
	store.AmbiguousNext(fake.OpPut, packKey)
	commitOK(t, b, "B")

	m := readManifest(t, store)
	if m.Generation != 2 || m.Increments[0].Key.String() != packKey {
		t.Fatalf("manifest = generation %d increment key %q, want the ambiguous upload accepted", m.Generation, m.Increments[0].Key)
	}
}

// TestLookupPublicationSearchesRetained proves acceptance lookup covers
// active and retained checkpoint and increment descriptors.
func TestLookupPublicationSearchesRetained(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	nb, _, _ := newNotebook(t, nbConfig{store: store, ids: ids})

	head := func(hex string) git.OID {
		id, err := git.ParseOID(strings.Repeat("0", 40-len(hex)) + hex)
		if err != nil {
			t.Fatalf("ParseOID() = %v", err)
		}
		return id
	}
	var sum storage.SHA256
	cpPub, cpID := testUUIDv7(1), testUUIDv7(2)
	incPub := testUUIDv7(3)
	activePub, activeID := testUUIDv7(4), testUUIDv7(5)
	h1, h2, h3 := head("1"), head("2"), head("3")
	m := storage.Manifest{
		Version:    1,
		Generation: 3,
		Head:       h3,
		Checkpoint: storage.Checkpoint{
			ID: activeID, Publication: activePub, ThroughGeneration: 3, Head: h3,
			Key:    storage.Key{Kind: storage.KindCheckpoint, Generation: 3, ID: activeID},
			SHA256: sum, Size: 10,
		},
		Increments: []storage.Increment{},
		Retained: []storage.Retained{{
			RetiredAtGeneration: 3,
			Head:                h2,
			Checkpoint: storage.Checkpoint{
				ID: cpID, Publication: cpPub, ThroughGeneration: 1, Head: h1,
				Key:    storage.Key{Kind: storage.KindCheckpoint, Generation: 1, ID: cpID},
				SHA256: sum, Size: 10,
			},
			Increments: []storage.Increment{{
				Generation: 2, Publication: incPub, Parent: h1, Head: h2,
				Key:    storage.Key{Kind: storage.KindIncrement, Generation: 2, ID: incPub},
				SHA256: sum, Size: 10,
			}},
		}},
	}
	data, err := storage.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	if _, err := store.CreateObject(context.Background(), storage.CurrentKey, data); err != nil {
		t.Fatalf("CreateObject() = %v", err)
	}

	for _, id := range []storage.UUID{activePub, cpPub, incPub} {
		found, err := nb.lookupPublication(context.Background(), id)
		if err != nil {
			t.Fatalf("lookupPublication(%s) = %v", id, err)
		}
		if !found {
			t.Fatalf("lookupPublication(%s) = false, want the descriptor found", id)
		}
	}
	found, err := nb.lookupPublication(context.Background(), testUUIDv7(99))
	if err != nil {
		t.Fatalf("lookupPublication(absent) = %v", err)
	}
	if found {
		t.Fatal("lookupPublication(absent) = true, want false")
	}
}

// TestCommitAtWorkspaceRoot proves the visible-directory replacement keeps
// the workspace usable when the requested path IS the workspace root
// (rel == "."): the first pull renames the root directory away and a new
// directory into place, which leaves the os.Root handle stale. The
// following commit's snapshot must still scan the new directory. The
// fresh-environment walkthrough caught this as a STORAGE_FAILURE on the
// second operation.
func TestCommitAtWorkspaceRoot(t *testing.T) {
	store := fake.New("")
	ids := &testIDSource{}
	root := t.TempDir()
	wcfg := workspace.Config{
		WorkspaceRoot: root,
		Path:          root,
		PrivateRoot:   t.TempDir(),
		Identity:      testIdentity(),
		Engine:        newFakeEngine(),
	}
	w, err := workspace.Open(context.Background(), wcfg)
	if err != nil {
		t.Fatalf("workspace.Open() = %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	nb, err := New(Config{
		Workspace:           w,
		Store:               store,
		RetryLimit:          DefaultRetryLimit,
		CheckpointPacks:     DefaultCheckpointPacks,
		RetainedCheckpoints: DefaultRetainedCheckpoints,
		NewID:               ids.next,
		Now:                 func() time.Time { return testNow },
		Waiter:              noSleepWaiter(),
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	pullOK(t, nb) // replaces the root directory itself
	writeLocal(t, w, map[string]string{"a.md": "hello"})
	commitOK(t, nb, "first commit at the root") // scans through the refreshed root
	writeLocal(t, w, map[string]string{"b.md": "world"})
	commitOK(t, nb, "second commit at the root") // every later operation keeps working

	snap := localSnapshot(t, w)
	if snap["a.md"] != "hello" || snap["b.md"] != "world" {
		t.Fatalf("visible state = %v, want a.md and b.md", snap)
	}
	m := readManifest(t, store)
	if m.Generation != 2 || len(m.Increments) != 1 {
		t.Fatalf("manifest = %+v, want generation 2 with one increment", m)
	}
}
