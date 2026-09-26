# Checkpoints, shallow history, and cleanup

Each normal commit adds one small incremental pack to the manifest's active tail. An unbounded tail makes a cold pull slow and request-heavy, so once the tail reaches a threshold the committing process compacts its oldest increments into one state-complete checkpoint pack, swaps it into `current` with the same ETag CAS as commits, keeps the replaced generation as a retained root, and then deletes storage no manifest references. All of it is best-effort and can never change the result of the commit that triggered it. This doc answers "when does a checkpoint run, what does it write, what does it keep, and what may cleanup delete".

Read this when: changing the checkpoint trigger, compaction, retention count, shallow boundaries, cleanup rules, or the related metrics.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/checkpoint.go` | `checkpointPlan`, `planCheckpoint`, `prefixPresent`, `runCheckpoint`, `acceptCheckpoint`, `failCheckpoint`, `checkpointAccepted`, `compactManifest`, `cleanup`, `failCleanup`, `cleanupRoots`, `recordTail`, `cleanupBatchSize` |
| `internal/notebook/commit.go` | Trigger at the end of `attemptPublication`; `buildFirstProposal` (the generation-1 checkpoint) |
| `internal/notebook/remote.go` | `importRemote` (imports checkpoint then tail; `MarkShallow`), `readRemote` (stale-pack restart) |
| `internal/notebook/metrics.go` | `Checkpoint*` and `Cleanup*` counters, `TailCount`, `TailBytes` |
| `internal/notebook/notebook.go` | `DefaultCheckpointPacks` (256), `MinCheckpointPacks`, `DefaultRetainedCheckpoints` (1), `MaxRetainedCheckpoints` (64) |
| `internal/git/pack.go` | `ExportCheckpoint`, `MarkShallow`, `ValidateHistory` |
| `internal/git2/engine.go` | `repository.MarkShallow`, `reloadShallowGrafts` |
| `internal/git2/native.go` | `libgit2MarkShallow` (appends to the repo's `shallow` file) |
| `internal/storage/manifest.go` | `Manifest.Checkpoint`, `Increments`, `Retained`, `Retained.RetiredAtGeneration`; `validateManifest`: cross-field rules the compacted manifest must satisfy (`EncodeManifest` refuses otherwise) |
| `internal/storage/key.go` | `ParseKey`, `KindCheckpoint`, `KindIncrement` |
| `internal/storage/store.go` | `ListObjects`, `DeleteObjects` |

## Flow

```text
attemptPublication (after Accept succeeded)
  recordTail(proposal.manifest)
  len(Increments) >= checkpointPacks → runCheckpoint(ctx, proposal.manifest)
    planCheckpoint: oldest `threshold` increments; cutoff = last selected generation
    git.ExportCheckpoint(repo, plan.head) → pack
    storage.UploadUnique(packs/checkpoints/<cutoff>-<cpID>.pack)
    loop attempt:
      readRemote → prefixPresent? no → discard (warn) and return
      compactManifest(latest, plan) → EncodeManifest → ReplaceObject(current, latest.etag)
        ok                    → git.MarkShallow(plan.head) → acceptCheckpoint → cleanup(cutoff)
        ErrPreconditionFailed → retry (bounded by retryLimit, same waiter) with the same pack
        ErrTransport          → checkpointAccepted(cpID)? acceptCheckpoint : failCheckpoint
        other                 → failCheckpoint
cleanup(cutoff)
  ListObjects("packs/checkpoints/"), ListObjects("packs/increments/")
    → ParseKey (malformed ignored) → keep Generation <= cutoff as candidates
  per batch of 1000: cleanupRoots (reread + validate current) → delete unreferenced → DeleteObjects
```

## Behavior

### Trigger

- Only an accepted, changing commit can trigger: after local acceptance, if the new manifest's active increment count is at least `checkpointPacks` (`--checkpoint-packs`, default 256, minimum 1), one effort runs. Retained tails do not count. A no-change commit, a pull, or a failed commit never triggers.
- The effort runs synchronously inside the triggering `Commit` call. Every failure increments `CheckpointFailures`, logs a warning through `LoggerFrom(ctx)`, and returns; the commit's `OK` is already decided.
- Only the pack count triggers. `TailBytes` is recorded so a byte threshold can be added later without a storage format change.

### What a checkpoint contains

- `ExportCheckpoint(head)` writes the head commit, its complete tree closure, and every referenced blob; it omits all commit ancestors, refs, and tags. The pack has no external delta base, so it imports into an empty repository and makes the head commit and full tree readable.
- The checkpoint head becomes a shallow boundary: its parents may be absent. `git.MarkShallow` appends the OID to the repository's `shallow` file and `git2` reopens the repository (`reloadShallowGrafts`) because libgit2 reads that file only on open. Readers call `MarkShallow(m.Checkpoint.Head)` after importing the checkpoint pack, before importing increments.
- `ValidateHistory(head, shallow)` fails on any missing commit, tree, or blob except the parents of the declared boundary. With libgit2, every commit listed in the `shallow` file (every past checkpoint head this repository recorded) reads with zero parents, so the walk ends at the first such commit. Later increments use the checkpoint head as a parent.
- Result: a checkpoint preserves current file state and supports new increments. It does not preserve permanent history.

```text
before: C0 -> I1 -> I2 -> ... -> I256 -> I257 -> I258
              \______ compacted ______/   \_ tail _/
after:  C1(at I256) -> I257 -> I258
```

### Stable-prefix compaction

- `planCheckpoint` fixes an immutable prefix of the triggering manifest: its oldest `threshold` increments. The cutoff is the last selected increment's generation; the new checkpoint's head and publication ID are that increment's.
- The pack is built and uploaded once, before any manifest change. Normal writers are never blocked.
- `compactManifest(latest, plan)`: generation `latest.Generation + 1`; the new `Checkpoint{ID: cpID, Publication, ThroughGeneration: cutoff, Head, Key, SHA256, Size}`; `Increments` = everything after the prefix (preserved, including increments added since the trigger); `Head` = last remaining increment's head, or the checkpoint head if none remain; a new `Retained{RetiredAtGeneration: new generation, Head: plan.head, Checkpoint: old checkpoint, Increments: the compacted prefix}` prepended to the existing retained list, trimmed to `retainedCheckpoints`.
- The replacement is an ordinary ETag CAS. A lost CAS rereads `current` and rewrites only the small manifest; the checkpoint pack is never rebuilt. If the latest manifest no longer starts with the exact selected prefix (`prefixPresent`: another checkpoint at or past the cutoff won), the proposal is discarded and its pack becomes an orphan; this logs a warning but does not count in `CheckpointFailures`. Competing checkpoint workers are safe: at most one replacement wins.
- `EncodeManifest` runs `validateManifest`, so the compacted manifest must hold: generation at least 1; positive pack sizes; each descriptor key's kind, generation, and ID match the descriptor; unique keys and checkpoint IDs; increment generations consecutive from the checkpoint cutoff with each parent equal to the preceding head; `Head` equal to the final tail head; checkpoint cutoff not above the generation; a publication ID never bound to two heads; retained entries with strictly decreasing `RetiredAtGeneration`, each above its own final content generation and below the manifest generation plus one, and each retained `Head` equal to its chain's final head.
- A lost CAS response is resolved by searching active and retained checkpoint descriptors for `cpID` (`checkpointAccepted`); unprovable acceptance counts as a failure.

### Retention

- slivingdoc promises current-state durability, not historical recovery.
- `--retained-checkpoints` (default 1, range 0..64) counts previous generations kept in addition to the active one; 0 keeps only the active generation. Each retained entry holds the replaced checkpoint, the complete ordered increment tail it compacted, and the head, so it can rebuild the exact state the newer checkpoint replaced. This is what lets a stale reader that observed an older manifest finish, or restart.
- Example with the default: C0 becomes C1, keep C0 and C1 storage; C1 becomes C2, keep C1 and C2, then C0 storage is deletable.

### Cleanup

- Runs only after a successful checkpoint CAS followed by a successful local `MarkShallow`, or after a proved lost-response acceptance, with the checkpoint's cutoff.
- Lists only `packs/checkpoints/` and `packs/increments/` (following every continuation in the store), parses each key's generation, ignores malformed keys, and considers only keys with generation at or before the cutoff. `current` is never a candidate and orphans after the cutoff are never touched.
- Before each delete batch of at most 1,000 keys, it rereads and strictly decodes `current` and rebuilds the full root set (active checkpoint, active increments, every retained checkpoint and increment). Only unreferenced candidates are deleted, so a stale listing can never delete a pack a newer manifest references. Deletable candidates include retired packs and never-accepted proposals.
- Failures are recorded (`CleanupErrors`, warning) at batch granularity and retried by a later checkpoint's cleanup. If checkpoints never succeed, cleanup never runs and old proposals remain.
- Readers never depend on timing for safety: a reader that hits a deleted pack rereads `current` and restarts ([pull.md](./pull.md)).
- S3 versioning may keep deleted versions; lifecycle rules for noncurrent versions are the operator's concern. Incomplete multipart uploads are not cleanup's concern: `s3store` aborts best-effort on any failure after the upload's creation, on a context detached from the request's cancellation, but a failed abort or a process that dies mid-upload can leave one behind, and only a bucket lifecycle rule removes it ([s3store.md](./s3store.md)).

### Metrics

`CheckpointRuns`, `CheckpointFailures`, `CheckpointCASAttempts`, `CheckpointSize` (last accepted pack bytes), `CheckpointDurationNanos` (selection to acceptance), `CleanupRuns`, `CleanupCandidates`, `CleanupDeleted`, `CleanupErrors`; `acceptCheckpoint` re-records the tail from the compacted manifest because the triggering commit's observation is stale.

## Gotchas

- Checkpoint keys use the cutoff (through-generation), increment keys the chain position; neither is the manifest generation. Cleanup relies on the generation embedded in the key, so a new pack kind must embed a comparable generation.
- The first publication already writes a checkpoint (`ThroughGeneration: 1`), so every manifest has a checkpoint; `importRemote` always imports one.
- On a lost-response checkpoint that is proved accepted, `runCheckpoint` does not call `MarkShallow` locally; the next `readRemote` marks the boundary when it imports the new manifest.
- `runCheckpoint` shares `retryLimit` and the backoff waiter with commits, so a busy notebook can extend the latency of the commit that triggered it by up to the full retry budget.
- The threshold default is 256 in code (`DefaultCheckpointPacks`), which also bounds how many increments a cold pull downloads while checkpoints succeed; failed checkpoints let the tail grow past it.
- If the local `MarkShallow` after a successful checkpoint CAS fails, the effort counts as a failure and cleanup is skipped although the manifest is accepted. The next `readRemote` records the boundary, and a later checkpoint's cleanup reclaims the storage.

## Related

- [commit.md](./commit.md), [pull.md](./pull.md), [notebook.md](./notebook.md), [git-engine.md](./git-engine.md), [guarantees.md](./guarantees.md)
- [running.md](./running.md#checkpoints-and-retention) (`--checkpoint-packs`, `--retained-checkpoints`)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow) (checkpoint and cleanup, synchronous inside the triggering commit), [Architecture](../AGENTS.md#architecture) (runtime layout of `packs/`)
- Other concerns: [storage.md](./storage.md), [s3store.md](./s3store.md)
