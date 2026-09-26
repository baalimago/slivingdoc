# Notebook orchestration

`internal/notebook` composes one workspace (L and P), the Git seam, and the object store into the two public operations. It owns the policy: when to read `current`, how to merge, when to publish, how to prove acceptance, when to recover, and when to checkpoint. It owns no filesystem layout (that is `internal/workspace`) and no native code (that is `internal/git2`). This doc answers "what is a `Notebook`, how is it configured, and where does each cross-cutting piece (remote read, result, metrics, failpoints, backoff, errors, logging) live".

Read this when: wiring a notebook, changing a default or range, adding a metric, failpoint or error reason, or finding the function behind a pull/commit step.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/notebook.go` | `Workspace` (consumer-owned interface), `Config`, `New`, `Notebook`, defaults and ranges, `ValidateMessage`, `holdWorkspace`, `entryRecovery`, `applyLocal`, `failAfterAccept`, `mapLocalError`, `rejectMarkers`, `materializeTree`, stage constants |
| `internal/notebook/pull.go` | `Pull`, `pinProtected`. See [pull.md](./pull.md) |
| `internal/notebook/commit.go` | `Commit`, `attemptPublication`, `buildProposal`, `publish`, `enforcePolicy`. See [commit.md](./commit.md) |
| `internal/notebook/remote.go` | `remoteState`, `readRemote`, `readCurrent`, `importRemote`, `prefetchPacks`, `ensurePack`, `cacheRead`, `cacheWrite`, `lookupPublication`, `recoverState` |
| `internal/notebook/checkpoint.go` | `runCheckpoint`, `compactManifest`, `cleanup`, `cleanupRoots`, `recordTail`. See [checkpoints.md](./checkpoints.md) |
| `internal/notebook/result.go` | `Result{Generation, Stat}`, `diffStat` |
| `internal/notebook/metrics.go` | `Metrics`: atomic counters and gauges |
| `internal/notebook/failpoints.go` | `Failpoints{CAS}` |
| `internal/notebook/backoff.go` | `BackoffWaiter`, `exponentialBackoff.Wait` |
| `internal/notebook/errors.go` | `Error`, `Code`, `Reason`, `FileReason`, `Action`, constructors (`invalidRequest`, `contentConflict`, `storageIntegrity`, `storageFailure`, `remoteBusy`, `recoveryFailure`), `errCASLost`, `errStaleManifest` |
| `internal/notebook/logger.go` | `WithLogger`, `LoggerFrom` (context logger for checkpoint, cleanup and cache warnings) |
| `internal/app/service.go` | `Service.notebookFor`: the only production constructor call |

Tests: `fake_test.go` (fake engine; the real workspace runs over it), `native_test.go` (real libgit2), `load_test.go` (benchmarks), plus per-concern `*_test.go`.

## Flow

```text
app.Service.notebookFor(path)
  → workspace.Open(ctx, workspace.Config{...})
  → notebook.New(notebook.Config{Workspace: ws, Store, RetryLimit, CheckpointPacks,
                                  RetainedCheckpoints, ReadOnlyPaths, WritablePaths, Failpoints})
Notebook.Pull(ctx)   → see pull.md
Notebook.Commit(ctx, message) → see commit.md
  both: RecoveryRequired()? → entryRecovery → recoverState → RECOVERY_FAILURE (stage entry)
        readRemote → readCurrent → storage.DecodeManifest → importRemote
                     → prefetchPacks → ensurePack (cache or ReadObject, verify)
                     → git.ImportPack(checkpoint) → git.MarkShallow(checkpoint head)
                     → git.ImportPack(each increment)
                     → git.ValidateHistory → ReadCommit(head) → git.ReadSnapshot(head tree)
```

## Behavior

### The `Notebook` type

- One `Notebook` per opened workspace path, built by `New`, held by `app.Service` for the process lifetime. All methods are safe for concurrent use; there is no mutex in `Notebook` itself.
- Fields: `ws Workspace`, `store storage.ObjectStore`, `retryLimit`, `checkpointPacks`, `retainedCheckpoints`, `policy git.PathPolicy`, `readOnly git.EntrySet` (only for refusal wording), `newID`, `now`, `waiter`, `failpoints`, `metrics`.
- `Workspace` is the consumer-owned view: `Snapshot`, `Baseline`, `Repo`, `Accept`, `Materialize`, `Recover`, `RecoveryRequired`, `CacheDir`, `Pulled`, `MarkPulled`. The real implementation is `*workspace.Workspace`; tests use it over a fake `git.Repository`, so only the engine and the object store are ever faked.

### Configuration (`Config`, validated by `New`)

| Field | Default | Range enforced by `New` |
|-------|--------------------------------------|-------------------------|
| `RetryLimit` | `DefaultRetryLimit` = 8 | 0..`MaxRetryLimit` (100); 0 means one attempt |
| `CheckpointPacks` | `DefaultCheckpointPacks` = 256 | at least `MinCheckpointPacks` (1) |
| `RetainedCheckpoints` | `DefaultRetainedCheckpoints` = 1 | 0..`MaxRetainedCheckpoints` (64) |
| `ReadOnlyPaths`, `WritablePaths` | empty | `git.NewPolicy` must accept them |
| `NewID` | `storage.NewUUIDv7` | |
| `Now` | `time.Now` | |
| `Waiter` | `newExponentialBackoff(25ms, 2s)` | |
| `Failpoints` | nil (disabled) | |

`RetryLimit`, `CheckpointPacks`, and `RetainedCheckpoints` have no fallback in `New`: `internal/app` resolves their defaults from `--commit-retries`, `--checkpoint-packs`, and `--retained-checkpoints` (see [running.md](./running.md#configuration)), and `internal/app/config.go` imports these constants so flag validation and `New` cannot drift. `NewID`, `Now`, and `Waiter` fall back inside `New` when nil. `Workspace` and `Store` are required.

### Remote read (`remote.go`)

- `readRemote` is the only way the notebook learns R. It reads `current` (absent means the implicit generation-0 state: `emptyRemote`, canonical empty tree, no head), strictly decodes it, imports the checkpoint pack, marks the checkpoint head shallow, imports every increment in manifest order, validates history from `m.Head` down to the shallow boundary, and proves the head tree is valid notebook text before returning. It also updates the tail metrics (`recordTail`). Every call re-imports the checkpoint and every active increment into the private repository; the byte cache only skips the download (see [pull.md](./pull.md)).
- A referenced pack that returns `ErrNotFound` yields `errStaleManifest`: the reader rereads `current` and restarts only if the ETag changed; an unchanged manifest is `STORAGE_INTEGRITY`/`PACK_INVALID`. Restarts are bounded by `retryLimit`.
- Pack bytes come from the byte cache when the file named by the SHA-256 has the right size and a fresh SHA-256 match (`cacheRead`; a mismatch deletes the entry). Otherwise they are downloaded, checked against descriptor size and SHA-256, and cached through temp file + rename (`cacheWrite`). A cache write failure is only a warning.
- `prefetchPacks` runs up to 16 downloads ahead of the sequential importer; `next()` yields packs in manifest order and every participant honors cancellation.

### Result (`result.go`)

`Result{Generation, Stat}` is valid only with a nil error; every error path returns the zero `Result`. `diffStat(base, result)` reads both trees and calls `git.DiffSnapshots`; it is computed before any local mutation or publication, so a read failure aborts with nothing changed (`STORAGE_INTEGRITY`/`ENGINE_FAILED`).

### Metrics (`metrics.go`)

`Metrics` fields are `atomic` values: tail gauges `TailCount`, `TailBytes` (active increments only, last observed manifest); checkpoint `CheckpointRuns`, `CheckpointFailures`, `CheckpointCASAttempts`, `CheckpointSize`, `CheckpointDurationNanos`; cleanup `CleanupRuns`, `CleanupCandidates`, `CleanupDeleted`, `CleanupErrors`. Read through `Notebook.Metrics()`. Nothing in production exports them yet; tests and the load harness read them.

### Failpoints (`failpoints.go`)

`Failpoints.CAS` fires after the manifest CAS accepted and before local acceptance. An error there goes through `failAfterAccept` (stage `commit.cas`, `remoteAccepted=yes`). The workspace keeps its own boundary failpoints; see [guarantees.md](./guarantees.md#failpoints).

### Backoff (`backoff.go`)

`BackoffWaiter.Wait(ctx, attempt)` sleeps between CAS retries (commit and checkpoint). The default is full jitter: the ceiling starts at 25 ms, doubles per attempt up to 2 s, and the wait is uniform in `[0, ceiling)`, so zero is valid. A cancelled context returns its error, which aborts the commit. Tests inject a deterministic waiter.

### Errors and recovery (`errors.go`, `notebook.go`)

- Almost every failure is a `*notebook.Error` with `Code`, `Reason`, `Action` (from `actionForPairing`; for `RECOVERY_FAILURE`, `PULL` when resynchronized, else `RETRY`), `Message`, `Files`, optional `Recovery`, and `Cause` (unwrapped for `errors.Is`). Two exceptions come back unwrapped: a workspace error from `applyLocal` raised before the workspace set its recovery flag (never after a proved CAS, which always goes through `failAfterAccept`), and the context error from a cancelled backoff wait. `mcp.MapError` maps the first to `STORAGE_FAILURE`/`INTERNAL` (`RETRY`) and treats cancellation as a protocol error. See [product-contract.md](./product-contract.md).
- `storageFailure` turns a store refusal cause (`storage.ErrQuotaExceeded`, `ErrRequestLimit`, `ErrRateLimited`, `ErrAccessDenied`, `ErrTooLarge`) into its own reason and message via `storeRefusal`, appending a `storage.Refusal` message after `The storage says:` ([errors.md](./errors.md), [hosted-mode.md](./hosted-mode.md)).
- `mapLocalError`: workspace `ErrInvalidContent`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidPath` become `INVALID_REQUEST`/`INVALID_CONTENT` with the offending path from `ScanError`; any other workspace error before mutation is `STORAGE_FAILURE`/`LOCAL_STATE`.
- `applyLocal(ctx, stage, accepted, fn)` wraps every call that mutates L. It runs `recoverState` and returns `RECOVERY_FAILURE` only when `ws.RecoveryRequired()` shows the flag was already durable; a failure before that (reading the target tree, staging; a cancelled request stays a protocol error; the operation lock is already held, so no lock wait fails here) returns the plain error. `failAfterAccept` handles every failure between a proved CAS and the end of local acceptance and always runs `recoverState` and returns `RECOVERY_FAILURE`. See [guarantees.md](./guarantees.md).

### Logging (`logger.go`)

The notebook never takes a logger at construction. `WithLogger` attaches a logger to the context: the MCP handler's request-scoped logger (carrying `mcpReqID`), or on the CLI the notebook module logger (`Runtime.Pull`, `Runtime.Commit`); `LoggerFrom` returns it or a discard logger. Only best-effort paths log: checkpoint and cleanup warnings, and cache-write failures.

## Gotchas

- Checkpoint and cleanup run synchronously inside the `Commit` call that triggered them (not in a goroutine), so the commit's latency includes them even though their outcome cannot change its result.
- `readRemote` re-imports every active pack on each call (pull, every commit attempt, checkpoint reread), so its cost grows with the tail length until a checkpoint compacts it. Imported objects are harmless cache, never state, even when the caller later fails.
- The notebook has no lock of its own: `Pull` and `Commit` hold the workspace operation lock for the whole operation (`holdWorkspace` over `Workspace.Hold`) and pass the held context to every workspace call; see [workspace.md](./workspace.md). `holdWorkspace` maps a failure to take the lock through `mapLocalError`, so a closed workspace, a lock-file error, or a lock wait ended by cancellation or a deadline is `STORAGE_FAILURE`/`LOCAL_STATE` before anything changed.

## Related

- [pull.md](./pull.md), [commit.md](./commit.md), [conflicts.md](./conflicts.md), [checkpoints.md](./checkpoints.md), [guarantees.md](./guarantees.md)
- [workspace.md](./workspace.md), [git-engine.md](./git-engine.md), [overview.md](./overview.md)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow), [Logging](../AGENTS.md#logging)
- Other concerns: [storage.md](./storage.md), [errors.md](./errors.md), [logging.md](./logging.md)
