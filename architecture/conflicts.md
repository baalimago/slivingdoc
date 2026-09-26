# Merge and conflicts

Every pull and every commit attempt runs one three-tree merge: base A (the accepted baseline tree in P), local L (the scanned visible directory), remote R (the tree of `current`'s head). libgit2 merges the trees; `internal/git/merge.go` turns its merge index into structured conflicts with exact marker content and row ranges; the notebook writes the full result, markers included, to L and returns `CONTENT_CONFLICT`. Commit separately refuses any complete marker block already in L. This doc answers "how is a conflict detected, what lands on disk, and how does it reach the MCP and CLI caller".

Read this when: changing merge semantics, marker format or detection, file-versus-directory handling, conflict materialization, or the conflict error shape.

## Key files

| File | Purpose |
|------|---------|
| `internal/git/merge.go` | `Merge`, `materializeConflict`, `formatDeleteConflict`, `isDirFileConflict`, `belowDirFileConflict`, `MaterializeTree`, `FindConflictBlocks`, marker constants `markerOpen`/`markerSep`/`markerClose` |
| `internal/git/types.go` | `MergeIndex`, `IndexEntry` (stages 0..3), `MergeFileResult`, `MergeResult`, `Conflict`, `MarkerRange` |
| `internal/git2/native.go` | `libgit2MergeTrees` (`git_merge_trees`, flags 0), `libgit2MergeFile` via C `sl_merge_file` (labels `base`/`local`/`remote`) |
| `internal/notebook/pull.go` | Conflict branch of `Pull` |
| `internal/notebook/commit.go` | Conflict branch of `attemptPublication`; marker check in `Commit` |
| `internal/notebook/notebook.go` | `rejectMarkers`, `materializeTree`, stage `stageConflict` (`merge.materialize`) |
| `internal/notebook/errors.go` | `contentConflict`, `contentConflictFiles`, `ReasonMergeConflict`, `ReasonUnresolvedMarkers`, `FileReasonTextConflict`, `FileReasonPathConflict`, `FileReasonUnresolvedMarkers` |
| `internal/mcp/errors.go` | `mapNotebookError`: files and ranges into `ToolError` |
| `internal/mcp/server.go` | `errorText`: `file: <path> · <reason> · lines a-b` |
| `internal/app/command.go` | `writeError`, `fileReasonWords` (`TEXT_CONFLICT` → "conflict", `PATH_CONFLICT` → "path conflict") |

## Flow

```text
git.Merge(repo, base, local, remote)
  → repo.MergeTrees  (git2: git_merge_trees, no rename detection, no merge-base discovery)
  → reject any index entry mode other than 100644/040000 (UnsupportedModeError)
  → group entries by path; for each path with a stage != 0:
       isDirFileConflict? → Conflict{Path}                       (no content)
       else materializeConflict:
            a side missing (modify/delete) → formatDeleteConflict → FindConflictBlocks
            else repo.MergeFile(base, local, remote) → Content; FindConflictBlocks only if !Automergeable
  → no conflicts: MergeResult{Tree, Index}; else MergeResult{Index, Conflicts} (Tree zero)

Notebook (pull or commit attempt), on len(Conflicts) > 0:
  materializeTree → git.MaterializeTree → git.BuildTree
  → applyLocal(stageConflict, ws.Materialize(remote.baseline(), tree))
  → [pull only] ws.MarkPulled
  → contentConflict(ReasonMergeConflict, msg, contentConflictFiles(conflicts))
  → mcp.MapError → ToolError{CONTENT_CONFLICT, MERGE_CONFLICT, EDIT_FILES, files[]}
     or app.Report → writeError

Commit precheck: ws.Snapshot → rejectMarkers (FindConflictBlocks per file)
  → contentConflict(ReasonUnresolvedMarkers, …, FileReasonUnresolvedMarkers + ranges)
```

## Behavior

### Merge semantics

- Local change is A → L, remote change is A → R, result is merge(A, L, R). The engine receives three explicit trees; there is no history-based merge base, no rename detection (`opts.flags = 0`), no external merge driver, Git config, or attributes.
- Changes to different files, or compatible changes to different lines of one file, produce one merged tree. The caller never sees a conflict for them.
- Conflicts are identified from the merge index (entries with stage 1/2/3), never from a text scan of results.
- A merged index with a mode other than `100644` or `040000` is an error, so a hostile pack cannot smuggle a symlink or submodule into a result.

### Marker format

A complete block is exactly these three lines at column zero, in order:

```text
<<<<<<< local
the caller's text
=======
the accepted remote text
>>>>>>> remote
```

- `FindConflictBlocks` accepts LF and CRLF, ignores the terminator when comparing, finds every complete non-nested block, and returns one-based inclusive `{Start, End}` rows. A nested opener inside a block is content. Changing any character of any signature line makes the block ordinary text, which is how a note can quote a marker example.
- libgit2's merge-file labels are set to `local` and `remote` in `sl_merge_file`. For modify/delete and delete/modify, libgit2 drops the label of the empty side, so `formatDeleteConflict` writes the block itself with exact labels and the deleted side empty.

### What lands in L (`MaterializeTree`)

- A conflicted merge is always materialized in full: resolved paths get their merged blobs, text conflicts get marker content, and nothing is left out except as below. Pull never reverts L.
- File versus directory (one side has a file at `p`, the other has a directory `p/…`): reported as a `PATH_CONFLICT` with no content and no ranges. libgit2 represents it as a lone blob stage at `p` plus index entries below it (the directory side's files). At `p` and below, `MaterializeTree` writes only stage-2 (local) blob entries and omits every other entry, so the result depends on the direction. Local file versus remote directory: the local file stays and the remote directory is omitted, assuming the index shape `internal/git/merge_test.go` builds (stage 2 at `p`, stage 0 below). `TestMergeFileDirectoryConflict` in `internal/git2/operations_test.go` proves natively only that this conflict is reported without markers. Local directory versus remote file: the local subtree survives only if its entries carry stage 2; `TestMaterializeTreeFileDirectoryKeepsLocalSide` in `internal/git/merge_test.go` and the notebook fake assume that with a hand-built index, and no native test covers this direction. When the local side survives, the omitted side is the remote one and is still in R; if real libgit2 does not stage the local directory's entries as 2, the local subtree is dropped and is not recoverable from R.
- `ValidateSnapshot` runs on the result, so a materialized conflict is still valid notebook content.
- `Materialize` records `remote.baseline()` as the new baseline in the same failure-atomic operation. On the retry, the resolved directory is the new local intent against that R; if R moved again another three-tree merge runs. `current` is never updated by a conflicting call.

### How conflicts surface

| Situation | Code / reason | File reason | Ranges | Message (notebook) |
|-----------|---------------|-------------|--------|--------------------|
| Pull merge conflict | `CONTENT_CONFLICT` / `MERGE_CONFLICT` | `TEXT_CONFLICT` or `PATH_CONFLICT` | marker rows / empty | "Resolve the conflict blocks in the visible files before continuing." |
| Commit merge conflict | `CONTENT_CONFLICT` / `MERGE_CONFLICT` | same | same | "Resolve the conflict blocks before notes_commit." |
| Commit finds existing markers | `CONTENT_CONFLICT` / `UNRESOLVED_MARKERS` | `UNRESOLVED_MARKERS` | marker rows | "Resolve the conflict blocks before notes_commit." |

- Action is always `EDIT_FILES`; `retryable` is false. `contentConflictFiles` picks `TEXT_CONFLICT` when `Conflict.Content != nil`, else `PATH_CONFLICT`.
- MCP: `files[]` entries carry the notebook-relative path, reason, and `ranges` (always an array, empty for path conflicts); the text item lists `file: <path> · <REASON> · lines 12-18`.
- CLI: `CONTENT_CONFLICT · MERGE_CONFLICT`, the message, one aligned line per file with the reason as words and `lines 12-18`, then `next: edit the files, then commit`, exit nonzero.
- `rejectMarkers` runs on every commit before any Git or S3 work and applies to blocks the process did not create (for example after a restart, or typed by a human). This is what guarantees no accepted state contains an unresolved conflict.
- Under a protected (read-only) path the merge never conflicts: pull pins those paths to the baseline before merging, so local and remote sides agree. A marker block there is still refused by commit as `UNRESOLVED_MARKERS` before the protected-path check runs.

## Gotchas

- `MergeResult.Tree` is the zero OID whenever there are conflicts; callers must use `MaterializeTree`, never `ReadSnapshot(res.Tree)`.
- A failure while writing the conflicted result to L, after the workspace's recovery flag is durable, is `RECOVERY_FAILURE` with stage `merge.materialize`, not `CONTENT_CONFLICT`. A failure before the flag (reading the target tree, staging; a cancelled request stays a protocol error; the operation lock is already held, so no lock wait fails here) returns the plain workspace error (`STORAGE_FAILURE`/`INTERNAL` over MCP unless it is a context error) with L unchanged. A `materializeTree` failure (building the conflicted snapshot) is `STORAGE_INTEGRITY`/`ENGINE_FAILED`.
- `Merge` keeps `Index` on the result so `MaterializeTree` can rebuild the full conflicted state; do not drop it when passing results around.
- Commit's marker check looks at the whole visible snapshot, so a note that must contain a literal example block needs one signature character changed.

## Related

- [pull.md](./pull.md), [commit.md](./commit.md), [git-engine.md](./git-engine.md), [workspace.md](./workspace.md), [product-contract.md](./product-contract.md)
- [running.md](./running.md#conflict-recovery): the operator and human view of resolving conflicts
- `../AGENTS.md`: [Conventions](../AGENTS.md#conventions) (marker-block invariant, error taxonomy)
- Other concerns: [mcp-server.md](./mcp-server.md), [cli.md](./cli.md), [errors.md](./errors.md)
