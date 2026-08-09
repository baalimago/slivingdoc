package git

import (
	"crypto/sha256"
	"reflect"
	"testing"
)

// seedHistory builds two commits in the fake repository: C1 with a.txt, and
// C2 (parent C1) that changes a.txt and adds b.txt.
func seedHistory(t *testing.T, repo *fakeRepository) (c1, c2 OID) {
	t.Helper()
	a1, err := repo.WriteBlob([]byte("one"))
	if err != nil {
		t.Fatalf("WriteBlob(a1) = %v", err)
	}
	t1, err := repo.WriteTree([]TreeEntry{{Name: "a.txt", Mode: ModeBlob, ID: a1}})
	if err != nil {
		t.Fatalf("WriteTree(t1) = %v", err)
	}
	c1, err = CreateCommit(repo, CommitSpec{Message: "one", Tree: t1, Time: fakeTime()})
	if err != nil {
		t.Fatalf("CreateCommit(c1) = %v", err)
	}

	a2, err := repo.WriteBlob([]byte("two"))
	if err != nil {
		t.Fatalf("WriteBlob(a2) = %v", err)
	}
	b2, err := repo.WriteBlob([]byte("bee"))
	if err != nil {
		t.Fatalf("WriteBlob(b2) = %v", err)
	}
	t2, err := repo.WriteTree([]TreeEntry{
		{Name: "a.txt", Mode: ModeBlob, ID: a2},
		{Name: "b.txt", Mode: ModeBlob, ID: b2},
	})
	if err != nil {
		t.Fatalf("WriteTree(t2) = %v", err)
	}
	c2, err = CreateCommit(repo, CommitSpec{Message: "two", Tree: t2, Parents: []OID{c1}, Time: fakeTime()})
	if err != nil {
		t.Fatalf("CreateCommit(c2) = %v", err)
	}
	return c1, c2
}

// closureSet computes the object set reachable from a commit for a fake
// repository, mirroring the native closure rules (commit + tree closure,
// optionally ancestors).
func closureSet(repo *fakeRepository, head OID, includeParents bool) map[OID]struct{} {
	set := map[OID]struct{}{}
	seen := map[OID]struct{}{}
	queue := []OID{head}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		commit := repo.commits[id]
		set[id] = struct{}{}
		walk := []OID{commit.Tree}
		for len(walk) > 0 {
			tid := walk[0]
			walk = walk[1:]
			set[tid] = struct{}{}
			for _, e := range repo.trees[tid] {
				if e.Mode == ModeTree {
					walk = append(walk, e.ID)
				} else {
					set[e.ID] = struct{}{}
				}
			}
		}
		if includeParents {
			queue = append(queue, commit.Parents...)
		}
	}
	return set
}

func TestExportIncrementOmitsBaseObjects(t *testing.T) {
	repo := newFakeRepository()
	c1, c2 := seedHistory(t, repo)

	pack, err := ExportIncrement(repo, c2, c1)
	if err != nil {
		t.Fatalf("ExportIncrement() = %v", err)
	}
	want := closureSet(repo, c2, true)
	for id := range closureSet(repo, c1, true) {
		delete(want, id)
	}
	if len(repo.packObjects) != len(want) {
		t.Fatalf("increment pack has %d objects, want %d (%v)", len(repo.packObjects), len(want), want)
	}
	got := make(map[OID]struct{}, len(repo.packObjects))
	for _, oid := range repo.packObjects {
		got[oid] = struct{}{}
		if _, ok := want[oid]; !ok {
			t.Errorf("increment pack contains base object %s", oid)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("increment pack objects = %v, want %v", got, want)
	}
	if pack.ObjectCount != len(want) {
		t.Fatalf("pack ObjectCount = %d, want %d", pack.ObjectCount, len(want))
	}
}

func TestExportIncrementRequiresNewObjects(t *testing.T) {
	repo := newFakeRepository()
	c1, _ := seedHistory(t, repo)
	if _, err := ExportIncrement(repo, c1, c1); err == nil {
		t.Fatal("ExportIncrement(same, same) = nil, want no-new-objects error")
	}
}

func TestExportCheckpointIncludesStateNotHistory(t *testing.T) {
	repo := newFakeRepository()
	c1, c2 := seedHistory(t, repo)

	pack, err := ExportCheckpoint(repo, c2)
	if err != nil {
		t.Fatalf("ExportCheckpoint() = %v", err)
	}
	want := map[OID]struct{}{c2: {}}
	for id := range closureSet(repo, c2, false) {
		want[id] = struct{}{}
	}
	if len(repo.packObjects) != len(want) {
		t.Fatalf("checkpoint pack has %d objects, want %d", len(repo.packObjects), len(want))
	}
	if _, ok := want[c1]; ok {
		t.Fatal("checkpoint pack must omit pre-checkpoint history")
	}
	got := make(map[OID]struct{}, len(repo.packObjects))
	for _, oid := range repo.packObjects {
		got[oid] = struct{}{}
		if _, ok := want[oid]; !ok {
			t.Errorf("checkpoint pack contains unexpected object %s", oid)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checkpoint pack objects = %v, want %v", got, want)
	}
	if pack.ObjectCount != len(want) {
		t.Fatalf("pack ObjectCount = %d, want %d", pack.ObjectCount, len(want))
	}
}

func TestExportCheckpointRequiresExistingCommit(t *testing.T) {
	repo := newFakeRepository()
	var ghost OID
	ghost[0] = 0xcd
	if _, err := ExportCheckpoint(repo, ghost); err == nil {
		t.Fatal("ExportCheckpoint(missing commit) = nil, want error")
	}
}

func TestPackSHA256CoversExportedBytes(t *testing.T) {
	repo := newFakeRepository()
	c1, c2 := seedHistory(t, repo)
	pack, err := ExportIncrement(repo, c2, c1)
	if err != nil {
		t.Fatalf("ExportIncrement() = %v", err)
	}
	sum := sha256.Sum256(pack.Data)
	if pack.SHA256 != sum {
		t.Fatalf("pack SHA256 = %x, want sha256(bytes) = %x", pack.SHA256, sum)
	}
}

func TestImportPackRejectsEmpty(t *testing.T) {
	repo := newFakeRepository()
	if err := ImportPack(repo, nil); err == nil {
		t.Fatal("ImportPack(empty) = nil, want error")
	}
	if err := ImportPack(repo, []byte{}); err == nil {
		t.Fatal("ImportPack(empty slice) = nil, want error")
	}
	if len(repo.imported) != 0 {
		t.Fatal("empty pack must not reach the repository")
	}
}

func TestImportPackRecords(t *testing.T) {
	repo := newFakeRepository()
	if err := ImportPack(repo, []byte("pack bytes")); err != nil {
		t.Fatalf("ImportPack() = %v", err)
	}
	if len(repo.imported) != 1 || string(repo.imported[0]) != "pack bytes" {
		t.Fatalf("ImportPack recorded %q, want the pack bytes", repo.imported)
	}
}

func TestMarkShallowRejectsZero(t *testing.T) {
	repo := newFakeRepository()
	if err := MarkShallow(repo, OID{}); err == nil {
		t.Fatal("MarkShallow(zero) = nil, want error")
	}
	if len(repo.shallow) != 0 {
		t.Fatal("zero OID must not be recorded as shallow")
	}
}

func TestMarkShallowRecords(t *testing.T) {
	repo := newFakeRepository()
	c1, _ := seedHistory(t, repo)
	if err := MarkShallow(repo, c1); err != nil {
		t.Fatalf("MarkShallow() = %v", err)
	}
	if len(repo.shallow) != 1 || repo.shallow[0] != c1 {
		t.Fatalf("MarkShallow recorded %v, want [%s]", repo.shallow, c1)
	}
}

func TestValidateHistoryAllowsDeclaredShallowGap(t *testing.T) {
	repo := newFakeRepository()
	c1, c2 := seedHistory(t, repo)

	if err := ValidateHistory(repo, c2, OID{}); err != nil {
		t.Fatalf("ValidateHistory(complete) = %v", err)
	}

	// The checkpoint pack omits C1: the walk must fail without a declared
	// shallow boundary and pass with one.
	delete(repo.commits, c1)
	if err := ValidateHistory(repo, c2, OID{}); err == nil {
		t.Fatal("ValidateHistory(missing parent, no shallow) = nil, want error")
	}
	if err := ValidateHistory(repo, c2, c2); err != nil {
		t.Fatalf("ValidateHistory(missing parent, shallow declared) = %v", err)
	}
}

func TestValidateHistoryFailsOnMissingBlob(t *testing.T) {
	repo := newFakeRepository()
	c1, _ := seedHistory(t, repo)
	commit := repo.commits[c1]
	for _, e := range repo.trees[commit.Tree] {
		if e.Mode == ModeBlob {
			delete(repo.blobs, e.ID)
		}
	}
	if err := ValidateHistory(repo, c1, OID{}); err == nil {
		t.Fatal("ValidateHistory(missing blob) = nil, want error")
	}
}

func TestValidateHistoryFailsOnUnsupportedMode(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("x"))
	evil, _ := repo.WriteTree([]TreeEntry{{Name: "run.sh", Mode: 0o100755, ID: blob}})
	commit, _ := CreateCommit(repo, CommitSpec{Message: "evil", Tree: evil, Time: fakeTime()})
	if err := ValidateHistory(repo, commit, OID{}); err == nil {
		t.Fatal("ValidateHistory(executable mode) = nil, want error")
	}
}
