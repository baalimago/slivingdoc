package git

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTreeEntryLessGitOrder(t *testing.T) {
	// Git tree order: byte-wise by name, with a tree entry comparing as if
	// its name carried a trailing slash. In particular "foo.txt" sorts
	// before the tree "foo", and "foo/" sorts before "foobar".
	cases := []struct {
		a, b TreeEntry
		want bool
	}{
		{TreeEntry{Name: "a.txt", Mode: ModeBlob}, TreeEntry{Name: "b.txt", Mode: ModeBlob}, true},
		{TreeEntry{Name: "b.txt", Mode: ModeBlob}, TreeEntry{Name: "a.txt", Mode: ModeBlob}, false},
		{TreeEntry{Name: "foo", Mode: ModeTree}, TreeEntry{Name: "foo.txt", Mode: ModeBlob}, false}, // foo.txt first
		{TreeEntry{Name: "foo.txt", Mode: ModeBlob}, TreeEntry{Name: "foo", Mode: ModeTree}, true},
		{TreeEntry{Name: "foo", Mode: ModeTree}, TreeEntry{Name: "foobar", Mode: ModeTree}, true},  // foo/ before foobar/
		{TreeEntry{Name: "a", Mode: ModeBlob}, TreeEntry{Name: "a.txt", Mode: ModeBlob}, true},     // a before a.txt
		{TreeEntry{Name: "A.txt", Mode: ModeBlob}, TreeEntry{Name: "a.txt", Mode: ModeBlob}, true}, // case-sensitive
	}
	for _, c := range cases {
		if got := treeEntryLess(c.a, c.b); got != c.want {
			t.Errorf("treeEntryLess(%q/%o, %q/%o) = %v, want %v", c.a.Name, c.a.Mode, c.b.Name, c.b.Mode, got, c.want)
		}
	}
}

func TestBuildTreeDeterministic(t *testing.T) {
	repo := newFakeRepository()
	snap := fakeSnapshot(map[string]string{
		"zeta.txt":        "z",
		"alpha/one.txt":   "1",
		"alpha/two/deep":  "2",
		"alpha.txt":       "top",
		"beta/empty":      "",
		"unicode-ümlaut":  "ü",
		"emoji-📝.md":      "note",
		"mixed-lines.txt": "a\r\nb\nc\r\n",
	})
	first, err := BuildTree(repo, snap)
	if err != nil {
		t.Fatalf("BuildTree() = %v", err)
	}

	// Reversed input order must produce the same tree.
	reversed := Snapshot{Files: append([]File(nil), snap.Files...)}
	for i, j := 0, len(reversed.Files)-1; i < j; i, j = i+1, j-1 {
		reversed.Files[i], reversed.Files[j] = reversed.Files[j], reversed.Files[i]
	}
	second, err := BuildTree(repo, reversed)
	if err != nil {
		t.Fatalf("BuildTree(reversed) = %v", err)
	}
	if first != second {
		t.Fatalf("BuildTree not deterministic: %s != %s", first, second)
	}
}

func TestBuildTreeRejectsInvalidSnapshot(t *testing.T) {
	repo := newFakeRepository()
	for name, snap := range map[string]Snapshot{
		"binary":  fakeSnapshot(map[string]string{"x.txt": "a\x00b"}),
		"badpath": fakeSnapshot(map[string]string{"../escape": "x"}),
		"nfc":     fakeSnapshot(map[string]string{"e\u0301.txt": "x"}),
	} {
		if _, err := BuildTree(repo, snap); err == nil {
			t.Errorf("BuildTree(%s) = nil, want error", name)
		}
	}
}

func TestReadSnapshotRoundTrip(t *testing.T) {
	repo := newFakeRepository()
	orig := fakeSnapshot(map[string]string{
		"empty":            "",
		"nested/deep/file": "content",
		"unicode-ümlaut":   "ü",
		"crlf.txt":         "a\r\nb\r\n",
		"z-last.txt":       "z",
	})
	tree, err := BuildTree(repo, orig)
	if err != nil {
		t.Fatalf("BuildTree() = %v", err)
	}
	got, err := ReadSnapshot(repo, tree)
	if err != nil {
		t.Fatalf("ReadSnapshot() = %v", err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("ReadSnapshot() = %+v, want %+v", got.Files, orig.Files)
	}
}

func TestReadSnapshotRejectsUnsupportedMode(t *testing.T) {
	repo := newFakeRepository()
	blob, err := repo.WriteBlob([]byte("x"))
	if err != nil {
		t.Fatalf("WriteBlob() = %v", err)
	}
	// A tree entry with an executable mode must be rejected by the policy
	// walk even though libgit2 accepts the mode in a tree.
	evil, err := repo.WriteTree([]TreeEntry{{Name: "run.sh", Mode: 0o100755, ID: blob}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	_, err = ReadSnapshot(repo, evil)
	if err == nil || !strings.Contains(err.Error(), "unsupported file mode") {
		t.Fatalf("ReadSnapshot() error = %v, want unsupported-file-mode error", err)
	}
}

func TestReadSnapshotRejectsMissingBlob(t *testing.T) {
	repo := newFakeRepository()
	var ghost OID
	ghost[0] = 0xab
	tree, err := repo.WriteTree([]TreeEntry{{Name: "ghost", Mode: ModeBlob, ID: ghost}})
	if err != nil {
		t.Fatalf("WriteTree() = %v", err)
	}
	if _, err := ReadSnapshot(repo, tree); err == nil {
		t.Fatal("ReadSnapshot() = nil, want missing-blob error")
	}
}

func TestEmptyTree(t *testing.T) {
	repo := newFakeRepository()
	first, err := EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() = %v", err)
	}
	second, err := EmptyTree(repo)
	if err != nil {
		t.Fatalf("EmptyTree() second = %v", err)
	}
	if first != second {
		t.Fatalf("EmptyTree not stable: %s != %s", first, second)
	}
	entries, err := repo.ReadTree(first)
	if err != nil {
		t.Fatalf("ReadTree(empty) = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty tree has %d entries", len(entries))
	}
	if first.IsZero() {
		t.Fatal("empty tree must have a real OID")
	}
}

func TestBuildTreePropagatesWriteError(t *testing.T) {
	repo := newFakeRepository()
	orig := repo.blobs
	repo.blobs = nil // force WriteBlob failures
	defer func() { repo.blobs = orig }()
	if _, err := BuildTree(repo, fakeSnapshot(map[string]string{"a.txt": "x"})); err == nil {
		t.Fatal("BuildTree() = nil, want WriteBlob error")
	}
	if _, err := BuildTree(repo, Snapshot{}); err != nil {
		t.Fatalf("BuildTree(empty) = %v, want nil", err)
	}
}

var _ = errors.Is // keep errors import for future assertions
