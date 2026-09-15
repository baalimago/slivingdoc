# slivingdoc read-only paths worklog

**Status:** Complete

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
| [1. Error reasons and actions](phase-1-error-reasons.md)        | Complete    | Every domain error carries `reason`, `action`, and per-file `reason`; MCP envelope and §2 updated.          |
| [2. Read-only path policy](phase-2-read-only-policy.md)         | Complete    | Commit refuses and restores; pull restores; the set is advertised in instructions, descriptions, envelopes. Review 5 fix: the `INVALID_CONTENT` precondition is pinned and documented; the baseline read is bounded to covered subtrees. |
| [3. Flag, environment, operator docs](phase-3-flag-and-docs.md) | Complete    | `--read-only-paths` resolves like every other flag; help, §17, §24, running.md, AGENTS.md updated.          |
| [4. CLI report](phase-4-cli-report.md)                          | Complete    | The human report renders code, reason, per-file reasons, the next step, and the read-only set.              |
| [5. Quality gate](phase-5-quality-gate.md)                      | Complete    | `make qa` green after the review 5 fix (coverage 84.0% vs. 70% floor); evidence check passes: all 90 cited test names resolve and all 33 tokens have an assertion. |

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
   merge exactly as they do today. Like every pull, this presupposes a
   workspace that passes the content rules: an invalid file under a
   read-only path is refused as `INVALID_CONTENT` naming the file, before
   the restore runs, and must be deleted by the caller (R5-01).
7. Every surface that advertises the read-only set (instructions, tool
   descriptions, success and error envelopes, success text item, CLI
   trailer, refusal message) shows the same normalized set in the same
   order. `readOnly` is present on every success and error envelope, empty
   when nothing is configured.
8. A process without read-only paths behaves byte-for-byte as before this
   effort on MCP, except for the additive fields: `reason`, `action`,
   `files[].reason`, and an empty `readOnly`. On the CLI the success report
   is byte-for-byte unchanged; the error report follows the CLI report
   skeleton below for every error (D7), which is a deliberate change of
   shape, not a regression (R5-02).
9. No error text or data contains a credential, S3 key, private path, Git
   ID, or Git vocabulary. `mcp.Redact` applies to message text; the token
   fields are constants and `readOnly` and `files[].path` are
   notebook-relative paths, so none of them is redacted (R5-03).
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
  docs/faq.md      read-only
  docs/pricing.md  read-only
next: edit the files, then commit
retryable: false
read-only: docs, faq.md
```

```text
CONTENT_CONFLICT · MERGE_CONFLICT
Resolve the conflict blocks before notes_commit.
  notes/today.md  conflict  lines 12-18, 40-42
  notes/plan.md   path conflict
next: edit the files, then commit
retryable: false
```

Rules: the status line is the code, a middle dot, and the reason token; the
message is the second line; one line per file with the path column padded to
the longest path in the report plus two spaces, the file reason rendered as lower-case words
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
| `git` snapshot helpers: match, changed-under, pin, read-covered | 2       | 2        |
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

### Review log

| Round | Date | Record |
| ----- | ---- | ------ |
| Review 1 | 2026-09-15 | Reviewed all five phases independently: re-ran `make qa` (exit 0, coverage 83.9 % against the 70 % floor), built a fresh binary and exercised the startup refusals, read every touched production file and traced invariants 4, 5, 6, and 7 through the commit, pull, conflict, retry, and recovery branches. Verdict: not ready. One major (R1-01) and five minors; no blocker. Cross-cutting: the MCP decode layer is a second error constructor site the Phase 1 classification missed, so any future reason token must be checked at both `internal/notebook` and `internal/mcp/decode.go`. Outcome: Findings R1-01 through R1-07; Phases 1 through 5 reopened. |
| Review 2 | 2026-09-15 | Re-verified every review 1 fix independently: re-ran `make qa` (exit 0, coverage 84.0 % against the 70 % floor) and read the fix diffs. R1-01 resolved: `decodeCommit` calls `notebook.ValidateMessage` and `decodeFailureError` routes the resulting `*notebook.Error` through `mapNotebookError`, proven at the MCP boundary by the extended strict-schema scenario. R1-02 resolved: the diagnostic reads `read-only paths: invalid read-only path "../x": …`, pinned exactly. R1-03 and R1-04 resolved: `CoveringEntry` is the single fold rule and the multi-entry scenario pins the plural message. R1-05 and R1-06 resolved: rune-count padding with a multi-byte fixture; running.md sentence corrected. No new finding. Verdict: ready.  |
| Review 3 | 2026-09-15 | Holistic review by an independent Opus agent of the orchestration and the code, relayed by the orchestrating session. Re-ran `make qa` (exit 0) and the read-only scenarios. Traced all ten invariants and found none broken. Found four evidence gaps of the same class as R1-01 (a token or contract row with no assertion holding it in place), one documentation regression, and one bookkeeping defect. Verdict: implementation sound; not ready until the evidence gaps close. Outcome: findings R3-01 through R3-06; Phases 1 and 4 reopened; the review rows were moved out of the decisions table into this log (R3-06). |
| Review 4 | 2026-09-15 | Final verification by the orchestrating session after the review 3 fixes: `make qa` exit 0 (coverage 84.0 % against the 70 % floor), every R3 pin present in the named scenarios, running.md shows the generic conflict example once and the refusal once, no throwaway test left in the tree. All thirteen findings resolved. Verdict: ready to commit, subject to the maintainer decisions listed in the Review 3 journal entry. |
| Review 5 | 2026-09-15 | Independent review by a fresh session with no memory of the earlier rounds. Re-ran `make qa` (exit 0, coverage 84.0 % against the 70 % floor, npm 35/35) and `go run github.com/mibk/dupl@v1.0.0 -t 80 .` (only the accepted clone group); re-ran the cited-test check mechanically (88 names, zero unresolved). Read every touched production file and traced invariants 3 through 7 through the commit loop (including the CAS retry and the empty remote), the pull conflict branch, and the recovery path. Wrote, ran, and deleted three throwaway scenarios to reach branches no existing scenario covers: the empty remote (sound), a marker block under a read-only path (reset, as designed), and an invalid file under a read-only path (refused as `INVALID_CONTENT`, restore never runs: R5-01). Verdict: implementation correct; not ready to close until R5-01's scenario and two document sentences land. One README self-contradiction (invariant 8 versus D7) corrected in this review (R5-02). Two notes (R5-03 redaction wording, R5-04 whole-tree baseline read per operation) need no change. Outcome: Phases 2 and 5 reopened. |

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

Severities that reopen a phase: blocker, major, minor. A note does not.

| ID    | Severity | Phase                                  | Summary                                                                                              |
| ----- | -------- | -------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| R1-01 | major    | [1](phase-1-error-reasons.md)          | `MESSAGE_BLANK`, `MESSAGE_TOO_LONG`, `MESSAGE_INVALID` unreachable on MCP; decode labels them `MALFORMED_INPUT` — resolved (fix 1) |
| R1-02 | minor    | [3](phase-3-flag-and-docs.md)          | Startup refusal for an invalid entry carries a `git:` prefix — resolved (fix 1)                        |
| R1-03 | minor    | [2](phase-2-read-only-policy.md)       | Plural refusal message and multi-entry attribution have no test — resolved (fix 1)                    |
| R1-04 | minor    | [2](phase-2-read-only-policy.md)       | `violatedEntries` duplicates `ReadOnlySet.Covers` folding logic — resolved (fix 1)                     |
| R1-05 | minor    | [4](phase-4-cli-report.md)             | Path column padded by byte length, misaligning multi-byte paths — resolved (fix 1)                     |
| R1-06 | minor    | [4](phase-4-cli-report.md)             | `docs/running.md` misstates when the `read-only:` trailer appears — resolved (fix 1)                   |
| R1-07 | note     | [4](phase-4-cli-report.md)             | README skeleton illustration corrected to the longest-plus-two rule; resolved in review               |
| R3-01 | minor    | [1](phase-1-error-reasons.md)          | `INVALID_CONTENT` file naming and reason unpinned at the MCP boundary (`TestScenarioContentRules`) — resolved (fix 3) |
| R3-02 | minor    | [1](phase-1-error-reasons.md)          | `PATH_OUTSIDE_ROOT` has no test assertion anywhere — resolved (fix 3)                                  |
| R3-03 | minor    | [1](phase-1-error-reasons.md)          | Acceptance table claims five scenarios were extended that were never touched — resolved (fix 3)        |
| R3-04 | minor    | [1](phase-1-error-reasons.md)          | `RECOVERY_FAILURE` with `RETRY` (resync failed) unpinned at the integration boundary — resolved (fix 3) |
| R3-05 | minor    | [4](phase-4-cli-report.md)             | `docs/running.md` lost its generic conflict report example and repeats the read-only one twice — resolved (fix 3) |
| R3-06 | note     | README                                 | Review rows sat inside the numbered decisions table; moved to a Review log in this review             |
| R5-01 | minor    | [2](phase-2-read-only-policy.md)       | Pull restore never runs on an invalid file under a read-only path (`INVALID_CONTENT` first); unpinned and undocumented — resolved (fix 5) |
| R5-02 | minor    | README                                 | Invariant 8 promised a byte-for-byte CLI while D7 and the skeleton change every error report; invariant reworded in this review — resolved |
| R5-03 | note     | [2](phase-2-read-only-policy.md)       | `readOnly`, `files[].path`, and tokens bypass `mcp.Redact` while the refusal message does not; invariant 9 reworded to the shipped rule — resolved (fix 5) |
| R5-04 | note     | [2](phase-2-read-only-policy.md)       | Whole-baseline `ReadSnapshot` on every commit and pull when the set is non-empty; bounded to covered subtrees by `ReadOnlySet.ReadCovered` — resolved (fix 5) |

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

### 2026-09-15 (Phase 1 execution)

Implemented Phase 1: `notebook.Reason`/`.Action`/`FileReason` and their
complete constant sets, `ConflictFile` renamed `ErrorFile` with a `Reason`
field, every notebook constructor site classified per the phase's table
with no `&Error{...}` literal left outside `errors.go`, a typed
`workspace.ScanError` (plus a minimal `git.PathCollisionError` to carry the
second case-fold-colliding path without parsing message text) so
`INVALID_CONTENT` names its file, and the matching `mcp.ToolError`/
`ErrorFile` fields with the package's own MALFORMED_INPUT/PATH_OUTSIDE_ROOT/
INTERNAL classifications. The scenario harness decoder now refuses any
error envelope with an empty reason, action, or file reason, which every
existing scenario inherits. `docs/slivingdoc-v1.md` §2, §12, and §15 were
updated with the token tables and the per-file/recovery reason text.

While extending the scenario suite, found and fixed two scenario/reality
mismatches (recorded in the phase's implementation notes): a marker block
written before any pull is `Commit`'s own pre-merge rejection
(`UNRESOLVED_MARKERS`), not a three-tree merge conflict, and the existing
`UnprovableNext` fault fixture used by `TestScenarioCommitUnprovableCAS`
actually fails the follow-up proof read (`MANIFEST_READ`), not a genuine
"read succeeded, ID not found" (`PUBLICATION_UNPROVEN`) — a new
`TestScenarioCommitPublicationNotFound` covers the latter with a plain
unlanded transport failure. Also found that `MESSAGE_BLANK`/
`MESSAGE_TOO_LONG`/`MESSAGE_INVALID` are unreachable through the MCP tool
boundary (the package's own decode-time message validation preempts
`notebook.Commit`'s), which is pre-existing behavior outside this phase's
scope; coverage for those three reasons lives in the extended
`TestCommitBlankMessage` notebook unit test instead.

`make qa` (gofumpt, go vet, staticcheck, go fix -diff, `go test -race
-count=3 -timeout=30s -coverpkg=./...`, npm test) and `dupl -t 80` all pass;
coverage is 83.3% against the 70% floor. Phase 1 is Complete. `reason`,
`action`, `ConflictFile`→`ErrorFile.Reason`, `mcp.ToolError.Reason/.Action`,
and the harness `CallExpectation`/`FileExpectation` reason/action pins are
ready for Phase 2 to build on; no README gap found.

### 2026-09-15 (Phase 2 execution)

Implemented Phase 2: `internal/git/readonly.go` (`NormalizeReadOnly`,
`ReadOnlySet.Entries/Covers/ChangedUnder/Pin`); `notebook.Config.ReadOnlyPaths`
and `Notebook.ReadOnlyPaths()`, the commit-time `enforceReadOnly` check
(after markers, before the local tree build) that resets violating files
through the conflict path's `applyLocal` mechanism under the new
`commit.readonly` recovery stage and returns `READ_ONLY_PATH` naming the
violated entries; `Pull`'s pinned-tree merge so read-only paths restore from
the baseline before every merge; `app.ServiceConfig.ReadOnlyPaths` and
`Service.ReadOnlyPaths()`; `mcp.Service.ReadOnlyPaths()`, `SuccessInfo.ReadOnly`,
`ToolError.ReadOnly` (both always non-nil), the exact instruction and
tool-description sentences from the phase spec, and the success text item's
`(read-only: <entries>)` suffix; the scenario harness's
`HarnessConfig.ReadOnlyPaths`, `CallExpectation.ReadOnly`, and the
envelope-decoder's now-mandatory `readOnly` key. Ten new scenario tests in
`internal/integrationtest/scenario_readonly_test.go` cover every
integration-contract row (refusal-and-reset, add/delete, case folding, pull
restoration including a fresh workspace, a writer's update flowing to an
agent without conflict, a root-file entry, a subdirectory request path, all
advertising surfaces together, the empty-set no-op case, and a reset
failure surfacing as `RECOVERY_FAILURE`), plus unit tests in `internal/git`,
`internal/notebook`, and `internal/app` for the pure helpers, the check
order, two injected-failure paths, and configuration validation.

One implementation-only deviation, recorded in the phase's implementation
notes: the phase's "pure helpers" list for `internal/git/readonly.go` has
no method for mapping a violated file back to the read-only entry that
covers it, but the refusal message must name "the violated entries." Added
a small unexported `violatedEntries` helper in `internal/notebook/commit.go`
instead of extending the git package's public surface; this is
implementation detail, not a contract change, and no README gap resulted.

`make qa` (gofumpt, go vet, staticcheck, go fix -diff, the full race/count/
timeout/coverage test run, npm test) and `dupl -t 80` all pass; coverage is
83.8% against the 70% floor. Phase 2 is Complete. `notebook.Config.ReadOnlyPaths`,
`app.ServiceConfig.ReadOnlyPaths`, `mcp.Service.ReadOnlyPaths()`,
`mcp.SuccessInfo.ReadOnly`/`ToolError.ReadOnly`, and the harness
`HarnessConfig.ReadOnlyPaths`/`CallExpectation.ReadOnly` are ready for Phase 3
(the flag) and Phase 4 (the CLI report) to build on.

### 2026-09-15 (Phase 3 execution)

Implemented Phase 3 exactly as specified: `app.Flags.readOnlyPaths` bound
as `--read-only-paths` (help text `notebook paths agents may read but
never change`); `config.readOnlyPaths` resolved flag-over-environment-
over-default (`SLIVINGDOC_READ_ONLY_PATHS`) with a new `splitReadOnlyPaths`
helper (split on `,`, trim, drop empty pieces) and normalized in `finish`
through `git.NormalizeReadOnly`, the error wrapped `"read-only paths:
%w"`; `serviceConfig` copies the normalized entries into
`ServiceConfig.ReadOnlyPaths`; one `FlagReference` row in the existing
column layout; and `app.Runtime.ReadOnlyPaths()` exposing
`Service.ReadOnlyPaths()` for `pull` and `commit`. Added
`TestLoadConfigReadOnlyPaths`, `TestLoadConfigReadOnlyPathsInvalid`,
`TestRuntimeReadOnlyPaths`, `TestPullAcceptsReadOnlyFlag`,
`TestCommitAcceptsReadOnlyFlag`, and three spawned-process scenarios
(`TestScenarioReadOnlyFlagProcess`, `TestScenarioReadOnlyEnvPrecedence`,
`TestScenarioReadOnlyInvalidFlagRefusesStartup`), and extended
`TestLoadConfigEmptyFlagDoesNotFallBackToEnv`,
`TestLoadConfigRefusalRemovesSessionDir`, and
`TestScenarioConfigInvalidAndEarlyExit`. Extended the process-scenario
helper `assertProcessArgsOK` to expect the `(read-only: ...)` text suffix
when a call's `ReadOnly` is non-empty, rather than adding a parallel
helper.

One implementation-only deviation, recorded in the phase's implementation
notes: adding a `[]string` field made `config` uncomparable with `==`, so
the pre-existing `TestLoadConfigDefaults` now uses `reflect.DeepEqual`; no
production behavior changed and no README gap resulted.

Updated `docs/slivingdoc-v1.md` §17 and §24, `docs/running.md` (flag row
plus a new "Read-only paths" section), `AGENTS.md` (key-flags paragraph
and the invariants list), and root `README.md`. The running.md refusal
example deliberately shows today's actual CLI report bytes, not Phase 4's
future `reason`/`next:`/`read-only:` trailer, since that phase has not
built it yet.

`make qa` (gofumpt, go vet, staticcheck, go fix -diff, the full race/count/
timeout/coverage test run, npm test) and `dupl -t 80` all pass; coverage is
83.8% against the 70% floor. Phase 3 is Complete. `app.Flags.readOnlyPaths`/
`config.readOnlyPaths` and `Runtime.ReadOnlyPaths()` are ready for Phase 4
(the CLI report); no README gap found.

### 2026-09-15 (Phase 4 execution)

Implemented Phase 4: `Report` (`internal/app/command.go`) gained the
`readOnly []string` argument (`cmd/pull`/`cmd/commit` pass
`Runtime.ReadOnlyPaths()`) and now maps through `mcp.MapSuccess`/
`mcp.MapError`, setting `.ReadOnly` on the envelope before rendering —
mirroring `internal/mcp/server.go`'s own pattern — so `writeSuccess`/
`writeError` render from `*mcp.SuccessInfo`/`*mcp.ToolError` instead of the
raw `notebook.Result`. Added the `statusSeparator`, the `fileReasonWords`/
`actionWordings` lookup tables (raw-token passthrough, never a panic), and
`painter.dim`. `writeError` renders the status line (code, separator,
reason), the message, one file line per entry padded to the report's
longest path plus two, the file reason as lower-case words with ranges
when present, `next:` with the action wording, `retryable:`, `recovery:`,
and `read-only:` only when non-empty. Extended `TestReport`, added
`TestWriteErrorReadOnly`, `TestWriteErrorAlignsPathColumn`,
`TestWriteSuccessReadOnlyTrailer`, `TestFileReasonWords`, `TestActionWording`,
`TestCommitReportsReadOnlyRefusal`, `TestScenarioCLIReadOnlyCommit`, and
extended `TestScenarioCLIMarkerConflictReport`, `TestScenarioCLICommitBeforePull`,
`TestScenarioCLISharedRemoteConflict`, `TestScenarioCLIColourOnTerminal`, and
`TestCommitReportsMarkerConflict`. Updated `docs/slivingdoc-v1.md` §2 CLI
report (with a byte-verified example) and both stale CLI-byte examples in
`docs/running.md` (the generic report skeleton and the read-only-specific
refusal Phase 3 left in the pre-Phase-4 shape on purpose).

Two deviations, recorded in the phase's implementation notes: (1) the
README's own two-file skeleton illustration is not byte-exact against its
own "longest path plus two" padding rule (this is exactly what the README
anticipates by naming Phase 4 as the byte-fixture owner); every fixture
here was built and verified against the rule, not the illustration's literal
spacing. (2) A marker block written before any pull renders
`UNRESOLVED_MARKERS`/"unresolved markers" (Phase 1's known finding), not
`MERGE_CONFLICT`/"conflict" — `TestScenarioCLIMarkerConflictReport` and
`TestCommitReportsMarkerConflict` fixtures were corrected accordingly, while
the genuine two-writer `TestScenarioCLISharedRemoteConflict` legitimately
keeps `MERGE_CONFLICT`. Neither is a README gap.

`make qa` (gofumpt, go vet, staticcheck, go fix -diff, the full race/count/
timeout/coverage test run, npm test) and `dupl -t 80` (one accepted
table-driven-test clone group, reviewed under the AGENTS.md duplication
policy) all pass; coverage is 83.9% against the 70% floor. The full
`TestScenarioCLI*` suite ran against the real SeaweedFS-backed test
container (Docker available). Phase 4 is Complete; ready for Phase 5.

### 2026-09-15 (Phase 5 execution)

Ran the full validation policy: `go fix -diff`, gofumpt, `go vet`,
staticcheck, `make lint`, `make test` (race, 3 counts, 30 s timeout,
`-coverpkg=./...`), `make npm-test`, `make qa`, and `dupl -t 80`. All green;
coverage held at 83.9% against the 70% floor; the one dupl clone group
(`TestFileReasonWords`/`TestActionWording` in
`internal/app/command_test.go`) is the same one Phase 4 already reviewed
and accepted under the AGENTS.md table-driven-test idiom — no new clone
appeared.

Document coherence sweep: the reason/file-reason/action token tables, the
flag reference, and the contract's decision 38 all agree across
`docs/slivingdoc-v1.md`, `docs/running.md`, `internal/app/config.go`, and
`AGENTS.md`. Found and fixed one genuine gap the checklist named directly:
`AGENTS.md`'s error-taxonomy paragraph never mentioned the additive
`reason`/`action`/`files[].reason`/`readOnly` fields; added two sentences
naming them. Also fixed, as within the phase's broader "documents agree
with shipped behavior" goal though not spelled out in the checklist
bullets: `cmd/pull` and `cmd/commit`'s `helpText` and root `README.md`'s
CLI section still described the pre-Phase-4 error report shape with no
`reason`/`next:`/`read-only:` mention — Phase 4's own implementation notes
had flagged exactly this for Phase 5. Reviewed and left unchanged, with
reasoning recorded in the phase's implementation notes: the README's own
CLI-skeleton illustration pads one space wider than the "longest path + 2"
rule Phase 4's byte fixtures actually follow (the README itself disclaims
this by naming Phase 4 the byte-fixture owner), and the operation-results
worklog is untouched. Re-ran the readiness checklist commands: the
duplicate-test-name grep now also matches `TestReport` (a pre-existing
test both Phase 1 and Phase 4 legitimately extend, not a competing
ownership claim) and the numeral grep now also matches a `-timeout 300s`
verification-command citation in Phase 4's notes (evidence text, not a
duplicated limit) — neither is a defect.

Every phase is now Complete. Status board and this top-level `**Status:**`
updated accordingly.

#### Review 1

The orchestrating session reviewed all five phases after the executing
agents reported them Complete. Re-ran `make qa` independently (exit 0),
built a fresh binary to observe the startup refusals, and traced the
read-only invariants through the commit, pull, conflict, retry, and
recovery branches. Filed R1-01 (major) through R1-07 (note), reopened
Phases 1 through 5, and corrected the README skeleton illustration to the
shipped padding rule. Fixes are routed phase by phase in numeric order;
Phase 5 re-runs its sweep last.

### 2026-09-15 (Phase 1 fix, review 1)

Fixed R1-01 (major): the MCP decode boundary exported
`notebook.ValidateMessage` for `internal/mcp/decode.go`'s `decodeCommit` to
call directly, and `internal/mcp/errors.go` gained `decodeFailureError`,
which copies a `*notebook.Error`'s own reason and action (via the existing
`mapNotebookError`) instead of collapsing every decode failure to
`MALFORMED_INPUT`; structural decode failures (unknown field, null, wrong
type, missing field, an invalid path) still get `MALFORMED_INPUT`
unchanged. `internal/integrationtest/scenario_validation_test.go`'s
`TestScenarioStrictSchema` now pins `MESSAGE_TOO_LONG`, `MESSAGE_BLANK`,
and `MESSAGE_INVALID` (each with `FIX_INPUT`) through the harness for the
three message rows, closing the phase's previously unmet white-space
integration-contract row. No `docs/slivingdoc-v1.md` wording change was
needed: §2 already promised the tokens unconditionally. `make lint`,
`make test` (coverage 83.9%), `make npm-test`, and `dupl -t 80` (the same
pre-existing `TestFileReasonWords`/`TestActionWording` clone group Phase 4
and 5 already accepted) all pass. Phase 1 returns to Complete.

### 2026-09-15 (Phase 2 fix, review 1)

Fixed R1-03 (minor) and R1-04 (minor). `internal/git/readonly.go` gained
`ReadOnlySet.CoveringEntry(path) (string, bool)` next to `Covers`, which now
delegates to it, so the case-fold and segment-boundary rule exists once;
`internal/notebook/commit.go`'s `violatedEntries` calls `CoveringEntry`
instead of re-deriving the fold and boundary test, and the now-unused
`golang.org/x/text/cases` import was removed from the file. Added
`TestReadOnlyCoveringEntry` in `internal/git/readonly_test.go` and a new
black-box scenario, `TestScenarioReadOnlyMultipleViolatedEntries`, in
`internal/integrationtest/scenario_readonly_test.go`, using the set
`docs,faq.md` with one commit editing both `docs/faq.md` and a root
`faq.md` (seeded via an extra writer commit, since the shared seed only
publishes `docs/faq.md` and `notes/a.md`), asserting the exact plural
refusal message, the two `READ_ONLY` file entries in sorted order, and that
both files are reset. `make lint`, `make test` (coverage 84.0%), `make
npm-test`, `make qa` (exit 0), and `dupl -t 80` (the same pre-existing
`TestFileReasonWords`/`TestActionWording` clone group already accepted) all
pass. Phase 2 returns to Complete.

### 2026-09-15 (Phase 3 fix, review 1)

Fixed R1-02 (minor): `internal/git/readonly.go`'s `NormalizeReadOnly`
stopped prefixing its per-entry error with `git: `, since `finish`'s own
`"read-only paths: %w"` wrapper already names the source and no other
configuration refusal in `internal/app/config.go` carries a package
prefix. `TestLoadConfigReadOnlyPathsInvalid` now pins the exact diagnostic
text (after the `"read-only paths: "` wrapper) for the `..` and `/abs`
entries and asserts no row contains `git:`.
`internal/git/readonly_test.go` and `internal/notebook/readonly_test.go`
assert only non-nil errors, not text, so neither needed a change. Verified
with a fresh binary built exactly per the finding's instructions: `commit
--read-only-paths '../x'` and `'/abs'` both now print the diagnostic with
no `git:` prefix. `make qa` (exit 0, coverage 84.0%) and `dupl -t 80` (the
same pre-existing accepted clone group) pass. Phase 3 returns to Complete.

### 2026-09-15 (Phase 4 fix, review 1)

Fixed R1-05 (minor) and R1-06 (minor). `internal/app/command.go`'s
`longestErrorFilePath` and `writeError`'s per-file padding now measure a
path with `utf8.RuneCountInString` instead of `len()` (bytes), so a
multi-byte path such as `docs/résumé.md` pads to the same visual column
as the ASCII rows in the same report; `TestWriteErrorAlignsPathColumn`
gained a third fixture file, `docs/résumé.md`, pinning the aligned
reason column. `docs/running.md`'s domain-error paragraph no longer says
the `read-only:` trailer appears "when the refusal touched one" — it now
says the trailer appears "whenever one is configured, on every success
and error report alike," matching the README skeleton and
`writeSuccess`/`writeError`. `make lint`, `make test` (coverage 84.0%),
`make npm-test`, `make qa` (exit 0), and `dupl -t 80` (the same
pre-existing `TestFileReasonWords`/`TestActionWording` clone group
already accepted) all pass. Phase 4 returns to Complete.

### 2026-09-15 (Phase 5 re-run, review 1)

Re-ran the full validation policy over the tree with Phases 1 through 4's
review-1 fixes landed: `go fix -diff`, gofumpt, `go vet`, staticcheck,
`make lint`, `make test` (race, 3 counts, 30 s timeout,
`-coverpkg=./...`), `make npm-test`, `make qa`, and `dupl -t 80`. All
green; coverage 84.0% against the 70% floor; the one dupl clone group
(`TestFileReasonWords`/`TestActionWording`) is the same one already
reviewed and accepted, unaffected by the review-1 fixes.

Re-ran the document coherence sweep against the fixed files
(`internal/mcp/decode.go`, `internal/mcp/errors.go`,
`internal/mcp/server.go`, `notebook.ValidateMessage`,
`git.ReadOnlySet.CoveringEntry`, the `git:`-prefix removal, and the
rune-count path padding in `internal/app/command.go`): the reason/file-
reason/action token tables, the flag reference, decision 38, AGENTS.md's
invariants and error-taxonomy paragraphs, the CLI help text in
`cmd/pull`/`cmd/commit`, and root `README.md`'s CLI section all still
agree with the shipped behavior. No new incoherence found; no doc edit was
needed this session. The operation-results worklog remains untouched.
Re-ran the README readiness checklist; the same previously-reviewed
duplicate-test-name and numeral-grep hits recur (evidence citations and a
pre-existing shared test, not new defects).

Phase 5 returns to Complete. Every phase on the status board is now
Complete; the README top-level `**Status:**` is set to `Complete`.

#### Review 2

Re-verified every review 1 fix independently (`make qa` exit 0, fix diffs
read, the startup diagnostic observed from a fresh binary). All seven
findings are resolved; no new finding. Verdict: ready. Every phase is
Complete and the worklog status is Complete.

#### Review 3

An independent Opus agent reviewed the orchestration and the code
holistically. It confirmed every invariant and the read-only scenarios, and
found four Phase 1 evidence gaps (R3-01 to R3-04), one docs regression in
Phase 4 (R3-05), and the review rows misplaced in the decisions table
(R3-06, fixed here by moving them to a Review log). Phases 1, 4, and 5
reopened. It also raised decisions for the maintainer that the
orchestrating session will relay: the CLI error report shape changed for
every error, the MCP message text for blank or invalid commit messages
changed, pull discards local edits under read-only paths for the one-shot
CLI too, and `readOnly` entries bypass `mcp.Redact`.

### 2026-09-15 (Phase 1 fix, review 3)

Resolved R3-01 through R3-04, all test-evidence gaps with no production
change: `TestScenarioContentRules` gained pinned `binary file` and
`symlink` rows; `exerciseOptionalPathSecurity` and a new portable row on
`TestScenarioPathSecurityOverlappingRoots` pin `PATH_OUTSIDE_ROOT`/
`FIX_INPUT`, plus a `TestMapErrorServicePath` unit assertion;
`TestScenarioRecoveryBoundaries` and `TestScenarioRecoveryRepairImpossible`
pin `LOCAL_MUTATION_FAILED` with `PULL`/`RETRY` respectively. Two of the
four findings' own test citations were imprecise
(`TestScenarioPathSecurityOverlappingRoots` for R3-02, and
`TestScenarioMalformedToolJSON` for R3-03, the latter a transport-frame
rejection with no envelope to pin at all); both are recorded in the
phase's implementation notes with the real evidence located and, for
`TestScenarioMalformedToolJSON`, the one acceptance-table cell corrected
rather than the underlying claim reworded. `make qa` (exit 0, coverage
84.0%) and `dupl -t 80` (the same pre-existing accepted clone group) pass.
Phase 1 returns to Complete.

### 2026-09-15 (Phase 4 fix, review 3)

Resolved R3-05: `docs/running.md`'s report-skeleton paragraph had drifted to
showing the read-only refusal example (duplicating the block already in the
"Read-only paths" section) instead of a generic conflict example. Replaced
it with a single-file `CONTENT_CONFLICT · MERGE_CONFLICT` block in the
README skeleton's shape, byte-verified against real `writeError` output
(the existing `TestWriteErrorAlignsPathColumn` fixture, plus a throwaway
test written, run, and deleted before finishing — never committed), and
reworded the sentence that introduced it since the old wording only made
sense for the read-only example. The read-only refusal example stays only
in the "Read-only paths" section; nothing else in `docs/running.md`
changed. `make lint`, `make npm-test`, and `make qa` all green (coverage
84.0% against the 70% floor). Phase 4 returns to Complete.

### 2026-09-15 (Phase 5 re-run, review 3)

Re-ran the full validation policy over the tree with review 3's Phase 1
and Phase 4 fixes landed: `go fix -diff`, gofumpt, `go vet`, staticcheck,
`make lint`, `make qa` (race, 3 counts, 30 s timeout, `-coverpkg=./...`,
npm test), and `dupl -t 80`. All green; coverage 84.0% against the 70%
floor; the one dupl clone group (`TestFileReasonWords`/`TestActionWording`)
is the same one every prior session already reviewed and accepted,
unaffected by the review-3 fixes (test-only changes plus a docs-only
`docs/running.md` edit).

Performed review 3's new mechanical evidence check: (a) extracted all 88
`Test…` names cited in the acceptance-criteria, invariant, limit, and
error-coverage tables of phase-1 through phase-4 (excluding implementation
notes and review-findings sections) and confirmed every one resolves to a
`func Test…` somewhere in the tree — no unresolved name. (b) Checked every
one of the 33 reason/file-reason/action tokens in the README's three
tables (confirmed to match `internal/notebook/errors.go`'s constants
exactly, 23/5/5) for at least one test assertion: 28 resolve by a direct
quoted-string grep; the remaining five (`MANIFEST_WRITE`, `LOCAL_STATE`,
`INTERNAL`, `HISTORY_INVALID`, `ENGINE_FAILED`) are asserted through the
typed Go constant instead of the bare string, in
`internal/notebook/errors_test.go`'s `TestActionForEveryReason` table (and
`ENGINE_FAILED` additionally in `readonly_test.go`) — a genuine
value-level assertion, not a documentation-only mention. No unresolved
name and no unasserted token; both checks pass.

Re-ran the document coherence sweep and the README readiness checklist;
no new incoherence and no new cross-phase test-name collision beyond the
ones already identified and cleared in the review-1-fix re-run
(`TestReport`; `TestFileReasonWords`/`TestActionWording`;
`TestNormalizeReadOnly`/`TestNewRejectsInvalidReadOnlyPaths`). Confirmed
`docs/running.md`'s report paragraph now shows a generic conflict example
and the read-only section keeps its own refusal example, per R3-05's fix.

Phase 5 returns to Complete. Every phase on the status board is now
Complete; the README top-level `**Status:**` is set to `Complete`.

#### Review 4

Final verification after the review 3 fixes: gate green, pins present,
docs coherent, worklog Complete. Nothing committed; the maintainer decides
on the four points raised in review 3 before committing.

#### Review 5

An independent session reviewed the finished tree against this README
without reading the earlier rounds' reasoning first. Re-ran every gate and
the review 3 evidence check, read the production code end to end, and
wrote three throwaway scenarios (run, then deleted) to reach the empty
remote, a marker block under a read-only path, and an invalid file under a
read-only path. The first two behave as the contract says; the third
exposes an unstated precondition of invariant 6 and D3: the content rules
run before the restore, so an agent that drops a binary under `docs/` is
told `INVALID_CONTENT` and cannot pull its way out until it deletes the
file. Filed R5-01 (minor) against Phase 2 for a scenario and two document
sentences, corrected invariant 8's stale byte-for-byte CLI promise in place
(R5-02), and recorded two notes (R5-03, R5-04). Phases 2 and 5 reopened;
the worklog status is `Reopened (review 5)`. The four maintainer decisions
from review 3 remain open and unchanged.

### 2026-09-15 (Phase 2 fix, review 5)

The maintainer asked for every review 5 item to be fixed in the reviewing
session. R5-01: added `TestScenarioReadOnlyPullInvalidContentUnderEntry`
pinning `INVALID_CONTENT`/`EDIT_FILES`/`files [{docs/blob, INVALID_CONTENT,
[]}]` on pull and commit with the set `docs`, the untouched local edit
while the file exists, and the restore on the pull after deletion; one
sentence in `docs/slivingdoc-v1.md` §2 "Read-only paths" and one paragraph
in `docs/running.md` state the precondition. R5-03: README invariant 9 now
describes the shipped redaction rule. R5-04: `git.ReadOnlySet.ReadCovered`
reads only covered subtrees of the baseline; `enforceReadOnly` and the
pull's pinned merge use it; `TestReadOnlyReadCovered` proves the exact
covered set and that no uncovered blob is read. Details in the phase's
"Review 5 fix" notes. Phase 2 returns to Complete.

### 2026-09-15 (Phase 5 re-run, review 5)

`make qa` exit 0, coverage 84.0 % against the 70 % floor, dupl unchanged,
90 cited test names all resolve, documents coherent. Every phase is
Complete and the worklog status is `Complete`. Nothing committed; the
remaining maintainer decisions from review 3 are: the CLI error report
shape changed for every error (D7, now stated by invariant 8), the MCP
message text for blank or invalid commit messages changed (R1-01), and
pull discards local edits under read-only paths for the one-shot CLI too
(D3). The `readOnly` redaction point is closed by the invariant 9 wording.
