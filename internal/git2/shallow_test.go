package git2

import (
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
)

// TestMarkShallowHonorsBoundaryWrittenByAnotherHandle proves a handle that
// outlives a boundary another handle appended reloads its graft table when
// it marks the same boundary, even though the shallow file already holds
// it: two processes share one private repository, and the one that did not
// import the checkpoint must still stop walking history at it
// (architecture/git-engine.md).
func TestMarkShallowHonorsBoundaryWrittenByAnotherHandle(t *testing.T) {
	e := New()
	if err := e.Open(); err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	dir := t.TempDir()
	first, err := e.CreateRepo(dir)
	if err != nil {
		t.Fatalf("CreateRepo() = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := e.OpenRepo(dir)
	if err != nil {
		t.Fatalf("OpenRepo() = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	blob, err := first.WriteBlob([]byte("x"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	tree, err := first.WriteTree([]git.TreeEntry{{Name: "a.md", Mode: git.ModeBlob, ID: blob}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	c1, err := first.CreateCommit(git.CommitSpec{Message: "one", Tree: tree, Time: fixedTime()})
	if err != nil {
		t.Fatalf("CreateCommit(one) = %v", err)
	}
	c2, err := first.CreateCommit(git.CommitSpec{Message: "two", Tree: tree, Parents: []git.OID{c1}, Time: fixedTime()})
	if err != nil {
		t.Fatalf("CreateCommit(two) = %v", err)
	}

	// The second handle records the boundary; the first only re-marks it.
	if err := second.MarkShallow(c2); err != nil {
		t.Fatalf("second.MarkShallow() = %v", err)
	}
	if err := first.MarkShallow(c2); err != nil {
		t.Fatalf("first.MarkShallow() = %v", err)
	}
	commit, err := first.ReadCommit(c2)
	if err != nil {
		t.Fatalf("first.ReadCommit() = %v", err)
	}
	if len(commit.Parents) != 0 {
		t.Fatalf("parents of the boundary on the first handle = %v, want none: the graft table was not reloaded", commit.Parents)
	}
	// Marking an already-loaded boundary again is a no-op that keeps it.
	if err := first.MarkShallow(c2); err != nil {
		t.Fatalf("first.MarkShallow() again = %v", err)
	}
	if commit, err = first.ReadCommit(c2); err != nil || len(commit.Parents) != 0 {
		t.Fatalf("boundary after the repeated mark = %v, %v; want it kept", commit.Parents, err)
	}
}
