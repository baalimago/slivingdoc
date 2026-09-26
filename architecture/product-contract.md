# Product contract

The public surface of slivingdoc: exactly two operations (`notes_pull`, `notes_commit`), their strict inputs, the success and domain-error envelopes, the stable reason/action/file-reason tokens, and the operator-configured read-only and writable path sets. This doc answers "what may a caller rely on, and which code produces each part of it".

Read this when: changing a tool schema, a result field, an error token, the CLI report, or the read-only/writable path policy.

## Key files

| File | Purpose |
|------|---------|
| `internal/mcp/server.go` | `NewServer` (registers exactly two tools), `handler.pull` / `handler.commit`, `successResult`, `successText`, `instructions`, `readOnlyDescriptionSuffix`, `writableDescriptionSuffix`, `errorResult`, `errorText` |
| `internal/mcp/decode.go` | `decodePull`, `decodeCommit`, `validatePath`: strict input decoding and byte bounds |
| `internal/mcp/success.go` | `SuccessInfo`, `ChangeFile`, `MapSuccess` |
| `internal/mcp/errors.go` | `ToolError`, `MapError`, `mapNotebookError`, `safeEngineDetail`, `retryable`, `Redact` |
| `internal/notebook/errors.go` | `Code`, `Reason`, `FileReason`, `Action`, `actionForPairing`, `actionFor`, `Error`, `RecoveryReport` |
| `internal/notebook/notebook.go` | `ValidateMessage`, `MaxMessageBytes`, `ReadOnlyListSeparator`, `PathSetsNestRule` |
| `internal/notebook/commit.go` | `enforcePolicy`, `policyRefusal`, `refusalMessage`, `writableMessage`, `readOnlyMessage`, `violatedEntries` |
| `internal/notebook/pull.go` | `pinProtected`: pull restores protected paths before the merge |
| `internal/git/policy.go` | `PathPolicy`, `NewPolicy`, `Protects`, `ChangedProtected`, `RestoreProtected`, `OverlapError` |
| `internal/git/readonly.go` | `EntrySet`, `NormalizeEntries`, `collapseEntries`, `CoveringEntry` |
| `internal/git/diffstat.go` | `DiffSnapshots`, `DiffStat`, `FileStat`: the result diffstat |
| `internal/app/command.go` | `Report`, `writeSuccess`, `writeError`, `fileReasonWords`, `actionWordings`: the CLI report; `OperationPath`, `resolvePath`: CLI path (`~/` expansion, clean, relative resolved against the working directory) |
| `internal/workspace/path.go` | `canonicalize`, `PathEscapeError` (behind `PATH_OUTSIDE_ROOT`) |

## Flow

```text
MCP:  handler.pull/commit → decodePull/decodeCommit → handler.path (empty → Service.Root)
        → Service.Pull/Commit → ok:  successResult(MapSuccess) + successText
        decode err: decodeFailureError → errorResult
        service err: MapError → errorResult(+diagnosticId, readOnly, writable) + errorText
CLI:  cmd/pull, cmd/commit → Runtime.Pull/Commit → app.Report → writeSuccess | writeError
```

## Behavior

### The two operations

| Tool | Input | Effect |
|------|-------|--------|
| `notes_pull` | `path` (optional) | Write the current notebook into the directory, merging unpublished local edits. See [pull.md](./pull.md). |
| `notes_commit` | `message`, `path` (optional) | Publish local changes, incorporating concurrent non-conflicting changes. See [commit.md](./commit.md). |

Workflow: `notes_pull` once, edit UTF-8 files, `notes_commit(message)`; repeat.

- An omitted or empty MCP `path` is the notebook root (`handler.path`, `Service.Root`): the configured workspace root; for `serve` with neither `--workspace-root` nor `--private-root`, a process-owned temporary session directory; otherwise the working directory (see [running.md](./running.md#the-session-directory)). Every success names the resolved directory, and so do the server `instructions`.
- A supplied path must be at or below the workspace root; one server serves several notebook directories that way. Outside the root is `INVALID_REQUEST`/`PATH_OUTSIDE_ROOT`; the message names the root and never echoes the rejected path (`invalidPathMessage`).
- CLI mirror: `slivingdoc pull [path]`, `slivingdoc commit [path] -m <message>`. The path may be relative (resolved against the working directory) and must stay below the workspace root; the one-shot commands never use a temporary directory.

### Input rules (`internal/mcp/decode.go`, `notebook.ValidateMessage`)

- Arguments are a strict JSON object: unknown fields, duplicate fields, and explicit `null` are `MALFORMED_INPUT`.
- `path`: a string; a leading `~/` expands to the home directory (`pathutil.ExpandHome`), then it must be absolute, valid UTF-8 without U+0000, at most 4,096 bytes.
- `message` (commit only, required): at most 16,384 bytes (`MaxMessageBytes`, `MESSAGE_TOO_LONG`), not only Unicode white space (`MESSAGE_BLANK`), valid UTF-8 without U+0000 (`MESSAGE_INVALID`). Every other byte is preserved. The same `ValidateMessage` runs in the MCP decoder and in `Notebook.Commit`.

### Success envelope

One MCP text item plus the structured `SuccessInfo`:

| Field | Meaning |
|-------|---------|
| `code` | Always `OK` |
| `path` | The resolved notebook directory (the caller's own directory, never private state) |
| `generation` | Accepted remote generation after the operation (`notebook.Result.Generation`) |
| `filesChanged`, `insertions`, `deletions` | Totals of the diffstat |
| `files[]` | `{path, insertions, deletions}` per changed file; always present, empty for a no-op |
| `readOnly`, `writable` | The normalized sets; always present, empty when unconfigured |

- Pull diffstat: raw visible snapshot before the pull versus the materialized result. Commit diffstat: observed remote parent tree versus the accepted merged tree; empty for a no-change sync (`notebook.Result`).
- Line rule (`git.DiffSnapshots`): a line is an LF-terminated run with one trailing CR stripped; a final run without LF counts as one line; empty content has zero lines; an added file counts every line as an insertion, a removed file every line as a deletion; any file with zero counted line changes (a byte-different file that splits to identical lines, such as CRLF to LF, or an added or removed empty file) has no entry and does not count toward `filesChanged`.
- Text item (`successText`): the bare path, or `<path> (writable: …; read-only: …; longest match decides)` with only the configured parts, writable first.

### Domain-error envelope

`isError=true`, one candid text item (`errorText`), and a `ToolError` with `code`, `reason`, `action`, `diagnosticId` (fresh 16 lowercase hex, the same `mcpReqID` as the call's log records), `retryable`, `message`, `files[]` (`{path, reason, ranges[]}`), `readOnly`, `writable`; optional `detail` (only for `ENGINE_FAILED`, chosen by `safeEngineDetail` from a closed set of typed causes, else absent) and `recovery` (`{stage, remoteAccepted: yes|no|unknown, resynchronized}` for `RECOVERY_FAILURE`).

- `files[].path` is relative to the request path in normalized slash form; ranges are one-based, inclusive, ordered, and non-overlapping; a file without markers has `ranges: []`.
- The text item repeats code, reason, message, detail, files, action, retryable, diagnosticId, recovery, and both path sets (writable, read-only, then `path-rule:` when both are set), so a client that drops structured content keeps the full safe diagnostic.
- `retryable` is true only for `REMOTE_BUSY`, `STORAGE_FAILURE`, `RECOVERY_FAILURE` (`mcp.retryable`).
- Message text may change between releases. `code`, `reason`, `action`, and every `files[].reason` may not.
- No envelope field carries credentials, S3 keys, private paths, or Git IDs Most messages are fixed strings; a few put a pack key or Git ID into the text (`internal/notebook/remote.go`), and `Redact`, applied to every message, masks pack and probe keys, 40/64-hex IDs, access keys and URL userinfo. Raw causes go only to the log, keyed by `diagnosticId`.
- An error that is not a `*notebook.Error` and not a workspace path error maps to retryable `STORAGE_FAILURE`/`INTERNAL`; context cancellation stays a protocol error (`MapError` returns `false`).
- `INCOMPATIBLE_STORE` is a startup diagnostic and never appears in a tool result.

### Reason and action tokens (`notebook.actionForPairing`)

The notebook emits every pairing below except `MALFORMED_INPUT` and `PATH_OUTSIDE_ROOT`, which the MCP layer emits itself (`decodeFailureError`, `MapError`). `INTERNAL` comes from both the notebook (ID generation failure in `commit.go`) and `MapError`'s fallback for unmapped errors.

| Code | Reason | Meaning | Action |
|------|--------|---------|--------|
| `INVALID_REQUEST` | `MALFORMED_INPUT` | Strict decode failure | `FIX_INPUT` |
| `INVALID_REQUEST` | `PATH_OUTSIDE_ROOT` | Path escapes the workspace root, or a symlink component | `FIX_INPUT` |
| `INVALID_REQUEST` | `MESSAGE_BLANK` / `MESSAGE_TOO_LONG` / `MESSAGE_INVALID` | Message rules above | `FIX_INPUT` |
| `INVALID_REQUEST` | `PULL_REQUIRED` | Commit before any pull on this P | `PULL` |
| `INVALID_REQUEST` | `INVALID_CONTENT` | A visible file breaks the content or path rules; `files` names it | `EDIT_FILES` |
| `INVALID_REQUEST` | `READ_ONLY_PATH` | Commit touched a protected path; files were reset | `EDIT_FILES` |
| `CONTENT_CONFLICT` | `MERGE_CONFLICT` | Three-tree merge conflicted; markers written | `EDIT_FILES` |
| `CONTENT_CONFLICT` | `UNRESOLVED_MARKERS` | Commit found complete marker blocks | `EDIT_FILES` |
| `REMOTE_BUSY` | `RETRIES_EXHAUSTED` | CAS lost every attempt | `RETRY` |
| `STORAGE_FAILURE` | `MANIFEST_READ`, `PACK_DOWNLOAD`, `PACK_UPLOAD`, `MANIFEST_WRITE`, `LOCAL_STATE`, `INTERNAL` | Store or local-state failure with no accepted result | `RETRY` |
| `STORAGE_FAILURE` | `PUBLICATION_UNPROVEN` | CAS response lost; acceptance not provable | `PULL` |
| `STORAGE_INTEGRITY` | `MANIFEST_INVALID`, `PACK_INVALID`, `HISTORY_INVALID`, `ENGINE_FAILED` | Stored state untrusted, or engine failure | `OPERATOR` |
| `RECOVERY_FAILURE` | `LOCAL_MUTATION_FAILED` | Failure after local mutation began | `PULL` if `resynchronized`, else `RETRY` |

File reasons: `TEXT_CONFLICT` (marker ranges), `PATH_CONFLICT` (file versus directory, empty ranges), `UNRESOLVED_MARKERS` (ranges), `READ_ONLY` (empty), `INVALID_CONTENT` (empty). Action meanings: `FIX_INPUT` change the request; `EDIT_FILES` edit then commit; `PULL` pull then continue; `RETRY` repeat the call; `OPERATOR` a person must act.

### CLI report (`app.Report`)

Success prints to stdout and exits 0: `OK  generation N  <path>`, one line per changed file with `+ins -del` (zero side omitted), a totals line, then `writable:`, `read-only:`, and `path-rule: longest match decides` trailers when configured. A domain error prints the same skeleton and exits nonzero: `CODE · REASON`, the message, one line per file (path padded to the longest path plus two, the reason as words from `fileReasonWords`, `lines a-b`), `next:` from `actionWordings`, `retryable:`, the recovery report, and the same trailers. The CLI report omits `detail` and `diagnosticId`. File reason words (`fileReasonWords`): `TEXT_CONFLICT` "conflict", `PATH_CONFLICT` "path conflict", `UNRESOLVED_MARKERS` "unresolved markers", `READ_ONLY` "read-only", `INVALID_CONTENT` "invalid content". Next-step wording (`actionWordings`): `FIX_INPUT` "correct the request, then call again", `EDIT_FILES` "edit the files, then commit", `PULL` "pull, then continue", `RETRY` "retry the same call", `OPERATOR` "operator attention needed". Colour only on a real terminal with `NO_COLOR` empty (known bug: the released binary does not pass its environment to `app.Report`, so `NO_COLOR` does not disable the report colour; see [cli.md](./cli.md)); stripped of escapes, output is byte-identical to the plain form.

### Read-only and writable paths

Configured per process with `--read-only-paths` / `--writable-paths` (same for `serve`, `pull`, `commit`). Entries are notebook-relative and apply identically whatever request `path` is used.

- An entry covers itself and every path below it on a segment boundary, compared under Unicode case folding (`EntrySet.covering`). Each entry is validated with `git.ValidatePath` after trimming one trailing slash; an invalid entry refuses startup before the engine or store is touched.
- Composition (`PathPolicy.Protects`): the longest matching entry across both sets decides. With no match the default is writable while the writable set is empty and protected once it is non-empty, which confines a process to its writable directories (a root file or a directory created later is protected without being named).
- A path named by both sets (compared under case folding after trimming one trailing slash, as written, before collapsing) is an `OverlapError` and refuses startup.
- The normalized sets are sorted by path and joined with `, ` (`ReadOnlyListSeparator`) on every surface. With neither set configured every surface is unchanged except the always-present empty `readOnly` and `writable` arrays.
- Collapse (`collapseEntries`): an entry covered by an ancestor of its own set is dropped only if no entry of the other set lies between them, so adding a broader entry never disables a narrower one, and `ReadOnly()`/`Writable()` round-trip losslessly through `NewPolicy`. The advertised sets can therefore contain nested entries.
- Commit enforcement: after message, pulled marker, snapshot and marker checks, `enforcePolicy` compares the local tree with the baseline (`ChangedProtected`); any added, changed, or deleted protected file is reset to baseline content through `Workspace.Materialize` and the call returns `INVALID_REQUEST`/`READ_ONLY_PATH` with one `READ_ONLY` file per path. Nothing remote is touched. A failure after the reset began rewriting L is `RECOVERY_FAILURE` stage `commit.readonly`; a failure while staging (before L is touched) leaves L unchanged and surfaces as `STORAGE_FAILURE`/`INTERNAL`. A protected path that is a file in the baseline but a directory locally cannot be reset, so recovery resynchronizes L and takes every local file below that path, including a file a nested writable entry covers.
- Refusal wording: with a writable set, `Only <writable> is/are writable in this server. Your changes elsewhere were discarded…` (`writableMessage`); otherwise `<entries> is/are read-only in this server…` naming the most specific violated read-only entry per path (`readOnlyMessage`, `violatedEntries`).
- Pull enforcement: `pinProtected` restores changed protected paths from the baseline in the merge's local side, so the merge takes R there and local edits are discarded; the restore shows up in the ordinary diffstat. Invalid content under a protected path is still refused as `INVALID_CONTENT` before any restore.
- Advertisement on four surfaces: server `instructions`, both tool descriptions (`writableDescriptionSuffix` then `readOnlyDescriptionSuffix`), the `readOnly`/`writable` arrays on every result, and the success text item. With both sets configured, the server instructions' read-only sentence ends in "where the two sets nest, the longest matching entry decides" instead of "write elsewhere", and the read-only tool-description suffix appends "and where the two sets nest the longest matching entry decides". `readOnly` always means the read-only entries alone, never the protected region.
- This is a guardrail at the tool boundary, not a security boundary: the process holds the S3 credentials, so an agent that can read its environment or run its own slivingdoc bypasses it.

## Gotchas

- App config validation (`internal/app/config.go`), `app.NewService`, and `notebook.New` each build a `PathPolicy`; the notebook is fed `policy.ReadOnly()`/`Writable()` from the service, which is safe only because the collapse is lossless. Keep it that way.
- `readOnly` (the notebook's own `EntrySet`) is kept separately from the policy only to name the violated read-only entry in the refusal text.
- The commit message is kept only in recent internal Git commits; no permanent message or history retention is promised.

## Related

- [overview.md](./overview.md), [pull.md](./pull.md), [commit.md](./commit.md), [conflicts.md](./conflicts.md), [guarantees.md](./guarantees.md)
- [git-engine.md](./git-engine.md) for path validation rules used by entries
- [running.md](./running.md#read-only-paths), [running.md](./running.md#writable-paths), [running.md](./running.md#configuration)
- `../AGENTS.md`: [Conventions](../AGENTS.md#conventions) (error taxonomy stability, invariants for read-only and writable paths)
- Other concerns: [mcp-server.md](./mcp-server.md), [errors.md](./errors.md), [cli.md](./cli.md), [config.md](./config.md)
