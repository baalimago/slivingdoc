package notebook

import "sync/atomic"

// Metrics exposes the operational measurements of architecture sections 13
// and 16: the active tail shape, checkpoint efforts, and cleanup results.
// The notebook records every value; tests read them and a later MCP or
// observability layer can sample them. Values are monotonic counters or
// last-value gauges, safe for concurrent use.
type Metrics struct {
	// TailCount is the active increment count of the last observed
	// authoritative manifest. Retained tails do not count.
	TailCount atomic.Uint64
	// TailBytes is the sum of the active increment pack sizes of the last
	// observed authoritative manifest.
	TailBytes atomic.Uint64

	// CheckpointRuns counts scheduled checkpoint efforts.
	CheckpointRuns atomic.Uint64
	// CheckpointFailures counts checkpoint efforts that ended without a
	// successful manifest replacement. The failure never changes an
	// already accepted commit result.
	CheckpointFailures atomic.Uint64
	// CheckpointCASAttempts counts manifest CAS attempts by checkpoint
	// workers.
	CheckpointCASAttempts atomic.Uint64
	// CheckpointSize is the byte size of the last successfully indexed
	// checkpoint pack.
	CheckpointSize atomic.Uint64
	// CheckpointDurationNanos is the wall duration of the last successful
	// checkpoint effort, from selection to manifest acceptance.
	CheckpointDurationNanos atomic.Int64

	// CleanupRuns counts cleanup runs after a successful checkpoint CAS.
	CleanupRuns atomic.Uint64
	// CleanupCandidates counts listed pack keys at or before the cleanup
	// cutoff.
	CleanupCandidates atomic.Uint64
	// CleanupDeleted counts pack keys deleted by cleanup.
	CleanupDeleted atomic.Uint64
	// CleanupErrors counts failed delete batches and aborted cleanup
	// runs. A failed batch may have deleted part of its keys; the
	// semantic boundary cannot name them, so the error is recorded at
	// batch granularity and retried on a later checkpoint cleanup.
	CleanupErrors atomic.Uint64
}
