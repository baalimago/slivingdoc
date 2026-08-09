# Phase 7 — MCP application

**Status:** Complete

**Worklog:** [README](README.md)

**Architecture:** [`../../architecture/slivingdoc-v1.md`](../../architecture/slivingdoc-v1.md)
sections 2 (L26), 17 (L1040), 18 (L1093)

## Goal

Expose the completed notebook service as exactly two MCP tools over stdio.

## Specification

Create `internal/mcp`, `internal/app`, and root `main.go`. Use the official Go
MCP SDK.

Register exactly:

```text
notes_pull(path)
notes_commit(path, message)
```

Both return the text `OK` on success. Tool descriptions tell the caller to edit
UTF-8 text files between pull and commit.

Use strict input schemas from architecture section 2 (L26). Reject unknown fields,
null fields, relative paths, oversized values, blank messages, and U+0000.
Return exactly one text item with `OK` and no structured content on success.
Do not depend on permissive SDK binding. Apply explicit JSON Schema validation
or an equivalent strict decode before the notebook handler runs.

Map the shared error taxonomy to the exact MCP tool-error shape in architecture
section 2 (L26). Always include `code`, `retryable`, `message`, and `files`. Use
one-based inclusive conflict ranges. Add `recovery` only for
`RECOVERY_FAILURE`. Do not use Git terminology in recovery instructions.

Implement flags and environment variables from architecture section 17 (L1040).
Flags take precedence. Validate complete configuration before serving calls.

Use the defaults and numeric bounds in architecture section 17 (L1040).

Stdio writes protocol data only to stdout. Logs use stderr.

Run the S3 compatibility probe and native engine version check before accepting
tool calls. Shutdown stops the transport, waits for bounded operations, and closes
native and local resources. Use the fixed 30-second shutdown deadline and
signal behavior from architecture section 17 (L1040).

## Integration contract

| Trigger                    | Collaborators | Observable result                        | Required side effect           | Prohibited side effect          |
| -------------------------- | ------------- | ---------------------------------------- | ------------------------------ | ------------------------------- |
| MCP `notes_pull` request   | Fake notebook | Text result is `OK`                      | Exact path reaches service     | No third tool registration      |
| MCP `notes_commit` request | Fake notebook | Text result is `OK`                      | Exact message reaches service  | No commit ID in response        |
| Content conflict           | Fake notebook | Stable structured MCP error              | Paths and ranges are preserved | No Git command instruction      |
| Start stdio process        | Real SDK      | MCP initialization and tool call succeed | Logs stay on stderr            | No diagnostic bytes on stdout   |

## Acceptance criteria

- [x] Exactly two tools appear in MCP tool listing.
- [x] Input schemas require the documented fields and reject unknown unsafe values.
- [x] Successful tools return only `OK`.
- [x] Tool errors set `isError`, contain one candid text item, and preserve the exact structured schema.
- [x] Blank commit messages map to invalid request.
- [x] Every shared error category has an MCP mapping test.
- [x] Conflict data survives the SDK error envelope.
- [x] Error file paths are relative normalized paths, while request paths are absolute.
- [x] In-memory MCP tests require no S3, Docker, or native Git engine.
- [x] A real stdio process test completes initialize, list, and both tool calls.
- [x] Stdout remains protocol-only when startup and tool logs are emitted.
- [x] Startup refuses incompatible S3 and libgit2 versions before transport readiness.
- [x] Shutdown and cancellation tests pass under the race detector.
- [x] `--help`, `--version`, environment precedence, and invalid configuration are tested.
- [x] Help and version exit before native initialization or S3 access.
- [x] Process tests prove that SDK success results contain one text item and no structured content.
- [x] Error and log redaction tests reject credentials, S3 keys, private paths, and Git IDs.

## Error coverage

| Failure                          | Expected outcome                               | Required test        |
| -------------------------------- | ---------------------------------------------- | -------------------- |
| Tool JSON is malformed           | SDK invalid-params response                    | Protocol test        |
| Path is outside workspace        | `INVALID_REQUEST` mapping                      | Handler test         |
| Notebook reports conflict        | `CONTENT_CONFLICT` with data                   | Handler test         |
| Notebook reports storage failure | Stable storage category, no secret text        | Redaction test       |
| Stdio input closes               | Clean process shutdown                         | Process test         |
| Compatibility probe fails        | Process exits nonzero with stderr error        | Startup process test |
| Shutdown deadline expires        | Forced bounded shutdown and nonzero diagnostic | Cancellation test    |

## Implementation notes

`internal/mcp` (created in worker session 10, completed and validated in
session 10 continued) owns the strict schemas and decode, the stable
tool-error shape, the SDK envelope mapping, the redactor, and the two-tool
server. `internal/app` owns configuration, dependency construction, the
per-path notebook service, and the serve/shutdown lifecycle; `main.go` is
the two-line process entry.

Configuration follows architecture section 17 exactly: flags override the
environment, which overrides defaults; an explicitly empty flag does not
fall back to an environment value; booleans use `strconv.ParseBool`;
integers are unsigned decimal; the endpoint normalizes scheme/host
case, trailing slash, and a preserved non-root path while rejecting user
information, query, and fragment without echoing them; both roots resolve
absolute and disjoint before any engine or S3 work. `--help` and `--version`
exit before the engine opens, the store is built, or the probe runs.
Invalid configuration writes one redacted diagnostic to stderr.

Startup opens the pinned engine (the runtime version check), builds the
AWS SDK store (`--path-style` forces path-style addressing even without a
custom endpoint), runs the bounded S3 compatibility probe, and only then
serves the transport. `internal/app` adds the `mcp.Server.Serve` seam so
tests inject in-memory transports, and a `closeTransport` wrapper records
the live connection: the signal path cancels the run context and closes
the connection, because the SDK cancels in-flight request-handler contexts
only on a transport read/write failure. The bounded 30-second shutdown
deadline forces a nonzero exit for handlers that ignore cancellation.

`internal/notebook`'s per-path workspace service opens one workspace and
notebook lazily per canonical request path; distinct paths operate
concurrently, each serialized by its own workspace operation lock. Service
errors map through the shared taxonomy in `internal/mcp`; an unrecognized
service error becomes a retryable `STORAGE_FAILURE`, never a leak.

The process tests spawn the test binary itself with `os.StartProcess`
(`os/exec` and `syscall` are forbidden by the phase-2 seam scan) and
intercept the child through `TestMain` on `SLIVINGDOC_PROCESS_HELPER`.
The pipe ends are passed in the correct order (`os.Pipe` returns the read
end first), so the child's stdin is the read end and its stdout is the
write end. The parent drives the child with the SDK client over
`sdk.IOTransport`, records every stdout byte through a tee, and proves
that each line is a complete JSON protocol message; the child's logs land
in a stderr capture file. Startup-refusal helpers use a fake engine that
returns `VersionMismatchError` and a fake store whose probe fails; both
assert a nonzero exit, an empty stdout, and a redacted stderr diagnostic.

The Makefile gate fixes: `native-test` and `native-smoke` name the real
recipes (`test` and `smoke`), `smoke` proves `--version` instead of the
removed version report, and `integration-test` runs the MinIO suites with
`-json` into `.build/integration-test.json` and then fails on any skipped
test through `scripts/check-integration-skips.sh` (a tee pipeline would
have masked the test exit code).

## Review findings

No reviews recorded.
