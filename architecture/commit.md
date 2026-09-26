# Commit and optimistic publication

`notes_commit` publishes the caller's edits in L and folds in concurrent, non-conflicting changes by others. It validates locally, merges (A = accepted baseline, L, R = observed `current`), builds a commit and one immutable pack, uploads the pack, and then makes the only accepting move: a conditional write of `current` against the ETag it read (compare-and-swap). A lost race rereads R and rebuilds everything; a lost response is resolved by reading `current` back. `OK` is returned only when acceptance is proved. This doc answers "what does a commit do, how does it retry, and how does it decide a result it cannot see".

Read this when: changing commit validation order, proposal construction, the manifest CAS, retry and backoff, publication proof, or the first-publication path.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/commit.go` | `Commit`, `attemptPublication`, `proposal`, `buildProposal`, `buildFirstProposal`, `buildIncrementProposal`, `uploadProposal`, `publish`, `mapUploadError`, `enforcePolicy`, `restoreProtected`, `policyRefusal`, `rejectNewFoldedPairs`, `newFoldedPairFiles` |
| `internal/notebook/remote.go` | `readRemote`, `lookupPublication` |
| `internal/notebook/notebook.go` | `ValidateMessage`, `rejectMarkers`, `applyLocal`, `failAfterAccept` |
| `internal/notebook/backoff.go` | `exponentialBackoff.Wait` |
| `internal/notebook/errors.go` | `errCASLost`, `remoteBusy`, `ReasonPublicationUnproven` |
| `internal/git/commit.go` | `CreateCommit`, `ValidateCommitMessage` |
| `internal/git/pack.go` | `ExportIncrement`, `ExportCheckpoint`, `MarkShallow` |
| `internal/git/merge.go` | `Merge`, `FindConflictBlocks` |
| `internal/storage/upload.go` | `UploadUnique`, `VerifyObject` |
| `internal/storage/manifest.go` | `Manifest`, `Checkpoint`, `Increment`, `EncodeManifest`, `CurrentKey` |
| `internal/storage/store.go` | `ObjectStore.CreateObject` / `ReplaceObject`, `ErrPreconditionFailed`, `ErrTransport` |
| `internal/workspace/workspace.go` | `Accept`, `Materialize`, `Pulled` |

## Flow

```text
Notebook.Commit(ctx, message)
  0. holdWorkspace → ws.Hold(ctx): the op lock, held until the result
  1. RecoveryRequired()? → entryRecovery → RECOVERY_FAILURE stage entry (always; no commit runs)
  2. ValidateMessage(message)                    INVALID_REQUEST MESSAGE_*
  3. ws.Pulled()?                                INVALID_REQUEST PULL_REQUIRED
  4. ws.Snapshot → rejectMarkers                 CONTENT_CONFLICT UNRESOLVED_MARKERS
  5. git.BuildTree → localTree
  6. enforcePolicy(local, localTree)             reset via applyLocal(stage commit.readonly,
                                                 remoteAccepted=no) + INVALID_REQUEST READ_ONLY_PATH
  7. baseTree = Baseline().Tree; attemptStart = now()
  8. loop attempt = 1..retryLimit+1:
       attemptPublication:
         readRemote → git.Merge(baseTree, localTree, remote.tree)
           conflict  → materializeTree → applyLocal(Materialize(R baseline, tree)) → CONTENT_CONFLICT
           merged == remote.tree → applyLocal(Accept(R baseline)) → Result{R gen, empty stat}
           diffStat(remote.tree, merged.Tree)
           rejectNewFoldedPairs(remote.tree, merged.Tree) → INVALID_REQUEST/INVALID_CONTENT for a new case-folded file/directory pair
           buildProposal: gen 0 → buildFirstProposal | else buildIncrementProposal
           uploadProposal → storage.UploadUnique
           publish: EncodeManifest → CreateObject (gen 0) | ReplaceObject(etag)
             ErrPreconditionFailed → errCASLost → casLost=true
             ErrTransport → lookupPublication(pubID): found → accepted | else PUBLICATION_UNPROVEN
             other → STORAGE_FAILURE MANIFEST_WRITE
           Failpoints.CAS → failAfterAccept
           Accept(proposal.baseline) failure → failAfterAccept(stageCommit)
           recordTail; tail >= checkpointPacks → runCheckpoint (see checkpoints.md)
       casLost: attempt > retryLimit → REMOTE_BUSY; else waiter.Wait(attempt) and loop
```

## Behavior

### Local validation (no S3 access, no mutation)

- Order is fixed: recovery, message, pulled marker, snapshot, conflict markers, tree build, protected paths. At most one refusal per call; a marker block anywhere is reported before a protected-path violation, so no reset happens while markers remain.
- A commit needs the durable `pulled` marker in P (`Workspace.Pulled`). Commit never creates P state from an unknown L; `PULL_REQUIRED` involves no S3 request. A conflicting pull also sets the marker.
- `rejectMarkers` refuses every complete `<<<<<<< local` / `=======` / `>>>>>>> remote` block with its path and row ranges, whoever wrote it. This is why no accepted state can hold an unresolved conflict. See [conflicts.md](./conflicts.md).
- `enforcePolicy` compares `localTree` with the baseline under the path policy; see [product-contract.md](./product-contract.md#read-only-and-writable-paths).

### One publication attempt

- Each attempt starts from a fresh `readRemote`, so it imports any new tail before merging. A and L stay fixed across attempts; only R moves.
- **No-change result.** If the merge equals R (the caller changed nothing, or only what R already has), L and P are synchronized to R with `Accept` and the call returns `OK` with R's generation and an empty diffstat. No publication ID, commit, pack, or CAS is created.
- **First publication** (`remote.generation == 0`): a root commit (no parents), a state-complete checkpoint pack (`ExportCheckpoint`), `MarkShallow(head)`, key `packs/checkpoints/1-<cpID>.pack`, and a generation-1 manifest whose checkpoint has `ThroughGeneration: 1`, `Publication: pubID`, and an empty increment tail and retained list. Published with `CreateObject` (`If-None-Match: *`).
- **Normal publication**: a commit whose single parent is the observed R head, an incremental pack of exactly the objects reachable from the new head and not from R's head (`ExportIncrement`), key `packs/increments/<incGen>-<pubID>.pack`, and a manifest with `Generation = R.generation + 1`, the new head, and the new increment appended. Published with `ReplaceObject(current, observedETag)`.
- **Increment generation is not the manifest generation.** `incGeneration = Checkpoint.ThroughGeneration + len(Increments) + 1`, the position in the active chain; the manifest generation also advances on checkpoint replacements.
- **Commit metadata.** Author and committer are `slivingdoc <slivingdoc@localhost>` (`git.AuthorName`, `git.AuthorEmail`); time is the operation-attempt start (`attemptStart`, taken once per `Commit`), UTC, offset zero, one-second precision.
- **Pack before manifest.** `UploadUnique` writes the pack with `slivingdoc` metadata (SHA-256, size, kind, generation) before any manifest names it. A pack alone publishes nothing; an unreferenced pack is an orphan proposal that cleanup may delete once a later checkpoint's cutoff is at or above its key generation.
- **Diffstat.** Computed from R's tree to the merged tree before the upload, so a read failure aborts with no remote or local change.
- **Case-folded file/directory pairs.** A clean merge can pair a file from one writer with a directory from another whose names differ only in case (`P` from this caller, `p/x.md` from R). Before the upload, `rejectNewFoldedPairs` reads the merged snapshot and runs `git.FoldedDirectoryPairs`; every pair R does not already hold whole is refused as `INVALID_REQUEST`/`INVALID_CONTENT` (action `EDIT_FILES`) naming both paths of each pair, sorted (`newFoldedPairFiles`). Nothing was uploaded and L and P are unchanged, so the caller renames one side and commits again. A pair R already holds is tolerated; see [git-engine.md](./git-engine.md) for how such a pair reaches L and how an operator repairs one under a protected path. The check reads the merged snapshot on every publishing attempt, and R's only when the merged state holds a pair.

### Compare-and-swap

- `current` is replaced only when its ETag still equals the one `readRemote` observed. S3 accepts exactly one replacement per observed ETag, so concurrent writers that read the same manifest race only at this single small write; everything before it runs concurrently. There is no lock object, lease, renewal, or clock-based expiry.
- A precondition failure is normal contention, not a storage error: `publish` returns `errCASLost` and `Commit` waits (`BackoffWaiter`, full jitter from 25 ms to 2 s) and runs a fresh attempt. After `retryLimit` retries (`--commit-retries`, default 8, so 9 attempts) the result is `REMOTE_BUSY`/`RETRIES_EXHAUSTED` and L is untouched.
- Every retry rebuilds the whole proposal: new publication ID, generation, key, commit, and pack. A losing attempt's pack is never republished at a later generation.

### Uncertain responses

- **Lost CAS response** (`ErrTransport` from the write): read `current` and search the active checkpoint, every active increment, and every retained checkpoint and increment for the publication ID (`lookupPublication`). Found means accepted; not found is `STORAGE_FAILURE`/`PUBLICATION_UNPROVEN` with action `PULL`. The proposal is not republished and L is preserved; the write may still have landed (for example if its descriptors already aged out of retention). If `lookupPublication`'s own read fails, that error is returned as is (for example `STORAGE_FAILURE`/`MANIFEST_READ`, action `RETRY`, or `STORAGE_INTEGRITY`/`MANIFEST_INVALID`) while acceptance stays unproven; a retry builds a new proposal.
- **Lost pack-upload response**: `UploadUnique` reads the unique key back and proves size and SHA-256; matching bytes are reused, absence is a transport failure (`PACK_UPLOAD`), different bytes are `STORAGE_INTEGRITY`/`PACK_INVALID`. Metadata alone is never proof.
- `OK` is never returned while acceptance is uncertain.

### After acceptance

- `Accept(proposal.baseline)` rewrites L to the exact accepted merged tree and records its generation, head, and tree in `state.json`. Any `Accept` failure goes through `failAfterAccept`: `RECOVERY_FAILURE` with stage `commit.accept` and `remoteAccepted=yes`, whether it happened after the workspace's recovery flag was durable or before it (reading the target tree, staging, a cancelled request). The failpoint path (stage `commit.cas`) does the same with stage `commit.cas`. In every case the publication is already accepted; the immediate resynchronization rewrites L to it, and when that cannot run (a cancelled request) the report says `resynchronized=false` and the next pull or commit converges.
- The result is `Generation` = new manifest generation and `Stat` = R tree to merged tree.
- When the accepted active tail length reaches `checkpointPacks`, one checkpoint effort runs before `Commit` returns; its outcome never changes the result.

## Gotchas

- `buildFirstProposal` calls `git.MarkShallow` before the CAS. If that CAS loses, the local `shallow` file keeps an extra boundary for an unpublished commit; it is harmless because that commit is a root commit and no accepted manifest references it. libgit2 reads every shallow-listed commit with zero parents, so do not rely on the shallow file as a record of accepted state.
- Objects from losing attempts stay in the private repository. They are never referenced by accepted state.
- `attemptStart` is taken once, so all attempts of one call share the commit time; the commit OID still differs per attempt because the parent differs.
- A conflict during a commit rewrites L and records R as the new baseline just like pull; the caller resolves and commits again without pulling.

## Related

- [conflicts.md](./conflicts.md), [pull.md](./pull.md), [checkpoints.md](./checkpoints.md), [guarantees.md](./guarantees.md), [notebook.md](./notebook.md)
- [git-engine.md](./git-engine.md) for pack export and commit creation
- [running.md](./running.md#configuration) (`--commit-retries`)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow), [Conventions](../AGENTS.md#conventions)
- Other concerns: [storage.md](./storage.md), [s3store.md](./s3store.md), [errors.md](./errors.md)
