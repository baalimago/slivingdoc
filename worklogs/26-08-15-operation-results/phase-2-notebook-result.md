# Phase 2 — Notebook result and plumbing

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§10 Pull, §11 Commit

## Goal

Compute one `notebook.Result` per successful pull or commit and thread it to
every consumer without changing the externally visible `OK` result yet.

## Specification

Add to `internal/notebook`:

```go
type Result struct {
    Generation uint64
    Stat       git.DiffStat
}
```

Change the notebook operation signatures:

```go
func (n *Notebook) Pull(ctx context.Context) (Result, error)
func (n *Notebook) Commit(ctx context.Context, message string) (Result, error)
```

Semantics:

- `Pull` success returns `Result{Generation: remote.generation}` where
  `Stat` is `git.DiffStat(local, materialized)`: the before-pull visible
  snapshot versus the after-pull materialized result. On conflict the result
  is the zero value and the error is the existing `CONTENT_CONFLICT`.
- `Commit` success returns the result of the winning attempt:
  `Result{Generation: <accepted generation>}` where `Stat` is
  `git.DiffStat(remoteSnapshot, mergedSnapshot)` — the observed remote parent
  tree versus the accepted merged tree. A no-op commit returns the remote
  generation and an empty `Stat`. On conflict or any error the result is the
  zero value.
- The zero `Result` is always paired with a non-nil error. Consumers only read
  `Result` when `err == nil`.

Plumb the result through without changing the visible output:

- `app.Service.Pull` / `app.Service.Commit` return `(notebook.Result, error)`.
- `app.Runtime.Pull` / `app.Runtime.Commit` return `(notebook.Result, error)`.
- `mcp.Service` gains the result return in both methods; the MCP handler
  discards it and still returns the bare `OK` text result for this phase.
- `cmd/pull` and `cmd/commit` receive the result but `app.Report` still prints
  exactly `OK` for this phase (its signature now accepts the result and ignores
  it).

The `workspace.Workspace` interface is unchanged. Snapshot reads for the
diffstat use `git.ReadSnapshot` (or `git.MaterializeTree`) over the private
repository, which is already populated by the merge.

## Integration contract

| Trigger                            | Collaborators           | Observable result                                             | Required side effect                 | Prohibited side effect          |
| ---------------------------------- | ----------------------- | ------------------------------------------------------------- | ------------------------------------ | ------------------------------- |
| Clean pull over an advanced remote | Fake engine, fake store | Returned `Result` generation and stat match the on-disk delta | L materialized as before             | No MCP/CLI output change yet    |
| Commit that publishes an increment | Fake engine, fake store | Returned stat equals the published increment                  | Pack precedes manifest CAS as before | No remote mutation for a no-op  |
| No-op commit                       | Fake engine, fake store | Empty stat, remote generation                                 | L and P synchronize to R             | No publication ID, pack, or CAS |
| Conflict                           | Fake engine, fake store | Zero `Result` plus `CONTENT_CONFLICT`                         | Markers materialized as before       | No success envelope             |

## Acceptance criteria

- [x] `Pull` computes the pull delta stat against the materialized result.
      Test: `TestPullResultStat` in `internal/notebook/pull_test.go`.
- [x] `Commit` computes the increment stat against the observed remote tree.
      Test: `TestCommitResultStat` in `internal/notebook/commit_test.go`.
- [x] A no-op commit returns an empty stat and the remote generation.
      Test: `TestCommitNoChangeSynchronizes` in `internal/notebook/commit_test.go`
      (both the generation-0 and the post-publication no-op).
- [x] A conflict returns the zero result with the existing error.
      Tests: `TestPullConflictWritesMarkersAndKeepsL` and
      `TestCommitOverlappingConflictWritesMarkers` assert `assertZeroResult`
      alongside the exact `CONTENT_CONFLICT`.
- [x] `Service` and `Runtime` forward the result unchanged.
      Test: `TestServiceForwardsResult` in `internal/app/service_test.go`;
      `cmd/pull` and `cmd/commit` thread the result into `app.Report`.
- [x] The MCP handler and CLI still produce exactly the old `OK` success output
      for this phase. Verified by the unchanged existing tests passing:
      `TestPullSuccessReturnsOK` / `TestCommitSuccessReturnsOK` and
      `assertOKResult` in `internal/mcp/server_test.go`, `TestReport` in
      `internal/app/command_test.go`, `TestPullPrintsOK` in
      `cmd/pull/pull_test.go`, `TestCommitPublishesAndPrintsOK` in
      `cmd/commit/commit_test.go`, and the black-box scenario suite
      (`internal/integrationtest`) with its `OK`-envelope assertions.

Each checked criterion cites its unit test in `internal/notebook` or
`internal/app`, and the unchanged MCP/CLI tests that still pass.

## Error coverage

| Failure                          | Expected outcome                                                   | Required test      |
| -------------------------------- | ------------------------------------------------------------------ | ------------------ |
| Snapshot read for the stat fails | Existing `STORAGE_INTEGRITY` / `STORAGE_FAILURE` path, zero result | Notebook unit test |
| Merge conflicts                  | Zero result, `CONTENT_CONFLICT`                                    | Notebook unit test |
| CAS lost then retried            | Result reflects the winning attempt only                           | Notebook unit test |

Covered:

- `TestPullDiffStatReadFailureMapsToIntegrity` and
  `TestCommitDiffStatReadFailureMapsToIntegrity` inject a repository
  `ReadTree` failure (via the `readFailEngine` / `readFailRepo` wrappers in
  `internal/notebook/fake_test.go`) at exactly the merged-result tree and
  assert the zero `Result` with `STORAGE_INTEGRITY` and no local or remote
  mutation.
- `TestPullConflictWritesMarkersAndKeepsL` and
  `TestCommitOverlappingConflictWritesMarkers` assert the zero result with
  `CONTENT_CONFLICT`.
- `TestCommitCASRaceOneWinnerOneRetry` asserts that the returned result
  carries the winning retry's generation (3) and increment stat, proving a
  lost attempt's summary is discarded.

## Implementation notes

- `internal/notebook/result.go` defines `Result{Generation, Stat}` and the
  shared `(*Notebook).diffStat(base, result git.OID)` helper. Pull passes its
  already-built local tree and the merged result tree; commit passes the
  observed remote tree and the merged result tree. A snapshot-read failure
  maps to the existing `storageIntegrity` path.
- The diffstat is computed **before** the local mutation (pull) or the
  publication (commit): a summary read failure aborts with L, P, and the
  remote untouched, keeping the summary strictly presentation-only.
- `attemptPublication` now returns `(casLost bool, result Result, err error)`;
  the commit loop returns the winning attempt's result and discards a lost
  attempt's summary.
- Plumbing: `app.Service.Pull/Commit`, `app.Runtime.Pull/Commit`, and the
  `mcp.Service` interface return `(notebook.Result, error)`; the MCP handler
  discards the result and still returns the bare `OK`; `app.Report` accepts
  the result and ignores it for this phase.
- Test call sites that assert failures use the `errOnly` helper
  (`internal/notebook/fake_test.go`) so the one-line shape is kept; channel
  goroutines (`done <- errOnly(...)`) and `if _, err := ...` assignments
  adapt to the two-value signatures.

## Review findings

No reviews recorded.
