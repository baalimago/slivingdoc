package workspace

import (
	"fmt"
	"io"
	"os"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/git/gittest"
)

// fakeEngine is an in-memory Engine for pure workspace tests: it keeps one
// fakeData store per private repository path, so a reopened workspace sees
// the objects it persisted before the close, exactly like objects on disk.
type fakeEngine struct {
	data map[string]*fakeData
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{data: map[string]*fakeData{}}
}

func (e *fakeEngine) CreateRepo(path string) (git.Repository, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	d := &fakeData{
		blobs: map[git.OID][]byte{},
		trees: map[git.OID][]git.TreeEntry{},
	}
	e.data[path] = d
	return &fakeRepository{data: d}, nil
}

func (e *fakeEngine) OpenRepo(path string) (git.Repository, error) {
	d, ok := e.data[path]
	if !ok {
		return nil, fmt.Errorf("fake: repository %q not found", path)
	}
	return &fakeRepository{data: d}, nil
}

var _ Engine = (*fakeEngine)(nil)

// fakeData is the object store shared by every handle of one repository.
type fakeData struct {
	blobs map[git.OID][]byte
	trees map[git.OID][]git.TreeEntry
}

// fakeRepository is the in-memory Repository test double. Only the tree
// and blob operations are implemented: workspace materialization reads
// snapshots from trees and the empty tree is written at open. Every other
// operation reports a not-implemented error, exactly like the interface
// contract it mirrors. Close marks one handle closed; the underlying data
// survives, so a reopened workspace reads the persisted objects.
type fakeRepository struct {
	data   *fakeData
	closed bool
}

func (f *fakeRepository) WriteBlob(data []byte) (git.OID, error) {
	if f.closed {
		return git.OID{}, fmt.Errorf("fake: repository closed")
	}
	id := gittest.ObjectID("blob", data)
	f.data.blobs[id] = append([]byte(nil), data...)
	return id, nil
}

func (f *fakeRepository) ReadBlob(id git.OID) ([]byte, error) {
	data, ok := f.data.blobs[id]
	if !ok {
		return nil, fmt.Errorf("fake: blob %s not found", id)
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeRepository) HasObject(git.OID) (bool, error) {
	return false, fmt.Errorf("fake: HasObject not implemented")
}

func (f *fakeRepository) WriteTree(entries []git.TreeEntry) (git.OID, error) {
	if f.closed {
		return git.OID{}, fmt.Errorf("fake: repository closed")
	}
	sorted := append([]git.TreeEntry(nil), entries...)
	git.SortTreeEntries(sorted)
	id := fakeTreeID(sorted)
	f.data.trees[id] = sorted
	return id, nil
}

func (f *fakeRepository) ReadTree(id git.OID) ([]git.TreeEntry, error) {
	entries, ok := f.data.trees[id]
	if !ok {
		return nil, fmt.Errorf("fake: tree %s not found", id)
	}
	return entries, nil
}

func (f *fakeRepository) CreateCommit(git.CommitSpec) (git.OID, error) {
	return git.OID{}, fmt.Errorf("fake: CreateCommit not implemented")
}

func (f *fakeRepository) ReadCommit(git.OID) (git.Commit, error) {
	return git.Commit{}, fmt.Errorf("fake: ReadCommit not implemented")
}

func (f *fakeRepository) MergeTrees(_, _, _ git.OID) (git.MergeIndex, error) {
	return git.MergeIndex{}, fmt.Errorf("fake: MergeTrees not implemented")
}

func (f *fakeRepository) MergeFile(_, _, _ []byte) (git.MergeFileResult, error) {
	return git.MergeFileResult{}, fmt.Errorf("fake: MergeFile not implemented")
}

func (f *fakeRepository) WritePack([]git.OID, io.Writer) (int, error) {
	return 0, fmt.Errorf("fake: WritePack not implemented")
}

func (f *fakeRepository) ImportPack([]byte) error {
	return fmt.Errorf("fake: ImportPack not implemented")
}

func (f *fakeRepository) MarkShallow(git.OID) error {
	return fmt.Errorf("fake: MarkShallow not implemented")
}

func (f *fakeRepository) Close() error {
	f.closed = true
	return nil
}

var _ git.Repository = (*fakeRepository)(nil)

// fakeTreeID hashes a tree entry list to a deterministic OID using Git
// tree order, mirroring the deterministic fake of internal/git.
func fakeTreeID(entries []git.TreeEntry) git.OID {
	var b []byte
	for _, e := range entries {
		b = fmt.Appendf(b, "%o %s\x00", e.Mode, e.Name)
		b = append(b, e.ID[:]...)
	}
	return gittest.ObjectID("tree", b)
}
