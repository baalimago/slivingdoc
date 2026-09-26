# Error taxonomy

How failures are classified from the storage and Git layers up to the MCP tool result and the CLI exit code. The taxonomy is stable API: the `code`, `reason`, `action`, `retryable` flag, and structured `files` are contract; message text is not. This doc answers "which error does a caller see for this failure, and where is that decided?"

Read this when: adding a failure mode, a reason or action token, changing retryability, redaction, the engine `detail` allowlist, strict JSON rejection, or how the CLI exits on an error.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/errors.go` | `Code`, `Reason`, `FileReason`, `Action` tokens; `actionForPairing`, `actionFor`; `Error` (`Code`, `Reason`, `Action`, `Message`, `Files`, `Recovery`, `Cause`); `ErrorFile`; `RecoveryReport`, `RemoteAccepted`; constructors `invalidRequest`, `contentConflict`, `storageIntegrity`, `storageFailure`, `remoteBusy`, `recoveryFailure`; internal `errCASLost`, `errStaleManifest`; `contentConflictFiles` |
| `internal/notebook/notebook.go` | `mapLocalError`, `scanErrorFiles`, `failAfterAccept`, `ValidateMessage` |
| `internal/git/errors.go` | Named engine failures `ErrNoNewObjects`, `ErrObjectMissing`, `ErrEmptyPack`, `ErrHeadRequired`; `UnsupportedModeError` |
| `internal/git/policy.go` | `OverlapError` (both path sets name one path; a startup refusal) |
| `internal/storage/store.go`, `manifest.go`, `key.go`, `sha256.go`, `uuid.go` | Storage sentinels `ErrNotFound`, `ErrPreconditionFailed`, `ErrTransport`, `ErrIncompatible`, `ErrIntegrity`, `ErrInvalidKey`, `ErrInvalidPrefix`, `ErrInvalidSHA256`, `ErrInvalidUUID` |
| `internal/workspace/path.go`, `scan.go` | `ErrInvalidPath`, `PathEscapeError`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidContent`, `ScanError` |
| `internal/strictjson/json.go` | `Parse` rejections (malformed, duplicate field, `null`, non-`uint64` number, trailing data), `RejectUnknown` |
| `internal/mcp/errors.go` | `ToolError`, `ErrorFile`, `ErrorRange`, `RecoveryInfo`, `MapError`, `mapNotebookError`, `safeEngineDetail`, `retryable`, `invalidPathMessage`, `decodeFailureError`, `invalidRequest`, `Redact`, `redactValues`, the redaction regexes |
| `internal/app/command.go` | `Report` (CLI rendering; returns `errors.New(te.Code)`), `fileReasonWords`, `actionWordings` |
| `internal/app/app.go` | Startup refusal wrapping: `app: invalid configuration: ...`, `app: open native engine: ...`, `app: create object store: ...` (`realStoreFactory`), `app: INCOMPATIBLE_STORE: ...` |

## Flow

```text
storage / git / workspace error
  → notebook maps it to *notebook.Error{Code, Reason, Action(actionFor), Files, Recovery, Cause}
  → app.Service returns it unchanged
MCP:  handler.resultFor(err) → mcp.MapError(err)
        *notebook.Error                    → mapNotebookError (Redact message, safeEngineDetail)
        workspace.ErrInvalidPath/ErrSymlink → INVALID_REQUEST / PATH_OUTSIDE_ROOT / FIX_INPUT
        context.Canceled/DeadlineExceeded  → not a domain error: protocol error
        anything else                      → STORAGE_FAILURE / INTERNAL / RETRY, retryable
      → errorResult: isError + errorText + ToolError{DiagnosticID = mcpReqID, ReadOnly, Writable}
      decode failure → decodeFailureError → notebook message error keeps its tokens,
                                            else INVALID_REQUEST / MALFORMED_INPUT
CLI:  app.Report(out, result, err, ...) → MapError → writeError → return errors.New(code)
      → cmd.Run prints `failed to run: <CODE>` on stderr and exits 1
Startup: Setup error → router prints `<time> error: failed to setup command: <err>` on stderr, exit 1, empty stdout
```

## Behavior

**Codes.** `notebook.Code` values plus the startup-only `INCOMPATIBLE_STORE`:

| Code | Meaning | Retryable (`mcp.retryable`) |
|---|---|---|
| `INVALID_REQUEST` | bad input or a refused state before Git/S3 work, or a first pull refused before L or the pull state changes | no |
| `CONTENT_CONFLICT` | three-tree conflict, or unresolved markers in a commit | no |
| `REMOTE_BUSY` | CAS lost `--commit-retries` + 1 times | yes |
| `STORAGE_FAILURE` | store operation failed without a known accepted result; also the fallback for unknown errors | yes |
| `STORAGE_INTEGRITY` | stored or local state failed validation | no |
| `RECOVERY_FAILURE` | failure after local mutation began, or entry recovery of an earlier such failure; generic recovery ran | yes |
| `INCOMPATIBLE_STORE` | startup probe failed; never a tool result | n/a (process exits) |

**Reason to action table (`actionForPairing`).**

| Code | Reason | Action |
|---|---|---|
| `INVALID_REQUEST` | `MALFORMED_INPUT`, `PATH_OUTSIDE_ROOT`, `MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, `MESSAGE_INVALID`, `DIRECTORY_NOT_EMPTY` | `FIX_INPUT` |
| `INVALID_REQUEST` | `PULL_REQUIRED` | `PULL` |
| `INVALID_REQUEST` | `INVALID_CONTENT`, `READ_ONLY_PATH` | `EDIT_FILES` |
| `CONTENT_CONFLICT` | `MERGE_CONFLICT`, `UNRESOLVED_MARKERS` | `EDIT_FILES` |
| `REMOTE_BUSY` | `RETRIES_EXHAUSTED` | `RETRY` |
| `STORAGE_FAILURE` | `MANIFEST_READ`, `PACK_DOWNLOAD`, `PACK_UPLOAD`, `MANIFEST_WRITE`, `LOCAL_STATE`, `INTERNAL` | `RETRY` |
| `STORAGE_FAILURE` | `PUBLICATION_UNPROVEN` | `PULL` |
| `STORAGE_INTEGRITY` | `MANIFEST_INVALID`, `PACK_INVALID`, `HISTORY_INVALID`, `ENGINE_FAILED` | `OPERATOR` |
| `RECOVERY_FAILURE` | `LOCAL_MUTATION_FAILED` | `PULL` if `Recovery.Resynchronized`, else `RETRY` |

An unmapped pairing is a programming error: `actionFor` returns `RETRY` plus `errUnknownActionPairing`; the constructors discard that error, so tests must cover every pairing.

**File reasons.** `TEXT_CONFLICT` and `PATH_CONFLICT` (from `contentConflictFiles`: a conflict with content is text, else path), `UNRESOLVED_MARKERS`, `READ_ONLY`, `INVALID_CONTENT`, and `NOT_IN_NOTEBOOK` / `DIFFERS_FROM_NOTEBOOK` (from `guardFirstPull`). Each file carries one-based inclusive `ranges` (`git.MarkerRange`); the array is empty when the reason has no marker block (`READ_ONLY`, `INVALID_CONTENT`, `NOT_IN_NOTEBOOK`, `DIFFERS_FROM_NOTEBOOK`). File paths are relative to the request path in slash form.

**Recovery report.** `RECOVERY_FAILURE` carries `RecoveryReport{Stage, RemoteAccepted ("yes"|"no"|"unknown"), Resynchronized}`, exposed as `ToolError.Recovery`. The call never returns `OK`. See [commit.md](./commit.md).

**Layer mapping.** Storage sentinels are classified by the notebook: `ErrNotFound` on `current` is generation 0, not an error; `ErrPreconditionFailed` on a manifest write is contention (merge and retry, then `REMOTE_BUSY`); `ErrTransport` on a manifest write is ambiguous and resolved by reading back (`PUBLICATION_UNPROVEN` when unprovable); `ErrIntegrity` becomes `STORAGE_INTEGRITY`. Workspace content errors before mutation (`ErrInvalidContent`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidPath`, `ErrPathCollision`) become `INVALID_REQUEST`/`INVALID_CONTENT` naming the file, and both files of a case-folding collision (`mapLocalError`, `scanErrorFiles`); other workspace errors during an operation become `STORAGE_FAILURE`/`LOCAL_STATE`. That includes failing to take the operation lock at the start of a pull or commit (`holdWorkspace`), a lock wait ended by the request's cancellation or deadline among them: it is a retryable domain error, not a protocol error like a cancellation later in the call. Failures opening the workspace or notebook in `app.Service.notebookFor` (other than invalid-path/symlink), `app: open notebook: ...`, and `app: service is closed` reach `MapError` raw and become `STORAGE_FAILURE`/`INTERNAL`/`RETRY`. A request path outside the workspace root reaches `mcp.MapError` directly as a `workspace` error and becomes `PATH_OUTSIDE_ROOT`, whose message names the root but never the rejected path (`invalidPathMessage`).

**Strict JSON.** `strictjson` rejects malformed JSON, duplicate names, explicit `null`, numbers that are not unquoted `uint64`, and trailing data. In tool arguments this becomes `INVALID_REQUEST`/`MALFORMED_INPUT`; an absent or `null` `arguments` member is not a failure but the empty object (`parseArguments`). In the manifest (`storage.DecodeManifest`) it becomes `ErrIntegrity`, so `STORAGE_INTEGRITY`/`MANIFEST_INVALID`. The workspace private-state record uses the same parser.

**Engine detail.** `ToolError.Detail` is set only for `ENGINE_FAILED` with a cause that `safeEngineDetail` recognizes: `git.UnsupportedModeError` (names the entry, not a path), `ErrNoNewObjects`, `ErrObjectMissing`, `ErrEmptyPack`, `ErrHeadRequired`. Any other cause yields no detail; the raw cause is logged against `diagnosticId`. This is an allowlist on purpose: sanitizing free-form libgit2 text is not sound.

**Redaction.** `Redact` removes pack keys, probe keys, 40-hex Git IDs, 64-hex derived keys, `AKIA...` access key IDs, and URL user information, then trims. It runs on every notebook message, decode message, and the config and probe startup diagnostics; engine-open and store-construction errors are not redacted. `redactValues` adds absolute-path removal and is for log `cause` fields only. Caller-facing text must never contain a credential, S3 key, private path, Git ID, or Git vocabulary.

**Fallback.** Any non-domain, non-cancellation error maps to retryable `STORAGE_FAILURE`/`INTERNAL` with a fixed message, so an unexpected internal error never leaks text.

**CLI exit codes.** The process exit code comes from `cli.Run`, which returns `pkg/cmd.Run`'s code: exactly 1 for any `Setup` or `Run` error.

| Situation | Exit | Output |
|---|---|---|
| success, `version` | 0 | stdout |
| `-h` before the first positional | 0 | `[command help]: ...` on stdout |
| domain error from `pull`/`commit` | 1 | full report on stdout; `failed to run: <CODE>` on stderr |
| non-domain error from `pull`/`commit` (cancellation) | 1 | `failed to run: <err>` on stderr |
| `Setup` refusal (argument, config, engine, store, probe) | 1 | one stderr line `<time> error: failed to setup command: <diagnostic>` (config and probe parts redacted), empty stdout |
| flag parse error before the first positional | 1 | `unknown error: failed to parse flagset: ...` on stderr, command listing on stdout |
| missing or unknown command | 1 | command listing on stdout; an unknown command also prints `'<name>' is not a valid argument` on stderr |
| `serve` shutdown deadline expired | 1 | `failed to run: app: shutdown deadline expired` |
| `serve` client EOF or clean signal shutdown | 0 | none |

## Gotchas

- `notebook.ReasonMalformedInput` and `ReasonPathOutsideRoot` exist in the table, but the MCP layer raises those two with its own constants in `mcp/errors.go`; keep the spellings identical.
- `retryable` is decided by code only in `mcp.retryable`; adding a code means updating it.
- The `mcpReqID` is the `diagnosticId`; the CLI report has no diagnostic ID.
- `notebook.Error.Error()` includes the cause text; never render it to a caller. Use `MapError`.
- A backend error must wrap `ErrNotFound`, `ErrPreconditionFailed`, `ErrTransport`, or `ErrIntegrity` (`ErrIncompatible` is probe-only), or the notebook treats it as an opaque failure.

## Related

- [product-contract.md](./product-contract.md): the caller-facing contract these tokens belong to.
- [guarantees.md](./guarantees.md): recovery and `RECOVERY_FAILURE` semantics.
- [mcp-server.md](./mcp-server.md): the envelopes.
- [storage.md](./storage.md), [s3store.md](./s3store.md): where storage sentinels originate.
- [commit.md](./commit.md), [pull.md](./pull.md), [conflicts.md](./conflicts.md): the notebook paths that raise each code.
- [security.md](./security.md): redaction as a security property.
- [AGENTS.md, Conventions](../AGENTS.md#conventions): the stable-API rule.
