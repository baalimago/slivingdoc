// Package workspace implements managed caller directories: path policy,
// deterministic private state, visible-file snapshots, and local operation
// serialization (architecture sections 7 and 18.2).
//
// A Workspace binds one canonical visible path L to one notebook storage
// identity and one private repository directory P under the private root.
// P holds the Git repository, the strict state.json record, the operation
// lock, and transient staging state. The baseline Git tree in the state
// record is authoritative; a file snapshot is only a cache and is never
// persisted.
//
// The workspace never reads remote state. Recovery orchestration belongs to
// internal/notebook: it rereads the authoritative manifest, imports packs,
// and supplies the reconstructed tree; the workspace applies that tree to
// L, rebuilds the baseline, and reports whether repair completed.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/baalimago/slivingdoc/internal/git"
)

// lockRetryInterval is the retry interval for the local operation lock
// (architecture section 7.2): wait until the request context ends.
const lockRetryInterval = 50 * time.Millisecond

// Engine is the workspace's narrow view of the native Git engine: it
// creates or opens the private repository inside P. The caller owns
// Engine.Open and Engine.Close; the workspace owns the repository it
// receives.
type Engine interface {
	CreateRepo(path string) (git.Repository, error)
	OpenRepo(path string) (git.Repository, error)
}

// Config binds one workspace. WorkspaceRoot, Path, and PrivateRoot must be
// absolute; Path must stay at or below WorkspaceRoot, and PrivateRoot must
// not be at or below WorkspaceRoot (architecture section 17).
type Config struct {
	// WorkspaceRoot is the absolute configured workspace root.
	WorkspaceRoot string
	// Path is the absolute requested visible path.
	Path string
	// PrivateRoot is the absolute configured private root.
	PrivateRoot string
	// PackCacheRoot is the absolute shared pack-cache root, or empty for
	// the private per-workspace cache inside P. When set, the identity
	// selects one shared directory below it, so every workspace of one
	// notebook shares the verified pack bytes.
	PackCacheRoot string
	// Identity is the notebook storage identity; the derived key selects
	// the private directory.
	Identity Identity
	// Engine creates or opens the private Git repository.
	Engine Engine
	// Failpoints injects deterministic failures; nil disables injection.
	Failpoints *Failpoints
}

// Workspace is one managed visible path. All methods are safe for
// concurrent use; calls for one path serialize on the operation lock, and
// different paths operate independently.
type Workspace struct {
	mu         sync.Mutex // guards state, closed
	sem        chan struct{}
	root       *os.Root
	path       string // canonical visible path, host form
	rel        string // visible path relative to the workspace root, slash form
	wsRoot     string // canonical configured workspace root
	privDir    string // <private-root>/<derived-key>
	cacheDir   string // shared pack cache; empty selects <privDir>/pack-cache
	derivedKey string
	repo       git.Repository
	flock      *flock.Flock
	state      state
	failpoints *Failpoints
	closed     bool
}

// Open validates the configuration, creates the private state and the
// visible directory when they do not exist, and opens the private Git
// repository. A corrupt, mismatched, or interrupted private state record
// opens the workspace in the recovery-required mode instead of failing:
// normal operations refuse until Recover resynchronizes P and L from the
// reconstructed remote baseline. Open never modifies L beyond creating the
// missing visible directory.
func Open(ctx context.Context, cfg Config) (*Workspace, error) {
	canonical, rel, err := canonicalize(cfg.WorkspaceRoot, cfg.Path)
	if err != nil {
		return nil, err
	}
	if cfg.PrivateRoot == "" || !filepath.IsAbs(cfg.PrivateRoot) {
		return nil, fmt.Errorf("%w: private root %q is not absolute", ErrInvalidPath, cfg.PrivateRoot)
	}
	if RootsOverlap(cfg.PrivateRoot, cfg.WorkspaceRoot) {
		return nil, fmt.Errorf("%w: private root %q is at or below the workspace root", ErrInvalidPath, cfg.PrivateRoot)
	}
	var cacheDir string
	if cfg.PackCacheRoot != "" {
		if !filepath.IsAbs(cfg.PackCacheRoot) {
			return nil, fmt.Errorf("%w: pack cache root %q is not absolute", ErrInvalidPath, cfg.PackCacheRoot)
		}
		if RootsOverlap(cfg.PackCacheRoot, cfg.WorkspaceRoot) {
			return nil, fmt.Errorf("%w: pack cache root %q is at or below the workspace root", ErrInvalidPath, cfg.PackCacheRoot)
		}
		cacheDir = filepath.Join(cfg.PackCacheRoot, SharedCacheDirName(cfg.Identity))
	}
	if cfg.Engine == nil {
		return nil, errors.New("workspace: engine is required")
	}

	derivedKey := DerivedKey(canonical, cfg.Identity)
	privDir := filepath.Join(cfg.PrivateRoot, derivedKey)
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: create workspace root: %w", err)
	}
	if err := os.MkdirAll(privDir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: create private directory: %w", err)
	}
	root, err := os.OpenRoot(cfg.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace: open workspace root: %w", err)
	}
	defer func() {
		if root != nil {
			root.Close()
		}
	}()

	// Reject a symlink in any existing component of the requested path
	// before the root-relative operations touch it (architecture 18.2).
	if err := rejectSymlinkComponents(root, rel); err != nil {
		return nil, err
	}
	if err := root.MkdirAll(rel, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: create visible directory: %w", err)
	}

	// Create P and its records under the local operation lock so two
	// server processes cannot initialize the same private directory at
	// once (architecture section 7.2).
	fl := flock.New(filepath.Join(privDir, operationLockName))
	locked, err := fl.TryLockContext(ctx, lockRetryInterval)
	if err != nil {
		return nil, fmt.Errorf("workspace: operation lock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("workspace: operation lock: %w", ctx.Err())
	}
	defer fl.Unlock()

	repo, needsRecovery, err := openPrivateState(cfg.Engine, privDir, derivedKey)
	if err != nil {
		return nil, err
	}

	w := &Workspace{
		root:       root,
		path:       canonical,
		rel:        rel,
		wsRoot:     cfg.WorkspaceRoot,
		privDir:    privDir,
		cacheDir:   cacheDir,
		derivedKey: derivedKey,
		repo:       repo,
		flock:      fl,
		sem:        make(chan struct{}, 1),
		failpoints: cfg.Failpoints,
		state:      needsRecovery.state,
	}
	if needsRecovery.recovery {
		w.state.RecoveryRequired = true
	}
	root = nil // ownership moved to the workspace
	return w, nil
}

// openStateOut carries the private state load result: the loaded record
// and whether the workspace must recover before normal work.
type openStateOut struct {
	state    state
	recovery bool
}

// openPrivateState loads or creates the state record and the repository
// under the operation lock. The repository is always opened or created so
// recovery can import packs and materialize; a missing or corrupt
// repository is a server-owned cache and is rebuilt from remote state.
func openPrivateState(engine Engine, privDir, derivedKey string) (git.Repository, openStateOut, error) {
	repoPath := filepath.Join(privDir, repoDirName)
	statePath := filepath.Join(privDir, stateFileName)
	_, stateErr := os.Stat(statePath)
	_, repoErr := os.Stat(repoPath)
	_, tmpErr := os.Stat(filepath.Join(privDir, stateTmpName))

	stateMissing := errors.Is(stateErr, fs.ErrNotExist)
	repoMissing := errors.Is(repoErr, fs.ErrNotExist)
	tmpPresent := !errors.Is(tmpErr, fs.ErrNotExist)

	if stateMissing && repoMissing && !tmpPresent {
		// Fresh first use: create the repository, ensure the canonical
		// empty tree exists, and persist the initial generation-0 record.
		r, err := engine.CreateRepo(repoPath)
		if err != nil {
			return nil, openStateOut{}, fmt.Errorf("workspace: create private repository: %w", err)
		}
		if _, err := git.EmptyTree(r); err != nil {
			r.Close()
			return nil, openStateOut{}, fmt.Errorf("workspace: write empty tree: %w", err)
		}
		st := newWorkspaceState(derivedKey)
		st, err = persistState(privDir, derivedKey, st)
		if err != nil {
			r.Close()
			return nil, openStateOut{}, err
		}
		return r, openStateOut{state: st}, nil
	}

	// Reopen, or an anomaly: a missing or interrupted record, a corrupt
	// state file, a mismatched identity, or a missing or corrupt
	// repository. Any anomaly forces recovery.
	out := openStateOut{}
	r, err := engine.OpenRepo(repoPath)
	if err != nil {
		if !repoMissing {
			_ = os.RemoveAll(repoPath) // corrupt cache; rebuilt from remote
		}
		r, err = engine.CreateRepo(repoPath)
		if err != nil {
			return nil, openStateOut{}, fmt.Errorf("workspace: recreate private repository: %w", err)
		}
		out.recovery = true
	}
	if _, err := git.EmptyTree(r); err != nil {
		r.Close()
		return nil, openStateOut{}, fmt.Errorf("workspace: write empty tree: %w", err)
	}

	if stateMissing {
		out.recovery = true // interrupted first initialization
		return r, out, nil
	}
	st, err := readStateFile(privDir)
	if err != nil {
		out.recovery = true // corrupt record; recovery rewrites it
		return r, out, nil
	}
	out.state = st
	if st.Identity != derivedKey || tmpPresent {
		out.recovery = true
	}
	return r, out, nil
}

// rejectSymlinkComponents rejects an existing symlink in any component of
// the slash-form relative path. Components that do not exist yet are
// created later by MkdirAll, which never follows a symlink it creates.
func rejectSymlinkComponents(root *os.Root, rel string) error {
	cur := ""
	for _, seg := range splitRel(rel) {
		cur = joinRel(cur, seg)
		info, err := root.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("workspace: inspect %q: %w", cur, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("workspace: %q: %w", cur, ErrSymlink)
		}
	}
	return nil
}

// splitRel splits a slash-form relative path into its segments.
func splitRel(rel string) []string {
	if rel == "." {
		return nil
	}
	var segs []string
	for start := 0; start < len(rel); {
		end := start
		for end < len(rel) && rel[end] != '/' {
			end++
		}
		if end > start {
			segs = append(segs, rel[start:end])
		}
		start = end + 1
	}
	return segs
}

// Close releases the repository, the workspace root, and the operation
// lock. A closed workspace refuses further operations.
func (w *Workspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	var firstErr error
	if err := w.repo.Close(); err != nil {
		firstErr = fmt.Errorf("workspace: close repository: %w", err)
	}
	if err := w.root.Close(); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("workspace: close workspace root: %w", err)
		}
	}
	return firstErr
}

// Path returns the canonical visible path.
func (w *Workspace) Path() string { return w.path }

// Repo returns the open private Git repository. Notebook orchestration
// imports packs into it; the workspace owns its lifetime.
func (w *Workspace) Repo() git.Repository { return w.repo }

// RecoveryRequired reports whether the workspace refuses normal work until
// Recover resynchronizes it.
func (w *Workspace) RecoveryRequired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state.RecoveryRequired
}

// Baseline returns the accepted remote state recorded in P.
func (w *Workspace) Baseline() Baseline {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, _ := w.state.baseline() // the record is validated on load and on write
	return b
}

// BaselineSnapshot reads the accepted baseline tree from the private
// repository. The tree is authoritative; the visible directory is not
// consulted.
func (w *Workspace) BaselineSnapshot(ctx context.Context) (git.Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return git.Snapshot{}, errors.New("workspace: closed")
	}
	if w.state.RecoveryRequired {
		return git.Snapshot{}, ErrRecoveryRequired
	}
	b, err := w.state.baseline()
	if err != nil {
		return git.Snapshot{}, err
	}
	return git.ReadSnapshot(w.repo, b.Tree)
}

// Diff compares the visible directory against the accepted baseline and
// reports local additions, modifications, and deletions.
func (w *Workspace) Diff(ctx context.Context) (Diff, error) {
	var out Diff
	err := w.withOpLock(ctx, false, func() error {
		cur, err := w.scanLocked(ctx)
		if err != nil {
			return err
		}
		b, err := w.state.baseline()
		if err != nil {
			return err
		}
		base, err := git.ReadSnapshot(w.repo, b.Tree)
		if err != nil {
			return fmt.Errorf("workspace: read baseline tree: %w", err)
		}
		out = diffSnapshots(base, cur)
		return nil
	})
	return out, err
}

// Replace rewrites the visible directory to the target tree without
// changing the accepted baseline. It is the conflict materialization path:
// a semantic conflict intentionally rewrites L (architecture section 7.3).
func (w *Workspace) Replace(ctx context.Context, tree git.OID) error {
	return w.withOpLock(ctx, false, func() error {
		return w.applyLocked(ctx, tree, nil)
	})
}

// Accept rewrites the visible directory to the baseline tree and durably
// records the baseline as the accepted state. It is the normal acceptance
// path: after a successful publication, L and P advance together.
func (w *Workspace) Accept(ctx context.Context, baseline Baseline) error {
	return w.withOpLock(ctx, false, func() error {
		return w.applyLocked(ctx, baseline.Tree, &baseline)
	})
}

// Materialize rewrites the visible directory to the target tree and durably
// records the baseline as the accepted state in one failure-atomic
// operation. It is the conflict and pull path: L must show the merged
// result while P records the remote state the merge observed (architecture
// sections 10 and 12), which the two trees can express only together.
func (w *Workspace) Materialize(ctx context.Context, baseline Baseline, tree git.OID) error {
	return w.withOpLock(ctx, false, func() error {
		return w.applyLocked(ctx, tree, &baseline)
	})
}

// CacheDir returns the pack-byte cache directory: the identity-selected
// shared directory when a pack cache root is configured, else the private
// directory inside P. The notebook owns the cache protocol (SHA-256-keyed
// files, size and fresh SHA-256 on every hit), which is what makes the
// shared directory safe: no entry is trusted for its location. The
// directory is created lazily by the notebook on first use.
func (w *Workspace) CacheDir() string {
	if w.cacheDir != "" {
		return w.cacheDir
	}
	return filepath.Join(w.privDir, cacheDirName)
}

// Pulled reports whether a successful or conflicting pull has initialized P
// for this workspace (architecture section 11.1). The durable marker is
// notebook policy, not state schema: a fresh workspace and a pulled-empty
// workspace produce identical state records, so commit's baseline check
// uses the marker instead.
func (w *Workspace) Pulled() bool {
	_, err := os.Stat(filepath.Join(w.privDir, pulledMarkerName))
	return err == nil
}

// MarkPulled durably records that P has been initialized by a pull. The
// marker is an empty file written through a temporary file and atomic
// rename, so an interrupted write never leaves a half-visible marker.
func (w *Workspace) MarkPulled(ctx context.Context) error {
	return w.withOpLock(ctx, false, func() error {
		marker := filepath.Join(w.privDir, pulledMarkerName)
		tmp := marker + ".tmp"
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("workspace: mark pulled: %w", err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return fmt.Errorf("workspace: mark pulled: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("workspace: mark pulled: %w", err)
		}
		if err := os.Rename(tmp, marker); err != nil {
			return fmt.Errorf("workspace: mark pulled: %w", err)
		}
		return nil
	})
}

// Recover applies the reconstructed remote baseline to L and records it as
// the accepted state. It is the workspace side of the generic recovery
// path: notebook rereads the authoritative manifest, imports packs, and
// supplies the reconstructed baseline; the workspace rebuilds L and P and
// reports whether repair completed. Recover is the only operation allowed
// while the workspace requires recovery.
func (w *Workspace) Recover(ctx context.Context, baseline Baseline) error {
	return w.withOpLock(ctx, true, func() error {
		// Recovery itself can fail before applyLocked reaches its normal
		// pre-mutation marker. Record the requirement first, so a failed
		// repair never allows new pull or commit work to proceed against
		// partially trusted local state.
		if err := w.markRecoveryRequired(); err != nil {
			return err
		}
		if fp := w.failpoints; fp != nil && fp.Recover != nil {
			if err := fp.Recover(); err != nil {
				return fmt.Errorf("workspace: recover: injected: %w", err)
			}
		}
		return w.applyLocked(ctx, baseline.Tree, &baseline)
	})
}

// withOpLock serializes one visible path in-process (the semaphore, which
// honors the request context) and across server processes (the advisory
// lock file in P), waiting until the request context ends. The OS releases
// the advisory lock when a process exits; no PID or stale-lock recovery is
// stored (architecture section 7.2). Normal operations refuse while the
// workspace requires recovery; allowRecovery is true only for Recover.
func (w *Workspace) withOpLock(ctx context.Context, allowRecovery bool, fn func() error) error {
	select {
	case w.sem <- struct{}{}:
		defer func() { <-w.sem }()
	case <-ctx.Done():
		return fmt.Errorf("workspace: operation lock: %w", ctx.Err())
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return errors.New("workspace: closed")
	}
	recovery := w.state.RecoveryRequired
	w.mu.Unlock()
	if !allowRecovery && recovery {
		return ErrRecoveryRequired
	}

	locked, err := w.flock.TryLockContext(ctx, lockRetryInterval)
	if err != nil {
		return fmt.Errorf("workspace: operation lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("workspace: operation lock: %w", ctx.Err())
	}
	defer w.flock.Unlock()
	return fn()
}
