# Phase 1: one notebook directory per server

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Bind the MCP server to exactly one notebook directory, remove the `path`
member from both tools, and make `serve` read that directory's record.

## Specification

### Tool schemas

| Tool | Arguments |
| --- | --- |
| `notes_pull` | `{}` |
| `notes_commit` | `{message}` |

`message` keeps its current contract: required, non-null string, validated by
`notebook.ValidateMessage`. The schemas advertise no `path` property and
`additionalProperties: false`. `pullSchema` has an empty `properties` object
and an empty `required` array; `commitSchema` holds only `messageProperty`.

### The `path` member is refused

A present `path` member, of any JSON type or null, is refused before
`RejectUnknown`, so a request carrying both `path` and an unknown field reports
the path refusal. The message is self-contained and names the setting that
does control the directory:

```text
this server serves one notebook directory, set by --workspace-root; remove the path member
```

The result is `INVALID_REQUEST` / `MALFORMED_INPUT` / `FIX_INPUT`, not a
protocol error. Both tools use the same rule and the same sentence.

### Handler

`handler.pull` calls `decodePull` and then `svc.Pull(ctx, svc.Root())`.
`handler.commit` calls `decodeCommit` and then `svc.Commit(ctx, svc.Root(), message)`.
The success envelope reports `svc.Root()`. `handler.path`, `decodeOptionalPath`,
`validatePath`, `pathProperty`, and `maxPathBytes` are deleted. `maxMessageBytes`
stays. The `pathutil`, `filepath`, and `utf8` imports leave `internal/mcp`.

The server instructions and both tool descriptions state one directory and no
`path` member. They keep the pull/edit/commit sequence, the message bound, and
the path-set sentences.

### `serve` reads its directory's record

`cmd/serve` passes `opts = opts.WithReader("")` before `app.Setup`. A new
`ProcessOptions.WithReader(path)` sets `NotebookPath` and `Load` from the
injected environment and leaves `Record` nil. It is the reader-only sibling of
`WithAssociation`, and `status` and `log` may adopt it where they deliberately
never write.

`settingsAssociation.key("")` already maps an empty path to the workspace root,
so `serve` reads the record for its `--workspace-root` and a later `pull`,
`commit`, `status`, or `log` of that directory resolves the same target.
`serve` never writes a record.

### Harness rewrite

One harness has one notebook directory: its `WorkspaceRoot` is the notebook.
`Harness.Root()` returns it. `Harness.Path(rel)` continues to join below it,
but no tool call takes a path.

| Member | Before | After |
| --- | --- | --- |
| `CallTool` | `(name, tool, path, message)` | `(name, tool, message)` |
| `Pull` | `(name, path)` | `(name)` |
| `Commit` | `(name, path, message)` | `(name, message)` |
| `runCall` | carries a path | carries no path |
| `ToolCall.Path` | request path | the reported directory, for envelope assertions |

A scenario that needs two notebooks uses a second harness over the same store
and protocol prefix (`newSharedHarness`), not a second path in one server.
`TestScenarioCommitTwoDisjointCommitsRace` becomes two concurrent harnesses,
each at its own root. `TestScenarioPullDeltaStat` and every other two-directory
row follows the same shape.

### Path-boundary scenarios

The MCP boundary can no longer receive a foreign path, so rows that existed
only to prove that refusal move to the layer that still takes a path:

| Row | New home |
| --- | --- |
| request path outside the workspace root | `internal/app/service_test.go`, `TestServiceRejectsPathOutsideRoot`, calling `Service.Pull`/`Commit` directly |
| concurrent distinct paths in one server | `internal/app/service_test.go`, direct service calls; it proves the CLI's path-taking service, not the MCP surface |
| a symlink or special file below the notebook | unchanged; `internal/workspace` and the remaining `pull`/`commit` scenarios still own it |

The contract that no caller can address a directory outside the notebook stays
proven. A row that only asserted the old MCP `path` member is deleted.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| One notebook directory per server | `WithReader("")`; `svc.Root()` in both handlers | `TestScenarioOneNotebookPerServer` |
| No tool takes a path | schemas; strict decode | `TestPullSchemaHasNoPath`, `TestCommitSchemaHasNoPath` |
| A present `path` is refused, never ignored | `rejectPath` before `RejectUnknown` | `TestDecodePullRefusesPathMember`, `TestDecodeCommitRefusesPathMember` |
| The refusal names `--workspace-root` | one shared sentence | `TestPathMemberRefusalNamesTheSetting` |
| `serve` never writes a record | `Record` stays nil | `TestServeReadsItsDirectoryRecordWithoutWriting` |
| Message bound unchanged | `notebook.MaxMessageBytes` | existing `internal/mcp` rows |

### Human required

None.

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `notes_pull {}` | harness over the fake store | success at the harness root | files materialized at the root | a request to another directory |
| `notes_commit {message}` | harness over the fake store | success at the harness root | publication from the root | a request to another directory |
| `notes_pull {path}` | harness over the fake store | `INVALID_REQUEST`/`MALFORMED_INPUT`/`FIX_INPUT` naming `--workspace-root` | none | a pull, a materialization, a store request |
| `notes_commit {message,path}` | harness over the fake store | the same refusal | none | a commit, a publication |
| `serve --workspace-root <dir>` with a hosted record | `sitetest`, `gatewaytest`, planted `workspaces.json` | MCP pull and commit reach the recorded space | the record is read once | any write to `workspaces.json` |
| two harnesses over one store | shared store and prefix | each root publishes independently | both generations accepted | a shared workspace root |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| The pull schema advertises no `path` and no required member | `TestPullSchemaHasNoPath` |
| The commit schema advertises only `message` | `TestCommitSchemaHasNoPath` |
| A pull with no arguments succeeds at the server root | `TestScenarioPullWithoutPath`, `TestStdioProcessServe` |
| A commit with only `message` succeeds at the server root | `TestScenarioCommitWithoutPath`, `TestStdioProcessServe` |
| A present `path` is refused before an unknown field is considered | `TestDecodePullRefusesPathMember`, `TestDecodeCommitRefusesPathMember`, `TestScenarioPathMemberRefused` |
| The refusal names `--workspace-root` and is not a protocol error | `TestPathMemberRefusalNamesTheSetting` |
| Both handlers call the service with `Root()` | `TestPullUsesServerRoot`, `TestCommitUsesServerRoot` |
| `serve` reads the record of its `--workspace-root` | `TestScenarioServeUsesTheRememberedSpace` |
| `serve` writes no settings file | `TestServeReadsItsDirectoryRecordWithoutWriting` |
| Two harnesses over one store both publish | `TestScenarioCommitTwoDisjointCommitsRace` |
| A path outside the service root is still refused | `TestServiceRejectsPathOutsideRoot` |
| Every existing scenario runs against one directory per harness | `go test ./internal/integrationtest` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| `path` present in `notes_pull` | `INVALID_REQUEST`/`MALFORMED_INPUT`/`FIX_INPUT`, message names `--workspace-root` | `TestDecodePullRefusesPathMember` |
| `path` present in `notes_commit` | the same refusal | `TestDecodeCommitRefusesPathMember` |
| `path` plus an unknown field | the path refusal, not the unknown-field refusal | `TestPathMemberRefusalPrecedesUnknownField` |
| `path` null or non-string | the same refusal | `TestDecodeRefusesAnyPathMember` |
| `message` missing | `INVALID_REQUEST`/`MALFORMED_INPUT`, as today | existing `internal/mcp` rows |
| `message` blank or over bound | the notebook's own reason, as today | existing `internal/mcp` rows |
| arguments not an object | `MALFORMED_INPUT`, as today | existing `internal/mcp` rows |
| a request path outside the service root | `INVALID_REQUEST`/`PATH_OUTSIDE_ROOT`/`FIX_INPUT` | `TestServiceRejectsPathOutsideRoot` |
| `serve` on an unreadable settings file | the existing `unusableSettings` refusal | `TestScenarioServeRefusesUnusableSettings` |

## Implementation notes

Not started.

## Review findings

None.
