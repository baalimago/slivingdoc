package notebook

import (
	"context"

	"github.com/baalimago/slivingdoc/internal/git"
)

// Pull validates and ingests the visible directory, reads and validates the
// authoritative manifest, downloads only missing packs, imports the
// accepted state, and merges the accepted baseline, L, and R (architecture
// section 10). L is rewritten with the full merge result and R becomes the
// new baseline. A conflicting pull writes the markers and non-conflicting
// results to L, records R as the baseline, and returns the exact conflicted
// paths and ranges; it never reverts L.
//
// The returned Result reports the accepted remote generation and the
// diffstat of the on-disk delta between the visible state the pull
// observed and the materialized result. A conflict or any error returns
// the zero Result with the existing error.
func (n *Notebook) Pull(ctx context.Context) (Result, error) {
	if n.ws.RecoveryRequired() {
		if err := n.entryRecovery(ctx); err != nil {
			return Result{}, err
		}
	}

	local, err := n.ws.Snapshot(ctx)
	if err != nil {
		return Result{}, n.mapLocalError(err)
	}
	localTree, err := git.BuildTree(n.ws.Repo(), local)
	if err != nil {
		return Result{}, invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}

	remote, err := n.readRemote(ctx)
	if err != nil {
		return Result{}, err
	}

	// A changed protected path is pinned to the baseline on the local side,
	// so the merge takes R there and the diffstat (raw local vs. merged)
	// shows the restore (architecture section 10). With nothing changed the
	// pinned tree would equal the local tree, so no pin is built.
	mergeTree, err := n.pinProtected(local, localTree)
	if err != nil {
		return Result{}, err
	}

	merged, err := git.Merge(n.ws.Repo(), n.ws.Baseline().Tree, mergeTree, remote.tree)
	if err != nil {
		return Result{}, storageIntegrity(ReasonEngineFailed, err, "merge failed")
	}

	baseline := remote.baseline()
	if len(merged.Conflicts) > 0 {
		tree, err := n.materializeTree(merged)
		if err != nil {
			return Result{}, storageIntegrity(ReasonEngineFailed, err, "materialize conflict result")
		}
		if err := n.applyLocal(ctx, stageConflict, RemoteAcceptedNo, func() error {
			return n.ws.Materialize(ctx, baseline, tree)
		}); err != nil {
			return Result{}, err
		}
		// A conflicting pull also initializes P for the first commit: the
		// caller resolves the markers and commits from this baseline.
		if err := n.ws.MarkPulled(ctx); err != nil {
			return Result{}, n.mapLocalError(err)
		}
		return Result{}, contentConflict(ReasonMergeConflict, "Resolve the conflict blocks in the visible files before continuing.",
			contentConflictFiles(merged.Conflicts))
	}

	// The change summary is presentation-only: compute it against the
	// merged result before any local mutation, so a read failure aborts
	// with L and P untouched.
	stat, err := n.diffStat(localTree, merged.Tree)
	if err != nil {
		return Result{}, err
	}
	if err := n.applyLocal(ctx, stagePull, RemoteAcceptedNo, func() error {
		return n.ws.Materialize(ctx, baseline, merged.Tree)
	}); err != nil {
		return Result{}, err
	}
	if err := n.ws.MarkPulled(ctx); err != nil {
		return Result{}, n.mapLocalError(err)
	}
	return Result{Generation: remote.generation, Stat: stat}, nil
}

// pinProtected returns the tree the merge takes as its local side: the
// local tree when no protected path changed, and otherwise the local
// snapshot with those paths restored from the baseline.
func (n *Notebook) pinProtected(local git.Snapshot, localTree git.OID) (git.OID, error) {
	if !n.policy.Configured() {
		return localTree, nil
	}
	baseTree := n.ws.Baseline().Tree
	changed, err := n.policy.ChangedProtected(n.ws.Repo(), localTree, baseTree)
	if err != nil {
		return git.OID{}, storageIntegrity(ReasonEngineFailed, err, "read the baseline snapshot for the read-only check")
	}
	if len(changed) == 0 {
		return localTree, nil
	}
	pinned, err := n.restoreProtected(baseTree, local, changed)
	if err != nil {
		return git.OID{}, err
	}
	tree, err := git.BuildTree(n.ws.Repo(), pinned)
	if err != nil {
		return git.OID{}, invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}
	return tree, nil
}
