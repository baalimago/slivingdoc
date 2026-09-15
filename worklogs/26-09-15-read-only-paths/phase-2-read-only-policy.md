# Phase 2 — Read-only path policy

**Status:** Not Started

**README:** [README.md](README.md)

## Goal

A notebook configured with read-only paths never publishes a change under
them, resets them on a refused commit, restores them on every pull, and
advertises the set in the server instructions, both tool descriptions, and
every result envelope.

## Specification

### Pure helpers (`internal/git/readonly.go`)

- `NormalizeReadOnly(entries []string) (ReadOnlySet, error)` applies the
  README's read-only path semantics: trim one trailing slash, validate each
  entry with `ValidatePath`, drop duplicates, collapse an entry below
  another entry into the outer one, sort by path. An invalid entry returns
  an error naming the entry; entries are notebook-relative, so echoing them
  is safe.
- `ReadOnlySet.Entries() []string` returns the normalized sorted entries; an
  empty set returns an empty, non-nil slice.
- `ReadOnlySet.Covers(path string) bool` is true when the case-folded path
  equals a case-folded entry or begins with it followed by a slash. Folding
  uses the same `cases.Fold` as `ValidateSnapshot`.
- `ReadOnlySet.ChangedUnder(local, base Snapshot) []string` returns, sorted,
  every path covered by the set that is present only in `local`, present
  only in `base`, or present in both with different bytes.
- `ReadOnlySet.Pin(local, base Snapshot) Snapshot` returns `local` with
  every covered file removed and every covered file of `base` added, sorted
  by path. An empty set returns `local` unchanged.

### Notebook (`internal/notebook`)

- `Config.ReadOnlyPaths []string`; `New` normalizes it and fails on an
  invalid entry like the other range checks. The notebook keeps the
  `ReadOnlySet`.
- `Commit`, in the README's commit check order, after `rejectMarkers`: read
  the baseline snapshot from the baseline tree; compute `ChangedUnder`. When
  non-empty: build the pinned snapshot, build its tree, materialize it with
  the unchanged baseline through `applyLocal` under the recovery stage
  named in the README parameters, then return `INVALID_REQUEST` with reason
  `READ_ONLY_PATH`, one `READ_ONLY` file entry per changed path in sorted
  order, and the README's refusal message naming the violated entries. A
  materialize failure follows the existing `applyLocal` mapping to
  `RECOVERY_FAILURE`.
- `Pull`: after the snapshot, build the raw local tree for the diffstat as
  today; when the set is non-empty, merge with the tree of the pinned
  snapshot instead. The diffstat stays raw-local versus merged, so a
  restoration shows as a changed file. With an empty set the pull performs
  no extra tree build.
- The recovery stage constant joins the existing stage names.

### Service and MCP (`internal/app/service.go`, `internal/mcp`)

- `ServiceConfig.ReadOnlyPaths []string`. `NewService` normalizes once and
  fails on an invalid entry. `Service.ReadOnlyPaths() []string` returns the
  entries; every notebook the service opens receives the same slice.
- `mcp.Service` gains `ReadOnlyPaths() []string`.
- `SuccessInfo.ReadOnly []string` (`json:"readOnly"`) and
  `ToolError.ReadOnly []string` (`json:"readOnly"`), both always non-nil.
  The handler sets both from the service after mapping; `MapError` and
  `MapSuccess` initialize an empty slice so the CLI path stays valid until
  Phase 4 passes the set.
- Success text item: the resolved path alone with an empty set; otherwise
  the path, a space, and `(read-only: <entries>)` with entries joined by
  the README's read-only list separator.
- `instructions(root, entries)`: the current sentence, then with a
  non-empty set exactly: ` Read-only paths: <entries>. notes_commit refuses
any change under them, resets those files, and reports READ_ONLY_PATH;
write elsewhere.`
- Both tool descriptions: the current text, then with a non-empty set
  exactly: ` Read-only paths in this server: <entries>; changes under them
are refused and reset.`

### Scenario harness

- `HarnessConfig.ReadOnlyPaths []string` flows into `ServiceConfig`.
- `CallExpectation.ReadOnly []string`, asserted exactly on success and
  error envelopes when non-nil.
- `assertOK` derives the expected text item from the envelope's `readOnly`
  using the same rule as the server, so every existing success assertion
  keeps passing.

### Contract

Update the sections the README lists for this phase: the envelopes and
instructions, the visible-directory rule that every valid file is notebook
state (now: except files under a read-only path, which are ingested from
the baseline), the pull and commit sequences, the conflict section's note
that read-only paths cannot conflict, the recovery stage, and path
security.

### Invariant: a read-only path never reaches R from a configured process

| Bound actor                                     | Mechanism                                                     | Test                                                                   |
| ----------------------------------------------- | ------------------------------------------------------------- | ---------------------------------------------------------------------- |
| commit with a modified covered file             | `ChangedUnder` refusal before any proposal                    | `TestScenarioReadOnlyCommitRefusedAndReset`                            |
| commit with an added or deleted covered file    | same                                                          | `TestScenarioReadOnlyAddAndDeleteRefused`                              |
| commit through a case-variant path              | folded `Covers`                                               | `TestScenarioReadOnlyCaseFoldedEntry`                                  |
| commit after a pull restored the covered files  | local equals baseline under the set; nothing to publish there | `TestScenarioReadOnlyPullRestores`                                     |
| pull merge                                      | `Pin` replaces covered files with the baseline before merge   | `TestScenarioReadOnlyPullRestores`                                     |
| first pull with pre-existing covered files in L | `Pin` against the empty baseline drops them                   | `TestScenarioReadOnlyPullRestores` (fresh subtest)                     |
| a request path below the workspace root         | the same set applies to every notebook of the service         | `TestScenarioReadOnlySubdirectoryRequestPath`                          |
| checkpoint and cleanup                          | operate on accepted trees only; no local ingestion            | `TestScenarioReadOnlyCommitRefusedAndReset` (pack namespace unchanged) |

### Invariant: the set is advertised identically on every surface

| Surface                   | Mechanism                             | Test                                                                           |
| ------------------------- | ------------------------------------- | ------------------------------------------------------------------------------ |
| server instructions       | `instructions(root, entries)`         | `TestScenarioReadOnlyAdvertised`, `TestInstructionsNameReadOnlyPaths`          |
| both tool descriptions    | description suffix                    | `TestScenarioReadOnlyAdvertised`, `TestToolDescriptionsAdvertiseReadOnlyPaths` |
| success envelope and text | handler sets `ReadOnly`; text rule    | `TestScenarioReadOnlyAdvertised`, `TestSuccessTextItemCarriesReadOnly`         |
| error envelope            | handler sets `ReadOnly`               | `TestScenarioReadOnlyAdvertised`, `TestEnvelopesCarryReadOnlyAlways`           |
| refusal message           | names the violated entries            | `TestScenarioReadOnlyCommitRefusedAndReset`                                    |
| empty set                 | every surface byte-identical to today | `TestScenarioReadOnlyEmptySetUnchanged`                                        |

## Integration contract

Two harnesses share one fake store and prefix: `writer` has no read-only
set and plays the human; `agent` is configured with the set. R starts with
`docs/faq.md` = `Q: a\nA: 1\n` and `notes/a.md` = `x\n` at generation 1
unless a row says otherwise.

| Trigger                                                                                            | Collaborators or fakes          | Observable result                                                                                                                                          | Required side effects                                | Prohibited side effects                               |
| -------------------------------------------------------------------------------------------------- | ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | ----------------------------------------------------- |
| agent (`docs`) pulls, writes `docs/faq.md` = `A: 2\n` and `notes/a.md` = `y\n`, commits            | fake store, two harnesses       | `INVALID_REQUEST`, `READ_ONLY_PATH`, `EDIT_FILES`, retryable false, files `[{docs/faq.md, READ_ONLY, []}]`, readOnly `[docs]`, message names `docs`        | `docs/faq.md` = `Q: a\nA: 1\n`; `notes/a.md` = `y\n` | manifest generation change; any new pack object; `OK` |
| the same agent commits again with no further edit                                                  | same                            | `OK`, generation 2, files `[{notes/a.md, 1, 1}]`, readOnly `[docs]`, text item `<path> (read-only: docs)`                                                  | manifest generation 2                                | any change to `docs/faq.md` in R                      |
| agent adds `docs/new.md` = `n\n` and deletes `docs/faq.md`, commits                                | same                            | `READ_ONLY_PATH`, files `[{docs/faq.md, READ_ONLY, []}, {docs/new.md, READ_ONLY, []}]`                                                                     | `docs/faq.md` restored; `docs/new.md` absent         | manifest change                                       |
| agent writes `Docs/x.md` = `n\n`, commits                                                          | same                            | `READ_ONLY_PATH`, files `[{Docs/x.md, READ_ONLY, []}]`                                                                                                     | `Docs/x.md` absent after the call                    | manifest change                                       |
| agent writes `docs/faq.md` = `A: 2\n` and `notes/a.md` = `y\n`, pulls                              | same                            | `OK`, generation 1, files `[{docs/faq.md, 1, 1}]`, readOnly `[docs]`                                                                                       | `docs/faq.md` = `Q: a\nA: 1\n`; `notes/a.md` = `y\n` | any store write                                       |
| fresh agent workspace already holding `docs/faq.md` = `local\n`, first pull                        | same                            | `OK`, files show `docs/faq.md` restored                                                                                                                    | `docs/faq.md` = `Q: a\nA: 1\n`                       | any store write                                       |
| writer commits `docs/faq.md` = `Q: a\nA: 3\n`; agent with `notes/a.md` = `y\n` pulls, then commits | same                            | pull `OK` generation 2 with `docs/faq.md` changed and no conflict; commit `OK` generation 3, files `[{notes/a.md, 1, 1}]`                                  | agent sees `A: 3`                                    | `CONTENT_CONFLICT`                                    |
| agent (`faq.md`) edits root file `faq.md`, commits                                                 | R has `faq.md` and `notes/a.md` | `READ_ONLY_PATH`, files `[{faq.md, READ_ONLY, []}]`                                                                                                        | `faq.md` restored                                    | manifest change                                       |
| agent (`docs`) with request path `<root>/team` pulls, edits `docs/faq.md`, commits                 | same                            | `READ_ONLY_PATH` for `docs/faq.md`                                                                                                                         | file restored under `<root>/team/docs/faq.md`        | manifest change                                       |
| agent (`docs,faq.md`) initializes, lists tools, pulls, commits a docs edit                         | same                            | instructions end with the exact sentence; both descriptions end with the exact sentence; pull readOnly `[docs, faq.md]`; refusal readOnly `[docs, faq.md]` | none beyond the rows above                           | any surface showing a different order or spelling     |
| harness with no set: initialize, list tools, pull, commit before pull                              | fake store                      | instructions and descriptions equal today's text; readOnly `[]` on the success and the error; text item is the bare path                                   | none                                                 | any `(read-only:` text                                |
| agent commit hits a workspace materialize failpoint during the reset                               | workspace failpoint             | `RECOVERY_FAILURE`, `LOCAL_MUTATION_FAILED`, `recovery.stage` = `commit.readonly`, `remoteAccepted` = `no`                                                 | recovery resynchronizes L from R                     | `OK`; manifest change                                 |
| agent commit with a marker block in `notes/a.md` and a docs edit                                   | same                            | `CONTENT_CONFLICT`, `UNRESOLVED_MARKERS`                                                                                                                   | none (no reset yet)                                  | `READ_ONLY_PATH`                                      |

## Acceptance criteria

| Outcome                                                                        | Evidence                                                                                                                                                                                      |
| ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Normalization trims, validates, dedupes, collapses, and sorts                  | `TestNormalizeReadOnly`                                                                                                                                                                       |
| `Covers` matches on segment boundary under case folding only                   | `TestReadOnlyCovers`                                                                                                                                                                          |
| `ChangedUnder` reports added, changed, and deleted covered files, sorted       | `TestReadOnlyChangedUnder`                                                                                                                                                                    |
| `Pin` replaces covered files and leaves an empty set untouched                 | `TestReadOnlyPin`                                                                                                                                                                             |
| `notebook.New` and `app.NewService` refuse an invalid entry                    | `TestNewRejectsInvalidReadOnlyPaths`, `TestNewServiceRejectsInvalidReadOnlyPaths`, `TestNewServiceNormalizesReadOnlyPaths`                                                                    |
| Markers are checked before read-only                                           | `TestCommitMarkersBeforeReadOnly`                                                                                                                                                             |
| Commit refuses, resets, and the next commit publishes the rest                 | `TestScenarioReadOnlyCommitRefusedAndReset`                                                                                                                                                   |
| Add and delete under the set are refused and reset                             | `TestScenarioReadOnlyAddAndDeleteRefused`                                                                                                                                                     |
| A case variant of an entry is covered                                          | `TestScenarioReadOnlyCaseFoldedEntry`                                                                                                                                                         |
| Pull restores covered files and keeps other edits, including a fresh workspace | `TestScenarioReadOnlyPullRestores`                                                                                                                                                            |
| A writer's docs update reaches the agent without conflict                      | `TestScenarioReadOnlyWriterUpdatesFlowToAgent`                                                                                                                                                |
| A file entry protects a root file                                              | `TestScenarioReadOnlyFileEntry`                                                                                                                                                               |
| The set applies to a subdirectory request path                                 | `TestScenarioReadOnlySubdirectoryRequestPath`                                                                                                                                                 |
| All advertising surfaces agree                                                 | `TestScenarioReadOnlyAdvertised`, `TestInstructionsNameReadOnlyPaths`, `TestToolDescriptionsAdvertiseReadOnlyPaths`, `TestSuccessTextItemCarriesReadOnly`, `TestEnvelopesCarryReadOnlyAlways` |
| An empty set changes nothing but the empty `readOnly`                          | `TestScenarioReadOnlyEmptySetUnchanged`, `TestInstructionsNameNotebookRoot` and `TestToolDescriptionsAdvertiseEmptyPathDefault` (unchanged, still passing)                                    |
| A reset failure is a recovery failure with the named stage                     | `TestScenarioReadOnlyResetFailureIsRecovery`                                                                                                                                                  |
| Contract sections describe the shipped behavior                                | reviewer reads the sections the README lists for this phase against the scenario file                                                                                                         |

## Error coverage

| Failure                                          | Expected outcome                                  | Test                                         |
| ------------------------------------------------ | ------------------------------------------------- | -------------------------------------------- |
| entry `..`, `/abs`, `.git/x`, or an empty string | `NormalizeReadOnly` error naming the entry        | `TestNormalizeReadOnly`                      |
| entry equal to another after trailing-slash trim | one entry                                         | `TestNormalizeReadOnly`                      |
| baseline snapshot read fails in commit           | `STORAGE_INTEGRITY`, `ENGINE_FAILED`, no L change | `TestCommitReadOnlyBaselineReadFailure`      |
| pinned tree build fails                          | `INVALID_CONTENT`, no L change                    | `TestCommitReadOnlyPinBuildFailure`          |
| materialize fails during the reset               | `RECOVERY_FAILURE` with stage `commit.readonly`   | `TestScenarioReadOnlyResetFailureIsRecovery` |
| markers and a covered edit in one commit         | `UNRESOLVED_MARKERS` first, no reset              | `TestCommitMarkersBeforeReadOnly`            |

## Implementation notes

Not started.

## Review findings

None.
