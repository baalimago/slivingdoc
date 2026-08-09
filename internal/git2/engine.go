package git2

import (
	"fmt"
	"io"
	"sync"

	"github.com/baalimago/slivingdoc/internal/git"
)

// PinnedVersion is the exact libgit2 release this boundary is compiled and
// tested against. Open refuses a runtime release that differs from it.
const PinnedVersion = "1.9.6"

// New returns the native libgit2-backed engine. Open must succeed before any
// other call; the caller owns Close.
func New() git.Engine { return &engine{} }

type engine struct {
	mu     sync.Mutex
	opened bool
}

func (e *engine) Open() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opened {
		return &git.NativeError{Op: "open", Message: "engine is already open"}
	}
	if rc := initFn(); rc < 0 {
		return nativeError("open libgit2")
	}
	maj, min, rev := versionFn()
	runtime := fmt.Sprintf("%d.%d.%d", maj, min, rev)
	if runtime != PinnedVersion {
		shutdownFn() // keep the init/shutdown reference count balanced
		return &git.VersionMismatchError{Pinned: PinnedVersion, Runtime: runtime}
	}
	e.opened = true
	return nil
}

func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.opened {
		return &git.NativeError{Op: "close", Message: "engine is not open"}
	}
	e.opened = false
	if rc := shutdownFn(); rc < 0 {
		return nativeError("close libgit2")
	}
	return nil
}

func (e *engine) Version() (string, error) {
	if err := e.requireOpen(); err != nil {
		return "", err
	}
	maj, min, rev := versionFn()
	return fmt.Sprintf("%d.%d.%d", maj, min, rev), nil
}

func (e *engine) Features() (git.Features, error) {
	if err := e.requireOpen(); err != nil {
		return git.Features{}, err
	}
	return git.FeaturesFromMask(uint32(featuresFn())), nil
}

func (e *engine) CreateRepo(path string) (git.Repository, error) {
	if err := e.requireOpen(); err != nil {
		return nil, err
	}
	handle, err := createRepoFn(path, false)
	if err != nil {
		return nil, err
	}
	return newRepository(e, handle)
}

func (e *engine) OpenRepo(path string) (git.Repository, error) {
	if err := e.requireOpen(); err != nil {
		return nil, err
	}
	handle, err := openRepoFn(path)
	if err != nil {
		return nil, err
	}
	return newRepository(e, handle)
}

func (e *engine) requireOpen() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.opened {
		return &git.NativeError{Op: "engine", Message: "engine is not open"}
	}
	return nil
}

// newRepository wraps a native repository handle and its object database.
// The repository keeps the engine reference so operations fail
// deterministically once the engine is closed.
func newRepository(e *engine, handle *repoHandle) (*repository, error) {
	odb, err := repoODBFn(handle)
	if err != nil {
		handle.free()
		return nil, err
	}
	return &repository{engine: e, handle: handle, odb: odb}, nil
}

type repository struct {
	engine *engine
	mu     sync.Mutex
	handle *repoHandle
	odb    *odbHandle
	closed bool
}

func (r *repository) WriteBlob(data []byte) (git.OID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.OID{}, err
	}
	return odbWriteFn(r.odb, data)
}

func (r *repository) ReadBlob(id git.OID) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return nil, err
	}
	return odbReadFn(r.odb, id)
}

func (r *repository) WriteTree(entries []git.TreeEntry) (git.OID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.OID{}, err
	}
	return writeTreeFn(r.handle, entries)
}

func (r *repository) ReadTree(id git.OID) ([]git.TreeEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return nil, err
	}
	return readTreeFn(r.handle, id)
}

func (r *repository) CreateCommit(spec git.CommitSpec) (git.OID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.OID{}, err
	}
	return createCommitFn(r.handle, spec)
}

func (r *repository) ReadCommit(id git.OID) (git.Commit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.Commit{}, err
	}
	return readCommitFn(r.handle, id)
}

func (r *repository) MergeTrees(base, local, remote git.OID) (git.MergeIndex, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.MergeIndex{}, err
	}
	return mergeTreesFn(r.handle, base, local, remote)
}

func (r *repository) MergeFile(base, local, remote []byte) (git.MergeFileResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return git.MergeFileResult{}, err
	}
	return mergeFileFn(base, local, remote)
}

func (r *repository) WritePack(objects []git.OID, w io.Writer) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return 0, err
	}
	return writePackFn(r.handle, objects, w)
}

func (r *repository) ImportPack(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return err
	}
	return importPackFn(r.odb, data)
}

func (r *repository) MarkShallow(oid git.OID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.usable(); err != nil {
		return err
	}
	return markShallowFn(r.handle, oid)
}

func (r *repository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.odb.free()
	r.handle.free()
	return nil
}

func (r *repository) usable() error {
	if r.closed {
		return &git.NativeError{Op: "repository", Message: "repository is closed"}
	}
	return r.engine.requireOpen()
}
