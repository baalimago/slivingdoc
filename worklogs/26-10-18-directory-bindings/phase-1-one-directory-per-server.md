# Phase 1 — One notebook directory per server

**Parent:** root · **Class:** contract · [README](README.md)

## Goal

Give the MCP server one notebook directory by implementing the two tool schemas over that one directory, with no `path` member.

## Interface

Written to the MCP tool registry in `internal/mcp`.

| Tool           | Input schema                                                                                                                |
| -------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `notes_pull`   | `{"type": "object", "properties": {}, "required": [], "additionalProperties": false}`                                       |
| `notes_commit` | `{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"], "additionalProperties": false}` |

The server's notebook directory is the configured root, the working directory when any root is configured, or a per-process scratch directory when neither root is configured.

`notes_pull`:

1. A call with an empty argument object pulls at the server's notebook directory and reports that directory. (I1)
2. A present `path` member of any JSON type, null included, is refused before an unknown member is considered; the result is `INVALID_REQUEST` / `MALFORMED_INPUT` / `FIX_INPUT`, never a protocol error. (I1, I2)
3. The refusal names `--workspace-root` and carries no path, key, token, or private path. (I1, I2)

`notes_commit`:

1. A call with `message` alone publishes from the server's notebook directory and reports that directory. (I1)
2. `message` is required, non-null, non-blank UTF-8 without U+0000, and at most 16,384 bytes.
3. A present `path` member is refused exactly as `notes_pull` refuses it (`notes_pull.2`, `notes_pull.3`). (I1, I2)

`serve`:

1. It binds exactly one notebook directory. A configured `--workspace-root` or `SLIVINGDOC_WORKSPACE_ROOT` names it; with neither root configured, the process owns a scratch directory and removes it on exit; otherwise the working directory is the notebook directory. (I1)
2. The two tools operate at that one directory for the lifetime of the process.
3. A tool call cannot switch the directory or the store.

The schema has no type checker. Its clauses are proven by calls through the schema.

Consumes:

- `mcp:Service (import)`: the notebook view both tools call, at its reported directory
- `notebook:Result (import)`: the operation result the success envelope reports

## Acceptance criteria

| #   | Criterion                                                                              | Proves                           | Proof                                                                           |
| --- | -------------------------------------------------------------------------------------- | -------------------------------- | ------------------------------------------------------------------------------- |
| 1   | The published schemas declare no per-call target.                                      | `serve.3`                        | `go test ./internal/integrationtest -run '^TestScenarioToolListing$'`           |
| 2   | A present `path` is refused before any other input is considered.                      | `notes_pull.2`, `notes_commit.3` | `go test ./internal/integrationtest -run '^TestScenarioStrictSchema$'`          |
| 3   | The refusal names `--workspace-root` and echoes no path.                               | `notes_pull.3`                   | `go test ./internal/integrationtest -run '^TestScenarioStrictSchema$'`          |
| 4   | A pull with no arguments operates at and reports the server directory.                 | `notes_pull.1`                   | `go test ./internal/integrationtest -run '^TestScenarioAbsentOrNullArguments$'` |
| 5   | A commit with `message` alone operates at and reports the server directory.            | `notes_commit.1`                 | `go test ./internal/integrationtest -run '^TestScenarioServerDirectory$'`       |
| 6   | A `message` that breaks its rule maps to its own reason.                               | `notes_commit.2`                 | `go test ./internal/integrationtest -run '^TestScenarioStrictSchema$'`          |
| 7   | A server with neither root configured owns a scratch directory and removes it at exit. | `serve.1`, `serve.2`             | `go test ./internal/integrationtest -run '^TestScenarioEphemeralNotebook$'`     |
| 8   | A configured root is the one directory both tools report.                              | `serve.1`                        | `go test ./internal/integrationtest -run '^TestScenarioServerDirectory$'`       |

## Test cases

| Test                                | Level                | Proves                                                                                           | Given                                                                             | Then                                                                                                                               | Must not                                              |
| ----------------------------------- | -------------------- | ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `TestScenarioToolListing`           | integration scenario | `serve.3`, criterion 1                                                                           | a running server and its tool listing                                             | exactly two tools, and neither input schema declares a target member                                                               | open a store                                          |
| `TestScenarioStrictSchema`          | integration scenario | `notes_pull.2`, `notes_pull.3`, `notes_commit.2`, `notes_commit.3`, `I1`, `I2`, criteria 2, 3, 6 | a pulled notebook and one row per invalid input                                   | each row is `INVALID_REQUEST` with its own reason and `FIX_INPUT`; the `path` rows name `--workspace-root`; no store counter moves | reach the store, write the notebook, or echo the path |
| `TestScenarioAbsentOrNullArguments` | integration scenario | `notes_pull.1`, `notes_commit.2`, criterion 4                                                    | absent and null argument objects                                                  | pull succeeds at the server directory; commit refuses `MALFORMED_INPUT`                                                            | write the notebook                                    |
| `TestScenarioServerDirectory`       | integration scenario | `notes_pull.1`, `notes_commit.1`, `serve.1`, criteria 5, 8                                       | servers started with `--workspace-root`, and with no root but a working directory | both tools report and operate at that one directory                                                                                | report any other directory                            |
| `TestScenarioEphemeralNotebook`     | integration scenario | `notes_pull.1`, `notes_commit.1`, `serve.1`, `serve.2`, `I1`, criterion 7                        | a server with neither root configured                                             | both tools report the scratch directory and operate in it, and shutdown removes it                                                 | leave the session directory behind                    |

## Error management

| Failure                                 | Expected outcome                                                               | Test                       |
| --------------------------------------- | ------------------------------------------------------------------------------ | -------------------------- |
| `path` present, any JSON type           | `INVALID_REQUEST` / `MALFORMED_INPUT` / `FIX_INPUT`, naming `--workspace-root` | `TestScenarioStrictSchema` |
| `path` present beside an unknown member | the `path` refusal, not the unknown-member one                                 | `TestScenarioStrictSchema` |
| `message` absent, null, or not a string | `INVALID_REQUEST` / `MALFORMED_INPUT` / `FIX_INPUT`                            | `TestScenarioStrictSchema` |
| `message` blank                         | `INVALID_REQUEST` / `MESSAGE_BLANK` / `FIX_INPUT`                              | `TestScenarioStrictSchema` |
| `message` with U+0000 or invalid UTF-8  | `INVALID_REQUEST` / `MESSAGE_INVALID` / `FIX_INPUT`                            | `TestScenarioStrictSchema` |
| `message` over 16,384 bytes             | `INVALID_REQUEST` / `MESSAGE_TOO_LONG` / `FIX_INPUT`                           | `TestScenarioStrictSchema` |

## Golden diff

None.

## Removes

None.

## Documentation

Update every architecture doc that states the old tool input, in this phase's
commit: `architecture/mcp-server.md` (the schemas, the strict decode, and the
`mcp.Service` view), `architecture/product-contract.md` (the tool table and
the workflow line), `architecture/overview.md` (the public API and the package
map), `architecture/testing.md` (the renamed and removed rows), and
`architecture/decisions.md` (append a decision; never rewrite decision 2).
`architecture/compatibility.md` also records that the tool parameters are not
yet declared stable and that `path` is removed without a deprecation release,
as the maintainer decided on 2026-10-21; no compatibility migration is built.

## Local facts

### Invariants

| ID  | Invariant | Members bound |
| --- | --------- | ------------- |
| I1  | The MCP server binds one notebook directory, and no MCP tool takes a per-call path. | `notes_pull`, `notes_commit`, `serve` |

### Parameters

| Parameter | Default | Decided by |
| --------- | ------- | ---------- |
| Notebook directory with no root configured | a per-process scratch directory | existing `resolveRoots` |
| Notebook directory with a configured root | `--workspace-root`, its variable, or the working directory | existing `resolveRoots` |

## Human required

None.

## Implementation notes

Complete. Worker session: 2026-10-21, session 1 (single worker); session 2
finished the documentation, the review cleanups, and the phase gates after a
context-limit handover.

Code and tests (session 1, all green):

- `internal/mcp`: `Service` is now `Root`, `ReadOnlyPaths`, `WritablePaths`, `Pull(ctx)`, `Commit(ctx, message)`; `handler.path` removed; `successResult` reports `svc.Root()`; `pullSchema` has no properties and `commitSchema` only `message`; `pathProperty`, `maxPathBytes`, `validatePath`, `decodeOptionalPath`, and the `pathutil`/`filepath`/`utf8` imports are gone; instructions and descriptions name one directory.
- `internal/mcp/decode.go`: `decodePull` returns only an error; `decodeCommit` returns the message. `rejectPath` inspects the raw document before `strictjson.Parse` (which rejects null first), so any present `path` of any JSON type, null included, is `MALFORMED_INPUT` before an unknown member, with the message `the notes tools take no path; this server serves the directory named by --workspace-root`.
- `internal/app`: `Service.PullAt`/`CommitAt` are the CLI path-taking seam; `Service.Pull`/`Commit` serve `Service.Root()` for MCP. `Runtime.Pull`/`Commit` call the `At` variants. App fakes and `service_test.go` updated.
- `internal/integrationtest` harness: `Path(rel)` removed; `CallTool`/`Pull`/`Commit`/`runCall` send no `path`; the harness serves `WorkspaceRoot()`. `PrivateDir()`, `PackCacheDir()`, `StateRecord(t)`, `commitFirst`/`commitNext`, `assertVisibleFiles`, `assertRemoteGeneration`, `assertPulledMarker` no longer take a directory. Every scenario now writes into and asserts on `WorkspaceRoot()`.
- `TestScenarioCommitTwoDisjointCommitsRace` split into two harnesses over one store; the recorder assertion is `a: 1 create / 0 replace`, `b: 1 failed create / 1 replace`.
- `scenario_path_security_test.go`: the outside-root MCP row is removed (the capability is gone); overlapping-root startup rows stay.
- `scenario_path_security_unix_test.go`: `exerciseOptionalPathSecurity` plants a symlink and then a FIFO inside the one notebook directory, one at a time, and asserts `INVALID_REQUEST`/`INVALID_CONTENT`/`EDIT_FILES` naming only that entry (`assertProcessPullRefused`).
- `scenario_ephemeral_test.go`: no `path` calls; both tools report and operate in the scratch directory.
- `scenario_transport_test.go`: `assertProcessCallOK`/`assertProcessArgsOK` send no path; a non-empty `path` pins the reported directory; the process scenario uses `h.workspaceRoot`.
- `TestScenarioServerDirectory` (new file `scenario_server_directory_test.go`) covers a configured `--workspace-root` and the working-directory case.
- `TestScenarioToolListing` asserts the schemas declare no target (`notes_pull` no property, `notes_commit` only `message`). `TestScenarioStrictSchema` has rows for `path` of every JSON type (string, null, number, bool, array, object), `path` beside an unknown field, and `path` on commit, each asserting `--workspace-root` and no echo; message rows no longer send `path`. `TestScenarioAbsentOrNullArguments` still proves absent/null arguments.
- `scenario_config_test.go`, `scenario_hosted_test.go`, `scenario_login_test.go`, `scenario_remembered_test.go`, `scenario_readonly_test.go`, `scenario_logging_test.go` updated so spawned `serve` processes serve the workspace root (dedicated subdirectory only where a CLI already used the env root).
- Removed `TestScenarioReadOnlySubdirectoryRequestPath` (tests a removed capability).

Documentation (session 1): `architecture/product-contract.md`,
`architecture/mcp-server.md`, `architecture/overview.md`,
`architecture/decisions.md` (decision 55 appended; decision 2 untouched),
`architecture/compatibility.md` (tool parameters not yet stable; `path`
removed without a deprecation release per the maintainer on 2026-10-21).

Session 2:

- `architecture/security.md`: the key-file and decode rows, the flow, and one
  "one notebook directory" paragraph; the `PATH_OUTSIDE_ROOT` rules became
  the one-shot command's path rule.
- `architecture/running.md`: `--workspace-root` is the directory `serve`
  serves, and the MCP host section tells the agent to send no path.
- `architecture/errors.md`: a `files[].path` is relative to the notebook
  directory; `PATH_OUTSIDE_ROOT` is a one-shot path failure, while a present
  tool `path` is `MALFORMED_INPUT` in the decoder.
- `architecture/testing.md`: the `scenario_server_directory_test.go` row, the
  harness `h.Pull(name)`/`h.Commit(name, msg)` flow, and the
  `StateRecord`/`PackCacheDir` signatures.
- `architecture/overview.md`, `architecture/workspace.md`,
  `architecture/product-contract.md`: "notebook path" wording and the
  at-or-below rule for a one-shot path.
- `examples/seaweedfs/README.md`: the walkthrough calls `notes_pull` with no
  argument and names `--workspace-root` as the served directory.
- `AGENTS.md`: the public API line, the Event Flow preamble, the serve step,
  and a new one-directory invariant.
- `helperProc.closeClient` in `internal/integrationtest/main_test.go` replaces
  17 inline close-and-wait blocks; the ad-hoc `closeHelperClient` is gone and
  `dupl` reports no clone group in the package.
- `TestScenarioStrictSchema` gained the null-message row; the MCP path refusal
  message now says "the notebook path" (the tools take no path, so the error
  is CLI-only).

Command results:

```sh
go test ./internal/mcp ./internal/app -count=1 -timeout 180s      # ok
go test ./internal/integrationtest -run '^(TestScenarioToolListing|TestScenarioStrictSchema|TestScenarioAbsentOrNullArguments|TestScenarioServerDirectory|TestScenarioEphemeralNotebook)$' -count=1 -timeout 600s   # ok
make qa                                                          # ok, coverage 89.5%
go run github.com/mibk/dupl@v1.0.0 -t 80 ./internal/integrationtest   # 0 clone groups
```

## Review findings

### Review 1, 2026-10-21

No phase-1 `R` finding remains. The schemas have no `path` property. The
decoder rejects a present `path` before unknown fields. The process scenarios
prove the configured-root, working-directory, and scratch-directory paths.

Root review filed R1-02 for backend-precedence text in
`architecture/security.md`; that fix is in the shared architecture docs.

### Review 2, 2026-10-21

Verified good: each phase-1 interface clause maps to its integration row.
Strict-schema rows cover every JSON kind of `path`, its precedence over an
unknown field, and missing, null, invalid, blank, and oversized messages.
The configured-root, working-directory, and scratch-server scenarios prove
the directory binding and shutdown cleanup. The error rows assert the stable
category, reason, action, and no-store/no-mutation outcomes.

R1-01 and R1-02 remain closed; the gate policy lists `dupl` separately from
`make qa`, and the storage-selection documents were checked against the
implementation and the authoritative table in `architecture/login.md`.
