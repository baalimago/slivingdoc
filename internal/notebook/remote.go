package notebook

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// remoteState is the reconstructed authoritative remote state: the
// validated manifest, its ETag, and the accepted head tree. present is
// false only for the implicit empty-notebook state at generation 0, whose
// tree is the canonical empty tree.
type remoteState struct {
	manifest   storage.Manifest
	etag       storage.ETag
	present    bool
	generation uint64
	head       git.OID
	tree       git.OID
}

// baseline converts the remote state into the accepted baseline P records.
func (r remoteState) baseline() workspace.Baseline {
	return workspace.Baseline{RemoteGeneration: r.generation, Head: r.head, Tree: r.tree}
}

// emptyRemote is the implicit generation-0 state: no current object, the
// canonical empty tree, and no head.
func emptyRemote() remoteState {
	return remoteState{present: false, tree: workspace.EmptyTreeID}
}

// readRemote reads and validates current, downloads only the packs absent
// from the local byte cache, imports the complete descriptor chain into the
// private repository, and validates the accepted history and text before
// returning. A stale observation whose referenced pack disappeared is
// discarded and current is re-read; an unchanged manifest that still
// references the missing pack is a storage-integrity error (architecture
// section 10). The reader never guesses state from object names.
func (n *Notebook) readRemote(ctx context.Context) (remoteState, error) {
	for restart := 0; ; restart++ {
		data, etag, present, err := n.readCurrent(ctx)
		if err != nil {
			return remoteState{}, err
		}
		if !present {
			return emptyRemote(), nil
		}
		m, err := storage.DecodeManifest(data)
		if err != nil {
			return remoteState{}, storageIntegrity(err, "current is not a valid manifest")
		}

		if err := n.importRemote(ctx, m); err != nil {
			if !errors.Is(err, errStaleManifest) {
				return remoteState{}, err
			}
			if restart >= n.retryLimit {
				return remoteState{}, storageIntegrity(nil, "manifest did not stabilize after %d stale reads", restart)
			}
			// The referenced pack disappeared during cleanup: re-read
			// current and restart only when the manifest actually moved.
			_, newETag, newPresent, rerr := n.readCurrent(ctx)
			if rerr != nil {
				return remoteState{}, rerr
			}
			if !newPresent || newETag == etag {
				return remoteState{}, storageIntegrity(nil, "manifest references a pack that is missing and unchanged after re-read")
			}
			continue
		}

		st := remoteState{manifest: m, etag: etag, present: true, generation: m.Generation, head: m.Head}
		if err := git.ValidateHistory(n.ws.Repo(), m.Head, m.Checkpoint.Head); err != nil {
			return remoteState{}, storageIntegrity(err, "accepted state is incomplete")
		}
		commit, err := n.ws.Repo().ReadCommit(m.Head)
		if err != nil {
			return remoteState{}, storageIntegrity(err, "accepted state %s is unreadable", m.Head)
		}
		if _, err := git.ReadSnapshot(n.ws.Repo(), commit.Tree); err != nil {
			return remoteState{}, storageIntegrity(err, "accepted state is not valid notebook text")
		}
		n.recordTail(m)
		st.tree = commit.Tree
		return st, nil
	}
}

// readCurrent reads the authoritative manifest object. ErrNotFound maps to
// the implicit empty-notebook state, not an error.
func (n *Notebook) readCurrent(ctx context.Context) (data []byte, etag storage.ETag, present bool, err error) {
	rc, info, err := n.store.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, "", false, nil
		}
		return nil, "", false, storageFailure(err, "read current manifest")
	}
	defer rc.Close()
	data, err = io.ReadAll(rc)
	if err != nil {
		return nil, "", false, storageFailure(err, "read current manifest body")
	}
	return data, info.ETag, true, nil
}

func (n *Notebook) importRemote(ctx context.Context, m storage.Manifest) error {
	data, err := n.ensurePack(ctx, packSpec{
		key:  m.Checkpoint.Key.String(),
		sha:  m.Checkpoint.SHA256,
		size: m.Checkpoint.Size,
	})
	if err != nil {
		return err
	}
	if err := git.ImportPack(n.ws.Repo(), data); err != nil {
		return storageIntegrity(err, "import checkpoint pack %s", m.Checkpoint.Key)
	}
	if err := git.MarkShallow(n.ws.Repo(), m.Checkpoint.Head); err != nil {
		return storageIntegrity(err, "record checkpoint boundary %s", m.Checkpoint.Head)
	}
	for _, inc := range m.Increments {
		data, err := n.ensurePack(ctx, packSpec{key: inc.Key.String(), sha: inc.SHA256, size: inc.Size})
		if err != nil {
			return err
		}
		if err := git.ImportPack(n.ws.Repo(), data); err != nil {
			return storageIntegrity(err, "import increment pack %s", inc.Key)
		}
	}
	return nil
}

// packSpec identifies one pack descriptor: the protocol key and the
// authoritative SHA-256 and size from the manifest.
type packSpec struct {
	key  string
	sha  storage.SHA256
	size uint64
}

// ensurePack returns the exact pack bytes for a descriptor. A cache hit
// requires the expected byte size and a fresh SHA-256 check; a corrupt
// cache entry is discarded and re-downloaded, never a false hit. Downloads
// verify the descriptor checksum and size before caching and importing.
func (n *Notebook) ensurePack(ctx context.Context, spec packSpec) ([]byte, error) {
	if data, ok := n.cacheRead(spec); ok {
		return data, nil
	}
	rc, _, err := n.store.ReadObject(ctx, spec.key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// A referenced pack disappeared: the manifest observation is
			// stale, not the pack. The caller re-reads current.
			return nil, fmt.Errorf("notebook: pack %s: %w", spec.key, errStaleManifest)
		}
		return nil, storageFailure(err, "download pack %s", spec.key)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, storageFailure(err, "download pack %s", spec.key)
	}
	if uint64(len(data)) != spec.size || sha256.Sum256(data) != spec.sha {
		return nil, storageIntegrity(nil, "pack %s does not match its descriptor checksum and size", spec.key)
	}
	if err := n.cacheWrite(spec.sha, data); err != nil {
		return nil, &Error{Code: CodeStorageFailure, Message: "write pack cache", Cause: err}
	}
	return data, nil
}

// cacheRead returns the cached pack bytes only when their size and fresh
// SHA-256 match the descriptor. A mismatch discards the entry.
func (n *Notebook) cacheRead(spec packSpec) ([]byte, bool) {
	path := filepath.Join(n.ws.CacheDir(), spec.sha.String())
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if uint64(len(data)) != spec.size || sha256.Sum256(data) != spec.sha {
		_ = os.Remove(path) // corrupt cache: never a false hit
		return nil, false
	}
	return data, true
}

// cacheWrite stores verified pack bytes under their SHA-256 through a
// temporary file and atomic rename.
func (n *Notebook) cacheWrite(sha storage.SHA256, data []byte) error {
	dir := n.ws.CacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create pack cache: %w", err)
	}
	path := filepath.Join(dir, sha.String())
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create cache temporary: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write cache temporary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync cache temporary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close cache temporary: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("place cache entry: %w", err)
	}
	return nil
}

// lookupPublication searches the active and retained checkpoint and
// increment descriptors of the authoritative manifest for a publication ID.
// The notebook returns success only when the ID is present (architecture
// section 11.3); an ID that no descriptor records cannot be proved accepted.
func (n *Notebook) lookupPublication(ctx context.Context, id storage.UUID) (bool, error) {
	data, _, present, err := n.readCurrent(ctx)
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return false, storageIntegrity(err, "current is not a valid manifest")
	}
	if m.Checkpoint.Publication == id {
		return true, nil
	}
	for _, inc := range m.Increments {
		if inc.Publication == id {
			return true, nil
		}
	}
	for _, ret := range m.Retained {
		if ret.Checkpoint.Publication == id {
			return true, nil
		}
		for _, inc := range ret.Increments {
			if inc.Publication == id {
				return true, nil
			}
		}
	}
	return false, nil
}

// recoverState is the generic recovery path (architecture section 15): it
// rereads authoritative current, imports the complete descriptor chain,
// reconstructs the head tree, and applies it to L and P through the
// workspace. The report states the failed stage, whether remote acceptance
// is known, and whether resynchronization succeeded. A failed repair leaves
// P durably marked as requiring recovery, so the next call retries entry
// recovery.
func (n *Notebook) recoverState(ctx context.Context, stage string, accepted RemoteAccepted) (recoveryReport, error) {
	report := recoveryReport{stage: stage, remoteAccepted: accepted}
	remote, err := n.readRemote(ctx)
	if err != nil {
		return report, err
	}
	if err := n.ws.Recover(ctx, remote.baseline()); err != nil {
		return report, fmt.Errorf("notebook: recovery apply: %w", err)
	}
	report.resynchronized = true
	return report, nil
}

// recoveryReport is the workspace-visible outcome of one recovery run.
type recoveryReport struct {
	stage          string
	remoteAccepted RemoteAccepted
	resynchronized bool
}

// public converts the internal report into the stable error shape.
func (r recoveryReport) public() RecoveryReport {
	return RecoveryReport{Stage: r.stage, RemoteAccepted: r.remoteAccepted, Resynchronized: r.resynchronized}
}
