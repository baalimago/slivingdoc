package notebook

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Workspace is the notebook's narrow view of a managed visible path. The
// interface is consumer-owned and path-free: the notebook never touches the
// caller's path directly, only the workspace's locked operations. The real
// implementation is internal/workspace.Workspace; tests use the real
// workspace over a fake engine, so only the Git engine and the object store
// are ever faked.
type Workspace interface {
	Snapshot(ctx context.Context) (git.Snapshot, error)
	Baseline() workspace.Baseline
	Repo() git.Repository
	Accept(ctx context.Context, baseline workspace.Baseline) error
	Materialize(ctx context.Context, baseline workspace.Baseline, tree git.OID) error
	Recover(ctx context.Context, baseline workspace.Baseline) error
	RecoveryRequired() bool
	CacheDir() string
	Pulled() bool
	MarkPulled(ctx context.Context) error
}

// Config wires one notebook. RetryLimit is the number of CAS attempts after
// the first (the application resolves the default; the notebook validates
// the range 0..100, where 0 means a single attempt). CheckpointPacks is the
// active-tail length that triggers one checkpoint effort (the application
// resolves the default 1,024; the notebook requires at least 1, so an
// explicitly configured zero fails). RetainedCheckpoints is the number of
// previous checkpoint generations kept as cleanup roots (the application
// resolves the default 1; the notebook validates 0..64). NewID and Now
// default to storage.NewUUIDv7 and time.Now; tests inject deterministic
// sources.
type Config struct {
	// Workspace is the managed visible path.
	Workspace Workspace
	// Store is the semantic object-store boundary.
	Store storage.ObjectStore
	// RetryLimit bounds CAS retries after the first attempt.
	RetryLimit int
	// CheckpointPacks triggers one checkpoint effort when the active tail
	// length reaches this count.
	CheckpointPacks int
	// RetainedCheckpoints keeps this many previous checkpoint generations
	// as cleanup roots in addition to the active generation.
	RetainedCheckpoints int
	// NewID produces protocol publication and checkpoint IDs.
	NewID func() (storage.UUID, error)
	// Now is the operation-attempt clock for commit timestamps.
	Now func() time.Time
	// Waiter sleeps between CAS retries; nil uses bounded full-jitter
	// exponential backoff. Tests inject a deterministic waiter.
	Waiter BackoffWaiter
	// Failpoints injects deterministic failures; nil disables injection.
	Failpoints *Failpoints
}

// DefaultRetryLimit is the application-resolved CAS retry bound.
const DefaultRetryLimit = 8

// DefaultCheckpointPacks is the application-resolved checkpoint threshold:
// the active tail length that schedules one checkpoint effort.
const DefaultCheckpointPacks = 1024

// DefaultRetainedCheckpoints is the application-resolved retention count:
// the number of previous checkpoint generations kept in addition to the
// active generation.
const DefaultRetainedCheckpoints = 1

// MaxRetryLimit, MinCheckpointPacks, and MaxRetainedCheckpoints are the
// documented operational ranges (architecture section 17). The application
// package validates its flags against the same values, so the two range
// checks cannot drift.
const (
	MaxRetryLimit          = 100
	MinCheckpointPacks     = 1
	MaxRetainedCheckpoints = 64
)

// MaxMessageBytes is the notes_commit message byte bound (architecture
// section 2). The MCP schema advertises the same value.
const MaxMessageBytes = 16384

const (
	minRetryLimit     = 0
	defaultBackoffMin = 25 * time.Millisecond
	defaultBackoffMax = 2 * time.Second
)

// Notebook executes pull and commit against one workspace and one store.
// All methods are safe for concurrent use; per-path serialization comes
// from the workspace operation lock. Checkpoint scheduling is opportunistic
// and never determines commit success (architecture section 13).
type Notebook struct {
	ws                  Workspace
	store               storage.ObjectStore
	retryLimit          int
	checkpointPacks     int
	retainedCheckpoints int
	newID               func() (storage.UUID, error)
	now                 func() time.Time
	waiter              BackoffWaiter
	failpoints          *Failpoints
	metrics             *Metrics
}

// New validates the configuration and returns a ready notebook.
func New(cfg Config) (*Notebook, error) {
	if cfg.Workspace == nil {
		return nil, errors.New("notebook: workspace is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("notebook: store is required")
	}
	if cfg.RetryLimit < minRetryLimit || cfg.RetryLimit > MaxRetryLimit {
		return nil, fmt.Errorf("notebook: retry limit %d is outside %d..%d", cfg.RetryLimit, minRetryLimit, MaxRetryLimit)
	}
	if cfg.CheckpointPacks < MinCheckpointPacks {
		return nil, fmt.Errorf("notebook: checkpoint packs threshold %d must be at least %d", cfg.CheckpointPacks, MinCheckpointPacks)
	}
	if cfg.RetainedCheckpoints < 0 || cfg.RetainedCheckpoints > MaxRetainedCheckpoints {
		return nil, fmt.Errorf("notebook: retained checkpoints %d is outside %d..%d", cfg.RetainedCheckpoints, 0, MaxRetainedCheckpoints)
	}
	newID := cfg.NewID
	if newID == nil {
		newID = storage.NewUUIDv7
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	waiter := cfg.Waiter
	if waiter == nil {
		waiter = newExponentialBackoff(defaultBackoffMin, defaultBackoffMax)
	}
	return &Notebook{
		ws:                  cfg.Workspace,
		store:               cfg.Store,
		retryLimit:          cfg.RetryLimit,
		checkpointPacks:     cfg.CheckpointPacks,
		retainedCheckpoints: cfg.RetainedCheckpoints,
		newID:               newID,
		now:                 now,
		waiter:              waiter,
		failpoints:          cfg.Failpoints,
		metrics:             &Metrics{},
	}, nil
}

// Metrics returns the operational measurements of the notebook.
func (n *Notebook) Metrics() *Metrics { return n.metrics }

// RecoveryStage names identify the failed stage in a recovery report.
const (
	stageEntry    = "entry"
	stagePull     = "pull.accept"
	stageCommit   = "commit.accept"
	stageCAS      = "commit.cas"
	stageConflict = "merge.materialize"
)

// entryRecovery runs the authoritative resynchronization the next MCP call
// must perform before any pull or commit work when P requires recovery
// (architecture section 15). A successful repair clears the flag and the
// caller proceeds; a failed repair returns RECOVERY_FAILURE.
func (n *Notebook) entryRecovery(ctx context.Context) error {
	report, err := n.recoverState(ctx, stageEntry, RemoteAcceptedUnknown)
	if err != nil {
		return recoveryFailure(report.public(), err)
	}
	return nil
}

// applyLocal runs one workspace local mutation and maps its outcome: a
// failure after the mutation started (the workspace marks recovery durably
// before touching L) triggers the generic recovery path and returns
// RECOVERY_FAILURE; a failure before any mutation maps to the plain error.
// stage names the failed operation and accepted states remote acceptance.
func (n *Notebook) applyLocal(ctx context.Context, stage string, accepted RemoteAccepted, fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}
	if !n.ws.RecoveryRequired() {
		return err
	}
	report, _ := n.recoverState(ctx, stage, accepted)
	return recoveryFailure(report.public(), err)
}

// failAfterAccept handles a failure between the proved manifest acceptance
// and the local acceptance: the remote accepted the proposal, so the
// generic recovery path runs unconditionally and the call reports
// RECOVERY_FAILURE even when resynchronization succeeds (architecture
// section 15).
func (n *Notebook) failAfterAccept(ctx context.Context, stage string, cause error) error {
	report, _ := n.recoverState(ctx, stage, RemoteAcceptedYes)
	return recoveryFailure(report.public(), cause)
}

// mapLocalError maps a workspace error that occurred before any local
// mutation. Invalid visible content is a caller-input error; any other
// workspace failure is a local-state failure the caller cannot fix by
// editing files.
func (n *Notebook) mapLocalError(err error) error {
	if errors.Is(err, workspace.ErrInvalidContent) ||
		errors.Is(err, workspace.ErrSymlink) ||
		errors.Is(err, workspace.ErrUnsupportedFile) ||
		errors.Is(err, workspace.ErrInvalidPath) {
		return &Error{Code: CodeInvalidRequest, Message: "visible files violate the notebook contract", Cause: err}
	}
	return &Error{Code: CodeStorageFailure, Message: "local private state operation failed", Cause: err}
}

// validateMessage applies the notes_commit message contract before any scan
// or S3 access: non-blank (not only Unicode white space), at most 16,384
// bytes, valid UTF-8 without U+0000.
func validateMessage(message string) error {
	if len(message) > MaxMessageBytes {
		return invalidRequest("commit message exceeds %d bytes", MaxMessageBytes)
	}
	if strings.TrimSpace(message) == "" {
		return invalidRequest("commit message must not be blank")
	}
	if err := git.ValidateCommitMessage(message); err != nil {
		return &Error{Code: CodeInvalidRequest, Message: "invalid commit message", Cause: err}
	}
	return nil
}

// rejectMarkers returns the conflicted files of a snapshot: every complete
// conflict-marker block with its exact path and row ranges (architecture
// section 12). The check runs before any Git or S3 mutation.
func rejectMarkers(snap git.Snapshot) []ConflictFile {
	var files []ConflictFile
	for _, f := range snap.Files {
		if ranges := git.FindConflictBlocks(f.Data); len(ranges) > 0 {
			files = append(files, ConflictFile{Path: f.Path, Ranges: ranges})
		}
	}
	return files
}

// materializeTree converts a conflicted merge result into a tree the
// workspace can materialize: the full result with markers, resolved blobs,
// and local file/directory sides.
func (n *Notebook) materializeTree(merged git.MergeResult) (git.OID, error) {
	snap, err := git.MaterializeTree(n.ws.Repo(), merged)
	if err != nil {
		return git.OID{}, fmt.Errorf("notebook: materialize merge result: %w", err)
	}
	tree, err := git.BuildTree(n.ws.Repo(), snap)
	if err != nil {
		return git.OID{}, fmt.Errorf("notebook: build materialized tree: %w", err)
	}
	return tree, nil
}
