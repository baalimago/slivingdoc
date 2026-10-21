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

Not started.

## Review findings

None.
