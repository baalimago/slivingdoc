# Pull

`notes_pull` brings the visible directory L up to the accepted remote state R while keeping the caller's unpublished edits. It scans L, reads and validates `current`, imports only the descriptor packs the private repository lacks (downloading only those absent from the pack-byte cache), runs one three-tree merge (accepted baseline A, L, R), rewrites L with the full merge result, and records R as the new baseline. A conflict still rewrites L (with markers) and still advances the baseline. This doc answers "what exactly happens, in what order, on `notes_pull`".

Read this when: changing pull ordering, remote reading, the pack cache, the pulled marker, the first-pull behavior, or protected-path restore on pull.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/pull.go` | `Notebook.Pull`, `Notebook.pullAttempt`, `guardFirstPull`, `pinProtected` |
| `internal/notebook/commit.go` | `restoreProtected` (used by `pinProtected`) |
| `internal/notebook/remote.go` | `readRemote`, `readCurrent`, `emptyRemote`, `loadRemote`, `reuseAccepted`, `validateRemote`, `importRemote` (`importMissing`, `importAll`), `prefetchPacks`, `ensurePack`, `cacheRead`, `cacheWrite`, `remoteState.baseline` |
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
       loadRemote: reuseAccepted: head == baseline head → HasObject(each descriptor head)
         → ReadCommit(head) == baseline tree → done
         (no import, no history walk, no tree read, no graft); any gap falls through to:
         importRemote(importMissing): HasObject(each descriptor head) → skip present packs;
         prefetchPacks → ensurePack (cache | ReadObject + size/SHA-256) →
         git.ImportPack(checkpoint if absent) → git.MarkShallow(checkpoint head) → git.ImportPack(each absent increment)
         missing pack → errStaleManifest → reread current; restart only if ETag moved
       validateRemote: git.ValidateHistory(head, checkpoint head) → ReadCommit(head) → git.ReadSnapshot(head tree)
         validation fails → importRemote(importAll) once (repair) → validateRemote again
  4b. guardFirstPull(local, remote)       → first pull only: INVALID_REQUEST/DIRECTORY_NOT_EMPTY
                                             unless L is empty, R is empty, or L ⊆ R byte for byte
  5. pinProtected(local, localTree)       → merge local side (protected paths pinned to baseline)
  6. git.Merge(repo, Baseline().Tree, mergeTree, remote.tree)
  7a. conflicts: materializeTree → applyLocal(stageConflict, ws.Materialize(R baseline, tree))
                 → ws.MarkPulled → CONTENT_CONFLICT/MERGE_CONFLICT
  7b. clean:     diffStat(localTree, merged.Tree)
                 → applyLocal(stagePull, ws.Materialize(R baseline, merged.Tree))
                 → ws.MarkPulled → Result{Generation: R generation, Stat}
```

## Behavior

- **Order of checks.** Recovery first, then the visible scan, then the remote read. An invalid visible file (bad UTF-8, U+0000, symlink, special file, invalid name (unless an ignore rule names the entry), file-versus-directory ambiguity, two names that normalize to one NFC path, two paths equal under case folding, a file whose name folds to a directory's) is refused as `INVALID_REQUEST`/`INVALID_CONTENT` naming the file before any S3 request (`mapLocalError`, `scanErrorFiles`); a case-folding collision names both paths.
- **Baseline is the merge base.** A = `ws.Baseline().Tree` from `state.json`, never a file snapshot. On the first pull A is the canonical empty tree `4b825dc642cb6eb9a060e54bf8d69288fbee4904` (`workspace.EmptyTreeID`), so any file in L would be a local addition.
- **First-pull guard.** A first pull (no `pulled` marker in P) proceeds only when every file in L passes (`guardFirstPull`): an unprotected file must exist in R with identical bytes, or R must be the empty tree (no `current`, or an accepted state with no files: the directory seeds the notebook); a protected file must exist in R, whose bytes the pull restores whatever L holds (`pinProtected`), and its content is not compared. A protected file R lacks is refused as `NOT_IN_NOTEBOOK` even against an empty R: its first-pull baseline is empty, so the pull would delete it, and no commit from this process could publish it. Otherwise it returns `INVALID_REQUEST`/`DIRECTORY_NOT_EMPTY` (action `FIX_INPUT`) naming every offending file, sorted by path, with file reason `NOT_IN_NOTEBOOK` or `DIFFERS_FROM_NOTEBOOK` and empty ranges; the message says which of the two it found. The refusal comes after the remote read and before any change to L, the `pulled` marker, or `state.json`: no marker, no baseline, so a later commit still gets `PULL_REQUIRED`. The remote read may already have imported R into P's repository and filled the pack cache; that is invisible state the next pull reuses. A directory that is not a copy of this notebook would otherwise be merged against the empty baseline, publishing its unrelated files on the next commit or conflicting add/add with the notebook. Every later pull merges local additions as usual. Because the guard admits only identical files, a first pull can no longer conflict.
- **Empty remote.** No `current` object means R is the empty tree at generation 0. Pull keeps valid local additions in L, records the empty tree as the baseline, marks P pulled, and creates no remote state.
- **An unchanged head is reused, not re-validated.** When the manifest head equals the workspace's accepted baseline head, `reuseAccepted` proves presence instead of walking history: every descriptor head commit exists (a pack import is atomic, so a present head proves its pack) and the head commit names the baseline tree. It does not walk or read the tree: the diffstat, the merge and the materialization read every object they need anyway, and an engine failure there drives one strict reload, so paying for a third read of the head tree on every warm pull would buy nothing. The baseline was validated when it was accepted and Git objects are immutable, so only what the store can still supply can have changed. The checkpoint boundary is deliberately *not* recorded on this path: only a validated history proves the manifest's checkpoint head is an ancestor of the accepted head, and a wrong graft would silently truncate every later walk. A first pull (empty baseline) never takes this path. Any gap is a miss that falls through to the import-and-validate path below, so whole-pack damage is repaired, not reported. Damage the sweep cannot see — an object lost or unreadable inside a pack whose head survives — and a shallow graft table this handle loaded before another process appended a boundary surface as an engine failure in the caller that reads the object: the diffstat for a pull, the export for a commit. Both then retry once with `loadStrict`, which validates, reloads the graft table, re-imports every pack and repairs (`Notebook.pull` / `Notebook.commit`, `engineFailed`), and `recoverState` always loads strictly. Every engine failure on both paths precedes any local mutation, so a retry never runs over a half-written L; a failure after it is `RECOVERY_FAILURE`, which `engineFailed` does not match.
- **Only packs the repository lacks are imported.** `importRemote` asks the private repository for each descriptor's head commit (`HasObject`) and imports only the packs whose head is absent: a pack import is atomic, so a present head proves its pack landed whole. An unchanged tail therefore costs one existence check per descriptor and no import; a new publication costs exactly its own pack. `MarkShallow` still runs after the checkpoint step every time (it is a no-op when the boundary is already recorded).
- **Only packs missing from the byte cache are downloaded.** A pack that must be imported is fetched from the pack-byte cache (the shared identity-keyed directory, or P-local `pack-cache/` on a host without one, see [running.md](./running.md#the-shared-pack-cache)) when it holds bytes with the exact size and a fresh SHA-256, else from the store. A corrupt entry is deleted and refetched. If the cache cannot be written, every call downloads again (warning only). Downloads may overlap (16 in flight); import order is the manifest order: checkpoint, shallow boundary, increments.
- **The repository is a cache, never an authority.** When the head moved, validation walks the whole accepted history. If it fails because objects are missing (`validationObjectsMissing`), `loadRemote` re-imports every descriptor pack from its verified bytes once (`importAll`) and validates again, so a private repository whose pack files were deleted or damaged heals itself from the cache or the store; only a second failure, or a head tree that is present but not valid notebook text (`validationContentInvalid`, which no import can change), is reported as `STORAGE_INTEGRITY`.
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
- `readRemote` no longer re-imports present packs, and an unchanged head skips the history walk entirely; when the head moved, `ValidateHistory` still visits every commit from the head to the shallow boundary, so that part of the cost scales with the tail length until a checkpoint compacts it.
- A present head commit is taken as proof of its whole pack. That holds because `ImportPack` is atomic and because the notebook's own commits are built locally before they are published. `BuildTree` writes L's blobs and trees loose before the remote read, so an object the store lost but L still holds is present again by the time the reuse path looks; that is harmless (the object is there), but it means a test for object damage must remove the file from L too.
- When the head moved, `readRemote` validates the whole history from head to the shallow boundary; `ValidateHistory` shares one seen set so the cost is the number of unique objects, not commits times files. A reused head skips that walk, so a process that never needs a pack no longer audits it: a cold reader is the only detector of a corrupt remote pack.
- `MarkPulled` failure is reported as `STORAGE_FAILURE`/`LOCAL_STATE` even though L and the baseline were already updated.
- `readCurrent` treats `ErrNotFound` on `current` as the empty notebook, so a backend must return it only for a key that is really absent. The hosted adapter does so only for a 404 with reason `no_object`; a space the token cannot reach (404 `no_space`, or any 404 without that reason) is `ErrAccessDenied`, so the pull fails as `STORAGE_FAILURE`/`ACCESS_DENIED` before any merge and L is untouched ([hosted-mode.md](./hosted-mode.md)).

## Related

- [notebook.md](./notebook.md) (remote read details), [conflicts.md](./conflicts.md), [commit.md](./commit.md), [workspace.md](./workspace.md), [git-engine.md](./git-engine.md), [guarantees.md](./guarantees.md), [product-contract.md](./product-contract.md#read-only-and-writable-paths)
- [running.md](./running.md#conflict-recovery) (operator view of conflicts), [running.md](./running.md#the-shared-pack-cache)
- `../AGENTS.md`: [Event Flow](../AGENTS.md#event-flow)
- Other concerns: [storage.md](./storage.md)
