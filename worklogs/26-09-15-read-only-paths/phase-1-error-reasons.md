# Phase 1 — Error reasons and actions

**Status:** Complete

**README:** [README.md](README.md)

## Goal

Every domain error carries a stable `reason`, a next-step `action`, and a
`reason` on every file entry, on the notebook error, in the MCP envelope,
and in the scenario harness, while every existing envelope field keeps its
meaning.

## Specification

### Notebook error model (`internal/notebook/errors.go`)

- Add three string types, `Reason`, `Action`, and `FileReason`, with one
  exported constant per token in the README's reason, file-reason, and
  action tables. No other token exists.
- `Error` gains `Reason Reason` and `Action Action`. `ConflictFile` is
  renamed `ErrorFile` and gains `Reason FileReason`; the field name `Files`
  stays. The rename is mechanical across the package and its tests.
- Every constructor requires the reason as a parameter, so a call site
  cannot omit it. `Action` is never passed by a call site: one pure function
  `actionFor(code, reason, report)` derives it from the README table, with
  the `RECOVERY_FAILURE` row choosing by `report.Resynchronized`. No
  `&Error{...}` literal remains outside `errors.go`; the direct literals in
  `pull.go`, `commit.go`, and `notebook.go` become constructor calls.
- `invalidRequest` takes an optional `[]ErrorFile`, so `INVALID_CONTENT`
  and (in Phase 2) `READ_ONLY_PATH` can name files.
- `contentConflictFiles` assigns `TEXT_CONFLICT` to a conflict with marker
  content and `PATH_CONFLICT` to one without. `rejectMarkers` assigns
  `UNRESOLVED_MARKERS`.

### Site classification

Each existing constructor site takes exactly this reason. Lines refer to
the code at the time of writing; the executor re-locates by message text.

| Site (file: message)                                                                                                                                         | Reason                  |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------- |
| `notebook.go`: visible files violate the notebook contract                                                                                                   | `INVALID_CONTENT`       |
| `notebook.go`: local private state operation failed                                                                                                          | `LOCAL_STATE`           |
| `notebook.go`: commit message exceeds the byte bound                                                                                                         | `MESSAGE_TOO_LONG`      |
| `notebook.go`: commit message must not be blank                                                                                                              | `MESSAGE_BLANK`         |
| `notebook.go`: invalid commit message                                                                                                                        | `MESSAGE_INVALID`       |
| `notebook.go`: every `recoveryFailure` call                                                                                                                  | `LOCAL_MUTATION_FAILED` |
| `result.go`: read the base / result snapshot for the change summary                                                                                          | `ENGINE_FAILED`         |
| `pull.go`, `commit.go`: visible files cannot be represented as notebook state                                                                                | `INVALID_CONTENT`       |
| `pull.go`, `commit.go`: merge failed; materialize conflict result                                                                                            | `ENGINE_FAILED`         |
| `pull.go`, `commit.go`: resolve the conflict blocks (merge result)                                                                                           | `MERGE_CONFLICT`        |
| `commit.go`: resolve the conflict blocks (markers found before merge)                                                                                        | `UNRESOLVED_MARKERS`    |
| `commit.go`: commit requires a successful or conflicting pull first                                                                                          | `PULL_REQUIRED`         |
| `commit.go`: another writer kept winning the publication race                                                                                                | `RETRIES_EXHAUSTED`     |
| `commit.go`: generate publication id; generate checkpoint id                                                                                                 | `INTERNAL`              |
| `commit.go`: create the first commit; export checkpoint pack; record the checkpoint boundary; create commit; export increment pack; encode proposal manifest | `ENGINE_FAILED`         |
| `commit.go`: manifest acceptance cannot be proved                                                                                                            | `PUBLICATION_UNPROVEN`  |
| `commit.go`: manifest CAS failed                                                                                                                             | `MANIFEST_WRITE`        |
| `commit.go`: pack upload collided with different bytes                                                                                                       | `PACK_INVALID`          |
| `commit.go`: pack upload failed                                                                                                                              | `PACK_UPLOAD`           |
| `remote.go`: current is not a valid manifest (both sites)                                                                                                    | `MANIFEST_INVALID`      |
| `remote.go`: manifest did not stabilize; references a pack that is missing                                                                                   | `PACK_INVALID`          |
| `remote.go`: accepted state is incomplete / unreadable / not valid notebook text                                                                             | `HISTORY_INVALID`       |
| `remote.go`: read current manifest; read current manifest body                                                                                               | `MANIFEST_READ`         |
| `remote.go`: import checkpoint pack; import increment pack                                                                                                   | `PACK_INVALID`          |
| `remote.go`: record checkpoint boundary                                                                                                                      | `ENGINE_FAILED`         |
| `remote.go`: download packs; download pack (both sites)                                                                                                      | `PACK_DOWNLOAD`         |
| `remote.go`: pack does not match its descriptor checksum and size                                                                                            | `PACK_INVALID`          |
| `mcp/errors.go`: strict decode failure (`invalidRequest`)                                                                                                    | `MALFORMED_INPUT`       |
| `mcp/errors.go`: workspace path or symlink error on the request path                                                                                         | `PATH_OUTSIDE_ROOT`     |
| `mcp/errors.go`: unrecognized error fallback                                                                                                                 | `INTERNAL`              |

### Typed scan error (`internal/workspace/scan.go`)

- Add `ScanError{Path string; Err error}` with `Error()` and `Unwrap()`
  returning `Err`, so every existing `errors.Is` check on `ErrInvalidPath`,
  `ErrSymlink`, `ErrUnsupportedFile`, and `ErrInvalidContent` keeps
  working. `Path` is the normalized internal path when known, else the raw
  on-disk relative path.
- Every scan rejection that names a path wraps it in `ScanError`,
  including the snapshot validation failure after the walk (case-fold
  collision or duplicate), where `Path` is the second colliding path.
- `notebook.mapLocalError` extracts the `ScanError` with `errors.As` and
  builds `INVALID_CONTENT` with one file entry carrying `INVALID_CONTENT`
  when a path is known, and an empty `Files` otherwise. No error text
  reaches the message; the message stays the current fixed text.

### MCP envelope (`internal/mcp/errors.go`)

- `ToolError` gains `Reason string` (`json:"reason"`) and `Action string`
  (`json:"action"`) placed after `Code`, so the wire order is code, reason,
  action, retryable, message, files, recovery. `ErrorFile` gains
  `Reason string` (`json:"reason"`) after `Path`.
- `mapNotebookError` copies the tokens. The package's own errors set
  `MALFORMED_INPUT`, `PATH_OUTSIDE_ROOT`, and `INTERNAL` with the actions
  from the README table.
- `Redact` runs over the message only; tokens are constants and never
  redacted.

### Scenario harness (`internal/integrationtest`)

- `envelope` gains `Reason`, `Action`, and `envelopeFile.Reason`.
  `decodeEnvelope` fails any error result whose reason or action is empty,
  or whose file entry has an empty reason.
- `CallExpectation` gains `Reason string` and `Action string`, asserted
  exactly when non-empty. `FileExpectation` gains `Reason string`,
  asserted exactly when non-empty.

### Contract

Update the sections the README lists for this phase: the error envelope
example and field description, the three token tables copied from the
README, the conflict-behavior section's file reasons, and the recovery
section's action rule.

### Invariant: every error carries reason and action

| Bound actor                    | Mechanism                                                     | Test                                        |
| ------------------------------ | ------------------------------------------------------------- | ------------------------------------------- |
| notebook constructors          | reason is a required parameter; action derived by `actionFor` | `TestErrorConstructorsCarryReasonAndAction` |
| notebook direct literals       | none remain outside `errors.go`                               | `TestNoErrorLiteralsOutsideErrorsFile`      |
| `RECOVERY_FAILURE` action      | `actionFor` reads `report.Resynchronized`                     | `TestActionForRecoveryReport`               |
| MCP-owned errors               | set in `invalidRequest`, path mapping, fallback               | `TestMapErrorReasonAndActionAlwaysPresent`  |
| MCP mapping of notebook errors | copied field by field                                         | `TestMapErrorEveryCategory` (extended)      |
| every file entry               | `ErrorFile.Reason` required by constructors                   | `TestMapErrorConflictShape` (extended)      |
| every black-box error result   | harness decoder refuses an empty token                        | every scenario through `decodeEnvelope`     |

## Integration contract

| Trigger                                                                 | Collaborators or fakes              | Observable result                                                                                   | Required side effects   | Prohibited side effects          |
| ----------------------------------------------------------------------- | ----------------------------------- | --------------------------------------------------------------------------------------------------- | ----------------------- | -------------------------------- |
| `notes_commit` before any pull                                          | fake store                          | `INVALID_REQUEST`, `PULL_REQUIRED`, `PULL`, empty files                                             | none                    | any store write                  |
| `notes_commit` with a white-space message                               | fake store                          | `INVALID_REQUEST`, `MESSAGE_BLANK`, `FIX_INPUT`                                                     | none                    | any store request                |
| `notes_pull` with a binary file `bin/blob` in L                         | fake store                          | `INVALID_REQUEST`, `INVALID_CONTENT`, `EDIT_FILES`, files `[{bin/blob, INVALID_CONTENT, []}]`       | none                    | any store request; any L change  |
| `notes_pull` with a symlink `link` in L                                 | fake store                          | `INVALID_REQUEST`, `INVALID_CONTENT`, files `[{link, INVALID_CONTENT, []}]`                         | none                    | any store request                |
| `notes_commit` with a complete marker block at lines 2-4 of `a.md`      | fake store after a pull             | `CONTENT_CONFLICT`, `UNRESOLVED_MARKERS`, `EDIT_FILES`, files `[{a.md, UNRESOLVED_MARKERS, [2-4]}]` | none                    | any store write                  |
| two sessions edit the same line of `a.md`; second commits               | fake store                          | `CONTENT_CONFLICT`, `MERGE_CONFLICT`, `EDIT_FILES`, file reason `TEXT_CONFLICT` with ranges         | markers written to L    | any manifest change by the loser |
| one session replaces `a.md` with directory `a.md/x`; other edits `a.md` | fake store                          | `CONTENT_CONFLICT`, `MERGE_CONFLICT`, file reason `PATH_CONFLICT` with empty ranges                 | local side kept in L    | manifest change by the loser     |
| commit with CAS retries set to zero and a concurrent winner             | fault store                         | `REMOTE_BUSY`, `RETRIES_EXHAUSTED`, `RETRY`                                                         | none                    | a second CAS attempt             |
| manifest read fails once                                                | fault store `FailNext` on `current` | `STORAGE_FAILURE`, `MANIFEST_READ`, `RETRY`                                                         | none                    | any L change                     |
| pack download fails                                                     | fault store on the pack key         | `STORAGE_FAILURE`, `PACK_DOWNLOAD`, `RETRY`                                                         | none                    | any L change                     |
| pack upload fails                                                       | fault store on the increment prefix | `STORAGE_FAILURE`, `PACK_UPLOAD`, `RETRY`                                                           | none                    | a manifest write                 |
| CAS response lost and the publication is not found                      | fault store `UnprovableNext`        | `STORAGE_FAILURE`, `PUBLICATION_UNPROVEN`, `PULL`                                                   | none                    | a republished proposal           |
| `current` is corrupt JSON                                               | fault store `CorruptRead`           | `STORAGE_INTEGRITY`, `MANIFEST_INVALID`, `OPERATOR`                                                 | none                    | any L change                     |
| a pack's bytes are corrupted                                            | raw store overwrite                 | `STORAGE_INTEGRITY`, `PACK_INVALID`, `OPERATOR`                                                     | none                    | any L change                     |
| materialize fails after mutation began; resync succeeds                 | workspace failpoint                 | `RECOVERY_FAILURE`, `LOCAL_MUTATION_FAILED`, `PULL`, `recovery.resynchronized=true`                 | L rebuilt from R        | `OK`                             |
| materialize fails and resync also fails                                 | workspace failpoint + fault store   | `RECOVERY_FAILURE`, `LOCAL_MUTATION_FAILED`, `RETRY`, `recovery.resynchronized=false`               | recovery flag persisted | `OK`                             |
| malformed tool arguments (unknown field)                                | in-memory transport                 | `INVALID_REQUEST`, `MALFORMED_INPUT`, `FIX_INPUT`                                                   | none                    | any service call                 |
| request path outside the workspace root                                 | in-memory transport                 | `INVALID_REQUEST`, `PATH_OUTSIDE_ROOT`, `FIX_INPUT`                                                 | none                    | any store request                |
| any of the above, CLI process                                           | spawned `pull`/`commit` subcommand  | exit nonzero; the existing report still prints the category line                                    | none                    | a panic or an empty report       |

Each row extends the scenario that already covers its trigger; the
acceptance table names which. The CLI row only proves that the additive
fields do not break the current report; Phase 4 renders them.

## Acceptance criteria

| Outcome                                                                   | Evidence                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Every constructor site is classified per the table and no literal remains | `TestNoErrorLiteralsOutsideErrorsFile` (reads the package sources), `TestErrorConstructorsCarryReasonAndAction`                                                                                                                                                                                                                                                                            |
| `actionFor` matches the README table for every code and reason            | `TestActionForEveryReason`, `TestActionForRecoveryReport`                                                                                                                                                                                                                                                                                                                                  |
| Scan rejections carry the offending path                                  | `TestScanErrorCarriesPath` (workspace), `TestMapLocalErrorNamesScanFile` (notebook)                                                                                                                                                                                                                                                                                                        |
| MCP envelope carries reason, action, and file reasons in the wire order   | `TestMapErrorEveryCategory`, `TestMapErrorConflictShape`, `TestMapErrorReasonAndActionAlwaysPresent`, `TestMapErrorInvalidContentFile`, `TestConflictDataSurvivesSDKEnvelope`                                                                                                                                                                                                              |
| Redaction never touches tokens                                            | `TestRedactPreservesReasonTokens`                                                                                                                                                                                                                                                                                                                                                          |
| Harness refuses an envelope with an empty token                           | `TestDecodeEnvelopeRequiresTokens`                                                                                                                                                                                                                                                                                                                                                         |
| Caller-fixable scenarios pin reason and action                            | `TestScenarioErrorTaxonomy`, `TestScenarioCommitWithoutPull`, `TestScenarioContentRules`, `TestScenarioConflictMarkerGrammar`, `TestScenarioPullConflict`, `TestScenarioConflictAfterRemoteMovement`, `TestScenarioStrictSchema`, `TestScenarioPathSecurityOverlappingRoots` (all extended); `TestScenarioMalformedToolJSON` proves the frame-level rejection carries no envelope to pin (fix 3, R3-03) — its contract row is pinned by `TestScenarioStrictSchema`'s "unknown field" case instead                                                               |
| Storage, integrity, and recovery scenarios pin reason and action          | `TestScenarioCommitRetryExhaustion`, `TestScenarioCommitUnprovableCAS`, `TestScenarioCommitAmbiguousPackUpload`, `TestScenarioIntegrityCorruptManifest`, `TestScenarioIntegrityCorruptPack`, `TestScenarioIntegrityPackTransportFailure`, `TestScenarioIntegrityMissingPackWithUnchangedManifest`, `TestScenarioRecoveryRepairImpossible`, `TestScenarioRecoveryBoundaries` (all extended) |
| CLI subcommands still print the current report                            | the existing CLI subcommand tests and CLI scenarios pass unchanged (declared and later updated in the CLI report phase)                                                                                                                                                                                                                                                                                        |
| Contract sections describe the shipped envelope                           | reviewer reads the sections the README lists for this phase against `TestMapErrorEveryCategory`                                                                                                                                                                                                                                                                                            |

## Error coverage

| Failure                                         | Expected outcome                                                | Test                                                     |
| ----------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------- |
| a scan rejection without a known path           | `INVALID_CONTENT` with empty files, non-empty reason and action | `TestMapLocalErrorNamesScanFile`                         |
| `BuildTree` validation fails after a clean scan | `INVALID_CONTENT` with empty files                              | `TestErrorConstructorsCarryReasonAndAction`              |
| a nil recovery report reaches `actionFor`       | `RETRY` (the conservative branch), never a panic                | `TestActionForRecoveryReport`                            |
| an unknown code reaches `actionFor`             | `RETRY` and a wrapped programming-error cause, never a panic    | `TestActionForEveryReason`                               |
| context cancellation                            | stays a protocol error with no envelope                         | `TestMapErrorCancellationKeepsProtocolError` (unchanged) |
| a message containing a token-like string        | token fields untouched, message redacted as today               | `TestRedactPreservesReasonTokens`                        |

## Implementation notes

Session: Claude Sonnet 5, 2026-09-15.

**Files changed.** `internal/notebook/errors.go` (rewritten: `Reason`,
`Action`, `FileReason` types and their complete constant sets;
`ConflictFile` renamed `ErrorFile` with a `Reason` field; every constructor
takes a `Reason`; `actionFor(code, reason, report)` keyed on the exact
`{code, reason}` pairing, falling back to `ActionRetry` plus a wrapped
`errUnknownActionPairing` for an unrecognized pairing, never a panic).
`internal/notebook/{pull,commit,notebook,remote,result}.go` (every
`&Error{...}` literal replaced by a constructor call carrying the reason
from the phase's site-classification table; no literal remains outside
`errors.go`, proved by `TestNoErrorLiteralsOutsideErrorsFile`).
`internal/workspace/scan.go` (`ScanError{Path, Err}` added; every scan
rejection that names a path — including `readDir`, invalid name/content,
symlink, unsupported file, file/directory ambiguity, and duplicate/case-fold
collision — wraps it). `internal/git/path.go` (added `PathCollisionError{
First, Path, Fold}`, replacing the two plain `fmt.Errorf` collision returns
of `ValidateSnapshot` with the same `Error()` text, so `workspace.scanLocked`
can recover the second colliding path programmatically instead of parsing
message text). `internal/mcp/errors.go` (`ToolError` gains `Reason`,
`Action` after `Code`; `ErrorFile` gains `Reason` after `Path`; the
package's own errors set `MALFORMED_INPUT`/`PATH_OUTSIDE_ROOT`/`INTERNAL`
with their README actions). `internal/integrationtest/{scenario,harness}.go`
(`CallExpectation.Reason/Action`, `FileExpectation.Reason`; `envelope` and
`envelopeFile` decode the new fields; `envelopeTokenViolation` is the pure
check `decodeEnvelope` uses, so a scenario-catalog defect and the meta-test
of the checker itself don't need the same mechanism). `docs/slivingdoc-v1.md`
§2, §12, §15 (error envelope example, the three token tables copied from the
README, per-file reason text, and the recovery action rule).

Tests added: `internal/notebook/errors_test.go`
(`TestActionForEveryReason`, `TestActionForRecoveryReport`,
`TestErrorConstructorsCarryReasonAndAction`,
`TestContentConflictFilesClassifiesByContent`,
`TestMapLocalErrorNamesScanFile`, `TestNoErrorLiteralsOutsideErrorsFile`).
`internal/workspace/scan_test.go` (`TestScanErrorCarriesPath`, extended
`TestScanRejectsCaseFoldingCollision`). `internal/mcp/errors_test.go`
(`TestMapErrorReasonAndActionAlwaysPresent`, `TestMapErrorInvalidContentFile`,
`TestRedactPreservesReasonTokens`; extended `TestMapErrorEveryCategory`,
`TestMapErrorConflictShape`). `internal/mcp/server_test.go` (extended
`TestConflictDataSurvivesSDKEnvelope`). `internal/integrationtest/*`
(extended `TestScenarioCommitWithoutPull`, `TestScenarioCommitRetryExhaustion`,
`TestScenarioCommitAmbiguousPackUpload`, `TestScenarioCommitUnprovableCAS`,
`TestScenarioConflictMarkerGrammar`, `TestScenarioConflictAfterRemoteMovement`,
`TestScenarioPullConflict`, `TestScenarioErrorTaxonomy`,
`TestScenarioIntegrityCorruptManifest`, `TestScenarioIntegrityPackTransportFailure`,
`TestScenarioIntegrityCorruptPack`, `TestScenarioIntegrityMissingPackWithUnchangedManifest`;
added `TestScenarioCommitPublicationNotFound`,
`TestDecodeEnvelopeRequiresTokens`). `internal/notebook/{commit_test,pull_test}.go`
(extended `TestCommitWithoutPull`, `TestCommitBlankMessage`,
`TestPullInvalidContentMapsToInvalidRequest` with reason/action/file
assertions). `internal/app/command_test.go` (mechanical `ConflictFile` →
`ErrorFile` rename in `TestReport`'s fixture; the CLI report byte output is
unchanged, since `writeError` does not render the new fields — that is
Phase 4's job).

**Deviations and surprises.**

1. `TestScenarioConflictMarkerGrammar` writes a complete marker block
   directly into a freshly-pulled workspace and commits with no concurrent
   remote change. That is `Commit`'s own `rejectMarkers` pre-merge check
   (`UNRESOLVED_MARKERS`), not a three-tree merge conflict
   (`MERGE_CONFLICT`) as I first assumed from the row's position in the
   README's advertising table. Fixed to `UNRESOLVED_MARKERS` /
   `EDIT_FILES` / file reason `UNRESOLVED_MARKERS`; verified by running the
   scenario and reading the actual `env.Reason` the assertion failure
   reported.
2. The phase's own Integration Contract row "CAS response lost and the
   publication is not found ... `PUBLICATION_UNPROVEN`" names
   `TestScenarioCommitUnprovableCAS`'s fault (`UnprovableNext`) as its
   collaborator, but `UnprovableNext` also fails the *follow-up proof read*
   of `current` (by design — see its doc comment), so `lookupPublication`'s
   own `readCurrent` returns first, and the actual, verified reason is
   `MANIFEST_READ`. This matches `TestScenarioCommitUnprovableCAS`'s own
   doc comment ("a landed manifest CAS whose follow-up read fails"), so I
   corrected that test's expectation to `MANIFEST_READ`/`RETRY` and added
   `TestScenarioCommitPublicationNotFound`, which uses a plain unlanded
   `FailNext(OpReplace, ..., ErrTransport)` (no read-after-write fault) so
   the follow-up read succeeds and genuinely does not find the publication
   ID — the real `PUBLICATION_UNPROVEN` path. `TestScenarioErrorTaxonomy`'s
   "storage failure" row was fixed the same way. This is a scenario-fixture
   mismatch I found and resolved myself (AGENTS.md: "the scenario is the
   spec"), not an unresolved gap: both reasons now have a genuine, passing,
   distinct scenario.
3. `notebook.Reason` values are unique per row in the README's table (no
   reason is shared across two codes), so a `map[Reason]Action` would have
   been sufficient; I keyed `actionForPairing` on the full `{Code, Reason}`
   pair instead, matching the literal `actionFor(code, reason, report)`
   signature the spec describes and closing the (currently unreachable)
   loophole of a reason attached to the wrong code silently resolving
   instead of hitting the unknown-pairing fallback.
4. `invalidRequest` needed a `cause error` parameter in addition to the
   spec's explicit `[]ErrorFile` addition, because `mapLocalError`'s
   `INVALID_CONTENT` case and the `BuildTree`-failure sites in `pull.go`/
   `commit.go` already wrapped a `Cause` in the literal they replaced.
5. `PathCollisionError` in `internal/git/path.go` was not named in the
   phase's file list, but is the minimal way to give
   `workspace.scanLocked` the second colliding path without parsing
   `git.ValidateSnapshot`'s error text; it changes no existing behavior
   (`Error()` reproduces the prior `fmt.Errorf` text exactly) and is
   covered by the existing `internal/git` test suite plus the new
   `TestScanRejectsCaseFoldingCollision` assertion.
6. `MESSAGE_BLANK`/`MESSAGE_TOO_LONG`/`MESSAGE_INVALID` are unreachable
   through the MCP tool boundary: `internal/mcp/decode.go`'s own
   `validateMessage` rejects a blank, oversized, or invalid-UTF-8 message
   as `MALFORMED_INPUT` before `decodeCommit` ever returns, so
   `notebook.Commit`'s own message check only ever fires for the CLI
   subcommand path (which does not pre-validate `-m`) or a direct
   `notebook.Notebook.Commit` caller. This is pre-existing behavior, not a
   Phase 1 change. Coverage for these three reasons lives at the correct
   layer: the extended `TestCommitBlankMessage` (notebook unit test)
   proves the classification directly; no MCP or CLI black-box scenario
   can exercise it differently without changing `mcp/decode.go`, which is
   outside this phase's scope.

**Verification commands run (2026-09-15):**

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no files (after `-w` on
  `internal/notebook/errors.go`, which the first pass flagged).
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — clean (via
  `make lint`).
- `go fix -diff ./...` — no output (via `make lint`).
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — 0 clone groups.
- `make lint` — pass.
- `make test` (`go test -race -count=3 -timeout=30s -coverpkg=./... ./...`
  via the S3 lease) — pass; `== coverage: 83.3% (floor 70%) ==`.
- `make npm-test` — pass, 35/35.
- `make qa` — pass (re-run after the `actionForPairing` rework).
- Targeted runs during development:
  `go test ./internal/notebook/... ./internal/mcp/... ./internal/workspace/... ./internal/app/... -v`
  and `go test ./internal/integrationtest/... -run '<the named tests>' -v`
  — all pass; used to isolate and fix the two scenario-classification
  mistakes above before the full-suite run.

**What Phase 2 needs that is not already in the README.** None found;
`notebook.Error.Reason/.Action`, `ConflictFile`→`ErrorFile.Reason`, and
`mcp.ToolError.Reason/.Action`/`ErrorFile.Reason` are exactly the shared
interfaces the README's table names, and `READ_ONLY_PATH`/`READ_ONLY` exist
as reserved, unreachable tokens (documented as such in §2) ready for Phase
2 to make reachable.

### Review 1 fix (R1-01), 2026-09-15

Session: Claude Sonnet 5, 2026-09-15. Supersedes deviation item 6 above,
which recorded `MESSAGE_BLANK`/`MESSAGE_TOO_LONG`/`MESSAGE_INVALID` as
unreachable at the MCP boundary; that gap is R1-01 and is now closed.

**What changed.** `internal/notebook/notebook.go`: the package-private
`validateMessage` is exported as `notebook.ValidateMessage` (same body, same
`*notebook.Error` results), so the MCP decode boundary can classify a
blank, over-long, or invalid message without duplicating the notebook's own
rule; `internal/notebook/commit.go`'s one call site follows the rename.
`internal/mcp/decode.go`: `decodeCommit` calls `notebook.ValidateMessage`
in place of its own local `validateMessage` (removed), so a message
contract failure returns the *notebook.Error* the notebook layer would
itself construct, carrying the real `MESSAGE_BLANK`/`MESSAGE_TOO_LONG`/
`MESSAGE_INVALID` reason and `FIX_INPUT` action rather than a generic
decode error. `internal/mcp/errors.go`: added `decodeFailureError(err)`
(named to avoid colliding with Phase 2's unrelated `decodeToolError` test
helper in `internal/mcp/readonly_test.go`), which classifies a decode-time
failure by `errors.As`-matching a `*notebook.Error` first (routed through
the existing `mapNotebookError`, so `code`, `retryable`, `files`, and the
wire shape are unchanged) and falls back to the existing `invalidRequest`
(`MALFORMED_INPUT`) for every structural decode failure (unknown field,
null, wrong type, missing field, an unparsable/relative/oversized path).
`internal/mcp/server.go`'s `pull` and `commit` handlers call
`decodeFailureError` instead of unconditionally calling `invalidRequest`.
No change to `code`, `retryable`, or `files` computation, to the notebook's
own validation behavior, or to the CLI path (`cmd/pull`, `cmd/commit` do
not decode-validate the message before calling `notebook.Commit`, so they
were never affected by this gap and are untouched).

`internal/integrationtest/scenario_validation_test.go`'s
`TestScenarioStrictSchema` (already the extended acceptance-criteria test
for this row) now asserts the exact `Reason`/`Action` per row instead of
only `ErrorCode: "INVALID_REQUEST"`: the three message rows
(`oversized message`, `blank message`, `nul in message`) assert
`MESSAGE_TOO_LONG`, `MESSAGE_BLANK`, and `MESSAGE_INVALID` respectively
(closing the phase's unmet integration-contract row for a white-space
commit message, plus the two sibling reasons the finding also asked for),
and every structural row keeps asserting `MALFORMED_INPUT`; every row
asserts `Action: "FIX_INPUT"`. This is a black-box scenario through the
harness (`newFakeHarness`/`callWithArgs`/`assertEnvelope`) exercising the
real MCP `notes_commit`/`notes_pull` boundary, not a unit test on either
side of the seam. No `docs/slivingdoc-v1.md` wording change was needed:
§2's reason table already promised these three tokens unconditionally: the
promise was previously false at the MCP boundary and is now true.

**Verification commands run (2026-09-15, review 1 fix):**

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./internal/mcp/... ./internal/notebook/... -v` — all pass,
  including the pre-existing `TestDecodeCommit` subtests (`blank message`,
  `message with NUL`) and `TestDecodeCommitOversizedMessage`, which still
  pass unchanged because they assert on `err.Error()` substrings that
  `*notebook.Error`'s `Error()` still contains (e.g. "must not be blank",
  "U+0000"), and `TestPullBlankCommitMessageMapsToInvalidRequest`, which
  asserts only `code`/`retryable` and is unaffected.
- `go test ./internal/integrationtest/... -run TestScenarioStrictSchema -v`
  — pass, all nine rows including the three message rows with their new
  reason assertions.
- `make lint` (gofumpt, go vet, staticcheck, go fix -diff) — pass.
- `make test` (`go test -race -count=3 -timeout=30s -coverpkg=./...` via
  the S3 lease) — pass; `== coverage: 83.9% (floor 70%) ==`.
- `make npm-test` — pass, 35/35.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group
  (`internal/app/command_test.go:426,446` and `:451,471`), the same
  `TestFileReasonWords`/`TestActionWording` group Phase 4 and Phase 5
  already reviewed and accepted under the AGENTS.md table-driven-test
  idiom; untouched by this fix, no new clone.

Phase 1 returns to Complete. R1-01 is resolved: an MCP caller now sees
`MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, and `MESSAGE_INVALID` at the real
boundary for the three conditions the README and `docs/slivingdoc-v1.md`
§2 promise, `MALFORMED_INPUT` stays exact for structural decode failures,
and all other Phase 1 fields (`code`, `retryable`, `message`, `files`) are
unchanged in shape and computation.

### Review 3 fix (R3-01 to R3-04), 2026-09-15

Session: Claude Sonnet 5, 2026-09-15. Closes all four review-3 findings:
each was a test-evidence gap (a genuine token or contract row with no
black-box assertion holding it in place), not a production defect; no
production file changed in this fix.

**R3-01.** `internal/integrationtest/scenario_validation_test.go`'s
`TestScenarioContentRules` gained two new subtests, `binary file` and
`symlink`, matching the integration contract's own fixture names
(`bin/blob`, `link`). Each writes the offending entry (raw non-UTF-8
"PNG-magic" bytes for the binary file; a real host symlink via
`os.Symlink`, skipped on Windows per the existing convention in
`internal/workspace/scan_test.go`), commits, and asserts `Reason:
INVALID_CONTENT`, `Action: EDIT_FILES`, and `Files: [{<path>,
INVALID_CONTENT, []}]` through `h.assertEnvelope`, then confirms the store
took no write and L kept the caller's bytes (or the symlink) untouched.
Running the test first (`go test ./internal/integrationtest/... -run
TestScenarioContentRules -v`) confirmed the harness reports the offending
path exactly as `bin/blob` and `link` — relative to the request path,
normalized slash form, no leading slash — before pinning those literals.
The two pre-existing rows (`invalid utf8`, `nul byte`) are untouched: they
already exist and are not the rows this finding named.

**R3-02.** Tracing `mcp.MapError`'s raw `workspace.ErrInvalidPath`/
`ErrSymlink` branch to where it is actually driven at the integration
boundary found the finding's own test citation was imprecise:
`TestScenarioPathSecurityOverlappingRoots` (before this fix) exercised only
a startup configuration refusal (an overlapping private root), which exits
before any transport starts and never produces an MCP envelope — there was
never anything to pin there. The scenario that genuinely drives this
mapping is the unix-only `exerciseOptionalPathSecurity` helper
(`internal/integrationtest/scenario_path_security_unix_test.go`, invoked
from `TestScenarioTransportStdioProcess`): its "path outside the workspace
root" and "symlinked path component" rows call `notes_pull` with a path the
service rejects before ever opening a notebook. Both rows now assert
`env.Reason == "PATH_OUTSIDE_ROOT"` and `env.Action == "FIX_INPUT"` in
addition to the existing code/retryable check; the third row ("special file
inside the request path") is left unpinned since it fails later, inside
the notebook's own workspace scan, as `INVALID_CONTENT` — a different,
already-covered contract row, not `PATH_OUTSIDE_ROOT`.

To also make the literally-named test true (and because a portable,
non-symlink proof of this row is strictly better evidence),
`TestScenarioPathSecurityOverlappingRoots` itself
(`internal/integrationtest/scenario_path_security_test.go`) gained a third
row, "request path outside the workspace root", using `newFakeHarness` and
a plain `t.TempDir()` path outside the harness's workspace root — no
filesystem attack fixture needed, so it runs on every platform. It pins the
same `INVALID_REQUEST`/`PATH_OUTSIDE_ROOT`/`FIX_INPUT` shape through
`h.assertEnvelope`.

`internal/mcp/errors_test.go`'s `TestMapErrorServicePath` (which already
iterated `workspace.ErrInvalidPath` and `workspace.ErrSymlink` through
`MapError`, asserting only code/retryable) now also asserts
`te.Reason == reasonPathOutsideRoot` and `te.Action == actionFixInput` —
the unit assertion the finding asked for, extending the existing test
rather than adding a parallel one.

**R3-03.** `TestScenarioContentRules` and `TestScenarioPathSecurityOverlappingRoots`
are extended per R3-01/R3-02 above.
`internal/integrationtest/scenario_recovery_test.go`'s
`TestScenarioRecoveryBoundaries` (every row completes its immediate
resynchronization, `Resynchronized: new(true)`) now pins `Reason:
"LOCAL_MUTATION_FAILED"` and `Action: "PULL"` on its shared
`CallExpectation`, matching `actionFor`'s `RECOVERY_FAILURE` branch.
`TestScenarioRecoveryRepairImpossible` is R3-04's target and is described
there.

`TestScenarioMalformedToolJSON` is the one name in the table's list that
cannot be given a reason/action pin: it proves `callProtocolError`'s
transport-frame rejection of unparsable raw JSON, which the SDK client
raises before any request reaches the handler, `mapNotebookError`, or
`MapError` — there is no `ToolError` envelope in existence to assert
against, a fact the test's own pre-existing doc comment already states
("malformed tool JSON never reaches the handler"). Forcing an envelope
assertion onto a call that returns a bare Go `error` is not possible
without changing what the test proves. This is the same class of
scenario/contract mismatch the phase's original implementation notes
already recorded and fixed twice (the marker-block row that was actually
`UNRESOLVED_MARKERS` not `MERGE_CONFLICT`, and the CAS fault that was
actually `MANIFEST_READ` not `PUBLICATION_UNPROVEN`), under AGENTS.md's
"the scenario is the spec": rather than reword the table to excuse an
untouched test, the fix adds a code comment to
`TestScenarioMalformedToolJSON` explaining why no pin belongs there and
corrects the acceptance-criteria table row to say so and to name the test
that actually proves the integration contract's "malformed tool arguments
(unknown field)" row — `TestScenarioStrictSchema`'s "unknown field" case,
which has pinned `MALFORMED_INPUT`/`FIX_INPUT` since the review-1 fix.

**R3-04.** `TestScenarioRecoveryRepairImpossible` — the scenario that
already drives a failed immediate resynchronization
(`Resynchronized: new(false)`) — now pins `Reason: "LOCAL_MUTATION_FAILED"`
and `Action: "RETRY"` on its `CallExpectation`, at the MCP boundary,
closing the previously-unpinned failed-resync half of the
`RECOVERY_FAILURE` action rule (only the successful-resync `PULL` branch
was pinned before, in `TestScenarioErrorTaxonomy`).

**Files changed.** `internal/integrationtest/scenario_validation_test.go`
(two new `TestScenarioContentRules` subtests; a doc comment on
`TestScenarioMalformedToolJSON`). `internal/integrationtest/scenario_path_security_unix_test.go`
(`exerciseOptionalPathSecurity`'s row table gained `reason`/`action`
fields, asserted for the two path-outside-root rows).
`internal/integrationtest/scenario_path_security_test.go`
(`TestScenarioPathSecurityOverlappingRoots` gained a third, portable row).
`internal/integrationtest/scenario_recovery_test.go`
(`TestScenarioRecoveryBoundaries` and `TestScenarioRecoveryRepairImpossible`
each gained `Reason`/`Action` pins). `internal/mcp/errors_test.go`
(`TestMapErrorServicePath` extended with the same pins). No production
file changed.

**Verification commands run (2026-09-15, review 3 fix):**

- `go test ./internal/integrationtest/... -run TestScenarioContentRules -v`
  — pass, all four subtests (`invalid_utf8`, `nul_byte`, `binary_file`,
  `symlink`); used first to observe the harness's exact reported path
  before pinning `bin/blob`/`link` literally.
- `go test ./internal/mcp/... -run TestMapErrorServicePath -v` — pass.
- `go test ./internal/integrationtest/... -run 'TestScenarioPathSecurityOverlappingRoots|TestScenarioTransportStdioProcess' -v`
  — pass, including the new `request_path_outside_the_workspace_root`
  subtest and the two newly-pinned rows of
  `TestScenarioTransportStdioProcess`'s `exerciseOptionalPathSecurity`
  chain.
- `go test ./internal/integrationtest/... -run 'TestScenarioRecoveryBoundaries|TestScenarioRecoveryRepairImpossible' -v`
  — pass, all rows.
- `make qa` (gofumpt, go vet, staticcheck, go fix -diff, `go test -race
  -count=3 -timeout=30s -coverpkg=./...` via the S3 lease, npm test) —
  exit 0; `== coverage: 84.0% (floor 70%) ==`; npm test 35/35.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group
  (`internal/app/command_test.go:432,452` and `:457,477`), the same
  pre-existing `TestFileReasonWords`/`TestActionWording` group already
  reviewed and accepted under the AGENTS.md table-driven-test idiom in
  every prior session; untouched by this fix, no new clone.

No pinned assertion in this fix revealed a production mismatch: every new
or extended assertion passed on the first run against the existing
production code. The only surprises were the two citation errors in the
review-3 findings themselves (R3-02's and R3-03's `TestScenarioPathSecurityOverlappingRoots`
reference, and R3-03's `TestScenarioMalformedToolJSON` reference), both
resolved by locating the real evidence and, where a name was genuinely
untestable, correcting the one table cell that named it rather than the
underlying claim.

Phase 1 returns to Complete. R3-01 through R3-04 are resolved.

## Review findings (review 1, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1), independent of the executing
agent. Gates re-run: `make qa` exit 0, coverage 83.9 % against the 70 %
floor. Code read: `internal/notebook/errors.go`, every constructor site in
`commit.go`, `pull.go`, `notebook.go`, `remote.go`, `result.go`,
`internal/mcp/errors.go`, `internal/mcp/decode.go`,
`internal/workspace/scan.go`, and the harness decoder.

- [x] **R1-01 (major, resolved fix 1, 2026-09-15).** The phase's integration contract row "`notes_commit`
  with a white-space message → `INVALID_REQUEST`, `MESSAGE_BLANK`,
  `FIX_INPUT`" is unmet at the real boundary. `internal/mcp/decode.go`
  `validateMessage` rejects a blank, oversized, or non-UTF-8 message before
  the notebook runs, and `internal/mcp/errors.go` `invalidRequest` labels
  every decode failure `MALFORMED_INPUT`. The implementation notes record
  this as pre-existing behaviour and move the evidence to a notebook unit
  test, but the README reason table and `docs/slivingdoc-v1.md` §2 promise
  `MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, and `MESSAGE_INVALID` to an MCP
  caller, and the notebook constructors that carry them are reachable only
  from the CLI. Failure scenario: an agent sends `{"message": "   "}`; the
  envelope reads `reason: MALFORMED_INPUT`, so a caller branching on the
  documented `MESSAGE_BLANK` token never sees it. Fix in this phase: make
  the MCP decode path emit the same three reason tokens the notebook uses
  for the same three conditions (for example a typed message-validation
  error in `decode.go` whose reason `invalidRequest` copies), keep
  `MALFORMED_INPUT` for structural decode failures, and add the missing
  black-box scenario for the contract row plus one row each for
  `MESSAGE_TOO_LONG` and `MESSAGE_INVALID` through the harness.

  Resolved: see "Review 1 fix (R1-01), 2026-09-15" in the Implementation
  notes above. `decodeCommit` now calls the exported
  `notebook.ValidateMessage` and `internal/mcp/errors.go`'s
  `decodeFailureError` copies a `*notebook.Error`'s own reason and action
  instead of collapsing it to `MALFORMED_INPUT`; `TestScenarioStrictSchema`
  pins all three message reasons through the harness alongside the
  structural rows.

Verified good:

- Every `&Error{...}` literal outside `errors.go` is gone; `actionFor` is
  keyed on the exact code/reason pair and never panics; `RECOVERY_FAILURE`
  branches on `Resynchronized` as the README table requires.
- `MapError` matches `*notebook.Error` before the raw `workspace` sentinels,
  so an `INVALID_CONTENT` error whose cause is `ErrInvalidPath` is never
  relabelled `PATH_OUTSIDE_ROOT`.
- `INVALID_CONTENT` names its file through `workspace.ScanError` on the scan
  path; the harness decoder refuses any envelope missing a token, so every
  scenario in the suite enforces invariant 3.

## Review findings (review 2, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). R1-01 resolved and re-verified: `MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, and `MESSAGE_INVALID` now reach an MCP caller through `notebook.ValidateMessage` and `decodeFailureError`; the message-validation order (length, blank, UTF-8) cannot misclassify a message because invalid bytes are never white space. No new finding.

## Review findings (review 3, 2026-09-15)

Reviewer: independent Opus agent (holistic review), verified and relayed by
the orchestrating session. Gates re-run: `make qa` exit 0.

- [x] **R3-01 (minor, resolved fix 3, 2026-09-15).** The integration-contract rows for `notes_pull` with
  a binary file and with a symlink require `INVALID_CONTENT`, `EDIT_FILES`,
  and `files [{<path>, INVALID_CONTENT, []}]`, but `TestScenarioContentRules`
  (`internal/integrationtest/scenario_validation_test.go`) still pins only
  `ErrorCode` and `Retryable`. D11 is therefore proven only by unit tests on
  each side of the seam. Failure scenario: `scanErrorFiles` returns nil or
  the reason regresses and the scenario still passes. Fix: pin `Reason`,
  `Action`, and `Files` on both rows.

  Resolved: see "Review 3 fix (R3-01 to R3-04), 2026-09-15" in the
  Implementation notes below. Added a "binary file" row (`bin/blob`, raw
  PNG-magic bytes) and a "symlink" row (`link`, a real host symlink) to
  `TestScenarioContentRules`, each pinning `Reason: INVALID_CONTENT`,
  `Action: EDIT_FILES`, and `Files: [{<path>, INVALID_CONTENT, []}]` with
  the exact path the harness reports.
- [x] **R3-02 (minor, resolved fix 3, 2026-09-15).** `PATH_OUTSIDE_ROOT` has no test assertion anywhere
  (only the two constant declarations and the contract table mention it).
  The integration-contract row for a request path outside the root names
  `TestScenarioPathSecurityOverlappingRoots` as evidence, but that test is
  untouched and pins only the code. Fix: pin `Reason: PATH_OUTSIDE_ROOT`
  and `Action: FIX_INPUT` there (and in any sibling path-security scenario
  that exercises the same mapping) and add a unit assertion in
  `internal/mcp/errors_test.go` for the `ErrInvalidPath`/`ErrSymlink`
  mapping.

  Resolved: see "Review 3 fix (R3-01 to R3-04), 2026-09-15" below. The
  finding's own citation was imprecise — `TestScenarioPathSecurityOverlappingRoots`
  is a startup configuration refusal (private root overlapping the
  workspace root) that never reaches the MCP envelope, so it had nothing to
  pin. The scenario that actually drives `mcp.MapError`'s
  `ErrInvalidPath`/`ErrSymlink` branch is `exerciseOptionalPathSecurity`
  (`internal/integrationtest/scenario_path_security_unix_test.go`, run from
  `TestScenarioTransportStdioProcess`); its "path outside the workspace
  root" and "symlinked path component" rows now pin `Reason:
  PATH_OUTSIDE_ROOT`/`Action: FIX_INPUT`. `TestScenarioPathSecurityOverlappingRoots`
  itself (`scenario_path_security_test.go`) gained a new, portable
  "request path outside the workspace root" row through the fake harness
  (no filesystem attack fixture needed) that pins the same tokens, so the
  literal test name the finding cites is genuinely extended too.
  `internal/mcp/errors_test.go`'s `TestMapErrorServicePath` now asserts
  `Reason`/`Action` for both `workspace.ErrInvalidPath` and
  `workspace.ErrSymlink`.
- [x] **R3-03 (minor, resolved fix 3, 2026-09-15).** The acceptance table's "(all extended)" rows name
  `TestScenarioContentRules`, `TestScenarioMalformedToolJSON`,
  `TestScenarioPathSecurityOverlappingRoots`,
  `TestScenarioRecoveryRepairImpossible`, and
  `TestScenarioRecoveryBoundaries` as extended; `git status` shows the
  recovery and path-security files unmodified, and the two validation tests
  carry no reason or action pin. Fix: extend each named test to pin reason
  and action (which also closes R3-01, R3-02, and R3-04), so the table
  becomes true rather than rewording the table.

  Resolved: see "Review 3 fix (R3-01 to R3-04), 2026-09-15" below.
  `TestScenarioContentRules` and `TestScenarioPathSecurityOverlappingRoots`
  are extended per R3-01/R3-02 above. `TestScenarioRecoveryBoundaries` and
  `TestScenarioRecoveryRepairImpossible` now pin `Reason:
  LOCAL_MUTATION_FAILED` and their respective `PULL`/`RETRY` actions.
  `TestScenarioMalformedToolJSON` is the one name in the table that cannot
  be extended: it proves a transport-frame rejection the SDK client raises
  before any request reaches the handler, so no `ToolError` envelope ever
  exists to carry a reason or action — its own doc comment already says so
  ("malformed tool JSON never reaches the handler"). Rather than force an
  assertion onto a result that does not exist, the acceptance table was
  corrected to say so explicitly and point to the test that actually proves
  the contract's "malformed tool arguments (unknown field)" row
  (`TestScenarioStrictSchema`'s "unknown field" case, already pinning
  `MALFORMED_INPUT`/`FIX_INPUT` since the review-1 fix). This is the same
  class of scenario/contract mismatch the phase's original implementation
  notes recorded and fixed for `MERGE_CONFLICT`/`UNRESOLVED_MARKERS` and
  `PUBLICATION_UNPROVEN`/`MANIFEST_READ` (AGENTS.md: "the scenario is the
  spec"), not a rewording to dodge work.
- [x] **R3-04 (minor, resolved fix 3, 2026-09-15).** The integration-contract row "materialize fails and
  resync also fails → `RECOVERY_FAILURE`, `LOCAL_MUTATION_FAILED`, `RETRY`,
  `resynchronized=false`" has no scenario pin; only the `PULL` branch is
  pinned in `TestScenarioErrorTaxonomy`. Fix: pin `Action: RETRY` in the
  existing recovery scenario that already drives a failed resync
  (`TestScenarioRecoveryRepairImpossible` or the boundaries scenario), at
  the MCP boundary.

  Resolved: see "Review 3 fix (R3-01 to R3-04), 2026-09-15" below.
  `TestScenarioRecoveryRepairImpossible` (the scenario that already drives a
  failed resync, `Resynchronized: new(false)`) now pins `Reason:
  LOCAL_MUTATION_FAILED` and `Action: RETRY` at the MCP boundary.

Verified good by the same review: all ten README invariants traced and
held; every read-only scenario proves behaviour at the real boundary; the
harness decoder makes Definition of success 5 structural.
