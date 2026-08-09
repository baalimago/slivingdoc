package git

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"sort"
	"time"
)

// fakeRepository is an in-memory Repository for policy tests. Blobs, trees,
// and commits hash deterministically so tests can assert exact object sets.
// The merge operations are scripted: the test sets the expected MergeIndex
// and MergeFileResult, and the fake replays them. The real merge semantics
// are covered by the libgit2 component tests in internal/git2; this fake
// exists only to prove the policy structuring logic.
type fakeRepository struct {
	blobs   map[OID][]byte
	trees   map[OID][]TreeEntry
	commits map[OID]Commit

	mergeIndex  MergeIndex
	mergeFile   MergeFileResult
	mergeErr    error
	fileErr     error
	packObjects []OID
	imported    [][]byte
	shallow     []OID
	closed      bool
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		blobs:   map[OID][]byte{},
		trees:   map[OID][]TreeEntry{},
		commits: map[OID]Commit{},
	}
}

func (f *fakeRepository) WriteBlob(data []byte) (OID, error) {
	if f.blobs == nil {
		return OID{}, fmt.Errorf("fake: blob store unavailable")
	}
	oid := fakeBlobID(data)
	f.blobs[oid] = append([]byte{}, data...)
	return oid, nil
}

func (f *fakeRepository) ReadBlob(id OID) ([]byte, error) {
	data, ok := f.blobs[id]
	if !ok {
		return nil, fmt.Errorf("fake: blob %s not found", id)
	}
	return append([]byte{}, data...), nil
}

func (f *fakeRepository) WriteTree(entries []TreeEntry) (OID, error) {
	sorted := append([]TreeEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return treeEntryLess(sorted[i], sorted[j]) })
	oid := fakeTreeID(sorted)
	f.trees[oid] = sorted
	return oid, nil
}

func (f *fakeRepository) ReadTree(id OID) ([]TreeEntry, error) {
	entries, ok := f.trees[id]
	if !ok {
		return nil, fmt.Errorf("fake: tree %s not found", id)
	}
	return entries, nil
}

func (f *fakeRepository) CreateCommit(spec CommitSpec) (OID, error) {
	oid := fakeCommitID(spec)
	f.commits[oid] = Commit{Tree: spec.Tree, Parents: append([]OID(nil), spec.Parents...), Message: spec.Message}
	return oid, nil
}

func (f *fakeRepository) ReadCommit(id OID) (Commit, error) {
	commit, ok := f.commits[id]
	if !ok {
		return Commit{}, fmt.Errorf("fake: commit %s not found", id)
	}
	return commit, nil
}

func (f *fakeRepository) MergeTrees(base, local, remote OID) (MergeIndex, error) {
	if f.mergeErr != nil {
		return MergeIndex{}, f.mergeErr
	}
	idx := f.mergeIndex
	idx.Entries = append([]IndexEntry(nil), f.mergeIndex.Entries...)
	return idx, nil
}

func (f *fakeRepository) MergeFile(base, local, remote []byte) (MergeFileResult, error) {
	if f.fileErr != nil {
		return MergeFileResult{}, f.fileErr
	}
	return f.mergeFile, nil
}

func (f *fakeRepository) WritePack(objects []OID, w io.Writer) (int, error) {
	f.packObjects = append([]OID(nil), objects...)
	for _, oid := range objects {
		if _, err := w.Write([]byte(oid.String())); err != nil {
			return 0, err
		}
	}
	return len(objects), nil
}

func (f *fakeRepository) ImportPack(data []byte) error {
	f.imported = append(f.imported, append([]byte(nil), data...))
	return nil
}

func (f *fakeRepository) MarkShallow(oid OID) error {
	f.shallow = append(f.shallow, oid)
	return nil
}

func (f *fakeRepository) Close() error {
	f.closed = true
	return nil
}

// fakeBlobID hashes blob content to a deterministic OID.
func fakeBlobID(data []byte) OID {
	return fakeObjectID("blob", data)
}

func fakeTreeID(entries []TreeEntry) OID {
	var b bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&b, "%o %s\x00", e.Mode, e.Name)
		b.Write(e.ID[:])
	}
	return fakeObjectID("tree", b.Bytes())
}

func fakeCommitID(spec CommitSpec) OID {
	var b bytes.Buffer
	fmt.Fprintf(&b, "tree %s\n", spec.Tree)
	for _, p := range spec.Parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	fmt.Fprintf(&b, "author %s <%s> %d +0000\n", AuthorName, AuthorEmail, spec.Time.Unix())
	fmt.Fprintf(&b, "committer %s <%s> %d +0000\n\n", AuthorName, AuthorEmail, spec.Time.Unix())
	b.WriteString(spec.Message)
	return fakeObjectID("commit", b.Bytes())
}

func fakeObjectID(kind string, data []byte) OID {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(data))
	h.Write(data)
	var oid OID
	copy(oid[:], h.Sum(nil))
	return oid
}

// fakeSnapshot builds a snapshot from a path->content map.
func fakeSnapshot(files map[string]string) Snapshot {
	snap := Snapshot{}
	for path, content := range files {
		snap.Files = append(snap.Files, File{Path: path, Data: []byte(content)})
	}
	sort.Slice(snap.Files, func(i, j int) bool { return snap.Files[i].Path < snap.Files[j].Path })
	return snap
}

var _ Repository = (*fakeRepository)(nil)

// fakeTime is the injected attempt clock used in policy tests.
func fakeTime() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) }
