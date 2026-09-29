package git

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestMergeConflictFree(t *testing.T) {
	repo := newFakeRepository()
	blobA, _ := repo.WriteBlob([]byte("a"))
	blobB, _ := repo.WriteBlob([]byte("b"))
	mergedTree, _ := repo.WriteTree([]TreeEntry{
		{Name: "a.txt", Mode: ModeBlob, ID: blobA},
		{Name: "b.txt", Mode: ModeBlob, ID: blobB},
	})
	repo.mergeIndex = MergeIndex{
		Tree: mergedTree,
		Entries: []IndexEntry{
			{Path: "a.txt", Mode: ModeBlob, ID: blobA, Stage: 0},
			{Path: "b.txt", Mode: ModeBlob, ID: blobB, Stage: 0},
		},
	}

	res, err := Merge(repo, OID{}, OID{}, OID{})
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if res.Tree != mergedTree {
		t.Fatalf("Merge() tree = %s, want %s", res.Tree, mergedTree)
	}
	if len(res.Conflicts) != 0 {
		t.Fatalf("Merge() conflicts = %+v, want none", res.Conflicts)
	}
}

func TestMergeStructuresTextConflict(t *testing.T) {
	repo := newFakeRepository()
	base, _ := repo.WriteBlob([]byte("one\ntwo\nthree\n"))
	local, _ := repo.WriteBlob([]byte("ONE\ntwo\nthree\n"))
	remote, _ := repo.WriteBlob([]byte("one\nTWO\nthree\n"))
	repo.mergeIndex = MergeIndex{Entries: []IndexEntry{
		{Path: "notes/a.md", Mode: ModeBlob, ID: base, Stage: 1},
		{Path: "notes/a.md", Mode: ModeBlob, ID: local, Stage: 2},
		{Path: "notes/a.md", Mode: ModeBlob, ID: remote, Stage: 3},
		{Path: "clean.txt", Mode: ModeBlob, ID: base, Stage: 0},
	}}
	markers := []byte("<<<<<<< local\nONE\ntwo\nthree\n=======\none\nTWO\nthree\n>>>>>>> remote\n")
	repo.mergeFile = MergeFileResult{Content: markers, Automergeable: false}

	res, err := Merge(repo, OID{}, OID{}, OID{})
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if !res.Tree.IsZero() {
		t.Fatalf("conflicted Merge() tree = %s, want zero", res.Tree)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Merge() conflicts = %+v, want one", res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Path != "notes/a.md" {
		t.Fatalf("conflict path = %q, want notes/a.md", c.Path)
	}
	if !reflect.DeepEqual(c.Content, markers) {
		t.Fatalf("conflict content = %q, want marker text", c.Content)
	}
	if want := FindConflictBlocks(markers); !reflect.DeepEqual(c.Ranges, want) {
		t.Fatalf("conflict ranges = %+v, want %+v", c.Ranges, want)
	}
}

func TestMergeFileDirectoryConflictHasNoMarkers(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("file"))
	// libgit2 represents a file-versus-directory replacement as a lone
	// blob stage at the path plus resolved entries below it (the other
	// side's directory content).
	repo.mergeIndex = MergeIndex{Entries: []IndexEntry{
		{Path: "p", Mode: ModeBlob, ID: blob, Stage: 2},
		{Path: "p/q.txt", Mode: ModeBlob, ID: blob, Stage: 0},
	}}
	local, _ := repo.WriteTree([]TreeEntry{{Name: "p", Mode: ModeBlob, ID: blob}})

	res, err := Merge(repo, OID{}, local, OID{})
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("Merge() conflicts = %+v, want one", res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Path != "p" {
		t.Fatalf("conflict path = %q, want p", c.Path)
	}
	if c.Content != nil || len(c.Ranges) != 0 {
		t.Fatalf("file/directory conflict must carry no markers, got content=%q ranges=%+v", c.Content, c.Ranges)
	}
}

func TestMergeRejectsUnsupportedIndexMode(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("x"))
	repo.mergeIndex = MergeIndex{Entries: []IndexEntry{
		{Path: "run.sh", Mode: 0o100755, ID: blob, Stage: 0},
	}}
	if _, err := Merge(repo, OID{}, OID{}, OID{}); err == nil {
		t.Fatal("Merge() = nil, want unsupported-mode error")
	}
}

func TestMergeConflictFreeWithoutTreeFails(t *testing.T) {
	repo := newFakeRepository()
	repo.mergeIndex = MergeIndex{Entries: []IndexEntry{
		{Path: "a", Mode: ModeBlob, Stage: 0},
	}}
	if _, err := Merge(repo, OID{}, OID{}, OID{}); err == nil {
		t.Fatal("Merge() = nil, want missing-tree error")
	}
}

func TestMergePropagatesIndexError(t *testing.T) {
	repo := newFakeRepository()
	repo.mergeErr = errors.New("merge index exploded")
	if _, err := Merge(repo, OID{}, OID{}, OID{}); err == nil {
		t.Fatal("Merge() = nil, want index error")
	}
}

func TestMergeResultCarriesIndex(t *testing.T) {
	repo := newFakeRepository()
	blobA, _ := repo.WriteBlob([]byte("a"))
	mergedTree, _ := repo.WriteTree([]TreeEntry{{Name: "a.txt", Mode: ModeBlob, ID: blobA}})
	idx := MergeIndex{
		Tree: mergedTree,
		Entries: []IndexEntry{
			{Path: "a.txt", Mode: ModeBlob, ID: blobA, Stage: 0},
		},
	}
	repo.mergeIndex = idx
	res, err := Merge(repo, OID{}, OID{}, OID{})
	if err != nil {
		t.Fatalf("Merge() = %v", err)
	}
	if !reflect.DeepEqual(res.Index, idx) {
		t.Fatalf("Merge().Index = %+v, want the raw index %+v", res.Index, idx)
	}
}

func TestMaterializeTreeConflictFreeReadsMergedTree(t *testing.T) {
	repo := newFakeRepository()
	blobA, _ := repo.WriteBlob([]byte("a"))
	blobB, _ := repo.WriteBlob([]byte("b"))
	mergedTree, _ := repo.WriteTree([]TreeEntry{
		{Name: "a.txt", Mode: ModeBlob, ID: blobA},
		{Name: "sub/b.txt", Mode: ModeBlob, ID: blobB},
	})
	res := MergeResult{Tree: mergedTree}
	snap, err := MaterializeTree(repo, res, OID{})
	if err != nil {
		t.Fatalf("MaterializeTree() = %v", err)
	}
	if len(snap.Files) != 2 || snap.Files[0].Path != "a.txt" || snap.Files[1].Path != "sub/b.txt" {
		t.Fatalf("MaterializeTree() = %+v, want both merged files", snap.Files)
	}
}

func TestMaterializeTreeTextConflictKeepsMarkersAndResolved(t *testing.T) {
	repo := newFakeRepository()
	base, _ := repo.WriteBlob([]byte("one\ntwo\n"))
	local, _ := repo.WriteBlob([]byte("ONE\ntwo\n"))
	remote, _ := repo.WriteBlob([]byte("one\nTWO\n"))
	clean, _ := repo.WriteBlob([]byte("clean"))
	markers := []byte("<<<<<<< local\nONE\ntwo\n=======\none\nTWO\n>>>>>>> remote\n")
	res := MergeResult{Conflicts: []Conflict{{
		Path: "notes/a.md", Content: markers, Ranges: []MarkerRange{{1, 7}},
	}}, Index: MergeIndex{Entries: []IndexEntry{
		{Path: "notes/a.md", Mode: ModeBlob, ID: base, Stage: 1},
		{Path: "notes/a.md", Mode: ModeBlob, ID: local, Stage: 2},
		{Path: "notes/a.md", Mode: ModeBlob, ID: remote, Stage: 3},
		{Path: "clean.txt", Mode: ModeBlob, ID: clean, Stage: 0},
	}}}
	snap, err := MaterializeTree(repo, res, OID{})
	if err != nil {
		t.Fatalf("MaterializeTree() = %v", err)
	}
	if len(snap.Files) != 2 {
		t.Fatalf("MaterializeTree() = %+v, want conflicted and resolved files", snap.Files)
	}
	if snap.Files[0].Path != "clean.txt" || string(snap.Files[0].Data) != "clean" {
		t.Fatalf("resolved file = %+v", snap.Files[0])
	}
	if snap.Files[1].Path != "notes/a.md" || !bytes.Equal(snap.Files[1].Data, markers) {
		t.Fatalf("conflicted file = %+v, want exact marker content", snap.Files[1])
	}
}

// TestMaterializeTreeFileDirectoryKeepsLocalSide proves a file/directory
// conflict keeps the local side in both directions: the local file when
// the remote replaced it with a directory, and the whole local subtree when
// the local side replaced the file. The index has libgit2's shape: the file
// side's stages at p and the directory side's one-sided entries resolved to
// stage 0 below it, so only the local tree tells the sides apart. A text
// conflict below p keeps its markers when p is a local directory, and is
// neither reported nor materialized when p is a local file: its markers
// cannot sit beside the file.
func TestMaterializeTreeFileDirectoryKeepsLocalSide(t *testing.T) {
	tests := []struct {
		name      string
		local     map[string]string
		entries   func(blob func(string) OID) []IndexEntry
		conflicts []string
		want      map[string]string
	}{
		{
			name:  "local file remote directory",
			local: map[string]string{"p": "local file", "keep.txt": "kept"},
			entries: func(blob func(string) OID) []IndexEntry {
				return []IndexEntry{
					{Path: "keep.txt", Mode: ModeBlob, ID: blob("kept"), Stage: 0},
					{Path: "p", Mode: ModeBlob, ID: blob("local file"), Stage: 2},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("remote q"), Stage: 0},
				}
			},
			conflicts: []string{"p"},
			want:      map[string]string{"keep.txt": "kept", "p": "local file"},
		},
		{
			name:  "local directory remote file",
			local: map[string]string{"p/q.txt": "local q", "p/sub/r.txt": "local r", "z.txt": "z"},
			entries: func(blob func(string) OID) []IndexEntry {
				return []IndexEntry{
					{Path: "p", Mode: ModeBlob, ID: blob("remote file"), Stage: 3},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("local q"), Stage: 0},
					{Path: "p/sub/r.txt", Mode: ModeBlob, ID: blob("local r"), Stage: 0},
					{Path: "z.txt", Mode: ModeBlob, ID: blob("z"), Stage: 0},
				}
			},
			conflicts: []string{"p"},
			want:      map[string]string{"p/q.txt": "local q", "p/sub/r.txt": "local r", "z.txt": "z"},
		},
		{
			// Local edited p/q.txt; remote deleted the directory and added
			// the file p: the edit conflicts with the deletion.
			name:  "text conflict below a local directory",
			local: map[string]string{"p/q.txt": "mine\n", "p/s.txt": "same"},
			entries: func(blob func(string) OID) []IndexEntry {
				return []IndexEntry{
					{Path: "p", Mode: ModeBlob, ID: blob("remote file"), Stage: 3},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("base\n"), Stage: 1},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("mine\n"), Stage: 2},
					{Path: "p/s.txt", Mode: ModeBlob, ID: blob("same"), Stage: 0},
				}
			},
			conflicts: []string{"p", "p/q.txt"},
			want: map[string]string{
				"p/q.txt": "<<<<<<< local\nmine\n=======\n\n>>>>>>> remote\n",
				"p/s.txt": "same",
			},
		},
		{
			// Local deleted the directory and added the file p; remote
			// edited p/q.txt: the edit conflicts with the local deletion.
			name:  "text conflict below a local file",
			local: map[string]string{"p": "local file"},
			entries: func(blob func(string) OID) []IndexEntry {
				return []IndexEntry{
					{Path: "p", Mode: ModeBlob, ID: blob("local file"), Stage: 2},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("base\n"), Stage: 1},
					{Path: "p/q.txt", Mode: ModeBlob, ID: blob("theirs\n"), Stage: 3},
				}
			},
			conflicts: []string{"p"},
			want:      map[string]string{"p": "local file"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			blob := func(data string) OID {
				id, err := repo.WriteBlob([]byte(data))
				if err != nil {
					t.Fatalf("WriteBlob() = %v", err)
				}
				return id
			}
			var tree []TreeEntry
			for path, data := range tt.local {
				tree = append(tree, TreeEntry{Name: path, Mode: ModeBlob, ID: blob(data)})
			}
			local, err := repo.WriteTree(tree)
			if err != nil {
				t.Fatalf("WriteTree() = %v", err)
			}
			repo.mergeIndex = MergeIndex{Entries: tt.entries(blob)}
			res, err := Merge(repo, OID{}, local, OID{})
			if err != nil {
				t.Fatalf("Merge() = %v", err)
			}
			var paths []string
			for _, c := range res.Conflicts {
				paths = append(paths, c.Path)
			}
			if !reflect.DeepEqual(paths, tt.conflicts) {
				t.Fatalf("Merge() conflicts = %v, want %v", paths, tt.conflicts)
			}
			snap, err := MaterializeTree(repo, res, local)
			if err != nil {
				t.Fatalf("MaterializeTree() = %v", err)
			}
			got := make(map[string]string, len(snap.Files))
			for i, f := range snap.Files {
				if i > 0 && snap.Files[i-1].Path >= f.Path {
					t.Fatalf("MaterializeTree() files out of order: %q before %q", snap.Files[i-1].Path, f.Path)
				}
				got[f.Path] = string(f.Data)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("MaterializeTree() = %v, want %v", got, tt.want)
			}
			if _, err := BuildTree(repo, snap); err != nil {
				t.Fatalf("BuildTree(materialized) = %v", err)
			}
		})
	}
}

func TestMergeFileDirectoryLocalReadFails(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("remote file"))
	repo.mergeIndex = MergeIndex{Entries: []IndexEntry{
		{Path: "p", Mode: ModeBlob, ID: blob, Stage: 3},
		{Path: "p/q.txt", Mode: ModeBlob, ID: blob, Stage: 0},
	}}
	if _, err := Merge(repo, OID{}, OID{1}, OID{}); err == nil {
		t.Fatal("Merge() = nil, want the local-tree read error")
	}
}

func TestMaterializeTreeFileDirectoryLocalReadFails(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("remote file"))
	res := MergeResult{Conflicts: []Conflict{{Path: "p"}}, Index: MergeIndex{Entries: []IndexEntry{
		{Path: "p", Mode: ModeBlob, ID: blob, Stage: 3},
	}}}
	if _, err := MaterializeTree(repo, res, OID{1}); err == nil {
		t.Fatal("MaterializeTree() = nil, want the local-tree read error")
	}
}

func TestMaterializeTreeRejectsUnexpectedIndexShape(t *testing.T) {
	repo := newFakeRepository()
	blob, _ := repo.WriteBlob([]byte("x"))
	// An index entry that no conflict describes is an inconsistent result:
	// the policy cannot decide what the caller must see.
	res := MergeResult{Conflicts: []Conflict{{Path: "other"}}, Index: MergeIndex{Entries: []IndexEntry{
		{Path: "p", Mode: ModeBlob, ID: blob, Stage: 2},
		{Path: "p", Mode: ModeBlob, ID: blob, Stage: 3},
	}}}
	if _, err := MaterializeTree(repo, res, OID{}); err == nil {
		t.Fatal("MaterializeTree() = nil, want index-shape error")
	}
}

func TestFindConflictBlocks(t *testing.T) {
	block := "<<<<<<< local\nours\n=======\ntheirs\n>>>>>>> remote\n"
	cases := []struct {
		name string
		data string
		want []MarkerRange
	}{
		{"single block", block, []MarkerRange{{1, 5}}},
		{"two blocks", block + "clean\n" + block, []MarkerRange{{1, 5}, {7, 11}}},
		{"no markers", "plain text\n", nil},
		{"opener only", "<<<<<<< local\n", nil},
		{"missing separator", "<<<<<<< local\ntext\n>>>>>>> remote\n", nil},
		{"missing closer", "<<<<<<< local\n=======\ntext\n", nil},
		{"nested opener is content", "<<<<<<< local\n<<<<<<< local\n=======\ntext\n>>>>>>> remote\n", []MarkerRange{{1, 5}}},
		{"label change is ordinary text", "<<<<<<< mine\n=======\n>>>>>>> remote\n", nil},
		{"crlf line endings", "<<<<<<< local\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> remote\r\n", []MarkerRange{{1, 5}}},
		{"block at end without newline", "<<<<<<< local\n=======\n>>>>>>> remote", []MarkerRange{{1, 3}}},
	}
	for _, c := range cases {
		if got := FindConflictBlocks([]byte(c.data)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("FindConflictBlocks(%s) = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestFindConflictBlocksRowsAreOneBased(t *testing.T) {
	data := "intro\n<<<<<<< local\na\n=======\nb\n>>>>>>> remote\noutro\n"
	if got := FindConflictBlocks([]byte(data)); !reflect.DeepEqual(got, []MarkerRange{{2, 6}}) {
		t.Fatalf("FindConflictBlocks() = %+v, want rows 2..6", got)
	}
}
