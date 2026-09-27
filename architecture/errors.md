# Error taxonomy

How failures are classified from the storage and Git layers up to the MCP tool result and the CLI exit code. The taxonomy is stable API: the `code`, `reason`, `action`, `retryable` flag, and structured `files` are contract; message text is not. This doc answers "which error does a caller see for this failure, and where is that decided?"

Read this when: adding a failure mode, a reason or action token, a store refusal, changing retryability, redaction, the engine `detail` allowlist, strict JSON rejection, or how the CLI exits on an error.

## Key files

| File | Purpose |
|------|---------|
| `internal/notebook/errors.go` | `Code`, `Reason`, `FileReason`, `Action` tokens; `actionForPairing`, `actionFor`; `Error` (`Code`, `Reason`, `Action`, `Message`, `Files`, `Recovery`, `Cause`); `ErrorFile`; `RecoveryReport`, `RemoteAccepted`; constructors `invalidRequest`, `contentConflict`, `storageIntegrity`, `storageFailure` (with `refusalMessage`, `storageSays` and `storeRefusal`), `remoteBusy`, `recoveryFailure` (with `recoveryRefusalMessages`); `isRefusalReason`; internal `errCASLost`, `errManifestRefused`, `errStaleManifest`; `contentConflictFiles` |
| `internal/notebook/notebook.go` | `mapLocalError`, `scanErrorFiles`, `failAfterAccept`, `ValidateMessage` |
| `internal/git/errors.go` | Named engine failures `ErrNoNewObjects`, `ErrObjectMissing`, `ErrEmptyPack`, `ErrHeadRequired`; `UnsupportedModeError` |
| `internal/git/policy.go` | `OverlapError` (both path sets name one path; a startup refusal) |
| `internal/storage/store.go`, `manifest.go`, `key.go`, `sha256.go`, `uuid.go` | Storage sentinels `ErrNotFound`, `ErrPreconditionFailed`, `ErrTransport`, `ErrIncompatible`, `ErrQuotaExceeded`, `ErrRequestLimit`, `ErrRateLimited`, `ErrAccessDenied`, `ErrTooLarge`, `ErrIntegrity`, `ErrInvalidKey`, `ErrInvalidPrefix`, `ErrInvalidSHA256`, `ErrInvalidUUID`; the `Refusal` type |
| `internal/workspace/path.go`, `scan.go` | `ErrInvalidPath`, `PathEscapeError`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidContent`, `ScanError` |
| `internal/strictjson/json.go` | `Parse` rejections (malformed, duplicate field, `null`, non-`uint64` number, trailing data), `RejectUnknown` |
| `internal/mcp/errors.go` | `ToolError`, `ErrorFile`, `ErrorRange`, `RecoveryInfo`, `MapError`, `mapNotebookError`, `safeEngineDetail`, `retryable`, `permanentRefusal`, `invalidPathMessage`, `decodeFailureError`, `invalidRequest`, `Redact`, `redactValues`, the redaction regexes |
| `internal/app/command.go` | `Report` (CLI rendering; returns `errors.New(te.Code)`), `fileReasonWords`, `actionWordings` |
| `internal/app/app.go` | Startup refusal wrapping: `app: invalid configuration: ...`, `app: open native engine: ...`, `app: create object store: ...` / `app: create hosted store: ...` (`realStoreFactory`), and in `checkStore` `app: INCOMPATIBLE_STORE: ...`, `app: hosted storage refused the token: ...` (or `refused the stored login: ...; run 'slivingdoc login' again` for a token from a stored login), `app: hosted storage check failed: ...` |

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
      retryable(code, reason): STORAGE_FAILURE or RECOVERY_FAILURE with STORAGE_FULL,
                               REQUEST_LIMIT, ACCESS_DENIED or OBJECT_TOO_LARGE → false
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
| `STORAGE_FAILURE` | store operation failed without a known accepted result; also the fallback for unknown errors | yes, except reasons `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE` |
| `STORAGE_INTEGRITY` | stored or local state failed validation | no |
| `RECOVERY_FAILURE` | failure after local mutation began, or entry recovery of an earlier such failure; generic recovery ran | yes, except reasons `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE` |
| `INCOMPATIBLE_STORE` | startup probe failed, or the hosted server is not a compatible storage API; never a tool result | n/a (process exits) |

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
| `STORAGE_FAILURE` | `RATE_LIMITED` | `RETRY` |
| `STORAGE_FAILURE` | `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE` | `OPERATOR` |
| `STORAGE_INTEGRITY` | `MANIFEST_INVALID`, `PACK_INVALID`, `HISTORY_INVALID`, `ENGINE_FAILED` | `OPERATOR` |
| `RECOVERY_FAILURE` | `LOCAL_MUTATION_FAILED` | `PULL` if `Recovery.Resynchronized`, else `RETRY` |
| `RECOVERY_FAILURE` | `RATE_LIMITED` | `RETRY` (the store refused the resynchronizing read) |
| `RECOVERY_FAILURE` | `STORAGE_FULL`, `REQUEST_LIMIT`, `ACCESS_DENIED`, `OBJECT_TOO_LARGE` | `OPERATOR` (the store refused the resynchronizing read) |

An unmapped pairing is a programming error: `actionFor` returns `RETRY` plus `errUnknownActionPairing`; the constructors discard that error, so tests must cover every pairing.

**File reasons.** `TEXT_CONFLICT` and `PATH_CONFLICT` (from `contentConflictFiles`: a conflict with content is text, else path), `UNRESOLVED_MARKERS`, `READ_ONLY`, `INVALID_CONTENT`, and `NOT_IN_NOTEBOOK` / `DIFFERS_FROM_NOTEBOOK` (from `guardFirstPull`). Each file carries one-based inclusive `ranges` (`git.MarkerRange`); the array is empty when the reason has no marker block (`READ_ONLY`, `INVALID_CONTENT`, `NOT_IN_NOTEBOOK`, `DIFFERS_FROM_NOTEBOOK`). File paths are relative to the request path in slash form.

**Recovery report.** `RECOVERY_FAILURE` carries `RecoveryReport{Stage, RemoteAccepted ("yes"|"no"|"unknown"), Resynchronized}`, exposed as `ToolError.Recovery`. The call never returns `OK`. See [commit.md](./commit.md).

**Layer mapping.** Storage sentinels are classified by the notebook: `ErrNotFound` on `current` is generation 0, not an error; `ErrPreconditionFailed` on a manifest write is contention (merge and retry, then `REMOTE_BUSY`); `ErrTransport` on a manifest write is ambiguous and resolved by reading back (`PUBLICATION_UNPROVEN` when unprovable); `ErrIntegrity` becomes `STORAGE_INTEGRITY`. Workspace content errors before mutation (`ErrInvalidContent`, `ErrSymlink`, `ErrUnsupportedFile`, `ErrInvalidPath`, `ErrPathCollision`) become `INVALID_REQUEST`/`INVALID_CONTENT` naming the file, and both files of a case-folding collision (`mapLocalError`, `scanErrorFiles`); other workspace errors during an operation become `STORAGE_FAILURE`/`LOCAL_STATE`. That includes failing to take the operation lock at the start of a pull or commit (`holdWorkspace`), a lock wait ended by the request's cancellation or deadline among them: it is a retryable domain error, not a protocol error like a cancellation later in the call. Failures opening the workspace or notebook in `app.Service.notebookFor` (other than invalid-path/symlink), `app: open notebook: ...`, and `app: service is closed` reach `MapError` raw and become `STORAGE_FAILURE`/`INTERNAL`/`RETRY`. A request path outside the workspace root reaches `mcp.MapError` directly as a `workspace` error and becomes `PATH_OUTSIDE_ROOT`, whose message names the root but never the rejected path (`invalidPathMessage`).

**Store refusals.** The account-level sentinels (raised today only by the hosted adapter, [hosted-mode.md](./hosted-mode.md)) are classified inside `storageFailure`: when `storeRefusal` recognizes the cause (`ErrQuotaExceeded` → `STORAGE_FULL`, `ErrRequestLimit` → `REQUEST_LIMIT`, `ErrRateLimited` → `RATE_LIMITED`, `ErrAccessDenied` → `ACCESS_DENIED`, `ErrTooLarge` → `OBJECT_TOO_LARGE`), its reason and fixed message replace the operation's own (`PACK_UPLOAD`, `MANIFEST_READ`, and so on), because the next step is about the account, not the operation. The fixed `ACCESS_DENIED` text covers every cause the adapter maps there (a token missing, revoked, read-only or not granted the space, a space that does not exist, an `--endpoint` that is not the storage API) and asks to check `SLIVINGDOC_TOKEN`, `--space` and `--endpoint`. The notebook does not know whether its token came from `SLIVINGDOC_TOKEN` or a stored login, so this text names the variable in both cases; only the startup refusal distinguishes them ([login.md](./login.md)). When the cause is a `*storage.Refusal` with a non-empty `Message`, the server's sanitized one-line text is appended after `. The storage says: `; that text is untrusted server output and still passes through `Redact`. Only these refusals carry server text into a result. `recoveryFailure` applies the same classification (`storeRefusal` for the reason, `storageSays` for the server's line) to the failure of a recovery's resynchronizing read: the code stays `RECOVERY_FAILURE`, which keeps the rule that a failure after local mutation began is never reported as anything else, while the reason and action are the refusal's. The message is not `storeRefusal`'s, whose "nothing was published" is false after an accepted CAS and unknown at entry: it is `unexpected failure after local mutation started; recovery could not resynchronize the notebook directory: ` followed by the reason's text from `recoveryRefusalMessages`, which names the fix without any publication claim (every entry but `OBJECT_TOO_LARGE`, which asks an operator to check the storage, ends in "then pull"), and the same `The storage says:` line. `actionFor` gives a `RECOVERY_FAILURE` with a refusal reason (`isRefusalReason`) the `STORAGE_FAILURE` pairing's action.

**Strict JSON.** `strictjson` rejects malformed JSON, duplicate names, explicit `null`, numbers that are not unquoted `uint64`, and trailing data. In tool arguments this becomes `INVALID_REQUEST`/`MALFORMED_INPUT`; an absent or `null` `arguments` member is not a failure but the empty object (`parseArguments`). In the manifest (`storage.DecodeManifest`) it becomes `ErrIntegrity`, so `STORAGE_INTEGRITY`/`MANIFEST_INVALID`. The workspace private-state record uses the same parser.

**Engine detail.** `ToolError.Detail` is set only for `ENGINE_FAILED` with a cause that `safeEngineDetail` recognizes: `git.UnsupportedModeError` (names the entry, not a path), `ErrNoNewObjects`, `ErrObjectMissing`, `ErrEmptyPack`, `ErrHeadRequired`. Any other cause yields no detail; the raw cause is logged against `diagnosticId`. This is an allowlist on purpose: sanitizing free-form libgit2 text is not sound.

**Redaction.** `Redact` removes hosted API tokens (`sld_...`, first, so no later pattern cuts a token short), pack keys, probe keys, 40-hex Git IDs, 64-hex derived keys, `AKIA...` access key IDs, and URL user information, then trims. It runs on every notebook message, decode message, the config, probe and hosted-check startup diagnostics, and the hosted store-construction error; engine-open and S3 store-construction errors are not redacted. `redactValues` adds absolute-path removal and is for log `cause` fields only. Caller-facing text must never contain a credential, S3 key, private path, Git ID, or Git vocabulary.

**Fallback.** Any non-domain, non-cancellation error maps to retryable `STORAGE_FAILURE`/`INTERNAL` with a fixed message, so an unexpected internal error never leaks text.

**CLI exit codes.** The process exit code comes from `cli.Run`, which returns `pkg/cmd.Run`'s code: exactly 1 for any `Setup` or `Run` error.

| Situation | Exit | Output |
|---|---|---|
| success, `version` | 0 | stdout |
| `-h` before the first positional | 0 | `[command help]: ...` on stdout |
| domain error from `pull`/`commit` | 1 | full report on stdout; `failed to run: <CODE>` on stderr |
| non-domain error from `pull`/`commit` (cancellation) | 1 | `failed to run: <err>` on stderr |
| `Setup` refusal (argument, config, engine, store, probe or hosted check) | 1 | one stderr line `<time> error: failed to setup command: <diagnostic>` (config, probe and hosted parts redacted), empty stdout |
| flag parse error before the first positional | 1 | `unknown error: failed to parse flagset: ...` on stderr, command listing on stdout |
| missing or unknown command | 1 | command listing on stdout; an unknown command also prints `'<name>' is not a valid argument` on stderr |
| `serve` shutdown deadline expired | 1 | `failed to run: app: shutdown deadline expired` |
| `serve` client EOF or clean signal shutdown | 0 | none |

## Gotchas

- `notebook.ReasonMalformedInput` and `ReasonPathOutsideRoot` exist in the table, but the MCP layer raises those two with its own constants in `mcp/errors.go`; keep the spellings identical.
- `retryable` is decided in `mcp.retryable` by code, and for `STORAGE_FAILURE` and `RECOVERY_FAILURE` also by reason (`permanentRefusal`); adding a code, or a refusal reason a retry cannot fix, means updating it. The CLI report's `retryable:` line comes from the same function.
- The `mcpReqID` is the `diagnosticId`; the CLI report has no diagnostic ID.
- `notebook.Error.Error()` includes the cause text; never render it to a caller. Use `MapError`.
- A backend error must wrap `ErrNotFound`, `ErrPreconditionFailed`, `ErrTransport`, `ErrIntegrity`, or one of the refusal sentinels (`ErrIncompatible` is startup-only), or the notebook treats it as an opaque failure.
- `storageFailure` replaces the reason for a store refusal, so code that tests a `STORAGE_FAILURE` reason never sees the operation's reason for such a cause. A fact about the operation that must survive the replacement goes into the cause chain as a sentinel instead: `publish` wraps a refused manifest write in `errManifestRefused`, which `discardCompaction` tests.

## Related

- [product-contract.md](./product-contract.md): the caller-facing contract these tokens belong to.
- [guarantees.md](./guarantees.md): recovery and `RECOVERY_FAILURE` semantics.
- [mcp-server.md](./mcp-server.md): the envelopes.
- [storage.md](./storage.md), [s3store.md](./s3store.md), [hosted-mode.md](./hosted-mode.md): where storage sentinels originate.
- [commit.md](./commit.md), [pull.md](./pull.md), [conflicts.md](./conflicts.md): the notebook paths that raise each code.
- [security.md](./security.md): redaction as a security property.
- [AGENTS.md, Conventions](../AGENTS.md#conventions): the stable-API rule.
