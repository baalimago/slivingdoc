# slivingdoc read-only paths worklog

**Status:** Not Started

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)

## Goal

Let an operator mark notebook paths as read-only for one slivingdoc process,
so a fleet of agents can read injected material (FAQ answers, documentation)
but can never publish a change to it, while a human process without the
setting keeps full write access. Because the only enforcement point in
slivingdoc is the pull/commit boundary, the feature is a commit-time refusal
that restores the protected files and a pull-time restore from the remote.
The set is advertised on every surface an agent reads, so an agent learns
the rule before it edits and again in the refusal, instead of looping on
the same mistake. The refusal needs a clearer error than today's coarse
envelope offers, so the same effort makes every error carry a stable
`reason` and a next-step `action`, on MCP and in the CLI report, without
changing any existing field.

## Status board

| Phase                                                           | Status      | Outcome                                                                                                     |
| --------------------------------------------------------------- | ----------- | ----------------------------------------------------------------------------------------------------------- |
| [1. Error reasons and actions](phase-1-error-reasons.md)        | In Progress | Every domain error carries `reason`, `action`, and per-file `reason`; MCP envelope and §2 updated.          |
| [2. Read-only path policy](phase-2-read-only-policy.md)         | Not Started | Commit refuses and restores; pull restores; the set is advertised in instructions, descriptions, envelopes. |
| [3. Flag, environment, operator docs](phase-3-flag-and-docs.md) | Not Started | `--read-only-paths` resolves like every other flag; help, §17, §24, running.md, AGENTS.md updated.          |
| [4. CLI report](phase-4-cli-report.md)                          | Not Started | The human report renders code, reason, per-file reasons, the next step, and the read-only set.              |
| [5. Quality gate](phase-5-quality-gate.md)                      | Not Started | `make qa` green, coverage floor held, contract and README coherent.                                         |

Phases complete in numeric order. Phase 2 depends on Phase 1 (the refusal
uses the new reason and action). Phase 3 depends on Phase 2. Phase 4 depends
on Phases 1 through 3 (it renders the reason, the action, and the read-only
set, and its fixtures run with the flag). Phase 5 depends on all of them.
There is no gating phase 0: the design rests on code already read, not on
an unverified external claim.

An executing agent reads this README and only its phase file. Anything two
phases share is written here.

## Strategy

### Evidence the design rests on

- L is a plain caller-owned directory that slivingdoc ingests only at
  operation boundaries (§7.3). Nothing an agent writes reaches the shared
  state R until `Notebook.Commit` publishes it, so the commit is the
  enforcement point. Filesystem permissions cannot enforce this: the agent
  and the server run as one user, and the server needs directory write
  access to rename files into place.
- The commit path already mutates L before returning an error: a merge
  conflict materializes the marker result through `applyLocal` and then
  returns `CONTENT_CONFLICT` (`internal/notebook/commit.go`). Refuse-and-
  restore reuses that exact mechanism, so the recovery invariants of §15
  hold unchanged.
- The error envelope's `files` is documented as "the files this result is
  about" (operation-results worklog), and the scenario harness decodes it
  leniently, so additive fields do not break existing pins.
- The server instructions and both tool descriptions are built once in
  `mcp.NewServer` from the service, and the success text item is what a
  host most often forwards to its model (`internal/mcp/server.go`). All
  three can carry the read-only set with no protocol change.
- The workspace scan reports an invalid file only in error text
  (`internal/workspace/scan.go`), which is why `INVALID_CONTENT` cannot
  name its file today. A typed scan error fixes that in Phase 1.
- Every domain error is built at a bounded set of constructor sites in
  `internal/notebook` and `internal/mcp` (about 57), so classifying each one
  with a reason and an action is a finite table, not an open-ended change.

### Trust boundary

The read-only set is a guardrail at the MCP tool boundary, not a security
boundary against the agent. The serve process holds the S3 credentials. An
agent that can read that environment or launch its own slivingdoc process
bypasses the setting. This matches the operator's existing sftp model: the
policy lives in the server configuration, never in the data.

### Non-negotiable invariants

Every phase preserves:

1. MCP and the one-shot `pull`/`commit` subcommands remain the only public
   APIs and expose the same two operations. Every flag stays shared by
   `serve`, `pull`, and `commit`.
2. The existing error fields keep their meaning and presence: `code`,
   `retryable`, `message`, `files[].path`, `files[].ranges`, and `recovery`
   for `RECOVERY_FAILURE`. The seven codes stay the complete code set. New
   fields are additive.
3. `reason` and `action` are present on every domain error, and
   `files[].reason` on every file entry. No error carries an empty reason
   or action.
4. A process configured with read-only paths never publishes a change under
   a read-only path: no commit from it adds, modifies, or deletes a file
   there.
5. A refused read-only commit leaves R untouched and leaves L equal to the
   caller's edits outside the read-only paths plus the baseline content
   inside them. A refused commit never returns `OK`.
6. A pull always materializes read-only paths from R. Local edits elsewhere
   merge exactly as they do today.
7. Every surface that advertises the read-only set (instructions, tool
   descriptions, success and error envelopes, success text item, CLI
   trailer, refusal message) shows the same normalized set in the same
   order. `readOnly` is present on every success and error envelope, empty
   when nothing is configured.
8. A process without read-only paths behaves byte-for-byte as before this
   effort, on MCP and on the CLI, except for the additive fields: `reason`,
   `action`, `files[].reason`, and an empty `readOnly`.
9. No error text or data contains a credential, S3 key, private path, Git
   ID, or Git vocabulary. `mcp.Redact` applies to the new fields.
10. Colour is presentation-only, gated on a real terminal and `NO_COLOR`.

### Read-only path semantics (normative)

- An entry is a notebook-relative slash path obeying the §7.1 path rules.
  It protects itself and everything below it on a segment boundary: `docs`
  protects a file named `docs` and every path under `docs/`.
- Matching folds case the same way snapshot validation does, so `Docs/`
  cannot slip past `docs` on a case-insensitive host.
- Entries are notebook-relative and identical for every request `path`; a
  request that addresses a subdirectory of the workspace root still sees the
  whole notebook, so the set needs no translation.
- A trailing slash on an entry is trimmed. Duplicate entries and entries
  below another entry collapse to the outer entry. The normalized set is
  sorted by path; this order is what every surface shows. Any entry that
  fails the path rules refuses startup like every other configuration
  error.
- Commit order of checks: message, pulled marker, snapshot, conflict
  markers, then read-only. A read-only violation compares the snapshot
  against the baseline tree in P under every entry; an added, changed, or
  deleted file is a violation. The call materializes the snapshot with the
  read-only subtrees replaced by the baseline's, through the same local
  mutation path as a conflict, then returns the refusal listing every
  violating file.
- Pull replaces the snapshot's files under every entry with the baseline's
  before the merge, so the merge takes R's side there; materialization then
  writes R's content to disk. The pull diffstat is computed from the raw
  snapshot, so the restoration shows up in the success envelope.

### Advertising the read-only set (normative)

The set reaches the agent on four surfaces, so a host that drops one still
leaves the others. With an empty set every surface is byte-identical to
today except the always-present empty `readOnly` array.

| Surface                    | With a non-empty set                                                                                                                                     |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| server instructions        | one extra sentence naming the set and the consequence: a commit that changes a file under a read-only path is refused and the file is reset            |
| both tool descriptions     | one extra sentence naming the set                                                                                                                       |
| success envelope           | `readOnly` array of normalized entries; the text item becomes `<path> (read-only: <entries>)` with entries joined by the read-only list separator      |
| error envelope             | `readOnly` array of normalized entries on every domain error                                                                                            |
| refusal message            | names the violated entries, states that the files were reset, and tells the caller to write outside the read-only paths and commit again              |
| CLI success and error report | one trailer line `read-only: <entries>` after the existing trailer                                                                                    |

Phase 2 owns the exact instruction and description sentences and the
envelope field; Phase 4 owns the CLI trailer bytes.

### Result envelopes (normative)

MCP success structured content after this effort (the operation-results
shape plus `readOnly`):

```json
{
  "code": "OK",
  "path": "/tmp/slivingdoc-7f3a1c/notebook",
  "generation": 18,
  "filesChanged": 1,
  "insertions": 2,
  "deletions": 0,
  "files": [{ "path": "notes/c.md", "insertions": 2, "deletions": 0 }],
  "readOnly": ["docs", "faq.md"]
}
```

MCP error structured content after this effort:

```json
{
  "code": "INVALID_REQUEST",
  "reason": "READ_ONLY_PATH",
  "action": "EDIT_FILES",
  "retryable": false,
  "message": "docs is read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again.",
  "files": [{ "path": "docs/faq.md", "reason": "READ_ONLY", "ranges": [] }],
  "readOnly": ["docs", "faq.md"]
}
```

`reason` is one stable token from the table below, chosen by `code`.
`action` is one stable token telling the caller what to do next. Both are
always present. `files[].reason` is one stable token per file. `readOnly`
is always present on both envelopes. Message text can change; tokens
cannot.

Reason tokens by code (the complete set; Phase 1 classifies every
constructor site into exactly one row):

| Code                | Reason                  | Meaning                                                             | Action                                     |
| ------------------- | ----------------------- | ------------------------------------------------------------------- | ------------------------------------------ |
| `INVALID_REQUEST`   | `MALFORMED_INPUT`       | strict decode failure: unknown field, null, wrong type, size bound  | `FIX_INPUT`                                |
| `INVALID_REQUEST`   | `PATH_OUTSIDE_ROOT`     | request path escapes or is not below the workspace root             | `FIX_INPUT`                                |
| `INVALID_REQUEST`   | `MESSAGE_BLANK`         | commit message only white space                                     | `FIX_INPUT`                                |
| `INVALID_REQUEST`   | `MESSAGE_TOO_LONG`      | commit message over the byte bound                                  | `FIX_INPUT`                                |
| `INVALID_REQUEST`   | `MESSAGE_INVALID`       | commit message not valid UTF-8 or contains U+0000                   | `FIX_INPUT`                                |
| `INVALID_REQUEST`   | `PULL_REQUIRED`         | commit without a managed pull                                       | `PULL`                                     |
| `INVALID_REQUEST`   | `INVALID_CONTENT`       | a visible file violates the notebook contract; `files` names it     | `EDIT_FILES`                               |
| `INVALID_REQUEST`   | `READ_ONLY_PATH`        | commit touched a read-only path; the files were reset               | `EDIT_FILES`                               |
| `CONTENT_CONFLICT`  | `MERGE_CONFLICT`        | the three-tree merge conflicted; markers were written               | `EDIT_FILES`                               |
| `CONTENT_CONFLICT`  | `UNRESOLVED_MARKERS`    | commit found complete marker blocks                                 | `EDIT_FILES`                               |
| `REMOTE_BUSY`       | `RETRIES_EXHAUSTED`     | the CAS lost every attempt in the bound                             | `RETRY`                                    |
| `STORAGE_FAILURE`   | `MANIFEST_READ`         | `current` could not be read                                         | `RETRY`                                    |
| `STORAGE_FAILURE`   | `PACK_DOWNLOAD`         | a referenced pack could not be downloaded                           | `RETRY`                                    |
| `STORAGE_FAILURE`   | `PACK_UPLOAD`           | the proposal pack could not be uploaded                             | `RETRY`                                    |
| `STORAGE_FAILURE`   | `PUBLICATION_UNPROVEN`  | the CAS response was lost and acceptance could not be proved        | `PULL`                                     |
| `STORAGE_FAILURE`   | `MANIFEST_WRITE`        | the manifest CAS failed with a definite error other than a lost precondition | `RETRY`                           |
| `STORAGE_FAILURE`   | `LOCAL_STATE`           | a private-state operation failed before any mutation                | `RETRY`                                    |
| `STORAGE_FAILURE`   | `INTERNAL`              | an unrecognized error; the fallback mapping                         | `RETRY`                                    |
| `STORAGE_INTEGRITY` | `MANIFEST_INVALID`      | `current` failed strict validation                                  | `OPERATOR`                                 |
| `STORAGE_INTEGRITY` | `PACK_INVALID`          | a referenced pack is missing, contradicts its descriptor, or fails import | `OPERATOR`                           |
| `STORAGE_INTEGRITY` | `HISTORY_INVALID`       | the accepted history is missing an object or fails validation       | `OPERATOR`                                 |
| `STORAGE_INTEGRITY` | `ENGINE_FAILED`         | merge, commit, export, or snapshot read failed inside the engine    | `OPERATOR`                                 |
| `RECOVERY_FAILURE`  | `LOCAL_MUTATION_FAILED` | a failure after local mutation began; `recovery` carries the report | `PULL` when `resynchronized`, else `RETRY` |

`INCOMPATIBLE_STORE` is a startup diagnostic that never reaches a tool
result; it stays outside this envelope.

File reason tokens (the complete set):

| Files reason         | Used by                                | Ranges        |
| -------------------- | -------------------------------------- | ------------- |
| `TEXT_CONFLICT`      | `MERGE_CONFLICT` text conflicts        | marker ranges |
| `PATH_CONFLICT`      | `MERGE_CONFLICT` file-versus-directory | empty         |
| `UNRESOLVED_MARKERS` | `UNRESOLVED_MARKERS`                   | marker ranges |
| `READ_ONLY`          | `READ_ONLY_PATH`                       | empty         |
| `INVALID_CONTENT`    | `INVALID_CONTENT`                      | empty         |

Action tokens and their CLI wording (the complete set):

| Action       | Caller meaning                                 | CLI wording                            |
| ------------ | ---------------------------------------------- | -------------------------------------- |
| `FIX_INPUT`  | change the request, then call again            | `correct the request, then call again` |
| `EDIT_FILES` | edit the visible files, then commit            | `edit the files, then commit`          |
| `PULL`       | call pull, then continue                       | `pull, then continue`                  |
| `RETRY`      | repeat the same call                           | `retry the same call`                  |
| `OPERATOR`   | stored state is not trusted; a person must act | `operator attention needed`            |

### CLI report skeleton (normative)

The report keeps the status/detail/trailer skeleton of the operation-results
worklog. Success output gains only the read-only trailer line when the set
is non-empty. The error report becomes:

```text
INVALID_REQUEST · READ_ONLY_PATH
docs is read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again.
  docs/faq.md       read-only
  docs/pricing.md   read-only
next: edit the files, then commit
retryable: false
read-only: docs, faq.md
```

```text
CONTENT_CONFLICT · MERGE_CONFLICT
Resolve the conflict blocks before notes_commit.
  notes/today.md    conflict        lines 12-18, 40-42
  notes/plan.md     path conflict
next: edit the files, then commit
retryable: false
```

Rules: the status line is the code, a middle dot, and the reason token; the
message is the second line; one line per file with the path column padded to
the longest path in the report, the file reason rendered as lower-case words
(`TEXT_CONFLICT` as `conflict`, `PATH_CONFLICT` as `path conflict`,
`UNRESOLVED_MARKERS` as `unresolved markers`, `READ_ONLY` as `read-only`,
`INVALID_CONTENT` as `invalid content`), and ranges after it when present;
the trailer is `next:` with the action wording, `retryable:`, `recovery:`
only for `RECOVERY_FAILURE`, and `read-only:` only when the set is
non-empty. Colour: code red, reason dim, path yellow, file reason dim,
`next:` cyan, `read-only:` dim. With colour off the bytes are exactly the
plain form. Phase 4 owns the exact byte fixtures.

### Shared interfaces between phases

| Interface                                                    | Introduced | Consumed |
| ------------------------------------------------------------ | ---------- | -------- |
| `notebook.Error.Reason`, `.Action`, `ConflictFile.Reason`    | 1          | 2, 4     |
| `mcp.ToolError.Reason`, `.Action`, `ErrorFile.Reason`        | 1          | 2, 4     |
| `workspace.ScanError` (typed: path and kind)                 | 1          | 1        |
| scenario harness `CallExpectation` reason/action pins        | 1          | 2        |
| `git` snapshot helpers: match, changed-under, pin            | 2          | 2        |
| `notebook.Config.ReadOnlyPaths`                              | 2          | 3        |
| `app.ServiceConfig.ReadOnlyPaths`                            | 2          | 3        |
| `mcp.Service.ReadOnlyPaths()` (normalized, sorted set)       | 2          | 2, 4     |
| `mcp.SuccessInfo.ReadOnly`, `mcp.ToolError.ReadOnly`         | 2          | 4        |
| scenario harness `CallExpectation` readOnly pins             | 2          | 3        |
| scenario harness `HarnessConfig.ReadOnlyPaths`               | 2          | 2, 4     |
| `app.Runtime.ReadOnlyPaths()` and the `app.Report` read-only argument | 4  | 4        |
| `app.Flags.readOnlyPaths` and `config.readOnlyPaths`         | 3          | 3, 4     |

### Required architecture sections

| Phase | Required contract sections                            |
| ----- | ----------------------------------------------------- |
| 1     | §2 error envelope, §12 conflict behavior, §15         |
| 2     | §2 envelopes and instructions, §7.3, §10, §11.1, §12, §15, §18.2 |
| 3     | §17 configuration, §24 decisions                      |
| 4     | §2 CLI report                                         |
| 5     | all of the above, AGENTS.md, docs/running.md          |

Re-verify section references after any `docs/slivingdoc-v1.md` edit.

### Reference evidence

This effort has no recorded dataset or fixture. The oracle is the contract
document and the scenario assertions written in each phase.

### Severity taxonomy

| Severity | Meaning                                                      |
| -------- | ------------------------------------------------------------ |
| blocker  | an invariant above is broken or a `make qa` gate fails       |
| major    | a caller-visible deviation from this README or the contract  |
| minor    | internal quality: naming, duplication, a missing unit branch |
| note     | advisory; no change required                                 |

## Parameters and owners

Every default, limit, and tunable lives here once. Phase files refer to rows
by name and never restate a value.

| Parameter                                             | Default                                                                                                                                                                                   | Owner                                                      |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `--read-only-paths` / `SLIVINGDOC_READ_ONLY_PATHS`    | empty: no read-only path                                                                                                                                                                  | Phase 3                                                    |
| read-only entry separator (flag and environment)      | `,`                                                                                                                                                                                       | Phase 3                                                    |
| read-only entry path rules                            | §7.1 rules as implemented by `git.ValidatePath` (4,096-byte path, 255-byte segment); no entry-count limit                                                                                 | Phase 3 (validation), limits inherited from `internal/git` |
| read-only list separator (text item, CLI trailer)     | `, ` (comma, space)                                                                                                                                                                       | Phase 2                                                    |
| `app.ServiceConfig.ReadOnlyPaths`                     | empty slice                                                                                                                                                                               | Phase 2                                                    |
| `notebook.Config.ReadOnlyPaths`                       | empty slice                                                                                                                                                                               | Phase 2                                                    |
| `mcp.SuccessInfo.ReadOnly`, `mcp.ToolError.ReadOnly`  | always present; empty array                                                                                                                                                               | Phase 2                                                    |
| instruction and description sentences                 | exact wording fixed in the Phase 2 specification; each names the set once                                                                                                                 | Phase 2                                                    |
| read-only refusal message                             | `<entries> is read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again.` where `<entries>` lists the violated entries joined by the read-only list separator; `are` replaces `is` for more than one entry | Phase 2 |
| commit check order                                    | message, pulled, snapshot, markers, read-only                                                                                                                                             | Phase 2                                                    |
| recovery stage name of the read-only reset            | `commit.readonly`                                                                                                                                                                         | Phase 2                                                    |
| `notebook.Error.Reason`, `.Action`                    | required; no default                                                                                                                                                                      | Phase 1                                                    |
| `notebook.ConflictFile.Reason`                        | required; no default                                                                                                                                                                      | Phase 1                                                    |
| `mcp.ToolError.Reason`, `.Action`, `ErrorFile.Reason` | required; no default                                                                                                                                                                      | Phase 1                                                    |
| `workspace.ScanError`                                 | typed error with `Path` and `Kind`                                                                                                                                                        | Phase 1                                                    |
| CLI dim colour code                                   | `\x1b[2m`                                                                                                                                                                                 | Phase 4                                                    |
| CLI status separator                                  | `·` (space, U+00B7, space)                                                                                                                                                                | Phase 4                                                    |
| coverage floor                                        | existing `make test` floor of 70 %                                                                                                                                                        | Phase 5 (unchanged)                                        |

## Readiness checklist

The author runs these before requesting validation and records the outcome
in the session journal.

1. No numerals in phase files outside oracle rows:
   `grep -nE '(^|[^=])\b[0-9]+([.,][0-9]+)? ?(s|ms|MB|%|bytes)\b' worklogs/26-09-15-read-only-paths/phase-*.md`
2. Every test name is declared in exactly one phase and one file list:
   `grep -ohE 'Test[A-Za-z0-9_]+' worklogs/26-09-15-read-only-paths/phase-*.md | sort | uniq -d` prints nothing.
3. Every config field, flag, and injectable field has one owner: the
   parameters table above names each; every phase that introduces one
   matches its Owner row.
4. Every invariant and limit in a phase is a table with a test per row.
5. Every phase mentioning listening, manual, or paid has `Human required`:
   `grep -lniE 'listen|manual|paid' worklogs/26-09-15-read-only-paths/phase-*.md` lists only files with that subsection.
6. No phase references text scheduled for deletion.
7. New conventions do not contradict existing code conventions: error
   constructors follow `internal/notebook/errors.go`; flag resolution
   follows `internal/app/config.go`; CLI rendering follows
   `internal/app/command.go`; scenarios follow
   `internal/integrationtest/harness.go`; instructions and descriptions
   follow `internal/mcp/server.go`.

## Decisions log

| ID  | Date       | Decision                                                                                                      | Rationale                                                                                                                   | Replaces   |
| --- | ---------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ---------- |
| D1  | 2026-09-15 | The read-only set is per-process configuration, not notebook or manifest state.                               | A committed policy file would need protecting itself; the manifest is a versioned wire protocol; the CLI needs no override. | —          |
| D2  | 2026-09-15 | A commit that touches a read-only path is refused and the files are restored in the same call.                | Maintainer choice (Q1). Uses the conflict path's local mutation mechanism, so §15 recovery semantics are unchanged.         | —          |
| D3  | 2026-09-15 | Pull restores read-only paths from R and keeps every other local edit.                                        | Maintainer choice (Q2). Self-heals a workspace and is the documented way back after any refusal.                            | —          |
| D4  | 2026-09-15 | One comma-separated `--read-only-paths` flag with an environment variable, shared by all commands.            | Maintainer choice (Q3). Matches the existing flag style; an explicitly empty flag beats an inherited environment value.     | —          |
| D5  | 2026-09-15 | The refusal reuses `INVALID_REQUEST` and adds `reason`, `action`, and `files[].reason` instead of a new code. | Maintainer choice (Q4, Q5). The code set is stable API; additive fields give agents a finer, branchable signal.             | —          |
| D6  | 2026-09-15 | Every domain error carries `reason` and `action`; every file entry carries `reason`.                          | Maintainer choice (Q6). One rule, no optional fields, agents can always branch on `action`.                                 | —          |
| D7  | 2026-09-15 | The CLI report renders reason, per-file reasons, and the next step.                                           | Maintainer choice (Q7). The report is for humans and keeps one look and feel with the MCP envelope.                         | —          |
| D8  | 2026-09-15 | An entry protects itself and its subtree on a segment boundary, matched under case folding.                   | Covers a file or a directory with one rule; case folding closes the `Docs/` gap on case-insensitive hosts.                  | —          |
| D9  | 2026-09-15 | Entries are notebook-relative and identical for every request path.                                           | Every request path materializes the whole notebook, so no translation exists to get wrong.                                  | —          |
| D10 | 2026-09-15 | The action for `READ_ONLY_PATH` is `EDIT_FILES`, not `PULL`.                                                  | D2 restores the files inside the refused call, so the caller only needs to write elsewhere and commit again.                | Q5 preview |
| D11 | 2026-09-15 | `INVALID_CONTENT` names the offending file through a typed workspace scan error.                              | A per-file reason is only honest if the file is listed; today the path exists only in error text.                           | —          |
| D12 | 2026-09-15 | `INCOMPATIBLE_STORE` stays a startup diagnostic outside the tool envelope.                                    | It never reaches a tool result; extending it is outside the goal.                                                           | —          |
| D13 | 2026-09-15 | No cap on the number of entries; each entry obeys the existing path rules.                                    | A cap would be a limit no scenario needs; the path rules already bound each entry.                                          | —          |
| D14 | 2026-09-15 | Commit checks read-only paths after conflict markers and before any Git or S3 work.                           | Cheapest checks first; markers left in place survive the restore because they are outside the read-only paths.              | —          |
| D15 | 2026-09-15 | The pull restoration is visible through the existing diffstat; no restoration-specific success field.         | The diffstat already reports what the pull changed on disk.                                                                 | —          |
| D16 | 2026-09-15 | The read-only set is advertised on four surfaces: instructions, tool descriptions, every envelope and the success text item, and the CLI trailer. | Maintainer choice (Q8). Each surface covers a host that drops another; the agent learns the rule before editing and again in the refusal, which is what breaks the error loop. | — |
| D17 | 2026-09-15 | The refusal message names the violated entries, not just "read-only paths".                                   | The agent must learn the rule, not only the file it hit; `files` lists the files and `readOnly` the whole set.              | maintainer message draft |
| D18 | 2026-09-15 | The CLI report phase runs after the policy and flag phases.                                                   | The report renders the read-only trailer and its fixtures run with the flag, so both must exist first.                      | earlier phase order |
| D19 | 2026-09-15 | `MANIFEST_WRITE` joins the `STORAGE_FAILURE` reasons; `PACK_INVALID` also covers a referenced pack that is missing. | Classifying every constructor site left the definite CAS failure and the missing-pack verdicts without a truthful token. Added while writing Phase 1; flagged for the maintainer. | — |

## Definition of success

1. A serve process started with `--read-only-paths docs` never publishes a
   change under `docs/`: the read-only scenarios in Phase 2 prove a refused
   commit leaves the remote generation and manifest unchanged and the
   restored files byte-identical to the baseline.
2. The same process publishes edits under `notes/` in the commit that
   follows a refusal, with no further pull.
3. A `pull` or `commit` subcommand without the flag writes under `docs/`
   and the agent process observes the new content on its next pull without
   conflict.
4. A serve process started with `--read-only-paths docs,faq.md` names
   `docs` and `faq.md` in its instructions, in both tool descriptions, in
   the `readOnly` array and text item of a pull success, and in the
   `readOnly` array of the refusal; a Phase 2 scenario pins all of them.
5. Every error result on MCP carries non-empty `reason` and `action`, and
   every `files[]` entry carries `reason`; every success and error result
   carries `readOnly`; the harness envelope decoder fails any scenario
   where one is missing.
6. The CLI report matches the skeleton above byte-for-byte in the Phase 4
   fixtures, plain and coloured, with and without the flag.
7. `make qa` passes unedited with the coverage floor held, and
   `docs/slivingdoc-v1.md`, `docs/running.md`, `AGENTS.md`, and the flag
   reference describe the shipped behavior.

## Validation policy

The repository gates apply without exception: `make qa` (lint, test with
race, three counts, the timeout, the coverage floor, npm test), gofumpt,
staticcheck, `go vet`, `go fix -diff`, and the dupl signal reviewed under
the AGENTS.md duplication policy. No phase adds a skip, a build tag, or a
subset command. Scenarios in `internal/integrationtest` are written before
or alongside each behavior change, per AGENTS.md.

## Feedback index

None yet.

## Session journal

### 2026-09-15

Investigated the enforcement point (commit boundary), the existing conflict
path's local mutation, the error envelope, the CLI report, the scenario
harness decoder, and the workspace scan errors. Asked Q1 through Q7 and
recorded the answers as D2 through D7 and D10. Drafted this README.

The maintainer asked how agents learn which paths are read-only so they do
not loop on the refusal. Asked Q8; recorded D16 through D18, added the
advertising section and the `readOnly` envelope field, and moved the CLI
report phase after the policy and flag phases.

The maintainer signed off on the README ("Alright, write up the phases").
Wrote the five phase files. While classifying the error constructor sites
for Phase 1, added D19 to the README.

Readiness checklist run on the five phase files: (1) the numerals grep
prints nothing; (2) the duplicate-test grep prints nothing after two
cross-phase mentions of unchanged tests were reworded; (3) every owner in
the parameters table matches the phase that introduces the field; (4)
every invariant in Phases 1 through 4 is a table with a test per row, and
Phase 3's entry-validation limit names its injectable field; (5) the
human-required grep prints nothing; (6) no phase references text
scheduled for deletion; (7) conventions cite the files listed in the
checklist. Every phase is Not Started. Handed to worklog-validate.
