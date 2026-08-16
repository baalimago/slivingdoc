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
		return Result{}, &Error{Code: CodeInvalidRequest, Message: "visible files cannot be represented as notebook state", Cause: err}
	}

	remote, err := n.readRemote(ctx)
	if err != nil {
		return Result{}, err
	}

	merged, err := git.Merge(n.ws.Repo(), n.ws.Baseline().Tree, localTree, remote.tree)
	if err != nil {
		return Result{}, &Error{Code: CodeStorageIntegrity, Message: "merge failed", Cause: err}
	}

	baseline := remote.baseline()
	if len(merged.Conflicts) > 0 {
		tree, err := n.materializeTree(merged)
		if err != nil {
			return Result{}, &Error{Code: CodeStorageIntegrity, Message: "materialize conflict result", Cause: err}
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
		return Result{}, contentConflict("Resolve the conflict blocks in the visible files before continuing.",
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
