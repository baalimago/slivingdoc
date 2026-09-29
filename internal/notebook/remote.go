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

// acceptedState is the remote state of a validated manifest head.
func acceptedState(m storage.Manifest, etag storage.ETag, tree git.OID) remoteState {
	return remoteState{manifest: m, etag: etag, present: true, generation: m.Generation, head: m.Head, tree: tree}
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

// importMode selects which descriptor packs importRemote brings into the
// private repository.
type importMode uint8

const (
	// importMissing imports only the packs whose head commit the repository
	// lacks: a pack import is atomic, so a present head proves its pack
	// landed whole (architecture/pull.md).
	importMissing importMode = iota
	// importAll re-imports every pack from its verified bytes: the repair
	// for a repository whose objects no longer validate.
	importAll
)

// loadMode selects how far readRemote trusts the private repository.
type loadMode uint8

const (
	// loadReusing serves a head this workspace already accepted after a
	// presence check (reuseAccepted); every other head is imported and
	// validated.
	loadReusing loadMode = iota
	// loadStrict always imports the missing packs and validates the whole
	// accepted history: the recovery path, and the retry after an engine
	// failure on a reused state.
	loadStrict
)

// readRemote reads and validates current, imports the descriptor packs the
// private repository lacks (downloading only those absent from the local
// byte cache), and validates the accepted history and text before
// returning. A stale observation whose referenced pack disappeared is
// discarded and current is re-read; an unchanged manifest that still
// references the missing pack is a storage-integrity error
// (architecture/pull.md). The reader never guesses state from object names.
func (n *Notebook) readRemote(ctx context.Context) (remoteState, error) {
	return n.readRemoteMode(ctx, loadReusing)
}

// readRemoteStrict is readRemote without the reuse of an accepted head.
func (n *Notebook) readRemoteStrict(ctx context.Context) (remoteState, error) {
	return n.readRemoteMode(ctx, loadStrict)
}

func (n *Notebook) readRemoteMode(ctx context.Context, mode loadMode) (remoteState, error) {
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
			return remoteState{}, manifestError(err)
		}

		st, err := n.loadRemote(ctx, m, etag, mode)
		if err == nil {
			return st, nil
		}
		if !errors.Is(err, errStaleManifest) {
			return remoteState{}, err
		}
		if restart >= n.retryLimit {
			return remoteState{}, storageIntegrity(ReasonPackInvalid, nil, "manifest did not stabilize after %d stale reads", restart)
		}
		// The referenced pack disappeared during cleanup: re-read
		// current and restart only when the manifest actually moved.
		_, newETag, newPresent, rerr := n.readCurrent(ctx)
		if rerr != nil {
			return remoteState{}, rerr
		}
		if !newPresent || newETag == etag {
			return remoteState{}, storageIntegrity(ReasonPackInvalid, nil, "manifest references a pack that is missing and unchanged after re-read")
		}
	}
}

// loadRemote serves the manifest from the private repository. In
// loadReusing mode a head this workspace already accepted is reused after a
// presence check (reuseAccepted); otherwise the packs the repository lacks
// are imported and the accepted state validated. The repository is a cache
// the manifest describes, never an authority: objects an earlier call
// imported can since have been damaged, so a validation failure caused by
// missing objects re-imports every pack from its verified bytes once before
// the failure is reported (architecture/pull.md).
func (n *Notebook) loadRemote(ctx context.Context, m storage.Manifest, etag storage.ETag, mode loadMode) (remoteState, error) {
	if mode == loadReusing {
		if st, verdict := n.reuseAccepted(m, etag); verdict == reuseHit {
			return st, nil
		}
	}
	if err := n.importRemote(ctx, m, importMissing); err != nil {
		return remoteState{}, err
	}
	st, verdict, err := n.validateRemote(m, etag)
	if verdict != validationObjectsMissing {
		return st, err
	}
	if err := n.importRemote(ctx, m, importAll); err != nil {
		return remoteState{}, err
	}
	st, _, err = n.validateRemote(m, etag)
	return st, err
}

// reuseVerdict classifies one reuseAccepted attempt.
type reuseVerdict uint8

const (
	// reuseMiss means the head is not the accepted baseline, or the
	// repository cannot prove it: the caller imports and validates.
	reuseMiss reuseVerdict = iota
	// reuseHit means the accepted head was served from the repository.
	reuseHit
)

// reuseAccepted serves a manifest whose head this workspace already
// accepted without importing or walking history: the accepted baseline was
// validated when it was recorded, and Git objects are immutable, so only
// presence can have changed. It re-checks the presence of every descriptor
// head (a pack import is atomic, so a present head proves its pack), the
// head commit, and that it names the baseline tree. It deliberately does not
// walk or read the tree: every caller reads the objects it needs, and an
// engine failure there drives one strict reload that validates and repairs
// (Notebook.pull, Notebook.commit). Any gap here is a miss that sends the
// caller to the full import-and-validate path (architecture/pull.md).
func (n *Notebook) reuseAccepted(m storage.Manifest, etag storage.ETag) (remoteState, reuseVerdict) {
	base := n.ws.Baseline()
	if base.Head.IsZero() || base.Head != m.Head {
		return remoteState{}, reuseMiss
	}
	repo := n.ws.Repo()
	heads := make([]git.OID, 0, 1+len(m.Increments))
	heads = append(heads, m.Checkpoint.Head)
	for _, inc := range m.Increments {
		heads = append(heads, inc.Head)
	}
	for _, head := range heads {
		if present, err := repo.HasObject(head); err != nil || !present {
			return remoteState{}, reuseMiss
		}
	}
	commit, err := repo.ReadCommit(m.Head)
	if err != nil || commit.Tree != base.Tree {
		return remoteState{}, reuseMiss
	}
	// The boundary is deliberately not recorded here: only a validated
	// history proves the manifest's checkpoint head is an ancestor of the
	// accepted head, and a wrong graft would silently truncate every later
	// walk. A handle whose graft table predates a boundary another process
	// wrote therefore keeps walking past it until a strict load reloads the
	// table, which the commit retry forces (architecture/pull.md).
	n.recordTail(m)
	return acceptedState(m, etag, commit.Tree), reuseHit
}

// validationVerdict classifies a validateRemote failure so loadRemote
// repairs only what a re-import can fix.
type validationVerdict uint8

const (
	// validationOK means the accepted state is complete and valid.
	validationOK validationVerdict = iota
	// validationObjectsMissing means a commit, tree, or blob of the accepted
	// history is absent from or unreadable in the repository
	// (git.ErrObjectMissing): re-importing the verified packs can repair it.
	validationObjectsMissing
	// validationContentInvalid means the head tree is present but is not
	// valid notebook state; no import changes that.
	validationContentInvalid
)

// verdictFor separates the two validation failures the repository can
// produce: an object it cannot supply (repairable by re-importing the
// verified packs) from stored state that breaks a notebook content rule,
// which no import changes. A blob is proven by presence during the history
// walk and read during the snapshot read, so only the snapshot read meets a
// present-but-unreadable object.
func verdictFor(err error) validationVerdict {
	if errors.Is(err, git.ErrObjectMissing) {
		return validationObjectsMissing
	}
	return validationContentInvalid
}

// validateRemote proves the imported history from the head down to the
// shallow boundary and the head tree's text, then returns the remote state.
func (n *Notebook) validateRemote(m storage.Manifest, etag storage.ETag) (remoteState, validationVerdict, error) {
	if err := git.ValidateHistory(n.ws.Repo(), m.Head, m.Checkpoint.Head); err != nil {
		return remoteState{}, verdictFor(err), storageIntegrity(ReasonHistoryInvalid, err, "accepted state is incomplete")
	}
	// A head the manifest names that cannot be read is an object failure,
	// never a content rule: the commit either is not there or is damaged.
	commit, err := n.ws.Repo().ReadCommit(m.Head)
	if err != nil {
		return remoteState{}, validationObjectsMissing, storageIntegrity(ReasonHistoryInvalid, err, "accepted state %s is unreadable", m.Head)
	}
	if _, err := git.ReadSnapshot(n.ws.Repo(), commit.Tree); err != nil {
		return remoteState{}, verdictFor(err), storageIntegrity(ReasonHistoryInvalid, err, "accepted state is not valid notebook text")
	}
	n.recordTail(m)
	return acceptedState(m, etag, commit.Tree), validationOK, nil
}

// readCurrent reads the authoritative manifest object. ErrNotFound maps to
// the implicit empty-notebook state, not an error.
func (n *Notebook) readCurrent(ctx context.Context) (data []byte, etag storage.ETag, present bool, err error) {
	rc, info, err := n.store.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, "", false, nil
		}
		return nil, "", false, storageFailure(ReasonManifestRead, err, "read current manifest")
	}
	defer rc.Close()
	data, err = io.ReadAll(rc)
	if err != nil {
		return nil, "", false, storageFailure(ReasonManifestRead, err, "read current manifest body")
	}
	return data, info.ETag, true, nil
}

// importRemote imports the manifest's descriptor chain: the checkpoint
// pack, the shallow boundary, and every increment in manifest order. With
// importMissing a pack whose head commit the repository already holds is
// neither fetched nor imported, so an unchanged tail costs one existence
// check per descriptor instead of one import. The imports are strictly
// sequential — the boundary must exist before the tail lands on it — but
// the downloads behind them overlap through prefetchPacks: S3 has no bulk
// read, so a long tail fetched serially costs one round-trip latency per
// generation.
func (n *Notebook) importRemote(ctx context.Context, m storage.Manifest, mode importMode) error {
	specs := make([]packSpec, 0, 1+len(m.Increments))
	specs = append(specs, packSpec{
		key:  m.Checkpoint.Key.String(),
		sha:  m.Checkpoint.SHA256,
		size: m.Checkpoint.Size,
		head: m.Checkpoint.Head,
	})
	for _, inc := range m.Increments {
		specs = append(specs, packSpec{key: inc.Key.String(), sha: inc.SHA256, size: inc.Size, head: inc.Head})
	}
	wanted := make([]bool, len(specs))
	var missing []packSpec
	for i, spec := range specs {
		if mode == importMissing {
			present, err := n.ws.Repo().HasObject(spec.head)
			if err != nil {
				return storageIntegrity(ReasonEngineFailed, err, "check pack %s", spec.key)
			}
			if present {
				continue
			}
		}
		wanted[i] = true
		missing = append(missing, spec)
	}
	next, stop := n.prefetchPacks(ctx, missing)
	defer stop()

	for i, spec := range specs {
		if wanted[i] {
			data, err := next()
			if err != nil {
				return err
			}
			if err := git.ImportPack(n.ws.Repo(), data); err != nil {
				if i == 0 {
					return storageIntegrity(ReasonPackInvalid, err, "import checkpoint pack %s", spec.key)
				}
				return storageIntegrity(ReasonPackInvalid, err, "import increment pack %s", spec.key)
			}
		}
		if i == 0 {
			if err := git.MarkShallow(n.ws.Repo(), m.Checkpoint.Head); err != nil {
				return storageIntegrity(ReasonEngineFailed, err, "record checkpoint boundary %s", m.Checkpoint.Head)
			}
		}
	}
	return nil
}

// packFetchConcurrency bounds the pack downloads in flight during one
// import. Each download is one storage round trip, so the serial cost of
// a long tail is its length times the endpoint latency; this many in
// flight collapse that to roughly the bandwidth cost without opening an
// unbounded number of connections.
const packFetchConcurrency = 16

// packFetchResult carries one prefetched pack: the verified bytes or the
// first error of its download.
type packFetchResult struct {
	data []byte
	err  error
}

// prefetchPacks downloads every spec with at most packFetchConcurrency
// fetches in flight. next yields each pack in spec order, so the caller's
// import loop keeps its sequential shape; stop abandons the remaining
// downloads and must always be called.
//
// The dispatcher hands out indexes in order and every worker delivers on
// an unbuffered channel, so the fetches in flight are always the lowest
// unconsumed indexes — the one the importer needs next is always among
// them, and a worker that runs ahead waits with its bytes instead of
// buffering without bound. Cancellation reaches every side: workers and
// the dispatcher select on it, and next itself returns once the context
// ends, so no participant can wait on a partner that already left.
func (n *Notebook) prefetchPacks(ctx context.Context, specs []packSpec) (next func() ([]byte, error), stop func()) {
	if len(specs) == 0 {
		return func() ([]byte, error) {
			return nil, storageIntegrity(ReasonInternal, nil, "no pack was requested")
		}, func() {}
	}
	fctx, cancel := context.WithCancel(ctx)
	results := make([]chan packFetchResult, len(specs))
	for i := range results {
		results[i] = make(chan packFetchResult)
	}
	work := make(chan int)
	go func() {
		defer close(work)
		for i := range specs {
			select {
			case work <- i:
			case <-fctx.Done():
				return
			}
		}
	}()
	for w := 0; w < min(packFetchConcurrency, len(specs)); w++ {
		go func() {
			for i := range work {
				data, err := n.ensurePack(fctx, specs[i])
				select {
				case results[i] <- packFetchResult{data: data, err: err}:
				case <-fctx.Done():
					return
				}
			}
		}()
	}
	i := 0
	next = func() ([]byte, error) {
		select {
		case r := <-results[i]:
			i++
			return r.data, r.err
		case <-fctx.Done():
			return nil, storageFailure(ReasonPackDownload, fctx.Err(), "download packs")
		}
	}
	return next, cancel
}

// packSpec identifies one pack descriptor: the protocol key, the
// authoritative SHA-256 and size from the manifest, and the head commit
// whose presence proves the pack was imported.
type packSpec struct {
	key  string
	sha  storage.SHA256
	size uint64
	head git.OID
}

// ensurePack returns the exact pack bytes for a descriptor. A cache hit
// requires the expected byte size and a fresh SHA-256 check; a corrupt
// cache entry is discarded and re-downloaded, never a false hit. Downloads
// verify the descriptor checksum and size before caching and importing;
// caching itself is best-effort, so an unwritable cache directory costs
// future downloads, never the operation.
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
		return nil, storageFailure(ReasonPackDownload, err, "download pack %s", spec.key)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, storageFailure(ReasonPackDownload, err, "download pack %s", spec.key)
	}
	if uint64(len(data)) != spec.size || sha256.Sum256(data) != spec.sha {
		return nil, storageIntegrity(ReasonPackInvalid, nil, "pack %s does not match its descriptor checksum and size", spec.key)
	}
	if err := n.cacheWrite(spec.sha, data); err != nil {
		// The cache only saves future downloads; the verified bytes are
		// already in hand. An unwritable cache — a read-only shared
		// directory, permissions, a full disk — must not fail the pull.
		LoggerFrom(ctx).Warn("pack cache write failed", "error", err)
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
// The notebook returns success only when the ID is present
// (architecture/commit.md); an ID that no descriptor records cannot be
// proved accepted.
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
		return false, manifestError(err)
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

// recoverState is the generic recovery path (architecture/guarantees.md): it
// rereads authoritative current strictly (no reuse of the accepted head, so
// a damaged repository is repaired), imports the missing descriptor packs,
// reconstructs the head tree, and applies it to L and P through the
// workspace. The report states the failed stage, whether remote acceptance
// is known, and whether resynchronization succeeded. A failed repair leaves
// P durably marked as requiring recovery, so the next call retries entry
// recovery.
func (n *Notebook) recoverState(ctx context.Context, stage string, accepted RemoteAccepted) (recoveryReport, error) {
	report := recoveryReport{stage: stage, remoteAccepted: accepted}
	remote, err := n.readRemoteStrict(ctx)
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

// manifestError classifies a manifest that would not decode: a newer
// slivingdoc's is UPGRADE_REQUIRED, anything else is invalid.
func manifestError(err error) error {
	if errors.Is(err, storage.ErrUpgradeRequired) {
		return storageFailure(ReasonManifestRead, err, "current was written by a newer slivingdoc")
	}
	return storageIntegrity(ReasonManifestInvalid, err, "current is not a valid manifest")
}
