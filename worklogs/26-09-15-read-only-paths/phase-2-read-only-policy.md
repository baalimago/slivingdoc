# Phase 2 — Read-only path policy

**Status:** Complete

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
- `ReadOnlySet.ReadCovered(repo, tree) (Snapshot, error)` (added by the
  review 5 fix, R5-04) reads only the covered files of a tree, descending
  into a subtree only when it is covered or holds an entry below it, so no
  uncovered blob is read; the result is sorted and validated like
  `ReadSnapshot`. `Commit` and `Pull` read the baseline through it.

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
| refusal message           | names the violated entries            | `TestScenarioReadOnlyCommitRefusedAndReset`, `TestScenarioReadOnlyMultipleViolatedEntries` |
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
| agent pull or commit with a binary file `docs/blob` in L (review 5, R5-01)                         | same                            | `INVALID_REQUEST`, `INVALID_CONTENT`, `EDIT_FILES`, files `[{docs/blob, INVALID_CONTENT, []}]`, readOnly `[docs]`; after deleting the file the next pull restores `docs/faq.md` | none until the file is deleted                       | any restore of `docs/faq.md` while `docs/blob` exists |

## Acceptance criteria

| Outcome                                                                        | Evidence                                                                                                                                                                                      |
| ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Normalization trims, validates, dedupes, collapses, and sorts                  | `TestNormalizeReadOnly`                                                                                                                                                                       |
| `Covers` matches on segment boundary under case folding only                   | `TestReadOnlyCovers`                                                                                                                                                                          |
| `ChangedUnder` reports added, changed, and deleted covered files, sorted       | `TestReadOnlyChangedUnder`                                                                                                                                                                    |
| `Pin` replaces covered files and leaves an empty set untouched                 | `TestReadOnlyPin`                                                                                                                                                                             |
| `CoveringEntry` returns the entry that covers a path, once, shared by `Covers` and `violatedEntries` | `TestReadOnlyCoveringEntry`                                                                                                                                             |
| A refusal that violates two entries names both, sorted, joined, with the plural verb | `TestScenarioReadOnlyMultipleViolatedEntries`                                                                                                                             |
| `notebook.New` and `app.NewService` refuse an invalid entry                    | `TestNewRejectsInvalidReadOnlyPaths`, `TestNewServiceRejectsInvalidReadOnlyPaths`, `TestNewServiceNormalizesReadOnlyPaths`                                                                    |
| Markers are checked before read-only                                           | `TestCommitMarkersBeforeReadOnly`                                                                                                                                                             |
| Commit refuses, resets, and the next commit publishes the rest                 | `TestScenarioReadOnlyCommitRefusedAndReset`                                                                                                                                                   |
| Add and delete under the set are refused and reset                             | `TestScenarioReadOnlyAddAndDeleteRefused`                                                                                                                                                     |
| A case variant of an entry is covered                                          | `TestScenarioReadOnlyCaseFoldedEntry`                                                                                                                                                         |
| Pull restores covered files and keeps other edits, including a fresh workspace | `TestScenarioReadOnlyPullRestores`                                                                                                                                                            |
| A writer's docs update reaches the agent without conflict                      | `TestScenarioReadOnlyWriterUpdatesFlowToAgent`                                                                                                                                                |
| An invalid file under an entry is `INVALID_CONTENT` first; the restore runs once it is deleted (R5-01) | `TestScenarioReadOnlyPullInvalidContentUnderEntry`                                                                                                                                    |
| Only covered subtrees of the baseline are read, no uncovered blob (R5-04)      | `TestReadOnlyReadCovered`                                                                                                                                                                     |
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
| invalid file under an entry on pull or commit    | `INVALID_CONTENT` naming the file; no restore     | `TestScenarioReadOnlyPullInvalidContentUnderEntry` |
| covered-subtree read hits an unreadable tree     | error, no partial snapshot                        | `TestReadOnlyReadCovered`                    |
| markers and a covered edit in one commit         | `UNRESOLVED_MARKERS` first, no reset              | `TestCommitMarkersBeforeReadOnly`            |

## Implementation notes

Session: claude (Sonnet 5), 2026-09-15.

Implemented exactly as specified, with these deviations and decisions:

- **Entry-attribution helper not in the "pure helpers" list.** The refusal
  message must name "the violated entries" (README parameter row), but the
  specified `git.ReadOnlySet` surface (`NormalizeReadOnly`, `Entries`,
  `Covers`, `ChangedUnder`, `Pin`) has no way to map a changed file back to
  the entry that covers it — `Covers` only answers yes/no for the whole
  set. Added an unexported `violatedEntries` helper in
  `internal/notebook/commit.go` that re-derives this mapping with the same
  case-fold comparison, rather than extending the `git` package's public
  surface beyond what the phase named. This is implementation detail, not
  a behavior change; no scenario in this phase exercises a commit that
  violates two entries of a multi-entry set simultaneously (each
  integration-contract row violates exactly one entry), so the sort/dedupe
  logic in `violatedEntries` is exercised by unit coverage of the single-
  violation path only. Flagging for Phase 5 in case broader multi-entry
  violation coverage is wanted.
- **Test names folded together to match the phase's exact citations.** The
  error-coverage table cites `TestNormalizeReadOnly` for both "entry `..`,
  `/abs`, `.git/x`, or an empty string" and "entry equal to another after
  trailing-slash trim". The initial draft split these into a separate
  `TestNormalizeReadOnlyRejectsInvalidEntries` function and omitted the
  trailing-slash-collapse case; both were folded into `TestNormalizeReadOnly`
  itself (an "invalid entries" subtest plus an added table row) so the
  citation is literally true. `TestNormalizeReadOnlyEmptyEntriesNonNil`
  remains as uncited bonus coverage of `Entries()`'s non-nil contract.
- **Recovery-failure and pin-build-failure notebook tests needed dedicated
  fakes.** `TestCommitReadOnlyBaselineReadFailure` and
  `TestCommitReadOnlyPinBuildFailure` each needed a small toggle-able fake
  engine wrapper (`dynamicReadFailRepo`/`toggleWriteTreeRepo` in
  `internal/notebook/readonly_test.go`) rather than the package's existing
  static `readFailEngine`, because the target tree/every-WriteTree-call
  needed to succeed during setup (the pull that establishes the baseline)
  and fail only during the commit under test. Same pattern as the existing
  `atomic.Bool`-gated `Replace` failpoint used in
  `scenario_recovery_test.go`'s `TestScenarioRecoveryConflictMaterialization`,
  applied one level lower (a fake repo method) since the git-level failure
  has no workspace failpoint hook.
- **Chose single-line file contents for every new scenario test.** The
  README's illustrative integration-contract table uses two-line file
  bodies (e.g. `docs/faq.md` = `Q: a\nA: 1\n`) with abbreviated
  single-line edits in later rows; the exact diffstat those rows imply is
  ambiguous from the prose alone. Scenario tests instead use their own
  single-line file bodies (`answer: 1\n` / `answer: 2\n`) so every diffstat
  is an unambiguous one-insertion/one-deletion replacement. The behavior
  proved is identical; only the illustrative byte content differs from the
  README's example.
- Verified the pinned-entry-collapse invariant (`docs/sub` and `docs`
  collapsing to `docs`, including across a case fold) directly in
  `internal/git/readonly_test.go`; no scenario needed since it is a pure
  function.

No gap found in the phase specification itself; the deviations above are
implementation choices to satisfy the same acceptance criteria, not
contract changes.

### Verification

Commands run from the repository root, in order, all passing:

```
go build ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go test ./internal/git/... ./internal/notebook/... ./internal/app/... ./internal/mcp/... ./internal/integrationtest/...
make lint
make test        # -race -count=3 -timeout=30s -coverpkg=./...; coverage 83.8%, floor 70%
make npm-test     # 35/35 node --test cases pass
make qa           # full re-run after the last test-file edit; exit 0
go run github.com/mibk/dupl@v1.0.0 -t 80 .   # 0 clone groups
```

Every acceptance-criteria and error-coverage test named in this phase file
exists and passes; the full test list is unchanged from the tables above
(no test was renamed after the fold described above except as noted).
`make qa` is green with the coverage floor held. `docs/slivingdoc-v1.md`
§2 (envelope JSON, reason/file-reason tables, new "Read-only paths"
subsection), §7.3, §10, §11.1, §12, §15, and §18.2 were updated; every
cross-reference between them was re-checked after editing. No change was
made to `internal/app/config.go`, `internal/app/command.go`, or any CLI
flag/report surface — those remain Phase 3 and Phase 4 territory.

### Review 1 fix (R1-03, R1-04), 2026-09-15

Session: Claude Sonnet 5, 2026-09-15.

**What changed.** `internal/git/readonly.go`: added
`ReadOnlySet.CoveringEntry(path string) (string, bool)` next to `Covers`,
returning the normalized entry that covers `path` (case-folded equality or a
folded segment-boundary prefix) and `true`, or `""`/`false` when no entry
covers it; `Covers` is now `_, ok := s.CoveringEntry(path); return ok`, so
the fold and the segment-boundary test exist in exactly one place.
`internal/notebook/commit.go`: `violatedEntries` now calls
`set.CoveringEntry(path)` per changed path instead of re-deriving the fold
and boundary comparison itself; the `golang.org/x/text/cases` import is
removed from the file (the package's only other use of `cases`/`strings`/
`sort` was this duplicated logic — `sort.Strings` and `strings.Join` are
still used elsewhere in the file and remain imported). No change to
`readOnlyRefusal` or `readOnlyMessage`; the dedupe-and-sort behavior of
`violatedEntries` is unchanged, only its internals.

`internal/git/readonly_test.go`: added `TestReadOnlyCoveringEntry`,
covering an exact match, a nested path, a case variant of both, an entry
that is itself covered as a leaf (`faq.md`), and two non-covered paths
(closing R1-04's test gap for the extracted helper).

`internal/integrationtest/scenario_readonly_test.go`: added
`TestScenarioReadOnlyMultipleViolatedEntries`, a black-box scenario using
the existing `seedReadOnlyBaseline`/`newReadOnlyAgent` helpers. Since the
shared seed only publishes `docs/faq.md` and `notes/a.md`, the writer first
publishes a root `faq.md` in an extra commit (the same pattern
`TestScenarioReadOnlyFileEntry` uses to seed a root-file entry), then an
agent configured with `docs,faq.md` pulls, edits both `docs/faq.md` and
`faq.md` in one call, and commits. The scenario asserts the exact plural
message `docs, faq.md are read-only in this server. Your changes there were
discarded and the files reset. Write outside the read-only paths, then
commit again.` via `decodeEnvelope`, the two `READ_ONLY` file entries in
sorted order (`docs/faq.md` before `faq.md`), `readOnly: [docs, faq.md]`,
and that both files plus the seeded `notes/a.md` are left at their baseline/
unrelated content after the refused commit (closing R1-03's test gap).

No `docs/slivingdoc-v1.md` wording change was needed: the refusal message
and `CoveringEntry`'s behavior are unchanged from what the contract already
describes; this fix only adds coverage and removes a duplication.

**Verification commands run (2026-09-15, review 1 fix):**

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./internal/git/... -run TestReadOnly -v` — pass, including the
  new `TestReadOnlyCoveringEntry`.
- `go test ./internal/notebook/... -run ReadOnly -v` — pass (unaffected;
  proves the `violatedEntries` refactor changed no observable behavior).
- `go test ./internal/integrationtest/... -run TestScenarioReadOnly -v` —
  pass, including the new `TestScenarioReadOnlyMultipleViolatedEntries` and
  every pre-existing read-only scenario.
- `gofumpt -l .` — no output for any file touched by this fix.
- `make lint` (gofumpt, go vet, staticcheck, go fix -diff) — pass.
- `make test` (`go test -race -count=3 -timeout=30s -coverpkg=./...`) —
  pass; `== coverage: 84.0% (floor 70%) ==`.
- `make npm-test` — pass, 35/35.
- `make qa` — exit 0.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group
  (`internal/app/command_test.go:426,446` and `:451,471`), the same
  `TestFileReasonWords`/`TestActionWording` group Phases 1, 4, and 5 already
  reviewed and accepted under the AGENTS.md table-driven-test idiom;
  untouched by this fix, no new clone.

Phase 2 returns to Complete. R1-03 and R1-04 are resolved: the plural
refusal message and the multi-entry sort/dedupe are now exercised by a
scenario at the real MCP boundary, and the entry-covering fold/boundary
logic exists once in `git.ReadOnlySet.CoveringEntry`, shared by `Covers`
and `violatedEntries`.

### Review 5 fix (R5-01, R5-03, R5-04), 2026-09-15

Session: Fable 5.1 (the reviewing session, at the maintainer's request).

**R5-01.** Added `TestScenarioReadOnlyPullInvalidContentUnderEntry` to
`internal/integrationtest/scenario_readonly_test.go`: an agent with the set
`docs` edits `docs/faq.md` and writes a NUL-bearing `docs/blob`; both
`notes_pull` and `notes_commit` are pinned to `INVALID_REQUEST` /
`INVALID_CONTENT` / `EDIT_FILES` with `files [{docs/blob, INVALID_CONTENT,
[]}]` and `readOnly [docs]`, `docs/faq.md` is asserted to still hold the
local edit (the restore never ran), and after `RemoveFile` the next pull
is `OK` with a one-file diffstat and `docs/faq.md` back at the baseline.
Added one sentence to `docs/slivingdoc-v1.md` §2 "Read-only paths" (pull
bullet) and one paragraph to `docs/running.md` "Read-only paths" stating
the precondition. The README invariant 6 already carried the qualifier
from the review. No production change.

**R5-03.** README invariant 9 now states what the code does: `Redact`
applies to message text; tokens are constants and `readOnly` and
`files[].path` are notebook-relative paths, none redacted. No code or
contract change: `docs/slivingdoc-v1.md` never claimed the fields were
redacted.

**R5-04.** `internal/git/readonly.go` gained `ReadOnlySet.ReadCovered(repo,
tree)` with a pruned walk (`walkCovered`, `hasEntryBelow`): a subtree is
entered only when it is covered or some entry lies below it, and a blob is
read only when covered; an empty set reads nothing. `enforceReadOnly`
(`commit.go`) and the pinned merge in `pull.go` read the baseline through
it instead of `git.ReadSnapshot`, so the per-operation cost is bounded by
the read-only subtrees. `ChangedUnder` and `Pin` are unchanged: both only
ever consulted covered files of the baseline, so the semantics are
identical. `TestReadOnlyReadCovered` (`internal/git/readonly_test.go`)
builds a tree with covered, uncovered, nested-under-uncovered, and
case-variant paths, asserts the exact covered file set, counts `ReadBlob`
calls through a wrapper to prove no uncovered blob is read, and covers the
empty set and an unreadable tree. `TestCommitReadOnlyBaselineReadFailure`
still passes unchanged because the injected failure is on the baseline
tree read, which `ReadCovered` performs first.

**Verification.** `go run mvdan.cc/gofumpt@v0.11.0 -l` clean on the touched
files; `go build ./...`; `go vet`; `go test ./internal/git/ -run ReadOnly`,
`./internal/notebook/ -run ReadOnly`, `./internal/integrationtest/ -run
TestScenarioReadOnly` all pass; `go run github.com/mibk/dupl@v1.0.0 -t 80 .`
reports only the accepted `TestFileReasonWords`/`TestActionWording` group;
`make qa` result recorded in the Phase 5 re-run entry.

## Review findings (review 1, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). Gates re-run: `make qa` exit
0. Code read: `internal/git/readonly.go`, `enforceReadOnly`,
`readOnlyRefusal`, `violatedEntries`, and `readOnlyMessage` in
`internal/notebook/commit.go`, the pinned merge in `pull.go`,
`internal/workspace/materialize.go` `applyLocked`, `internal/mcp/server.go`,
`success.go`, `errors.go`, and every scenario in
`internal/integrationtest/scenario_readonly_test.go`.

- [x] **R1-03 (minor, resolved fix 1, 2026-09-15).** The README refusal-message parameter says `are`
  replaces `is` for more than one violated entry, and `violatedEntries`
  dedupes and sorts across entries, but no test exercises a refusal that
  violates two entries (`internal/notebook/commit.go` `readOnlyMessage`,
  `violatedEntries`). Failure scenario: a regression that drops the plural
  branch or the sort passes every gate. Fix: add a scenario with the set
  `docs,faq.md` where one commit edits `docs/faq.md` and `faq.md`, asserting
  the message `docs, faq.md are read-only in this server. …` and the two
  `READ_ONLY` file entries in sorted order.
- [x] **R1-04 (minor, resolved fix 1, 2026-09-15).** `violatedEntries` re-implements `ReadOnlySet.Covers`
  (a second `cases.Fold()` and the segment-boundary test) inside the
  notebook package. Fix: give `git.ReadOnlySet` a `CoveringEntry(path)
  (string, bool)` helper next to `Covers`, use it from `violatedEntries`,
  and cover it in `internal/git/readonly_test.go`, so the fold and boundary
  rule exist once.

Verified good:

- Invariant 4 holds on every commit path: `ChangedUnder` compares the raw
  snapshot with the baseline before any Git or S3 work, and because the
  pinned local side equals the baseline under every entry, the three-tree
  merge takes R's side there on the normal path, the conflict path, and
  every CAS retry. The pull merge uses the pinned tree while the diffstat
  uses the raw tree, so a restoration is visible (D15).
- Invariant 5 holds: the reset materializes through `applyLocal` with the
  unchanged baseline; `applyLocked` writes the pinned snapshot and removes
  files not in it, so an added file disappears and a deleted one returns;
  the `pulled` marker survives and the next commit publishes without a pull
  (`TestScenarioReadOnlyCommitRefusedAndReset`).
- The empty-baseline case (fresh workspace, empty remote) reads a zero tree
  without error, proven by the fresh-workspace subtest.
- Instructions, both descriptions, the success text item, and both envelope
  arrays show the same sorted set (`TestScenarioReadOnlyAdvertised`); an
  empty set is byte-identical to before except `readOnly: []`.

## Review findings (review 2, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). R1-03 and R1-04 resolved and re-verified: `ReadOnlySet.CoveringEntry` is the one fold-and-boundary rule, used by `Covers` and `violatedEntries`; `TestScenarioReadOnlyMultipleViolatedEntries` pins the plural refusal and sorted file entries at the MCP boundary. No new finding.

## Review findings (review 5, 2026-09-15)

Reviewer: independent session (Fable 5.1), starting from the worklog with
no memory of the earlier rounds' reasoning. Gates re-run from the working
tree: `make qa` exit 0 (lint clean, every package `ok` under `-race
-count=3 -timeout=30s -coverpkg=./...`, coverage 84.0 % against the 70 %
floor, npm suite 35/35); `go run github.com/mibk/dupl@v1.0.0 -t 80 .`
reports only the accepted `TestFileReasonWords`/`TestActionWording` clone
group. Code read in full: `internal/git/readonly.go`, `enforceReadOnly`,
`readOnlyRefusal`, `violatedEntries`, `readOnlyMessage` and the whole
`Commit`/`attemptPublication` loop in `internal/notebook/commit.go`, the
pinned merge in `pull.go`, every constructor in `errors.go`,
`internal/mcp/{server,errors,success,decode}.go`, and every scenario in
`scenario_readonly_test.go`. Three throwaway scenarios were written, run
against the in-process harness, and deleted (never committed) to exercise
branches no existing scenario reaches; their outcomes are cited below.

- [x] **R5-01 (minor, resolved fix 5, 2026-09-15).** Invariant 6 ("a pull always materializes read-only
  paths from R") and D3 ("the documented way back after any refusal") have
  an unenumerated branch. `Pull` (`internal/notebook/pull.go`) takes the
  workspace snapshot and builds the raw local tree *before* the read-only
  pin runs, and `Commit` does the same through `n.ws.Snapshot` before
  `enforceReadOnly`, so an invalid file under a read-only path is refused
  as `INVALID_CONTENT` and the restore never runs. Verified with a probe: an
  agent with the set `docs` writes `docs/blob` containing a NUL byte; both
  `notes_pull` and `notes_commit` then return `INVALID_REQUEST` /
  `INVALID_CONTENT` / `EDIT_FILES` with `files [{docs/blob,
  INVALID_CONTENT, []}]`, and `docs/blob` stays on disk. Failure scenario: an
  agent that drops a binary or symlink under `docs/` cannot pull its way
  out; the "pull heals" guidance in D3, §2 "Read-only paths", and
  `docs/running.md` is false for it until the agent deletes the file by
  hand, and nothing tells it so. The behavior itself is defensible (the
  error names the file and the action is `EDIT_FILES`, and every pull
  already refuses an invalid workspace before doing anything), which is why
  this is minor rather than major: the gap is in the contract wording and
  the evidence, not the enforcement. Fix, all within this phase's reading
  contract: (a) add one scenario next to `TestScenarioReadOnlyPullRestores`
  that pins the pull outcome above and, after removing the file, pins that
  the next pull succeeds and restores the covered path; (b) add one
  sentence to `docs/slivingdoc-v1.md` §2 "Read-only paths" and to the
  "Read-only paths" section of `docs/running.md` stating that the pull
  restore applies to a workspace that passes the content rules, and that an
  invalid file under a read-only path is reported as `INVALID_CONTENT`
  naming the file and must be deleted; (c) the README invariant 6 wording
  was amended in this review to carry the same qualifier.
- [x] **R5-03 (note, wording corrected fix 5, 2026-09-15).** Invariant 9 says `mcp.Redact`
  applies to the new fields, but `readOnly` entries, `files[].path`, and the
  token fields bypass it (`internal/mcp/errors.go` `mapNotebookError`,
  `internal/mcp/server.go` `errorResult`/`successResult`), while the refusal
  message, which embeds the same entries, does go through `Redact`. A
  read-only entry whose name is 40 hex characters would therefore render
  as `[redacted]` in `message` and verbatim in `readOnly`. This matches the
  pre-existing treatment of `files[].path`, entries are operator-configured
  notebook-relative paths that can never hold a protected value, and the
  point is already on the maintainer's list from review 3. Recorded here so
  the invariant's wording can be corrected once the maintainer decides:
  "Redact applies to message text; tokens and notebook-relative paths are
  never redacted" is what the code does.
- [x] **R5-04 (note, implemented fix 5, 2026-09-15 as `ReadOnlySet.ReadCovered`).** With a non-empty set, `enforceReadOnly`
  and `Pull` call `git.ReadSnapshot` on the *whole* baseline tree (every
  blob's bytes) on every commit and every pull, even when nothing under the
  set changed, because `ChangedUnder` and `Pin` compare file bytes. For a
  large notebook this roughly doubles the per-operation read work, which
  matters given the earlier pull-performance effort in this repository. A
  covered-subtree walk, or comparing blob IDs from the two trees and reading
  bytes only for covered paths, would bound the cost to the read-only
  subtrees. No change required for correctness.

Verified good (independently, not from the notes):

- Invariant 4 holds on every commit branch, including the two review 1 did
  not name: the empty remote (generation 0, baseline the empty tree) and
  the retry loop. Probe: a fresh agent with the set `docs` against an empty
  bucket pulls `OK`, a first commit adding `docs/x.md` and `notes/a.md` is
  refused with `READ_ONLY_PATH` naming `docs/x.md`, `docs/x.md` is gone from
  disk, and the next commit publishes `notes/a.md` alone as generation 1.
  Review 1's claim that the fresh-workspace subtest proves the empty-remote
  branch was imprecise (that subtest runs against a seeded generation-1
  remote); the branch is nevertheless sound because the workspace creates
  the empty tree object on open, so `ReadSnapshot(EmptyTreeID)` succeeds.
- Invariant 5 holds: `Pin` keeps every uncovered local file and every
  covered baseline file; `Materialize` under `stageReadOnly` writes that
  tree with the baseline unchanged, so the `pulled` marker survives and the
  next commit needs no pull; the refusal is built after the mutation
  succeeds, so a materialize failure is `RECOVERY_FAILURE` with stage
  `commit.readonly` and never `OK` (`TestScenarioReadOnlyResetFailureIsRecovery`).
- Invariant 6 holds on the conflict branch of `Pull`: the merge uses the
  pinned tree, so no conflict can arise under a covered path and the
  materialized conflict result carries R's content there. A hand-written
  marker block under `docs/` (probe, using a signature `FindConflictBlocks`
  treats as ordinary text) is reset by commit and by pull alike.
- Invariant 7 holds: every surface reads `ReadOnlySet.Entries()` from the
  one normalized set built in `app.NewService`; the notebook re-normalizes
  the same entries; `violatedEntries` returns members of that set.
  `NormalizeReadOnly` collapses `Docs` into `docs` and `docs/sub` into `docs`
  under the same `cases.Fold` that `ValidateSnapshot` uses.
- Invariant 3 holds at every construction site: the six notebook
  constructors and the three `internal/mcp` sites all set `Reason` and
  `Action`; all four `ErrorFile{` literals set `Reason` (grep over
  non-test sources). The harness refuses an envelope with an empty token or
  a missing `readOnly` key (`envelopeTokenViolation`).
- Every one of the 88 `Test…` names cited in the acceptance, invariant,
  limit, and error-coverage tables of phases 1 through 4 resolves to a
  `func Test…` in the tree (re-run mechanically with a C-locale sort;
  zero unresolved).
