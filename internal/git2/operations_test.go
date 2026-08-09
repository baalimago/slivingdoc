package git2

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
)

// fixedTime is the deterministic operation-attempt clock for component
// tests, matching the architecture's UTC-second commit time.
func fixedTime() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) }

// newTestRepo opens the native engine, creates one repository in a
// temporary directory, and registers cleanup that closes both.
func newTestRepo(t *testing.T) git.Repository {
	t.Helper()
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	repo, err := e.CreateRepo(t.TempDir())
	if err != nil {
		e.Close()
		t.Fatalf("CreateRepo() = %v", err)
	}
	t.Cleanup(func() {
		repo.Close()
		e.Close()
	})
	return repo
}

// buildSnapshotTree writes a deterministic tree from a path->content map.
func buildSnapshotTree(t *testing.T, repo git.Repository, files map[string]string) git.OID {
	t.Helper()
	snap := git.Snapshot{}
	for path, content := range files {
		snap.Files = append(snap.Files, git.File{Path: path, Data: []byte(content)})
	}
	sort.Slice(snap.Files, func(i, j int) bool { return snap.Files[i].Path < snap.Files[j].Path })
	tree, err := git.BuildTree(repo, snap)
	if err != nil {
		t.Fatalf("BuildTree() = %v", err)
	}
	return tree
}

// commitAt writes a commit with the fixed identity and test clock.
func commitAt(t *testing.T, repo git.Repository, tree git.OID, parents []git.OID, msg string) git.OID {
	t.Helper()
	id, err := git.CreateCommit(repo, git.CommitSpec{Message: msg, Tree: tree, Parents: parents, Time: fixedTime()})
	if err != nil {
		t.Fatalf("CreateCommit(%q) = %v", msg, err)
	}
	return id
}

// readSnapshotNames returns the sorted path list of a tree for assertions.
func readSnapshotNames(t *testing.T, repo git.Repository, tree git.OID) []string {
	t.Helper()
	snap, err := git.ReadSnapshot(repo, tree)
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	names := make([]string, len(snap.Files))
	for i, f := range snap.Files {
		names[i] = f.Path
	}
	return names
}

func TestTreeWriteReadRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	blobA, err := repo.WriteBlob([]byte("a"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	blobB, err := repo.WriteBlob([]byte("b"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	sub, err := repo.WriteTree([]git.TreeEntry{{Name: "inner.txt", Mode: git.ModeBlob, ID: blobB}})
	if err != nil {
		t.Fatalf("WriteTree(sub) = %v", err)
	}

	// Shuffled input must produce one canonical Git tree order.
	entries := []git.TreeEntry{
		{Name: "z.txt", Mode: git.ModeBlob, ID: blobA},
		{Name: "dir", Mode: git.ModeTree, ID: sub},
		{Name: "a.txt", Mode: git.ModeBlob, ID: blobA},
	}
	first, err := repo.WriteTree(entries)
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	reversed := []git.TreeEntry{
		{Name: "a.txt", Mode: git.ModeBlob, ID: blobA},
		{Name: "dir", Mode: git.ModeTree, ID: sub},
		{Name: "z.txt", Mode: git.ModeBlob, ID: blobA},
	}
	second, err := repo.WriteTree(reversed)
	if err != nil {
		t.Fatalf("WriteTree(reversed) = %v", err)
	}
	if first != second {
		t.Fatalf("WriteTree not deterministic: %s != %s", first, second)
	}

	got, err := repo.ReadTree(first)
	if err != nil {
		t.Fatalf("ReadTree() = %v", err)
	}
	want := []git.TreeEntry{
		{Name: "a.txt", Mode: git.ModeBlob, ID: blobA},
		{Name: "dir", Mode: git.ModeTree, ID: sub},
		{Name: "z.txt", Mode: git.ModeBlob, ID: blobA},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadTree() = %+v, want %+v", got, want)
	}
}

func TestWriteTreeRejectsUnsupportedModes(t *testing.T) {
	repo := newTestRepo(t)
	blob, err := repo.WriteBlob([]byte("x"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	for _, mode := range []git.FileMode{0o100755, 0o120000, 0o160000, 0o100664} {
		if _, err := repo.WriteTree([]git.TreeEntry{{Name: "evil", Mode: mode, ID: blob}}); err == nil {
			t.Errorf("WriteTree(mode %o) = nil, want unsupported-mode error", mode)
		}
	}
}

func TestSnapshotRoundTripByteForByte(t *testing.T) {
	repo := newTestRepo(t)
	orig := map[string]string{
		"empty":            "",
		"nested/deep/file": "content",
		"unicode-ümlaut":   "ü",
		"emoji-📝.md":       "note",
		"crlf.txt":         "a\r\nb\r\n",
		"mixed-lines.txt":  "a\r\nb\nc\r\n",
		"z-last.txt":       "z",
	}
	tree := buildSnapshotTree(t, repo, orig)
	got, err := git.ReadSnapshot(repo, tree)
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	byPath := make(map[string][]byte, len(orig))
	for _, f := range got.Files {
		byPath[f.Path] = f.Data
	}
	for path, want := range orig {
		if !bytes.Equal(byPath[path], []byte(want)) {
			t.Errorf("file %q = %q, want %q", path, byPath[path], want)
		}
	}
}

func TestSnapshotRejectsInvalidContentBeforeTreeCreation(t *testing.T) {
	repo := newTestRepo(t)
	for name, data := range map[string]string{
		"nul":      "a\x00b",
		"invalid":  string([]byte{0xff, 0xfe}),
		"bad path": "",
	} {
		snap := git.Snapshot{Files: []git.File{{Path: "x.txt", Data: []byte(data)}}}
		if name == "bad path" {
			snap.Files[0].Path = "../escape"
			snap.Files[0].Data = []byte("x")
		}
		if _, err := git.BuildTree(repo, snap); err == nil {
			t.Errorf("BuildTree(%s) = nil, want validation error", name)
		}
	}
}

func TestEmptyTreeNative(t *testing.T) {
	repo := newTestRepo(t)
	empty, err := git.EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	if empty.String() != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Fatalf("empty tree = %s, want the canonical empty tree", empty)
	}
	entries, err := repo.ReadTree(empty)
	if err != nil {
		t.Fatalf("ReadTree(empty) = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty tree has %d entries", len(entries))
	}
	snap, err := git.ReadSnapshot(repo, empty)
	if err != nil {
		t.Fatalf("ReadSnapshot(empty) = %v", err)
	}
	if len(snap.Files) != 0 {
		t.Fatalf("empty snapshot has %d files", len(snap.Files))
	}
}

func TestCommitRootAndParentNative(t *testing.T) {
	repo := newTestRepo(t)
	tree := buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"})

	root := commitAt(t, repo, tree, nil, "first")
	got, err := repo.ReadCommit(root)
	if err != nil {
		t.Fatalf("ReadCommit(root) = %v", err)
	}
	if got.Tree != tree || len(got.Parents) != 0 || got.Message != "first" {
		t.Fatalf("root commit = %+v", got)
	}

	next := commitAt(t, repo, tree, []git.OID{root}, "second")
	got, err = repo.ReadCommit(next)
	if err != nil {
		t.Fatalf("ReadCommit(next) = %v", err)
	}
	if len(got.Parents) != 1 || got.Parents[0] != root {
		t.Fatalf("next commit parents = %v, want [%s]", got.Parents, root)
	}
	if got.Message != "second" {
		t.Fatalf("next commit message = %q, want second", got.Message)
	}
}

func TestCommitRejectsMissingParentObject(t *testing.T) {
	repo := newTestRepo(t)
	tree := buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"})
	var ghost git.OID
	ghost[0] = 0xaa
	if _, err := git.CreateCommit(repo, git.CommitSpec{Message: "x", Tree: tree, Parents: []git.OID{ghost}, Time: fixedTime()}); err == nil {
		t.Fatal("CreateCommit(missing parent) = nil, want lookup error")
	}
}

func TestMergeDisjointTrees(t *testing.T) {
	repo := newTestRepo(t)
	empty, err := git.EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	local := buildSnapshotTree(t, repo, map[string]string{"local.txt": "local"})
	remote := buildSnapshotTree(t, repo, map[string]string{"remote.txt": "remote"})

	res, err := git.Merge(repo, empty, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("Merge() conflicts = %+v, want none", res.Conflicts)
	}
	if res.Tree.IsZero() {
		t.Fatal("Merge() produced no tree")
	}
	got := readSnapshotNames(t, repo, res.Tree)
	want := []string{"local.txt", "remote.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged tree files = %v, want %v", got, want)
	}
}

func TestMergeCompatibleLineChanges(t *testing.T) {
	repo := newTestRepo(t)
	base := buildSnapshotTree(t, repo, map[string]string{"f.txt": "one\ntwo\nthree\n"})
	local := buildSnapshotTree(t, repo, map[string]string{"f.txt": "ONE\ntwo\nthree\n"})
	remote := buildSnapshotTree(t, repo, map[string]string{"f.txt": "one\ntwo\nTHREE\n"})

	res, err := git.Merge(repo, base, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("Merge() conflicts = %+v, want none", res.Conflicts)
	}
	snap, err := git.ReadSnapshot(repo, res.Tree)
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	if got, want := string(snap.Files[0].Data), "ONE\ntwo\nTHREE\n"; got != want {
		t.Fatalf("merged content = %q, want %q", got, want)
	}
}

func TestMergeOverlappingTextProducesMarkers(t *testing.T) {
	repo := newTestRepo(t)
	base := buildSnapshotTree(t, repo, map[string]string{"notes/a.md": "one\ntwo\nthree\n"})
	local := buildSnapshotTree(t, repo, map[string]string{"notes/a.md": "ONE\ntwo\nthree\n"})
	remote := buildSnapshotTree(t, repo, map[string]string{"notes/a.md": "one\nTWO\nthree\n"})

	idx, err := repo.MergeTrees(base, local, remote)
	if err != nil {
		t.Fatalf("MergeTrees() = %v", err)
	}
	if !idx.Tree.IsZero() {
		t.Fatalf("conflicted merge tree = %s, want zero", idx.Tree)
	}
	stages := map[int]bool{}
	for _, e := range idx.Entries {
		if e.Path == "notes/a.md" {
			stages[e.Stage] = true
		}
	}
	if !stages[1] || !stages[2] || !stages[3] {
		t.Fatalf("merge index stages = %v, want 1, 2, and 3 for notes/a.md", stages)
	}

	res, err := git.Merge(repo, base, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Merge() conflicts = %+v, want one", res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Path != "notes/a.md" {
		t.Fatalf("conflict path = %q, want notes/a.md", c.Path)
	}
	content := string(c.Content)
	for _, marker := range []string{"<<<<<<< local", "=======", ">>>>>>> remote", "ONE", "TWO"} {
		if !strings.Contains(content, marker) {
			t.Errorf("conflict content missing %q:\n%s", marker, content)
		}
	}
	if len(c.Ranges) != 1 || c.Ranges[0].Start != 1 || c.Ranges[0].End != 7 {
		t.Fatalf("conflict ranges = %+v, want one block rows 1..7", c.Ranges)
	}
}

func TestMergeAddAddConflict(t *testing.T) {
	repo := newTestRepo(t)
	empty, err := git.EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	local := buildSnapshotTree(t, repo, map[string]string{"a.txt": "local"})
	remote := buildSnapshotTree(t, repo, map[string]string{"a.txt": "remote"})

	res, err := git.Merge(repo, empty, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "a.txt" {
		t.Fatalf("Merge() conflicts = %+v, want one add/add at a.txt", res.Conflicts)
	}
	if !strings.Contains(string(res.Conflicts[0].Content), "<<<<<<< local") {
		t.Fatalf("add/add conflict must carry markers, got %q", res.Conflicts[0].Content)
	}
}

func TestMergeModifyDeleteConflict(t *testing.T) {
	repo := newTestRepo(t)
	base := buildSnapshotTree(t, repo, map[string]string{"f.txt": "original\n"})
	local := buildSnapshotTree(t, repo, map[string]string{"f.txt": "changed\n"})
	remote := buildSnapshotTree(t, repo, map[string]string{})

	res, err := git.Merge(repo, base, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "f.txt" {
		t.Fatalf("Merge() conflicts = %+v, want one modify/delete at f.txt", res.Conflicts)
	}
	content := string(res.Conflicts[0].Content)
	if !strings.Contains(content, "<<<<<<< local") || !strings.Contains(content, ">>>>>>> remote") {
		t.Fatalf("modify/delete conflict must carry markers, got %q", content)
	}
}

func TestMergeFileDirectoryConflict(t *testing.T) {
	repo := newTestRepo(t)
	empty, err := git.EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	local := buildSnapshotTree(t, repo, map[string]string{"p": "local file"})
	remote := buildSnapshotTree(t, repo, map[string]string{"p/q.txt": "remote file"})

	res, err := git.Merge(repo, empty, local, remote)
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) == 0 {
		t.Fatal("Merge() = no conflicts, want file/directory conflict")
	}
	for _, c := range res.Conflicts {
		if c.Content != nil || len(c.Ranges) != 0 {
			t.Fatalf("file/directory conflict %q must carry no markers, got %q", c.Path, c.Content)
		}
	}
}

func TestMergeFileDirect(t *testing.T) {
	repo := newTestRepo(t)
	base := []byte("one\ntwo\nthree\n")

	res, err := repo.MergeFile(base, []byte("ONE\ntwo\nthree\n"), []byte("one\ntwo\nTHREE\n"))
	if err != nil {
		t.Fatalf("MergeFile(disjoint) = %v", err)
	}
	if !res.Automergeable || string(res.Content) != "ONE\ntwo\nTHREE\n" {
		t.Fatalf("MergeFile(disjoint) = %+v, want automerged content", res)
	}

	res, err = repo.MergeFile(base, []byte("ONE\ntwo\nthree\n"), []byte("one\nTWO\nthree\n"))
	if err != nil {
		t.Fatalf("MergeFile(overlap) = %v", err)
	}
	if res.Automergeable {
		t.Fatal("MergeFile(overlap) must not be automergeable")
	}
	for _, marker := range []string{"<<<<<<< local", "=======", ">>>>>>> remote"} {
		if !bytes.Contains(res.Content, []byte(marker)) {
			t.Errorf("MergeFile markers missing %q in %q", marker, res.Content)
		}
	}

	// A deleted remote side (nil input) produces a conflict with an empty
	// remote section.
	res, err = repo.MergeFile(base, []byte("ONE\ntwo\nthree\n"), nil)
	if err != nil {
		t.Fatalf("MergeFile(delete) = %v", err)
	}
	if res.Automergeable {
		t.Fatal("MergeFile(delete) must not be automergeable")
	}
}

func TestIncrementPackAfterCheckpoint(t *testing.T) {
	repo := newTestRepo(t)
	c1 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"}), nil, "one")
	c2 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "two", "b.txt": "bee"}), []git.OID{c1}, "two")

	checkpoint, err := git.ExportCheckpoint(repo, c1)
	if err != nil {
		t.Fatalf("ExportCheckpoint() = %v", err)
	}
	increment, err := git.ExportIncrement(repo, c2, c1)
	if err != nil {
		t.Fatalf("ExportIncrement() = %v", err)
	}

	// A fresh repository reconstructs the checkpoint, then the increment.
	restored := newTestRepo(t)
	if err := git.ImportPack(restored, checkpoint.Data); err != nil {
		t.Fatalf("ImportPack(checkpoint) = %v", err)
	}
	if err := git.MarkShallow(restored, c1); err != nil {
		t.Fatalf("MarkShallow() = %v", err)
	}
	if err := git.ImportPack(restored, increment.Data); err != nil {
		t.Fatalf("ImportPack(increment) = %v", err)
	}
	if err := git.ValidateHistory(restored, c2, c1); err != nil {
		t.Fatalf("ValidateHistory() = %v", err)
	}
	got, err := git.ReadSnapshot(restored, mustCommitTree(t, restored, c2))
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	byPath := map[string]string{}
	for _, f := range got.Files {
		byPath[f.Path] = string(f.Data)
	}
	if byPath["a.txt"] != "two" || byPath["b.txt"] != "bee" {
		t.Fatalf("restored files = %v", byPath)
	}
}

func TestIncrementPackRequiresBase(t *testing.T) {
	repo := newTestRepo(t)
	c1 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"}), nil, "one")
	c2 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "two"}), []git.OID{c1}, "two")

	increment, err := git.ExportIncrement(repo, c2, c1)
	if err != nil {
		t.Fatalf("ExportIncrement() = %v", err)
	}

	// Importing into an empty repository succeeds (the pack is
	// self-contained) but the base commit is unavailable, so history
	// validation fails.
	empty := newTestRepo(t)
	if err := git.ImportPack(empty, increment.Data); err != nil {
		t.Fatalf("ImportPack(increment) = %v", err)
	}
	if _, err := empty.ReadCommit(c1); err == nil {
		t.Fatal("ReadCommit(base) = nil, want missing-base error")
	}
	if err := git.ValidateHistory(empty, c2, git.OID{}); err == nil {
		t.Fatal("ValidateHistory() = nil, want missing-base error")
	}
}

func TestTruncatedPackRejected(t *testing.T) {
	repo := newTestRepo(t)
	c1 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"}), nil, "one")
	checkpoint, err := git.ExportCheckpoint(repo, c1)
	if err != nil {
		t.Fatalf("ExportCheckpoint() = %v", err)
	}

	truncated := checkpoint.Data[:len(checkpoint.Data)-12]
	empty := newTestRepo(t)
	if err := git.ImportPack(empty, truncated); err == nil {
		t.Fatal("ImportPack(truncated) = nil, want integrity error")
	}
	if _, err := empty.ReadCommit(c1); err == nil {
		t.Fatal("truncated pack must not import any object")
	}
}

func TestCheckpointPackRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	c0 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"f.txt": "zero"}), nil, "zero")
	c1 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"f.txt": "one", "dir/g.txt": "g"}), []git.OID{c0}, "one")
	c2 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"f.txt": "two", "dir/g.txt": "g"}), []git.OID{c1}, "two")

	checkpoint, err := git.ExportCheckpoint(repo, c2)
	if err != nil {
		t.Fatalf("ExportCheckpoint() = %v", err)
	}

	restored := newTestRepo(t)
	if err := git.ImportPack(restored, checkpoint.Data); err != nil {
		t.Fatalf("ImportPack() = %v", err)
	}

	// The checkpoint reconstructs its complete tree without any older pack.
	got, err := git.ReadSnapshot(restored, mustCommitTree(t, restored, c2))
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	byPath := map[string]string{}
	for _, f := range got.Files {
		byPath[f.Path] = string(f.Data)
	}
	if byPath["f.txt"] != "two" || byPath["dir/g.txt"] != "g" {
		t.Fatalf("checkpoint files = %v", byPath)
	}

	// Pre-checkpoint history is intentionally absent.
	if _, err := restored.ReadCommit(c0); err == nil {
		t.Fatal("checkpoint pack must omit pre-checkpoint commits")
	}

	// Without the declared shallow boundary the walk fails; with it, the
	// single declared history gap is permitted.
	if err := git.ValidateHistory(restored, c2, git.OID{}); err == nil {
		t.Fatal("ValidateHistory(no shallow) = nil, want missing-parent error")
	}
	if err := git.MarkShallow(restored, c2); err != nil {
		t.Fatalf("MarkShallow() = %v", err)
	}
	if err := git.ValidateHistory(restored, c2, c2); err != nil {
		t.Fatalf("ValidateHistory(shallow) = %v", err)
	}

	// The checkpoint head supports later increment commits.
	c3 := commitAt(t, restored, buildSnapshotTree(t, restored, map[string]string{"f.txt": "three", "dir/g.txt": "g"}), []git.OID{c2}, "three")
	gotCommit, err := restored.ReadCommit(c3)
	if err != nil {
		t.Fatalf("ReadCommit(c3) = %v", err)
	}
	if len(gotCommit.Parents) != 1 || gotCommit.Parents[0] != c2 {
		t.Fatalf("c3 parents = %v, want [%s]", gotCommit.Parents, c2)
	}
}

func TestMarkShallowWritesBoundaryFile(t *testing.T) {
	gitdir := filepath.Join(t.TempDir(), ".git")
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	repo, err := e.CreateRepo(filepath.Dir(gitdir))
	if err != nil {
		e.Close()
		t.Fatalf("CreateRepo() = %v", err)
	}
	t.Cleanup(func() {
		repo.Close()
		e.Close()
	})

	c1 := commitAt(t, repo, buildSnapshotTree(t, repo, map[string]string{"a.txt": "one"}), nil, "one")
	if err := git.MarkShallow(repo, c1); err != nil {
		t.Fatalf("MarkShallow() = %v", err)
	}

	// The boundary lives in the repository git directory in Git's own
	// format, so a reopened repository agrees with the session that marked
	// it.
	data, err := os.ReadFile(filepath.Join(gitdir, "shallow"))
	if err != nil {
		t.Fatalf("shallow file: %v", err)
	}
	if got, want := string(data), c1.String()+"\n"; got != want {
		t.Fatalf("shallow file = %q, want %q", got, want)
	}

	// Marking the same head again is idempotent.
	if err := git.MarkShallow(repo, c1); err != nil {
		t.Fatalf("MarkShallow(again) = %v", err)
	}

	// With the boundary loaded, libgit2 grafts the declared shallow commit
	// to zero parents.
	got, err := repo.ReadCommit(c1)
	if err != nil {
		t.Fatalf("ReadCommit() = %v", err)
	}
	if len(got.Parents) != 0 {
		t.Fatalf("shallow commit parents = %v, want none after MarkShallow", got.Parents)
	}
}

func TestRepositoryLifetimes(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	repo, err := e.CreateRepo(t.TempDir())
	if err != nil {
		e.Close()
		t.Fatalf("CreateRepo() = %v", err)
	}
	tree, err := git.EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}

	// Closing the repository invalidates every later operation.
	if err := repo.Close(); err != nil {
		t.Fatalf("repo Close() = %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("double repo Close() = %v", err)
	}
	if _, err := repo.WriteBlob([]byte("x")); err == nil {
		t.Fatal("WriteBlob() after Close() = nil, want error")
	}
	if _, err := repo.WriteTree(nil); err == nil {
		t.Fatal("WriteTree() after Close() = nil, want error")
	}
	if _, err := repo.ReadTree(tree); err == nil {
		t.Fatal("ReadTree() after Close() = nil, want error")
	}

	// Closing the engine invalidates every repository it handed out.
	repo2, err := e.CreateRepo(t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo() = %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("engine Close() = %v", err)
	}
	if _, err := repo2.WriteBlob([]byte("x")); err == nil {
		t.Fatal("WriteBlob() after engine Close() = nil, want error")
	}
	if _, err := repo2.CreateCommit(git.CommitSpec{Message: "x", Tree: tree, Time: fixedTime()}); err == nil {
		t.Fatal("CreateCommit() after engine Close() = nil, want error")
	}
	if err := repo2.Close(); err != nil {
		t.Fatalf("repo2 Close() = %v", err)
	}
}

func TestNativeErrorsCarryDetail(t *testing.T) {
	repo := newTestRepo(t)
	var ghost git.OID
	ghost[0] = 0xbb
	_, err := repo.ReadTree(ghost)
	var ne *git.NativeError
	if !errors.As(err, &ne) {
		t.Fatalf("ReadTree(missing) error = %v, want *git.NativeError", err)
	}
	if ne.Op != "lookup tree" || ne.Message == "" {
		t.Fatalf("NativeError = %+v, want operation and detail", ne)
	}
}

// mustCommitTree returns the tree of a commit, failing the test on error.
func mustCommitTree(t *testing.T, repo git.Repository, id git.OID) git.OID {
	t.Helper()
	commit, err := repo.ReadCommit(id)
	if err != nil {
		t.Fatalf("ReadCommit(%s) = %v", id, err)
	}
	return commit.Tree
}
