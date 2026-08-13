package notebook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
)

// cleanupBatchSize is the maximum number of pack keys in one delete
// request (architecture section 14). The notebook owns the batch boundary
// so it can reread current and rebuild the cleanup roots before every
// batch.
const cleanupBatchSize = 1000

// checkpointPlan is the immutable selected prefix one checkpoint effort
// compacts: the observed manifest that triggered the schedule and its
// oldest threshold increments. The cutoff is the generation of the last
// selected increment; the head and publication are that increment's.
type checkpointPlan struct {
	manifest    storage.Manifest
	count       int
	cutoff      uint64
	head        git.OID
	publication storage.UUID
}

// planCheckpoint selects the oldest threshold increments of the triggering
// manifest as the stable prefix (architecture section 13.1). The caller
// only schedules a plan when the active tail has reached the threshold.
func planCheckpoint(observed storage.Manifest, threshold int) checkpointPlan {
	last := observed.Increments[threshold-1]
	return checkpointPlan{
		manifest:    observed,
		count:       threshold,
		cutoff:      last.Generation,
		head:        last.Head,
		publication: last.Publication,
	}
}

// prefixPresent reports whether the active tail of m still contains the
// plan's exact selected prefix as its oldest increments. A normal commit
// appends after the prefix and preserves it; a checkpoint at or beyond the
// cutoff removes it, so the worker must discard its proposal.
func (p checkpointPlan) prefixPresent(m storage.Manifest) bool {
	if len(m.Increments) < p.count {
		return false
	}
	for i := 0; i < p.count; i++ {
		if m.Increments[i] != p.manifest.Increments[i] {
			return false
		}
	}
	return true
}

// runCheckpoint performs one bounded checkpoint effort for the observed
// manifest whose active tail reached the configured threshold (architecture
// section 13). It is opportunistic: it selects an immutable accepted prefix,
// builds and uploads a complete checkpoint pack, and replaces the prefix
// with the checkpoint through normal ETag CAS while preserving every later
// increment. Every failure is recorded in metrics and never changes the
// result of the already accepted commit that scheduled the effort.
func (n *Notebook) runCheckpoint(ctx context.Context, observed storage.Manifest) {
	start := n.now()
	n.metrics.CheckpointRuns.Add(1)
	logger := LoggerFrom(ctx)

	plan := planCheckpoint(observed, n.checkpointPacks)
	pack, err := git.ExportCheckpoint(n.ws.Repo(), plan.head)
	if err != nil {
		n.failCheckpoint(logger, "export checkpoint pack", err)
		return
	}
	cpID, err := n.newID()
	if err != nil {
		n.failCheckpoint(logger, "generate checkpoint id", err)
		return
	}
	key := storage.Key{Kind: storage.KindCheckpoint, Generation: plan.cutoff, ID: cpID}
	meta := storage.Metadata{
		SHA256:     pack.SHA256,
		Size:       uint64(len(pack.Data)),
		Kind:       key.Kind,
		Generation: key.Generation,
	}
	if err := storage.UploadUnique(ctx, n.store, key, bytes.NewReader(pack.Data), meta); err != nil {
		n.failCheckpoint(logger, "upload checkpoint pack", err)
		return
	}

	// The pack is only a proposal until the manifest CAS accepts it. A CAS
	// loss needs only another small manifest rewrite against the new tail:
	// the checkpoint pack is never rebuilt.
	for attempt := 1; ; attempt++ {
		latest, err := n.readRemote(ctx)
		if err != nil {
			n.failCheckpoint(logger, "reread current", err)
			return
		}
		if !plan.prefixPresent(latest.manifest) {
			// Another checkpoint removed the selected prefix: the proposal
			// cannot be referenced by any later manifest. Discard it.
			logger.Warn("checkpoint discarded", "reason", "selected prefix already compacted")
			return
		}
		next := n.compactManifest(latest.manifest, plan, cpID, key, pack)
		data, err := storage.EncodeManifest(next)
		if err != nil {
			n.failCheckpoint(logger, "encode compacted manifest", err)
			return
		}
		n.metrics.CheckpointCASAttempts.Add(1)
		_, err = n.store.ReplaceObject(ctx, storage.CurrentKey, latest.etag, data)
		switch {
		case err == nil:
			if err := git.MarkShallow(n.ws.Repo(), plan.head); err != nil {
				n.failCheckpoint(logger, "record checkpoint boundary", err)
				return
			}
			n.acceptCheckpoint(ctx, next, pack, start, plan.cutoff)
			return
		case errors.Is(err, storage.ErrPreconditionFailed):
			if attempt > n.retryLimit {
				n.failCheckpoint(logger, "checkpoint CAS lost all attempts", err)
				return
			}
			if err := n.waiter.Wait(ctx, attempt); err != nil {
				n.failCheckpoint(logger, "checkpoint retry wait", err)
				return
			}
		case errors.Is(err, storage.ErrTransport):
			if n.checkpointAccepted(ctx, cpID) {
				n.acceptCheckpoint(ctx, next, pack, start, plan.cutoff)
			} else {
				n.failCheckpoint(logger, "checkpoint acceptance unprovable", err)
			}
			return
		default:
			n.failCheckpoint(logger, "checkpoint CAS failed", err)
			return
		}
	}
}

// acceptCheckpoint records one accepted compaction. The authoritative tail
// after a successful compaction is the compacted manifest's tail: the
// pre-checkpoint observation recorded by the triggering commit is stale
// (architecture section 13.1). It also stores the checkpoint size and
// duration metrics and runs the best-effort cleanup.
func (n *Notebook) acceptCheckpoint(ctx context.Context, next storage.Manifest, pack git.Pack, start time.Time, cutoff uint64) {
	n.recordTail(next)
	n.metrics.CheckpointSize.Store(uint64(len(pack.Data)))
	n.metrics.CheckpointDurationNanos.Store(int64(n.now().Sub(start)))
	n.cleanup(ctx, cutoff)
}

// failCheckpoint records one failed checkpoint effort in metrics and as a
// warning. Checkpoint failure never changes the already accepted commit
// result that scheduled the effort (architecture section 13.1).
func (n *Notebook) failCheckpoint(logger *slog.Logger, reason string, cause error) {
	n.metrics.CheckpointFailures.Add(1)
	logger.Warn("checkpoint failed", "reason", reason, "cause", cause.Error())
}

// checkpointAccepted resolves a lost checkpoint CAS response: the effort
// succeeded when the checkpoint ID appears in an active or retained
// checkpoint descriptor of the authoritative manifest (architecture section
// 11.3 applied to checkpoints).
func (n *Notebook) checkpointAccepted(ctx context.Context, cpID storage.UUID) bool {
	data, _, present, err := n.readCurrent(ctx)
	if err != nil || !present {
		return false
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return false
	}
	if m.Checkpoint.ID == cpID {
		return true
	}
	for _, r := range m.Retained {
		if r.Checkpoint.ID == cpID {
			return true
		}
	}
	return false
}

// compactManifest rewrites the latest manifest against the plan: the new
// checkpoint replaces the selected prefix, every later increment is
// preserved, the replaced checkpoint and compacted increments become the
// newest retained generation, and the retained array is trimmed to the
// configured count (architecture sections 13.3 and 14).
func (n *Notebook) compactManifest(latest storage.Manifest, plan checkpointPlan, cpID storage.UUID, key storage.Key, pack git.Pack) storage.Manifest {
	next := latest
	next.Generation = latest.Generation + 1
	next.Checkpoint = storage.Checkpoint{
		ID:                cpID,
		Publication:       plan.publication,
		ThroughGeneration: plan.cutoff,
		Head:              plan.head,
		Key:               key,
		SHA256:            pack.SHA256,
		Size:              uint64(len(pack.Data)),
	}
	next.Increments = append([]storage.Increment(nil), latest.Increments[plan.count:]...)
	next.Head = plan.head
	if len(next.Increments) > 0 {
		next.Head = next.Increments[len(next.Increments)-1].Head
	}
	retained := storage.Retained{
		RetiredAtGeneration: next.Generation,
		Head:                plan.head,
		Checkpoint:          latest.Checkpoint,
		Increments:          append([]storage.Increment(nil), latest.Increments[:plan.count]...),
	}
	next.Retained = append([]storage.Retained{retained}, latest.Retained...)
	if n.retainedCheckpoints < len(next.Retained) {
		next.Retained = next.Retained[:n.retainedCheckpoints]
	}
	return next
}

// cleanup performs the best-effort garbage collection after a successful
// checkpoint CAS (architecture section 14). It lists only the checkpoint
// and increment namespaces, parses candidate generations from valid keys
// (malformed keys are ignored), considers only objects at or before the
// successful checkpoint cutoff, and — before each delete batch — rereads
// and validates current and rebuilds the complete active and retained root
// set. Cleanup failure is observable in metrics only and never fails a
// commit or checkpoint.
func (n *Notebook) cleanup(ctx context.Context, cutoff uint64) {
	n.metrics.CleanupRuns.Add(1)
	logger := LoggerFrom(ctx)
	var candidates []string
	for _, ns := range []string{"packs/checkpoints/", "packs/increments/"} {
		if err := n.store.ListObjects(ctx, ns, func(key string) error {
			k, err := storage.ParseKey(key)
			if err != nil {
				return nil // malformed keys are not cleanup candidates
			}
			if k.Generation <= cutoff {
				candidates = append(candidates, key)
			}
			return nil
		}); err != nil {
			n.failCleanup(logger, "list pack namespaces", err)
			return
		}
	}
	n.metrics.CleanupCandidates.Add(uint64(len(candidates)))

	for start := 0; start < len(candidates); start += cleanupBatchSize {
		end := min(start+cleanupBatchSize, len(candidates))
		batch := candidates[start:end]

		roots, err := n.cleanupRoots(ctx)
		if err != nil {
			n.failCleanup(logger, "reread cleanup roots", err)
			return
		}
		doomed := make([]string, 0, len(batch))
		for _, key := range batch {
			if _, referenced := roots[key]; !referenced {
				doomed = append(doomed, key)
			}
		}
		if len(doomed) == 0 {
			continue
		}
		if err := n.store.DeleteObjects(ctx, doomed); err != nil {
			// The semantic boundary cannot name which keys of a partial
			// batch landed; the failure is recorded at batch granularity
			// and retried on a later checkpoint cleanup. The warning is the
			// observable record the contract requires; the failure never
			// changes the commit result.
			n.failCleanup(logger, "delete cleanup batch", err)
			continue
		}
		n.metrics.CleanupDeleted.Add(uint64(len(doomed)))
	}
}

// failCleanup records one cleanup failure in metrics and as a warning.
// Cleanup is best-effort: a failure is retried on a later checkpoint
// cleanup and never fails the commit that scheduled it (architecture
// section 14).
func (n *Notebook) failCleanup(logger *slog.Logger, reason string, cause error) {
	n.metrics.CleanupErrors.Add(1)
	logger.Warn("cleanup failed", "reason", reason, "cause", cause.Error())
}

// cleanupRoots rereads and validates current and returns the complete set
// of pack keys referenced by the active and retained descriptors. The
// reread before every delete batch prevents a stale listing from deleting
// an object a newer manifest references.
func (n *Notebook) cleanupRoots(ctx context.Context) (map[string]struct{}, error) {
	data, _, present, err := n.readCurrent(ctx)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, errors.New("current manifest disappeared during cleanup")
	}
	m, err := storage.DecodeManifest(data)
	if err != nil {
		return nil, fmt.Errorf("cleanup: current is not a valid manifest: %w", err)
	}
	roots := make(map[string]struct{}, 1+len(m.Increments)+2*len(m.Retained))
	roots[m.Checkpoint.Key.String()] = struct{}{}
	for _, inc := range m.Increments {
		roots[inc.Key.String()] = struct{}{}
	}
	for _, r := range m.Retained {
		roots[r.Checkpoint.Key.String()] = struct{}{}
		for _, inc := range r.Increments {
			roots[inc.Key.String()] = struct{}{}
		}
	}
	return roots, nil
}

// recordTail updates the tail metrics from one validated authoritative
// manifest: the active increment count and the sum of active increment pack
// sizes. Retained tails do not count (architecture section 13.1).
func (n *Notebook) recordTail(m storage.Manifest) {
	var size uint64
	for _, inc := range m.Increments {
		size += inc.Size
	}
	n.metrics.TailCount.Store(uint64(len(m.Increments)))
	n.metrics.TailBytes.Store(size)
}
