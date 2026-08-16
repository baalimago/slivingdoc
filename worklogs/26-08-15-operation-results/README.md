# slivingdoc operation-results worklog

**Status:** Not Started

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)

## Objective

Replace the bare `OK` result of `notes_pull` / `notes_commit` and the one-shot
`slivingdoc pull` / `slivingdoc commit` subcommands with a unified result
envelope: a remote generation counter plus a per-file line-change diffstat,
delivered as structured JSON over MCP and as a coloured human-readable report
on the CLI. Success and conflict results share one look and feel.

## Status board

| Phase                                                              | Status      | Summary                                                                              |
| ------------------------------------------------------------------ | ----------- | ------------------------------------------------------------------------------------ |
| [1. Diff engine](phase-1-diffstat.md)                              | Complete    | Pure-Go line diff and `git.DiffSnapshots` over snapshots (Myers linear space, LCS-property-tested). |
| [2. Notebook result and plumbing](phase-2-notebook-result.md)      | Complete    | `notebook.Result` (generation + diffstat) computed per operation and threaded to Service, Runtime, MCP, and the CLI; `app.Report` still prints `OK`. |
| [3. MCP result envelope](phase-3-mcp-envelope.md)                  | Complete    | `SuccessInfo` / `ChangeFile` / `MapSuccess`; success tool result carries one `OK` text item plus the structured envelope; error envelope untouched. |
| [4. CLI render and colour](phase-4-cli-render.md)                  | Complete    | Unified coloured text report for success and conflict, honouring TTY and `NO_COLOR`. |
| [5. Scenarios, docs, quality gate](phase-5-scenarios-docs-gate.md) | Complete    | `CallExpectation.Success` pins the exact structured envelope; pull-delta, commit-increment, no-op, conflict, CLI-exact, and PTY colour scenarios; §2 and `running.md` updated; `make qa` passes. |

## Strategy

### Execution order

Phases run in numeric order. Phase 2 depends on Phase 1. Phase 3 depends on
Phase 2. Phase 4 depends on Phase 3 (the CLI renders the same envelope the MCP
layer produces). Phase 5 validates the whole change and updates the contract
docs; it depends on Phases 1 through 4.

An executing agent reads this README and only the phase file it works on.
Shared rules live here so they are not duplicated per phase.

### Required architecture sections

| Phase | Required contract sections                |
| ----- | ----------------------------------------- |
| 1     | §2 result envelope, §7.1 UTF-8 text files |
| 2     | §10 Pull, §11 Commit                      |
| 3     | §2 result envelope                        |
| 4     | §2 CLI report, §17 flags and environment  |
| 5     | §2, §10, §11, §12, §17                    |

Re-verify section references after any `docs/slivingdoc-v1.md` edit.

### Shared invariants

Every phase preserves:

1. MCP and the one-shot `pull`/`commit` subcommands remain the only public
   APIs, and expose the same two operations.
2. No Git object ID, pack name, S3 key, private path, or credential ever
   appears in a result, success or error. The existing `mcp.Redact` guarantee
   applies unchanged to the new success text and structure.
3. The error taxonomy stays stable: `code`, `retryable`, `message`, `files`
   (conflict paths and one-based inclusive marker ranges), and `recovery` for
   `RECOVERY_FAILURE`. This feature adds a success envelope; it never changes
   the error envelope's fields.
4. The notebook still accepts valid UTF-8 text without U+0000 only. The
   diffstat is presentation-only and never changes accepted state, the merge,
   or the publication protocol.
5. A conflicting pull or commit still returns `CONTENT_CONFLICT` with the exact
   paths and ranges. The success envelope never replaces an error.
6. Colour is presentation-only, gated on a real terminal and the `NO_COLOR`
   convention. Piped or redirected output stays plain text.

### Result envelope (normative)

`code` is the discriminator: `OK` for success, or the existing error category.
`files` means "the files this result is about": change stats on success,
conflict paths and marker ranges on error.

MCP success structured content:

```json
{
  "code": "OK",
  "generation": 18,
  "filesChanged": 3,
  "insertions": 3,
  "deletions": 4,
  "files": [
    { "path": "notes/a.md", "insertions": 1, "deletions": 1 },
    { "path": "notes/c.md", "insertions": 2, "deletions": 0 },
    { "path": "archive/old.md", "insertions": 0, "deletions": 3 }
  ]
}
```

The MCP error envelope is unchanged:

```json
{
  "code": "CONTENT_CONFLICT",
  "retryable": false,
  "message": "Resolve the conflict blocks in the visible files before continuing.",
  "files": [
    { "path": "notes/today.md", "ranges": [{ "start": 12, "end": 18 }] }
  ]
}
```

The MCP success result keeps one candid text item `OK`; the error result keeps
one candid text item equal to the message. Both carry `StructuredContent`.

CLI unified report skeleton (colour shown as tags):

```text
<status>  <summary>
  <file detail>
  ...
<trailer>
```

- Success status token `OK` (green); summary `generation <g>` (cyan).
- Error status token `<CODE>` (red); summary the candid message.
- Success file detail `path  +I -D` with insertions green and deletions red;
  a zero-count side is omitted.
- Error file detail `path: lines a-b` with the path yellow, or a bare `path`
  when the conflict has no marker range.
- Success trailer `N files changed, I insertions(+), D deletions(-)`.
- Error trailer `retryable: <bool>` and `recovery: ...` when present.

### Diff semantics (normative)

- `pull` diffstat = the before-pull visible snapshot versus the after-pull
  materialized result (the on-disk delta).
- `commit` diffstat = the observed remote parent tree versus the accepted
  merged tree (the increment this commit published; empty for a no-op sync).

A line is a LF-terminated run of bytes with a single trailing CR stripped for
comparison and counting. A final run without a trailing LF still counts as one
line; content ending in LF has no phantom empty final line; empty content has
zero lines. A file present only after counts every line as an insertion; a
file present only before counts every line as a deletion. A modified file uses
a deterministic Myers shortest-edit-script line diff.

### Review severity

| Severity | Meaning                                                                         | Phase effect                                |
| -------- | ------------------------------------------------------------------------------- | ------------------------------------------- |
| Critical | Data loss, silent lost update, credential exposure, or unsafe release artifact. | Reopen and block dependent phases.          |
| Major    | Acceptance contract or architecture invariant is not met.                       | Reopen the phase.                           |
| Minor    | Local maintainability or documentation defect with no contract failure.         | Record and fix without mandatory reopening. |

Every review appends findings to the phase file. Critical and major findings
change that phase to `Reopened (review N)` and update this status board.

## Decisions

| Date       | Decision                                                                                                                                                | Reason                                                                                                                                         |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-15 | Keep the error envelope's fields unchanged and add `code: "OK"` as the success discriminator.                                                           | The error taxonomy is stable API; a parallel success shape gives one look without churning the error contract.                                 |
| 2026-08-15 | Reuse `files` for both success (change stats) and error (conflict ranges).                                                                              | One field meaning "the files this result is about" keeps the two envelopes parallel.                                                           |
| 2026-08-15 | `pull` diffstat is the on-disk delta; `commit` diffstat is the published increment.                                                                     | The user wants "what is new to check out" (pull) and "what the agent just wrote" (commit).                                                     |
| 2026-08-15 | Colour is gated on a real TTY and `NO_COLOR`, matching the existing `release.go` and `NO_COLOR` convention.                                              | Piped output stays script-safe; interactive output is readable.                                                                                |
| 2026-08-15 | Phase 1 function name: `git.DiffSnapshots(base, cur Snapshot) DiffStat`, not `func DiffStat`. Types keep the spec names `FileStat` / `DiffStat`.          | Go shares one package-block namespace for types and functions, so `type DiffStat` and `func DiffStat` cannot coexist; the types are the carried data of every later phase. |
| 2026-08-15 | Phase 2 computes the diffstat **before** the local mutation (pull) or the publication (commit): `(*Notebook).diffStat(base, result git.OID)` shared by both operations, mapping a snapshot-read failure to the existing `STORAGE_INTEGRITY` path with a zero result. | The summary is presentation-only; a read failure then aborts with L, P, and the remote untouched instead of leaving a half-mutated operation or an uncertain post-acceptance error. |
| 2026-08-15 | The commit loop returns the winning attempt's `Result`; a lost attempt's summary is discarded with the attempt (`attemptPublication` returns `casLost, result, err`). | The result must reflect exactly the accepted increment, and a CAS loss is pure contention with no accepted state. |
| 2026-08-15 | Phase 3 updates the existing success-envelope assertion helpers (`assertOKResult`, `resultOK`, `assertProcessOK`, `Harness.assertOK`, `assertProcessCallOK`) to the new structured shape in the same change as the envelope itself. | The helpers encoded the removed "no structured content" contract; leaving them would break the suite at the phase-3 gate. The exact per-scenario generation and diffstat expectations stay Phase 5's `CallExpectation` work, so Phase 5 keeps its own scope. |
| 2026-08-15 | Phase 4 moves the CLI text rendering of the envelope into `internal/app`: `ToolError.Report()` is removed from `internal/mcp` and `app.writeError` owns the text form, so the structured `ToolError` value stays the only shape the MCP layer knows. The error trailer (`retryable`, `recovery`) moved after the file detail lines to match the unified status/detail/trailer skeleton. | One renderer for both outcomes keeps the look and feel identical, and the app layer already owns the CLI-facing `Report`; moving the text out of mcp keeps that package strictly structural. |
| 2026-08-15 | Phase 4 colour gate reuses the existing `environ` helper and the `logEnvNoColor` constant from `logging.go`; `cmd/pull` and `cmd/commit` pass `c.opts.Env` to `app.Report`. | The report must honour the same `NO_COLOR` convention as the logger, and the subprocess environment is the only place a CLI caller can set it. |
| 2026-08-15 | Phase 5 `CallExpectation` pins the exact structured success envelope via `Success *SuccessExpectation`; nil keeps the shape-only `assertOK` contract so the existing catalog rows stay unchanged. | Detailed per-scenario generation and diffstat expectations were explicitly deferred to Phase 5; a pointer keeps "shape only" distinguishable from "exact" without churning every call site. |
| 2026-08-15 | Phase 5 `decodeEnvelope` scans every error envelope for the success-only field names (`generation`, `filesChanged`, `insertions`, `deletions`), strengthening the "conflict returns the unchanged error envelope" requirement across the whole catalog at once. | The success and error envelopes share `code` and `files`; the four success-only fields are the complete discriminator, and one scan in the shared decode is stronger than per-scenario copies. |
| 2026-08-15 | Phase 5 proves ANSI on a real terminal with a Linux process-level PTY scenario (`runCLITTY`): x/sys ioctls allocate `/dev/ptmx` + `/dev/pts/n`, OPOST is disabled so the captured bytes are exact, and the master is drained concurrently with the wait. | The spec allows unit or process level, and the unit gate tests already covered the char-device branch; a process-level PTY proves the shipped CLI renders ANSI end to end, and the Linux build tag keeps the test honest about the platform capability. |
| 2026-08-15 | Phase 5 folds the unreferenced `docs/output.md` draft (envelope, CLI report, diff semantics, line rules, colour) into `docs/slivingdoc-v1.md` §2 and `docs/running.md`, then removes the draft. | The accepted architecture document is the single contract source; an unreferenced copy of the same contract is exactly the redundancy the duplication policy forbids. |

## Session journal

| Date       | Entry                                                                                                                                                                                                                                                                                                                                                         |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-15 | Investigated the current `OK`-only result path (MCP `okResult`, CLI `app.Report`) and the available internal data (`workspace.Diff`, `BaselineSnapshot`, `Baseline.RemoteGeneration`, `notebook.Metrics`, and the transient pull/commit snapshots). Confirmed the `git` seam exposes no line-diff, so a pure-Go diff is required for per-file `+`/`-` counts. |
| 2026-08-15 | Phase 1 (diff engine) complete: `internal/git/diffstat.go` with `FileStat`, `DiffStat`, and `DiffSnapshots`; Myers linear-space line diff; `diffstat_test.go` with all nine acceptance tests plus LCS-property, determinism, input-order, adversarial, and line-ending tests. Gate before: `go test ./internal/git/...` OK. Gate after: `go test ./internal/git/...` OK; full `go test ./... -race -count=3 -timeout=30s -coverpkg=./...` OK; `go vet`, `staticcheck`, `go fix -diff`, `gofumpt`, and `dupl` clean. diffstat.go coverage 100% except the intentionally unreachable middle-snake panic (97.3%). Worker session: 2026-08-15 slivingdoc phase-1 session. |
| 2026-08-15 | Phase 3 (MCP result envelope) complete: `internal/mcp/success.go` (`SuccessInfo`, `ChangeFile`, `MapSuccess`); `mcp/server.go` handlers capture the result and `successResult` sets `StructuredContent` while keeping one `OK` text item and no `isError`; `ToolError` untouched. New tests: `TestPullSuccessEnvelope`, `TestCommitSuccessEnvelope` (exact documented stat after the SDK round trip + no Git ID / pack key / credential scan), `TestNoOpCommitReturnsEmptyStatEnvelope`, `TestZeroResultWithNoErrorNeverPanics`, and the success-only-field scan inside `TestConflictDataSurvivesSDKEnvelope`. The five helpers that asserted the old "no structured content" contract (`assertOKResult`, `resultOK`, `assertProcessOK`, `Harness.assertOK`, `assertProcessCallOK`) assert the new shape, and `TestScenarioResultShape` scans the structured success content for internal values. Gate before: `go build ./...` OK, `go test ./internal/mcp/...` OK. Gate after: `go test ./internal/mcp/...`, `./internal/app/...`, `./internal/integrationtest/...` OK; full `go test ./... -race -count=3 -timeout=30s -coverpkg=./...` OK (aggregate coverage 83.6%, floor 70%); `go vet`, `staticcheck`, `go fix -diff`, `gofumpt -l` (nothing listed), and `dupl -t 80 .` (0 clone groups) clean; `npm test --prefix npm/slivingdoc` OK (35/35). Worker session: 2026-08-15 slivingdoc phase-3 session (worker: imago, cwd /home/imago/Projects/public/slivingdoc). |
| 2026-08-15 | Phase 4 (CLI render and colour) complete: `internal/app/colour.go` (ANSI helper: green/red/yellow/cyan `painter` plus the `colourEnabled` gate reusing `environ` / `logEnvNoColor`); `app.Report(out, result, err, env)` renders both outcomes with the one status/detail/trailer skeleton; `writeSuccess` / `writeError` build into a `strings.Builder` and write once; `ToolError.Report()` removed from `internal/mcp`, rendering moved to `app.writeError`; `cmd/pull` and `cmd/commit` pass `c.opts.Env`; help text, `docs/running.md`, and the top-level `README.md` describe the richer report and `NO_COLOR` gating. New tests: `TestReport` (exact documented plain success report, unified category report + terse category return, non-domain pass-through unprinted, piped output plain, `NO_COLOR` plain), `TestWriteSuccessColoured`, `TestWriteSuccessEmptyStat`, `TestWriteErrorColoured` (recovery trailer), `TestPainter`, `TestColourEnabled` (character device, pipe, buffer, nil writer, `NO_COLOR` variants); `cmd/pull` / `cmd/commit` in-process tests assert the OK-prefixed report. Gate before: `go build ./...` OK, `go test ./internal/app/... ./cmd/...` OK. Gate after: same package tests OK; full `go test ./... -race -count=3 -timeout=30s -coverpkg=./...` OK (aggregate coverage 83.7%, floor 70%); `make test` OK; `go vet`, `staticcheck`, `go fix -diff`, `gofumpt -l` (nothing listed), `dupl -t 80 .` (0 clone groups) clean; `npm test --prefix npm/slivingdoc` OK (35/35). Worker session: 2026-08-15 slivingdoc phase-4 session (worker: imago, cwd /home/imago/Projects/public/slivingdoc).
| 2026-08-15 | Phase 5 (scenarios, docs, quality gate) complete: `CallExpectation.Success` + `SuccessExpectation` / `FileStatExpectation`; `assertOK` refactored onto `successInfo`; `assertSuccessStat`; `decodeEnvelope` scans every error envelope for the success-only fields; new `scenario_result_test.go` (`TestScenarioPullDeltaStat`, `TestScenarioCommitIncrementStat`) and pinned empty-stat envelopes in `TestScenarioPullFirstPull` / `TestScenarioCommitNoChange`; CLI exact reports via `runCLIExact` + stricter `runCLIOK` + no-ANSI conflict check; Linux PTY colour scenario `TestScenarioCLIColourOnTerminal`; `docs/slivingdoc-v1.md` §2 carries the structured `SuccessInfo`, diff semantics, line rules, unified CLI report, and `NO_COLOR` gate; `docs/running.md` gained the diff-semantics paragraph; `docs/output.md` folded into §2 and removed. Gate before: `go build ./...`, `go vet`, and targeted scenario runs OK. Gate after: full `go test ./... -race -count=3 -timeout=30s -coverpkg=./...` OK (aggregate coverage 83.6%, floor 70%); `make qa` OK; `go vet`, `staticcheck`, `go fix -diff`, `gofumpt -l` (nothing listed), `dupl -t 80 .` (0 clone groups) clean; `npm test --prefix npm/slivingdoc` OK (35/35). Worker session: 2026-08-15 slivingdoc phase-5 session (worker: imago, cwd /home/imago/Projects/public/slivingdoc).

## Feedback index

No findings recorded.
