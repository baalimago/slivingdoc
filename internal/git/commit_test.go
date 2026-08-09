package git

import (
	"strings"
	"testing"
)

func TestValidateCommitMessage(t *testing.T) {
	valid := []string{
		"add notes",
		"",
		"multi\nline\nmessage",
		"café ☕",
	}
	for _, m := range valid {
		if err := ValidateCommitMessage(m); err != nil {
			t.Errorf("ValidateCommitMessage(%q) = %v, want nil", m, err)
		}
	}

	invalid := []string{
		"bad\x00nul",
		"invalid \xff utf8",
	}
	for _, m := range invalid {
		if err := ValidateCommitMessage(m); err == nil {
			t.Errorf("ValidateCommitMessage(%q) = nil, want error", m)
		}
	}
}

func TestCreateCommitRootAndParent(t *testing.T) {
	repo := newFakeRepository()
	empty, err := EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}

	// A root commit has no parents (first publication, architecture 8.2).
	root, err := CreateCommit(repo, CommitSpec{Message: "first", Tree: empty, Time: fakeTime()})
	if err != nil {
		t.Fatalf("CreateCommit(root) = %v", err)
	}
	if root.IsZero() {
		t.Fatal("root commit OID must not be zero")
	}
	got, err := repo.ReadCommit(root)
	if err != nil {
		t.Fatalf("ReadCommit(root) = %v", err)
	}
	if len(got.Parents) != 0 {
		t.Fatalf("root commit parents = %v, want none", got.Parents)
	}
	if got.Tree != empty || got.Message != "first" {
		t.Fatalf("root commit = %+v", got)
	}

	// A normal commit has exactly the observed remote head as its single
	// parent.
	next, err := CreateCommit(repo, CommitSpec{Message: "second", Tree: empty, Parents: []OID{root}, Time: fakeTime()})
	if err != nil {
		t.Fatalf("CreateCommit(parent) = %v", err)
	}
	got, err = repo.ReadCommit(next)
	if err != nil {
		t.Fatalf("ReadCommit(next) = %v", err)
	}
	if len(got.Parents) != 1 || got.Parents[0] != root {
		t.Fatalf("next commit parents = %v, want [%s]", got.Parents, root)
	}
}

func TestCreateCommitRejectsBeforeMutation(t *testing.T) {
	repo := newFakeRepository()
	empty, err := EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}

	cases := []struct {
		name string
		spec CommitSpec
	}{
		{"invalid message", CommitSpec{Message: "bad\x00nul", Tree: empty, Time: fakeTime()}},
		{"zero tree", CommitSpec{Message: "ok", Time: fakeTime()}},
	}
	for _, c := range cases {
		if _, err := CreateCommit(repo, c.spec); err == nil {
			t.Errorf("CreateCommit(%s) = nil, want validation error", c.name)
		}
	}
	if len(repo.commits) != 0 {
		t.Fatalf("validation failure must not mutate the repository, stored %d commits", len(repo.commits))
	}
}

func TestCreateCommitErrorMessageNamesRootCommit(t *testing.T) {
	// The R1-01 contract: the engine must create a root commit with no
	// parent for the first publication. The policy passes the empty parent
	// slice through unchanged.
	repo := newFakeRepository()
	empty, err := EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	if _, err := CreateCommit(repo, CommitSpec{Message: "root", Tree: empty, Parents: nil, Time: fakeTime()}); err != nil {
		t.Fatalf("CreateCommit(nil parents) = %v", err)
	}
	for oid, c := range repo.commits {
		if len(c.Parents) != 0 {
			t.Fatalf("commit %s parents = %v, want root commit", oid, c.Parents)
		}
		if !strings.Contains(c.Message, "root") {
			t.Fatalf("commit %s message = %q, want the supplied message", oid, c.Message)
		}
	}
}
