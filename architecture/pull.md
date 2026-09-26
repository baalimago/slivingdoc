# Pull

`notes_pull` brings the visible directory L up to the accepted remote state R while keeping the caller's unpublished edits. It scans L, reads and validates `current`, downloads only packs absent from the pack-byte cache, imports the complete descriptor chain, runs one three-tree merge (accepted baseline A, L, R), rewrites L with the full merge result, and records R as the new baseline. A conflict still rewrites L (with markers) and still advances the baseline. This doc answers "what exactly happens, in what order, on `notes_pull`".

Read this when: changing pull ordering, remote reading, the pack cache, the pulled marker, the first-pull behavior, or protected-path restore on pull.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/pull.go` | `Notebook.Pull`, `pinProtected` |
| `internal/notebook/commit.go` | `restoreProtected` (used by `pinProtected`) |
| `internal/notebook/remote.go` | `readRemote`, `readCurrent`, `emptyRemote`, `importRemote`, `prefetchPacks`, `ensurePack`, `cacheRead`, `cacheWrite`, `remoteState.baseline` |
| `internal/notebook/notebook.go` | `entryRecovery`, `applyLocal`, `mapLocalError`, `materializeTree` |
| `internal/notebook/result.go` | `diffStat` |
| `internal/workspace/scan.go` | `Workspace.Snapshot`, `scanLocked`, `scanWalk`, `readVisibleFile` |
| `internal/workspace/materialize.go` | `applyLocked` (via `Workspace.Materialize`) |
| `internal/workspace/workspace.go` | `Workspace.Materialize`, `MarkPulled`, `Baseline`, `CacheDir` |
| `internal/git/tree.go` | `BuildTree`, `ReadSnapshot` |
| `internal/git/merge.go` | `Merge`, `MaterializeTree` |
| `internal/git/pack.go` | `ImportPack`, `MarkShallow`, `ValidateHistory` |
| `internal/git/policy.go` | `PathPolicy.ChangedProtected`, `RestoreProtected` |
| `internal/mcp/server.go`, `cmd/pull/pull.go`, `internal/app/app.go` | Entry points: `handler.pull`; the CLI command calling `Runtime.Pull` |

## Flow

```text
mcp handler.pull → Service.Pull(path)
  | cmd/pull → app.Runtime.Pull (attaches the notebook logger) → Service.Pull(path)
  → notebookFor(path) → Notebook.Pull(ctx)
  0. holdWorkspace → ws.Hold(ctx)          → the op lock, held until the result (mapLocalError on failure)
  1. ws.RecoveryRequired()? → entryRecovery → recoverState → RECOVERY_FAILURE stage entry (always; no pull runs)
  2. ws.Snapshot(ctx)                     → scan L under the held op lock (mapLocalError on failure)
  3. git.BuildTree(repo, local)           → localTree
  4. readRemote(ctx)
       readCurrent → absent: emptyRemote() (gen 0, EmptyTreeID) → skip to 5
       storage.DecodeManifest
       importRemote: prefetchPacks → ensurePack (cache | ReadObject + size/SHA-256) →
         git.ImportPack(checkpoint) → git.MarkShallow(checkpoint head) → git.ImportPack(each increment)
         missing pack → errStaleManifest → reread current; restart only if ETag moved
       git.ValidateHistory(head, checkpoint head) → ReadCommit(head) → git.ReadSnapshot(head tree)
  5. pinProtected(local, localTree)       → merge local side (protected paths pinned to baseline)
  6. git.Merge(repo, Baseline().Tree, mergeTree, remote.tree)
  7a. conflicts: materializeTree → applyLocal(stageConflict, ws.Materialize(R baseline, tree))
                 → ws.MarkPulled → CONTENT_CONFLICT/MERGE_CONFLICT
  7b. clean:     diffStat(localTree, merged.Tree)
                 → applyLocal(stagePull, ws.Materialize(R baseline, merged.Tree))
                 → ws.MarkPulled → Result{Generation: R generation, Stat}
```

## Behavior

- **Order of checks.** Recovery first, then the visible scan, then the remote read. An invalid visible file (bad UTF-8, U+0000, symlink, special file, invalid name, file-versus-directory ambiguity, two names that normalize to one NFC path, two paths equal under case folding) is refused as `INVALID_REQUEST`/`INVALID_CONTENT` naming the file before any S3 request (`mapLocalError`, `scanErrorFiles`); a case-folding collision names both paths.
- **Baseline is the merge base.** A = `ws.Baseline().Tree` from `state.json`, never a file snapshot. On the first pull A is the canonical empty tree `4b825dc642cb6eb9a060e54bf8d69288fbee4904` (`workspace.EmptyTreeID`), so existing valid files in L are local additions; an add/add at one path conflicts normally.
- **Empty remote.** No `current` object means R is the empty tree at generation 0. Pull keeps valid local additions in L, records the empty tree as the baseline, marks P pulled, and creates no remote state.
- **Only packs missing from the byte cache are downloaded.** Every `readRemote` re-imports the checkpoint and every active increment into the private repository; only the download is skipped when the pack-byte cache (P-local `pack-cache/`, or the shared identity-keyed directory with `--shared-pack-cache`, see [running.md](./running.md#the-shared-pack-cache)) holds bytes with the exact size and a fresh SHA-256. A corrupt entry is deleted and refetched. If the cache cannot be written, every call downloads again (warning only). Downloads may overlap (16 in flight); import order is the manifest order: checkpoint, shallow boundary, increments.
- **Stale reads.** If cleanup deleted a pack the observed manifest references, the reader rereads `current` and restarts; the same ETag with the pack still missing is `STORAGE_INTEGRITY`/`PACK_INVALID`. It never guesses state from object names and never loops forever (bounded by the retry limit).
- **Validation before L changes.** `ValidateHistory` proves every commit, tree, and blob from the head down to the first shallow-listed commit (libgit2 reads every commit in the `shallow` file with zero parents); `ReadSnapshot` proves the head tree is valid text with safe paths and modes. Validation failures are `STORAGE_INTEGRITY` (`MANIFEST_INVALID`, `PACK_INVALID`, `HISTORY_INVALID`; `ENGINE_FAILED` if recording the shallow boundary fails); download or read failures are `STORAGE_FAILURE` (`MANIFEST_READ`, `PACK_DOWNLOAD`). L is untouched either way.
- **Full materialization.** L becomes the complete merge result: clean paths hold merged content, conflicted paths hold marker content. Empty directories disappear (Git does not store them). See [workspace.md](./workspace.md) for how L is rewritten in place.
- **Baseline always advances to R.** Clean or conflicted, `Materialize` durably records `remote.baseline()` (generation, head, tree). A conflict therefore never leaves the caller behind R: after resolving markers the next commit merges against that R. Pull never restores the pre-call bytes of L.
- **Pulled marker.** Both the clean and the conflict path call `ws.MarkPulled`, which is what makes a later `notes_commit` legal (`PULL_REQUIRED` otherwise).
- **Result.** `Generation` is R's generation; `Stat` is the diffstat from the raw scanned L to the merged tree, so it shows remote changes arriving and protected-path restores, while kept local edits (present on both sides) do not appear. A conflict returns the zero `Result` with the error.
- **Protected paths.** With `--read-only-paths` or `--writable-paths`, `pinProtected` replaces every changed protected path in the local side with baseline content (or removes it) before the merge, so the merge takes R there unconditionally and never reports a conflict under a protected path. With no policy configured it returns `localTree` unchanged.
- **Failure mapping.** Merge or materialize engine errors are `STORAGE_INTEGRITY`/`ENGINE_FAILED`. A failure inside `Materialize` after the recovery flag is durable becomes `RECOVERY_FAILURE` with stage `pull.accept` (clean) or `merge.materialize` (conflict) and `remoteAccepted=no`. A failure before the flag (reading the target tree, staging; a cancelled request stays a protocol error; the operation lock is already held, so no lock wait fails here) returns the plain workspace error, reported as `STORAGE_FAILURE`/`INTERNAL` unless it is a context error; L and P are unchanged.

## Gotchas

- The op lock is held from step 0 to the result, so another pull or commit on the same path waits; it does not stop a process that edits L directly, and such an edit between step 2 and step 7 is overwritten without being merged. The contract says callers edit only between tool calls.
- `diffStat` runs before the clean-path `Materialize` on purpose: a read failure must abort while L and P are untouched. Keep new presentation work before the mutation.
- `readRemote` re-imports every active pack on every pull and commit attempt, so cost scales with the tail length until a checkpoint compacts it.
- `readRemote` validates the whole history from head to the shallow boundary on every pull; `ValidateHistory` shares one seen set so the cost is the number of unique objects, not commits times files.
- `MarkPulled` failure is reported as `STORAGE_FAILURE`/`LOCAL_STATE` even though L and the baseline were already updated.

## Related

- [notebook.md](./notebook.md) (remote read details), [conflicts.md](./conflicts.md), [commit.md](./commit.md), [workspace.md](./workspace.md), [git-engine.md](./git-engine.md), [guarantees.md](./guarantees.md), [product-contract.md](./product-contract.md#read-only-and-writable-paths)
- [running.md](./running.md#conflict-recovery) (operator view of conflicts), [running.md](./running.md#the-shared-pack-cache)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow)
- Other concerns: [storage.md](./storage.md)
