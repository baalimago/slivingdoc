# Phase 1 — Error reasons and actions

**Status:** In Progress

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
| Caller-fixable scenarios pin reason and action                            | `TestScenarioErrorTaxonomy`, `TestScenarioCommitWithoutPull`, `TestScenarioContentRules`, `TestScenarioConflictMarkerGrammar`, `TestScenarioPullConflict`, `TestScenarioConflictAfterRemoteMovement`, `TestScenarioMalformedToolJSON`, `TestScenarioStrictSchema`, `TestScenarioPathSecurityOverlappingRoots` (all extended)                                                               |
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

Not started.

## Review findings

None.
