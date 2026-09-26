# MCP server

The MCP surface of `slivingdoc serve`: exactly two tools, `notes_pull` and `notes_commit`, over stdio. This concern owns the tool schemas and descriptions, the server instructions, the strict argument decoding, the success and error envelopes, per-call logging, and the SDK log demotion. It answers "what does an agent see and send, and how does a notebook result or error become a tool result?"

Read this when: changing a tool description, schema, input rule, the structured result shape, the text item, the instructions string, the read-only/writable advertisement, request logging, or the SDK logger.

## Key files

| File | Purpose |
|------|---------|
| `internal/mcp/server.go` | `Service` (consumer interface), `Server`, `NewServer`, `Server.Serve`/`Connect`, `handler.pull`/`commit`, `handler.path`, `requestLogger`, `newRequestID`, `resultFor`, `successResult`, `successText`, `errorResult`, `errorText`, `instructions`, `readOnlyDescriptionSuffix`, `writableDescriptionSuffix`, `pullSchema`, `commitSchema`, `pullDescription`, `commitDescription` |
| `internal/mcp/decode.go` | `decodePull`, `decodeCommit`, `decodeOptionalPath`, `validatePath`, `maxPathBytes`, `maxMessageBytes` |
| `internal/mcp/success.go` | `SuccessInfo`, `ChangeFile`, `MapSuccess` |
| `internal/mcp/errors.go` | `ToolError`, `ErrorFile`, `ErrorRange`, `RecoveryInfo`, `MapError`, `mapNotebookError`, `safeEngineDetail`, `retryable`, `decodeFailureError`, `invalidRequest`, `Redact`, `redactValues` (see [errors.md](./errors.md)) |
| `internal/mcp/sdklog.go` | `sdkLogger`, `sdkLogHandler` (demote SDK records to DEBUG, drop empty-string attrs) |
| `internal/strictjson/json.go` | `Parse`, `Value.Field`, `Value.RejectUnknown` (strict argument tree) |
| `internal/app/service.go` | `app.Service`, the production implementation of `mcp.Service` |
| `internal/app/app.go` | `Runtime.Serve` builds the server; `app.serve` runs it over `sdk.StdioTransport` |

## Flow

```text
Runtime.Serve → mcp.NewServer(svc, Version, Module(base, "mcp"))
  sdk.NewServer(Implementation{"slivingdoc", Version},
                ServerOptions{Instructions: instructions(root, readOnly, writable),
                              Logger: sdkLogger(logger)})
  AddTool(notes_pull,   pullSchema,   h.pull)
  AddTool(notes_commit, commitSchema, h.commit)
→ app.serve → Server.Serve(ctx, closeTransport{StdioTransport})

tools/call notes_commit {path?, message}
  → handler.commit
      requestLogger → logger.With(mcpReqID, tool); notebook.WithLogger(ctx, logger)
      "tool call started"
      decodeCommit(raw) → strictjson.Parse → RejectUnknown("path","message")
                        → message present and a string
                        → decodeOptionalPath → validatePath (skipped for omitted or "")
                        → notebook.ValidateMessage
        fail → errorResult(decodeFailureError(err))          # isError, INVALID_REQUEST
      path := handler.path(requested)                         # "" → svc.Root()
      svc.Commit(ctx, path, message)
        err → resultFor → MapError → domain? errorResult : protocol error
        ok  → successResult → MapSuccess + ReadOnly/Writable + successText
      "tool call completed" {outcome, duration[, cause]}
```

`notes_pull` is the same with `decodePull` and `svc.Pull`.

## Behavior

**Exactly two tools.** `toolPull = "notes_pull"` and `toolCommit = "notes_commit"`. No prompt, resource or other tool is registered. The `mcp.Service` interface is the whole dependency (`Root`, `ReadOnlyPaths`, `WritablePaths`, `Pull`, `Commit`), so in-memory tests need no S3, Docker or libgit2.

**Schemas.** Both schemas set `additionalProperties: false`. `path` is optional in both (`minLength 0`, `maxLength 4096`); `message` is required for commit (`minLength 1`, `maxLength 16384`). JSON Schema `maxLength` counts code points, so the byte limits are enforced by the decoder, not the schema.

**Strict decode.** `strictjson.Parse` rejects malformed JSON, duplicate field names, explicit `null`, numbers that are not unquoted unsigned 64-bit integers, and trailing data before any semantic check. A call whose `arguments` member is absent or `null` therefore fails `strictjson.Parse` and returns `INVALID_REQUEST`/`MALFORMED_INPUT` (known bug for `notes_pull`, whose arguments are all optional); `{}` is the empty argument object. The decoders then require an object, reject unknown fields, and require string kinds. `validatePath` expands a leading `~` or `~/`, then requires at most 4,096 bytes, valid UTF-8 without U+0000, and an absolute path. An omitted or empty path becomes `""`, which `handler.path` resolves to `svc.Root()`. The at-or-below-root rule is enforced by `workspace.Open` and surfaces as `INVALID_REQUEST`/`PATH_OUTSIDE_ROOT`. The commit message is checked by `notebook.ValidateMessage`, so it fails with the notebook's own reason (`MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, `MESSAGE_INVALID`).

**Success envelope.** `successResult` returns one `TextContent` and `StructuredContent: *SuccessInfo`. `SuccessInfo` has `code: "OK"`, `path` (the resolved directory), `generation`, `filesChanged`, `insertions`, `deletions`, `files` (never nil, empty for a no-op), `readOnly` and `writable` (always present, empty when unset). The text item is the path plus, when configured, `(writable: ...; read-only: ...; <PathSetsNestRule>)`, because many clients forward only the text item to the model.

**Error envelope.** A domain error returns `IsError: true`, one text item from `errorText`, and `StructuredContent: *ToolError`. The text item repeats every safe field: `CODE · REASON`, message, `detail:`, one `file:` line per file with ranges, `action:`, `retryable:`, `diagnosticId:`, `recovery:`, and the path-set lines. `diagnosticId` is the per-call `mcpReqID`, so the operator can find the full cause in the server log. A non-domain error (context cancellation or deadline) is returned as a protocol error, outside the envelope. The mapping rules are in [errors.md](./errors.md).

**Instructions and descriptions.** `instructions` names the notebook directory and tells the agent to pull, edit UTF-8 text files, then commit, omitting `path` except for a subdirectory. With a writable set it adds the writable sentence; with a read-only set it adds the read-only sentence, ending "write elsewhere." when no writable set exists and "where the two sets nest, the longest matching entry decides." when both exist. Both tool descriptions get `writableDescriptionSuffix` then `readOnlyDescriptionSuffix` with the same rule. `mcp/readonly_test.go` pins the exact strings.

**Readonly advertisement.** The path sets are advertised on every surface: instructions, both descriptions, `readOnly`/`writable` arrays of every success and error, and the text item. The server itself enforces nothing; enforcement and the reset happen in the notebook ([commit.md](./commit.md)).

**Per-call logging.** Every call logs `tool call started` (INFO) and `tool call completed` (INFO on `ok`, WARN on `invalid_request` or `error`) with `mcpReqID` (16 hex, `newRequestID`, crypto/rand with a clock fallback), `tool`, `duration`, `outcome`, and for errors `cause` passed through `redactValues` (which also strips absolute paths). The same logger is attached to the context with `notebook.WithLogger`, so checkpoint and cleanup warnings carry the same `mcpReqID`.

**SDK logging.** `sdkLogger` wraps the server logger for `ServerOptions.Logger`: every SDK record (session connected, initialized, disconnected) is re-emitted at DEBUG, and attributes whose value is an empty string (the absent stdio `session_id`) are dropped, in `Handle` and in `WithAttrs`. slivingdoc's own records use the unwrapped logger. See [logging.md](./logging.md).

## Gotchas

- The SDK does not expose the JSON-RPC request ID to handlers; `mcpReqID` is generated here and doubles as `diagnosticId`.
- `Redact` is for caller-facing text; `redactValues` (Redact plus absolute paths) is only for the server log. Never put `redactValues` output or raw `err.Error()` into a tool result.
- The instruction and description strings are asserted verbatim in `internal/mcp` tests and in `integrationtest` scenarios; change both when wording changes.
- `MapSuccess` and `MapError` are also used by the CLI report (`app.Report`), so a field change affects `slivingdoc pull`/`commit` output.
- The server logger must never write to stdout; stdout is the protocol stream.

## Related

- [product-contract.md](./product-contract.md): the public contract of the two tools.
- [errors.md](./errors.md): `ToolError`, codes, reasons, actions, redaction.
- [cli.md](./cli.md): how `serve` runs the server and shuts it down.
- [security.md](./security.md): stdio transport and path rules.
- [logging.md](./logging.md): modules and the SDK wrapper.
- [running.md, MCP host configuration](./running.md#mcp-host-configuration).
- [AGENTS.md, Conventions](../AGENTS.md#conventions): logging and error taxonomy invariants.
