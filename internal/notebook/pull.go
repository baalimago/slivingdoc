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
func (n *Notebook) Pull(ctx context.Context) error {
	if n.ws.RecoveryRequired() {
		if err := n.entryRecovery(ctx); err != nil {
			return err
		}
	}

	local, err := n.ws.Snapshot(ctx)
	if err != nil {
		return n.mapLocalError(err)
	}
	localTree, err := git.BuildTree(n.ws.Repo(), local)
	if err != nil {
		return &Error{Code: CodeInvalidRequest, Message: "visible files cannot be represented as notebook state", Cause: err}
	}

	remote, err := n.readRemote(ctx)
	if err != nil {
		return err
	}

	merged, err := git.Merge(n.ws.Repo(), n.ws.Baseline().Tree, localTree, remote.tree)
	if err != nil {
		return &Error{Code: CodeStorageIntegrity, Message: "merge failed", Cause: err}
	}

	baseline := remote.baseline()
	if len(merged.Conflicts) > 0 {
		tree, err := n.materializeTree(merged)
		if err != nil {
			return &Error{Code: CodeStorageIntegrity, Message: "materialize conflict result", Cause: err}
		}
		if err := n.applyLocal(ctx, stageConflict, RemoteAcceptedNo, func() error {
			return n.ws.Materialize(ctx, baseline, tree)
		}); err != nil {
			return err
		}
		// A conflicting pull also initializes P for the first commit: the
		// caller resolves the markers and commits from this baseline.
		if err := n.ws.MarkPulled(ctx); err != nil {
			return n.mapLocalError(err)
		}
		return contentConflict("Resolve the conflict blocks in the visible files before continuing.",
			contentConflictFiles(merged.Conflicts))
	}

	if err := n.applyLocal(ctx, stagePull, RemoteAcceptedNo, func() error {
		return n.ws.Materialize(ctx, baseline, merged.Tree)
	}); err != nil {
		return err
	}
	if err := n.ws.MarkPulled(ctx); err != nil {
		return n.mapLocalError(err)
	}
	return nil
}
