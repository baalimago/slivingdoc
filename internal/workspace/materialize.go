package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/baalimago/slivingdoc/internal/git"
)

// Diff reports how the visible directory differs from the accepted
// baseline: added, modified, and deleted paths, each sorted.
type Diff struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// Failpoints injects deterministic failures at the workspace mutation
// boundaries. A nil hook means no injection. Hooks run inside the
// operation lock, so tests can use them as barriers as well.
type Failpoints struct {
	// Scan fires before a visible-directory scan. A failure aborts the
	// scan with no mutation.
	Scan func() error
	// Stage fires before staged writes begin. A failure leaves the
	// visible directory and the private state record unchanged.
	Stage func() error
	// Replace fires after the recovery-required flag is durable and
	// before the visible-directory replacement begins. A failure leaves
	// the visible directory unchanged but marks the workspace as
	// requiring recovery.
	Replace func() error
	// Baseline fires before the durable baseline record is written after
	// a successful replacement. A failure leaves the visible directory
	// replaced and the workspace marked as requiring recovery.
	Baseline func() error
	// Recover fires before a recovery replacement begins.
	Recover func() error
}

// applyLocked rewrites the visible directory to the target tree. When
// newBaseline is nil the accepted baseline is kept (conflict
// materialization); otherwise it is durably recorded together with
// recoveryRequired=false after a successful replacement.
func (w *Workspace) applyLocked(ctx context.Context, targetTree git.OID, newBaseline *Baseline) error {
	snap, err := git.ReadSnapshot(w.repo, targetTree)
	if err != nil {
		return fmt.Errorf("workspace: read target tree: %w", err)
	}
	stageDir := filepath.Join(w.privDir, stagingDirName)
	backupDir := filepath.Join(w.privDir, backupDirName)
	if err := os.RemoveAll(stageDir); err != nil {
		return fmt.Errorf("workspace: clean staging: %w", err)
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("workspace: clean backup: %w", err)
	}
	if fp := w.failpoints; fp != nil && fp.Stage != nil {
		if err := fp.Stage(); err != nil {
			return fmt.Errorf("workspace: stage: injected: %w", err)
		}
	}
	if err := w.writeStage(ctx, stageDir, snap); err != nil {
		return fmt.Errorf("workspace: stage: %w", err)
	}

	// The recovery-required flag must be durable before any L mutation.
	if err := w.markRecoveryRequired(); err != nil {
		return err
	}

	if fp := w.failpoints; fp != nil && fp.Replace != nil {
		if err := fp.Replace(); err != nil {
			return fmt.Errorf("workspace: replace: %w: injected: %v", ErrPartial, err)
		}
	}
	if err := w.applyInPlace(ctx, stageDir, snap); err != nil {
		return err
	}

	w.mu.Lock()
	st := w.state
	w.mu.Unlock()
	if newBaseline != nil {
		st.RemoteGeneration = newBaseline.RemoteGeneration
		if newBaseline.Head.IsZero() {
			st.BaselineHead = ""
		} else {
			st.BaselineHead = newBaseline.Head.String()
		}
		st.BaselineTree = newBaseline.Tree.String()
	}
	st.RecoveryRequired = false
	if fp := w.failpoints; fp != nil && fp.Baseline != nil {
		if err := fp.Baseline(); err != nil {
			return fmt.Errorf("workspace: baseline: %w: injected: %v", ErrPartial, err)
		}
	}
	st, err = persistState(w.privDir, w.derivedKey, st)
	if err != nil {
		return fmt.Errorf("workspace: record baseline: %w", err)
	}
	w.mu.Lock()
	w.state = st
	w.mu.Unlock()
	return nil
}

// markRecoveryRequired durably records that normal operations must stop
// until Recover reconstructs L and P from current. It is called before any
// visible replacement and before an injected recovery boundary, so a failed
// repair cannot accidentally permit unrelated new work.
func (w *Workspace) markRecoveryRequired() error {
	w.mu.Lock()
	st := w.state
	w.mu.Unlock()
	if st.RecoveryRequired {
		return nil
	}
	st.RecoveryRequired = true
	st, err := persistState(w.privDir, w.derivedKey, st)
	if err != nil {
		return fmt.Errorf("workspace: mark recovery: %w", err)
	}
	w.mu.Lock()
	w.state = st
	w.mu.Unlock()
	return nil
}

// writeStage writes the target snapshot into the staging directory inside
// P with the final visible modes: directories 0755, files 0644, subject to
// the process umask. It is the pre-flight of a materialization: the whole
// target tree must be writable before L is touched at all, so an
// out-of-space or cancelled write fails while the visible directory is
// still intact and no recovery is required.
func (w *Workspace) writeStage(ctx context.Context, stageDir string, snap git.Snapshot) error {
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	for _, f := range snap.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		host := filepath.Join(stageDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
			return fmt.Errorf("create staged directory for %q: %w", f.Path, err)
		}
		if err := os.WriteFile(host, f.Data, 0o644); err != nil {
			return fmt.Errorf("write staged file %q: %w", f.Path, err)
		}
	}
	return nil
}

// applyInPlace writes the target tree into L without ever replacing L
// itself. Each file lands through a temporary file renamed over the
// target, so an individual file is replaced atomically and hard links are
// not preserved; obsolete entries and then obsolete directories are
// removed afterwards. Any failure after the first write is a partial
// mutation and requires recovery.
//
// L keeps its identity across the whole operation. An earlier design
// renamed L aside and renamed the staged tree into its place, which was
// cheaper but changed the directory inode on every pull: a shell standing
// in L, an open editor, a file watcher, and the workspace's own os.Root
// handle were all left pointing at a removed inode. The directory swap
// was never atomic either — between the two renames the path did not
// exist at all — so it bought no failure guarantee that the durable
// recovery flag does not already provide.
func (w *Workspace) applyInPlace(ctx context.Context, stageDir string, target git.Snapshot) error {
	// L is created rather than replaced, so a first materialization into a
	// missing directory has somewhere to land.
	if err := w.root.MkdirAll(w.rel, 0o755); err != nil {
		return fmt.Errorf("workspace: create visible directory: %w: %w", ErrPartial, err)
	}
	targetFiles := make(map[string]bool, len(target.Files))
	for _, f := range target.Files {
		targetFiles[f.Path] = true
	}
	var existingFiles, existingDirs []string
	if err := w.collectVisible(ctx, w.rel, "", &existingFiles, &existingDirs); err != nil {
		return err
	}

	// A directory where the target has a file, or a file where the target
	// has a directory, must be cleared before the copy so MkdirAll and the
	// per-file rename can land.
	for _, p := range existingDirs {
		if targetFiles[p] {
			if err := w.root.RemoveAll(joinRel(w.rel, p)); err != nil {
				return fmt.Errorf("workspace: clear replaced directory %q: %w: %w", p, ErrPartial, err)
			}
		}
	}
	targetDirs := targetDirSet(target)
	for _, p := range existingFiles {
		if targetDirs[p] {
			if err := w.root.Remove(joinRel(w.rel, p)); err != nil {
				return fmt.Errorf("workspace: clear replaced file %q: %w: %w", p, ErrPartial, err)
			}
		}
	}

	for _, f := range target.Files {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("workspace: copy into place: %w", err)
		}
		targetRel := joinRel(w.rel, f.Path)
		if dir := parentRel(f.Path); dir != "" {
			if err := w.root.MkdirAll(joinRel(w.rel, dir), 0o755); err != nil {
				return fmt.Errorf("workspace: create visible directory for %q: %w", f.Path, err)
			}
		}
		tmpRel := targetRel + tempSuffix()
		fh, err := w.root.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollowFlag, 0o644)
		if err != nil {
			return fmt.Errorf("workspace: create temporary file for %q: %w: %w", f.Path, ErrPartial, err)
		}
		if _, err := fh.Write(f.Data); err != nil {
			fh.Close()
			return fmt.Errorf("workspace: write %q: %w: %w", f.Path, ErrPartial, err)
		}
		if err := fh.Close(); err != nil {
			return fmt.Errorf("workspace: close %q: %w: %w", f.Path, ErrPartial, err)
		}
		if err := w.root.Rename(tmpRel, targetRel); err != nil {
			return fmt.Errorf("workspace: place %q: %w: %w", f.Path, ErrPartial, err)
		}
	}

	for _, p := range existingFiles {
		if !targetFiles[p] && !targetDirs[p] {
			if err := w.root.Remove(joinRel(w.rel, p)); err != nil {
				return fmt.Errorf("workspace: remove obsolete file %q: %w: %w", p, ErrPartial, err)
			}
		}
	}
	// Remove obsolete directories deepest-first; L itself is never
	// removed. RemoveAll on an already-removed subtree is harmless.
	sorted := append([]string(nil), existingDirs...)
	sort.Sort(sort.Reverse(sort.StringSlice(sorted)))
	for _, p := range sorted {
		if !targetDirs[p] {
			if err := w.root.RemoveAll(joinRel(w.rel, p)); err != nil {
				return fmt.Errorf("workspace: remove obsolete directory %q: %w: %w", p, ErrPartial, err)
			}
		}
	}
	if err := os.RemoveAll(stageDir); err != nil {
		return fmt.Errorf("workspace: remove staging: %w", err)
	}
	return nil
}

// targetDirSet returns every directory path of the target snapshot: all
// strict prefixes of target files.
func targetDirSet(target git.Snapshot) map[string]bool {
	dirs := make(map[string]bool)
	for _, f := range target.Files {
		for dir := parentRel(f.Path); dir != ""; dir = parentRel(dir) {
			dirs[dir] = true
		}
	}
	return dirs
}

// parentRel returns the slash-separated parent of an internal path, or ""
// for a top-level path.
func parentRel(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}

// tempSuffix returns a random file-name suffix for the copy-fallback
// temporary files. The name is unique enough that it cannot collide with a
// notebook file or a stale temporary from a crashed run.
func tempSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ".slivingdoc-tmp"
	}
	return ".slivingdoc-tmp-" + hex.EncodeToString(b[:])
}
