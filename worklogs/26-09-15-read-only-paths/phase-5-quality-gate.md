# Phase 5 — Quality gate

**Status:** Complete

**README:** [README.md](README.md)

## Goal

Every repository gate passes on the finished effort and every document that
describes the shipped behavior agrees with the code.

## Specification

- Run the README's validation policy in full: `make qa`, the formatter,
  staticcheck, `go vet`, `go fix -diff`, and the dupl signal. Fix every
  finding at its source; never add a skip, a build tag, or a subset
  command. Review each dupl report under the AGENTS.md duplication policy
  and record the verdict per clone in the implementation notes.
- Coverage: `make test` holds the floor named in the README parameters. If
  the new packages lower it, add unit tests to the phase that owns the
  uncovered code, not to this phase.
- Document coherence sweep, recorded as a checklist in the implementation
  notes:
  - every token in the README's reason, file-reason, and action tables
    appears in the contract's product-contract section and in the code
    constants, with no extra or missing token in either direction;
  - the flag reference in `internal/app/config.go`, `docs/running.md`, the
    contract configuration section, and `AGENTS.md` describe the same flag,
    variable, separator, and default;
  - the contract's decisions section carries the new decision;
  - `AGENTS.md`'s invariants and error-taxonomy paragraphs mention the
    read-only invariant and the additive fields;
  - the operation-results worklog is untouched (older worklogs are never
    rewritten).
- Run the README readiness checklist commands once more against the
  finished phase files and record the outcome.
- Flip every phase status and the README status board to `Complete` with a
  one-line outcome each, and write the session-journal entry.

## Integration contract

unit-test-only

## Acceptance criteria

| Outcome                       | Evidence                                                               |
| ----------------------------- | ---------------------------------------------------------------------- |
| All gates green               | `make qa` output pasted into the implementation notes                  |
| Formatter and analyzers clean | the formatter, staticcheck, `go vet`, and `go fix -diff` print nothing |
| Dupl reviewed                 | per-clone verdicts in the implementation notes                         |
| Coverage floor held           | the coverage line of `make test`                                       |
| Documents coherent            | the checklist above, each item ticked with the file checked            |
| README and phases closed      | status board rows `Complete`; journal entry dated                      |

## Error coverage

| Failure                                    | Expected outcome                                             | Test                    |
| ------------------------------------------ | ------------------------------------------------------------ | ----------------------- |
| a gate fails                               | fixed at the source in the owning phase; the run is repeated | `make qa`               |
| a token exists in code but not in the docs | docs updated in the owning phase's sections; sweep repeated  | the coherence checklist |
| coverage below the floor                   | tests added to the owning phase; run repeated                | `make test`             |

## Implementation notes

Session: 2026-09-15, executing agent (worklog-work, Phase 5).

### Gate commands run (all green)

- `go fix -diff ./...` — no output.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no output, run twice (before and
  after the doc/help-text fixes below).
- `go vet ./...` — no output, run twice.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — no output.
- `make lint` — exit 0 (gofumpt, go vet, staticcheck, go fix, in sequence).
- `make test` — exit 0; final coverage line:
  `== coverage: 83.9% (floor 70%) ==`. `internal/integrationtest` (the
  black-box scenario suite) reports `69.7%` of its own statements, but the
  gate measures with `-coverpkg=./...`, so the reported floor comparison is
  against the aggregate 83.9%, matching the README parameter ("existing
  `make test` floor of 70%, unchanged"). Ran once before the doc fixes and
  again (via `make qa`) after, both green, same coverage.
- `npm test --prefix npm/slivingdoc` — 35/35 pass, 0 failures.
- `make qa` (lint + test + npm-test together) — exit 0, run as the final
  post-fix gate.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group:
  `internal/app/command_test.go:426,446` and `:451,471`
  (`TestFileReasonWords` and `TestActionWording`). Verdict: accepted, no
  refactor. Both are independent table-driven tests over two different pure
  functions (`fileReasonWord`, `actionWording`); AGENTS.md's Duplication
  policy names "table-driven test loops... is the idiom, not a clone" and
  this is not the "bodies differ only in parameterised values" case (the
  two loops exercise different functions with different token domains).
  This is the same clone group Phase 4 already reviewed and accepted; no
  new clone appeared after this phase's edits.
- `go build ./...` — builds clean after the helpText edits below.

### Document coherence sweep (per this phase's checklist)

- **Token tables.** Every `reason` (23), `files[].reason` (5), and `action`
  (5) token in the README's tables appears verbatim, in the same code
  points, in `internal/notebook/errors.go` (`Reason`/`FileReason`/`Action`
  constants) and in `docs/slivingdoc-v1.md` §2's three token tables — no
  extra or missing token either direction. `internal/mcp/errors.go` mirrors
  only the decode-time subset it classifies itself
  (`MALFORMED_INPUT`/`PATH_OUTSIDE_ROOT`/`INTERNAL`), consistent with
  Phase 1's note that the package classifies its own constructor sites.
- **Flag reference.** `--read-only-paths` / `SLIVINGDOC_READ_ONLY_PATHS`,
  comma separator, empty default, described identically in
  `internal/app/config.go` (flag bind, `FlagReference` table,
  `splitReadOnlyPaths`), `docs/running.md` (flag table row and the
  "Read-only paths" section), `docs/slivingdoc-v1.md` §17 (flag table and
  prose), and `AGENTS.md` (Key Flags paragraph and the invariants list).
  No mismatch found.
- **Decisions section.** `docs/slivingdoc-v1.md` §24 decision 38 already
  carries the read-only-paths decision (per-process configuration,
  enforced at commit/restored at pull). No new decision needed for
  `reason`/`action`, since those are additive envelope fields, not an
  architectural decision distinct from the ones already recorded.
- **AGENTS.md invariants and error taxonomy.** The invariants list already
  named the read-only invariant (added in Phase 3's session). The
  **error-taxonomy paragraph did not mention the additive `reason`,
  `action`, `files[].reason`, or `readOnly` fields at all** — a genuine gap
  against this phase's checklist item. Fixed: added two sentences naming
  all four additive fields as always-present, stable tokens/array. See
  `AGENTS.md` around "Error taxonomy."
- **Operation-results worklog untouched.** `git status --porcelain
  worklogs/` shows changes only under `worklogs/26-09-15-read-only-paths/`;
  `worklogs/26-08-15-operation-results/` and every other worklog directory
  are untouched.

### Additional incoherences found and fixed (outside the phase's explicit
checklist bullets, but inside its "every document that describes the
shipped behavior agrees with the code" goal)

- `cmd/pull/pull.go` and `cmd/commit/commit.go` `helpText` still described
  the pre-Phase-4 error report ("the category and message, the conflicted
  files with their one-based inclusive line ranges, and the retryable
  verdict") with no mention of the `reason` status line, the per-file
  `reason`, the `next:` action line, or the `read-only:` trailer, and no
  prose mention of `--read-only-paths` (the flag itself was already listed
  in the appended `app.FlagReference` table). Phase 4's own implementation
  notes explicitly flagged this for "Phase 5's documentation-coherence
  pass." Fixed both `helpText` constants to describe the shipped report
  shape and name `--read-only-paths`. No test asserts the exact old
  wording (`internal/cli/cli_test.go` only checks `Help()` is non-empty);
  `go build ./...` and the full `make qa` pass after the edit.
- Root `README.md`'s CLI section had the same staleness ("the error
  category, the retryable verdict, and every conflicted file with its line
  ranges", no `reason`/`next:`/`read-only:` mention). Fixed the same way.
  No test references `README.md`.

### Reviewed and explicitly not changed (out of this phase's scope)

- The README's own CLI-report-skeleton illustration (lines ~254-271) pads
  the shorter file path to the longest path plus three spaces, not the
  "longest path + 2" rule the implementation and Phase 4's byte fixtures
  actually follow. This is the exact situation the README anticipates by
  naming Phase 4 as the byte-fixture owner ("Phase 4 owns the exact byte
  fixtures" — README normative CLI report skeleton section) and Phase 4's
  own implementation notes recorded the same observation and built its
  fixtures against the rule, not the illustration. Not fixed: the phase's
  document-coherence checklist does not list the README's own illustration
  as a target, and the illustration is explicitly non-normative by the
  README's own words. Recording here per the parent task's instruction to
  note out-of-scope findings rather than silently patch them.
- `TestReport` is named in both `phase-1-error-reasons.md` (noting its
  fixture needs the `ErrorFile` rename) and `phase-4-cli-report.md`
  (naming it as evidence for several Phase 4 acceptance rows). This is the
  one hit from re-running the README readiness checklist's duplicate-test
  grep (`grep -ohE 'Test[A-Za-z0-9_]+' phase-*.md | sort | uniq -d` no
  longer prints nothing — it prints dozens of names). Checked each: every
  other duplicate is a test name appearing more than once *within the same
  phase file* (once in a table, again in the implementation notes'
  verification-command list), which the rule does not forbid. `TestReport`
  is the only name spanning two files, and it is a pre-existing test from
  the operation-results worklog that both phases legitimately extend
  incrementally (Phase 1 renames a field in its fixture; Phase 4 adds new
  fixtures to it) — not two phases independently claiming ownership of a
  new test. No planning defect.
- The readiness checklist's numeral grep (item 1) now also matches
  `300s` inside a `go test ... -timeout 300s -v` command quoted in Phase
  4's implementation notes' verification-command list. This is a citation
  of the exact command run (required by the worklog-work skill's evidence
  rule), not a duplicated limit value from the parameters table; the
  timeout itself is not asserted as a product limit anywhere. No change
  made.

### Required architecture sections re-verified

No edit was made to `docs/slivingdoc-v1.md` in this phase, so the README's
"Required architecture sections" table needed no update. Confirmed the
referenced sections and subsections still exist at their numbered headings:
§2, §7.3, §10, §11.1, §12, §15, §17, §18.2, §24 (`grep -n '^## \|^### '
docs/slivingdoc-v1.md`).

### Outcome

All gates green, coverage floor held (83.9% vs. 70%), one dupl clone group
reviewed and accepted, one genuine documentation gap found and fixed
(AGENTS.md error-taxonomy paragraph) plus two more found and fixed outside
the checklist's literal bullets but inside the phase's stated goal (stale
CLI helpText / root README prose), and two items reviewed and left
unchanged as out of scope with reasoning recorded above. Phase 5 is
Complete.

### Quality gate re-run (review 1 fixes, 2026-09-15)

Session: 2026-09-15, executing agent (worklog-work, Phase 5 re-run after
review 1's Phase 1-4 fixes landed). This phase carried no finding of its
own (see "Review findings" below); it was reopened only so the full sweep
runs once more over the tree that now includes the R1-01 through R1-06
fixes (`internal/mcp/decode.go`, `internal/mcp/errors.go`,
`internal/mcp/server.go`, `internal/notebook/notebook.go`'s exported
`ValidateMessage`, `internal/git/readonly.go`'s `CoveringEntry` and the
`git:`-prefix removal, `internal/notebook/commit.go`, and
`internal/app/command.go`'s rune-count path padding).

Gate commands run (all green):

- `go fix -diff ./...` — no output.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no output.
- `go vet ./...` — no output.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — no output.
- `make lint` — exit 0.
- `make test` — exit 0; final line `== coverage: 84.0% (floor 70%) ==`
  (`internal/integrationtest` reports 69.7% of its own statements, measured
  under `-coverpkg=./...`; the gate compares the aggregate 84.0% against the
  README's unchanged 70% floor).
- `npm test --prefix npm/slivingdoc` (`make npm-test`) — 35/35 pass.
- `make qa` — exit 0 (lint + test + npm-test), run as the combined final
  gate; same 84.0% coverage line.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group:
  `internal/app/command_test.go:432,452` and `:457,477`
  (`TestFileReasonWords` / `TestActionWording`). Verdict: accepted, no
  refactor — same pre-existing pair of independent table-driven tests over
  two different pure functions (`fileReasonWord`, `actionWording`) that
  Phases 2 through 4 already reviewed and accepted under the AGENTS.md
  "table-driven test loops... is the idiom, not a clone" policy; the review-1
  fixes touched neither function's test, so no new clone appeared and the
  clone's line numbers only shifted with surrounding edits.
- `go build ./...` — builds clean.

Document coherence sweep (re-run against the fixed files):

- **Token tables.** `internal/notebook/errors.go` defines 23 `Reason`
  constants, 5 `FileReason` constants, and 5 `Action` constants; every one
  appears verbatim in the README's three token tables and in
  `docs/slivingdoc-v1.md` §2's three token tables, no extra or missing
  token either direction. `internal/mcp/errors.go` still classifies only
  its own decode-time subset (`MALFORMED_INPUT`, `PATH_OUTSIDE_ROOT`,
  `INTERNAL`) by copying `notebook`'s constants verbatim, consistent with
  the R1-01 fix that made `decodeFailureError` delegate to
  `notebook.ValidateMessage`/`mapNotebookError` for message-shaped failures
  instead of collapsing them to `MALFORMED_INPUT`.
- **Flag reference.** `--read-only-paths` / `SLIVINGDOC_READ_ONLY_PATHS`,
  comma separator, empty default, described identically in
  `internal/app/config.go`, `docs/running.md` (flag table row and the
  "Read-only paths" section), `docs/slivingdoc-v1.md` §17, and `AGENTS.md`.
  The R1-02 fix (dropping the `git:` prefix from `NormalizeReadOnly`'s
  entry-validation error) changes only diagnostic text, not the flag
  contract; no doc mismatch.
- **Decisions section.** `docs/slivingdoc-v1.md` §24 decision 38 still
  correctly states read-only paths are per-process configuration enforced
  at commit and restored at pull; no new decision needed for the review-1
  fixes, which are implementation corrections (decode delegation, shared
  fold logic, prefix removal, rune-count padding), not new architectural
  decisions.
- **AGENTS.md invariants and error-taxonomy paragraphs.** Both already
  name the read-only invariant and the additive `reason`/`action`/
  `files[].reason`/`readOnly` fields (added in the prior Phase 5 run); still
  accurate after the review-1 fixes, no further edit needed.
- **Operation-results worklog untouched.** `git status --porcelain
  worklogs/` shows changes only under `worklogs/26-09-15-read-only-paths/`.
- **CLI help text and root README.md.** `cmd/pull/pull.go` and
  `cmd/commit/commit.go`'s `helpText`, and root `README.md`'s CLI section,
  still describe the shipped report shape (status line with `reason`, per-
  file reason and ranges, `next:` action line, `retryable:`, the
  conditional `read-only:` trailer) fixed in the prior Phase 5 run; the
  review-1 fixes changed no user-visible report field, so no further edit
  was needed here.
- **Rune-count padding wording.** `docs/slivingdoc-v1.md` §2's CLI report
  section and the README's CLI report skeleton section describe the column
  padding as "the longest path... plus two spaces" with no byte-vs-rune
  claim; the R1-05 fix changed only the measurement (`utf8.RuneCountInString`
  instead of `len()`), not the documented rule, so no wording update was
  required.

Readiness checklist re-run:

1. Numerals grep — same two evidence citations already reviewed as
   verification-command text, not duplicated limits (`-timeout 300s` in
   Phase 4's notes, `docs/résumé.md (14 runes, 16 bytes...)` in Phase 4's
   R1-05 fixture description); no product-limit duplication.
2. Duplicate-test-name grep — checked every name that now recurs across
   more than one phase file: `TestReport` (pre-existing test both Phase 1
   and Phase 4 legitimately extend, already documented); `TestFileReasonWords`
   and `TestActionWording` (cited in every phase's implementation notes as
   the same already-accepted dupl clone group, not a competing test
   ownership claim); `TestNormalizeReadOnly` and
   `TestNewRejectsInvalidReadOnlyPaths` (Phase 2 owns and defines both;
   Phase 3's R1-02 fix notes cite them only as existing evidence that no
   test already pinned the removed `git:` prefix). No planning defect.
3. Owner table — unaffected by the review-1 fixes (no new config field,
   flag, or injectable field was added).
4. Invariant/limit tables — unaffected; no new invariant or limit was
   introduced by the review-1 fixes.
5. Human-required grep — prints nothing.
6. No phase references text scheduled for deletion — unchanged.
7. Conventions — error constructors still follow
   `internal/notebook/errors.go` (now also `internal/mcp/errors.go`
   delegating to it for message-shaped decode failures); flag resolution
   still follows `internal/app/config.go`; CLI rendering still follows
   `internal/app/command.go` (now with rune-aware padding); scenarios still
   follow `internal/integrationtest/harness.go`.

Required architecture sections re-verified: no edit was made to
`docs/slivingdoc-v1.md` in this re-run, so the README's "Required
architecture sections" table needed no update; §2, §7.3, §10, §11.1, §12,
§15, §17, §18.2, §24 all still exist at their numbered headings.

Outcome: all gates green (coverage 84.0% vs. the 70% floor), the one dupl
clone group reviewed and re-accepted, document coherence confirmed with no
new incoherence found against the review-1 fixes. Phase 5 returns to
Complete.

### Quality gate re-run (review 3 fixes, 2026-09-15)

Session: Claude Sonnet 5, executing agent (worklog-work, Phase 5 re-run
after review 3's Phase 1 and Phase 4 fixes landed). This phase carried no
finding of its own; review 3 reopened it only to re-run the full sweep and
to add the mechanical evidence check review 3 specified (see "Review
findings (review 3, 2026-09-15)" below).

**New mechanical evidence check (review 3's addition).**

(a) Every `Test[A-Za-z0-9_]+` name cited in the acceptance-criteria,
invariant, limit, and error-coverage tables of `phase-1-error-reasons.md`
through `phase-4-cli-report.md` (88 unique names, extracted by `sed`-ing
exactly those table line ranges out of each phase file, never the
implementation notes or review-findings sections) resolves to a `func
Test…` somewhere in the tree:

```
grep -rhoE '^func Test[A-Za-z0-9_]+' --include='*_test.go' . | sed -E 's/^func //' | sort -u
```

compared against the 88 cited names with `comm -23` (`LC_ALL=C` to avoid a
locale sort mismatch) — the result was empty: every cited name resolves.
No unresolved name found.

(b) Every reason token (23), file-reason token (5), and action token (5)
in the README's three tables — cross-checked against
`internal/notebook/errors.go`'s `Reason`/`FileReason`/`Action` constants,
which declare exactly the same 23/5/5 tokens, confirming no phase drift —
has at least one assertion in a `_test.go` file. Grepping each token as a
quoted string literal (`grep -l '"TOKEN"' **/*_test.go`) found 28 of the 33
tokens directly; five (`MANIFEST_WRITE`, `LOCAL_STATE`, `INTERNAL`,
`HISTORY_INVALID`, `ENGINE_FAILED`) returned zero quoted-string hits.
Investigating those five individually found they are asserted through the
typed Go constant instead of the raw string: `internal/notebook/errors_test.go`'s
`TestActionForEveryReason` walks a table of every `{code, reason, action}`
triple (using the symbols `ReasonManifestWrite`, `ReasonLocalState`,
`ReasonInternal`, `ReasonHistoryInvalid`, `ReasonEngineFailed`, each paired
with its action constant) and asserts `actionFor` returns the exact
expected action for each — a genuine value-level assertion of both the
reason and its action, just not spelled as the bare uppercase string in
the test source. `ENGINE_FAILED` additionally has a direct assertion in
`internal/notebook/readonly_test.go` (`TestCommitReadOnlyBaselineReadFailure`),
comparing `ne.Reason != ReasonEngineFailed`. No token has zero assertions
in either sense; this is the same class of naive-method false-positive
that R3-02/R3-03's own citations already illustrated (a mechanical proxy
undercounting real coverage that uses a different, equally valid,
mechanism) — recorded here rather than filed as a gap, since the review's
own wording asks for "at least one test assertion," which all 33 tokens
have.

**Gate commands run (all green):**

- `go fix -diff ./...` — no output.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no output.
- `go vet ./...` — no output.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — no output.
- `make lint` — exit 0.
- `make qa` (lint + `go test -race -count=3 -timeout=30s -coverpkg=./...`
  over every package, real SeaweedFS-backed containers via Docker, +
  `npm test --prefix npm/slivingdoc`) — exit 0; final line `== coverage:
  84.0% (floor 70%) ==`; npm suite 35/35 pass.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group,
  `internal/app/command_test.go:432,452` and `:457,477`
  (`TestFileReasonWords`/`TestActionWording`). Verdict: accepted, no
  refactor — the same pre-existing pair of independent table-driven tests
  over two different pure functions that every prior session in this
  worklog has reviewed and accepted under the AGENTS.md table-driven-test
  idiom; the review-3 fixes (test-only, in
  `internal/integrationtest/scenario_validation_test.go`,
  `scenario_path_security_test.go`, `scenario_path_security_unix_test.go`,
  `scenario_recovery_test.go`, `internal/mcp/errors_test.go`, plus a
  docs-only change in `docs/running.md`) touched neither function's test;
  no new clone group appeared, only line-number shifts.

**Document coherence sweep (re-run against the review-3 fixes):**

- **Token tables.** Unaffected: review 3's fixes added test assertions,
  they did not add, remove, or rename a token. `internal/notebook/errors.go`
  still declares exactly the 23/5/5 tokens the README and
  `docs/slivingdoc-v1.md` §2 name, confirmed again as part of the new
  mechanical check above.
- **Flag reference.** Unaffected: no flag or configuration field changed.
- **Decisions section.** Unaffected: no new architectural decision; the
  fixes added test coverage for already-decided behavior.
- **AGENTS.md invariants and error-taxonomy paragraphs.** Unaffected;
  still name the read-only invariant and the four additive fields, checked
  again and unchanged since the prior Phase 5 run.
- **Operation-results worklog untouched.** `git status --porcelain
  worklogs/` shows changes only under `worklogs/26-09-15-read-only-paths/`.
- **`docs/running.md` report paragraph and read-only section.** Re-read in
  full after R3-05's fix: the "Prints the unified result report" paragraph
  now shows a generic single-file `CONTENT_CONFLICT · MERGE_CONFLICT`
  example (no `read-only:` line, since no read-only paths are configured
  in that example), and the "Read-only paths" section further down keeps
  its own `INVALID_REQUEST · READ_ONLY_PATH` refusal example with the
  `read-only: docs` trailer — the two examples no longer duplicate each
  other, and the domain-error paragraph's prose ("a `read-only:` trailer
  ... whenever one is configured, on every success and error report
  alike") matches both examples. No further edit needed.
- **Required architecture sections.** No edit was made to
  `docs/slivingdoc-v1.md` in this re-run; §2, §7.3, §10, §11.1, §12, §15,
  §17, §18.2, §24 all still exist at their numbered headings (`grep -nE
  '^#{1,4} ' docs/slivingdoc-v1.md`).

**Readiness checklist re-run:**

1. Numerals grep — the same pre-existing evidence-citation hits
   (`-timeout 300s`, `docs/résumé.md (14 runes, 16 bytes...)`) plus this
   phase's own new citations of the same figures; no product-limit
   duplication.
2. Duplicate-test-name grep — re-run per-file (not just globally) to
   separate genuine cross-phase collisions from a name repeated twice
   within one phase file (table cell plus implementation-notes citation,
   which the rule does not forbid). Five names now recur across more than
   one phase *file*: `TestReport` (Phase 1 and Phase 4, already documented
   as a pre-existing test both legitimately extend), `TestFileReasonWords`/
   `TestActionWording` (cited in every phase's implementation notes only as
   the same already-accepted dupl-clone-group evidence, not a competing
   ownership claim), and `TestNormalizeReadOnly`/
   `TestNewRejectsInvalidReadOnlyPaths` (Phase 2 owns and defines both;
   Phase 3's R1-02 fix notes and this phase's own notes cite them only as
   existing evidence, not new tests). All five were already identified and
   cleared in the review-1-fix Phase 5 re-run; no new cross-phase
   collision was introduced by the review-3 fixes.
3. Owner table — unaffected; no new config field, flag, or injectable
   field was added by the review-3 fixes.
4. Invariant/limit tables — unaffected; no new invariant or limit.
5. Human-required grep — prints nothing.
6. No phase references text scheduled for deletion — unchanged.
7. Conventions — error constructors still follow
   `internal/notebook/errors.go`; flag resolution still follows
   `internal/app/config.go`; CLI rendering still follows
   `internal/app/command.go`; scenarios still follow
   `internal/integrationtest/harness.go`; instructions and descriptions
   still follow `internal/mcp/server.go`.

**Outcome:** all gates green (coverage 84.0% vs. the 70% floor), the one
dupl clone group re-accepted, document coherence confirmed with no new
incoherence, and both of review 3's new mechanical evidence checks pass
with no unresolved test name and no unasserted token. Phase 5 returns to
Complete.

## Review findings (review 1, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). Re-ran `make qa` from the
working tree: lint clean, every package `ok` under `-race -count=3
-timeout=30s`, coverage 83.9 % against the 70 % floor, npm suite green. No
finding is filed against this phase. Phases 1 through 4 are reopened by
R1-01 through R1-06; this phase's sweep must be re-run after those fixes
land, so its status is set to `Reopened (review 1)` on the board with no
work of its own beyond the re-run and the coherence check of the touched
documents.

## Review findings (review 2, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). Re-run verified: `make qa` exit 0, coverage 84.0 % against the 70 % floor, one accepted dupl group unchanged. No new finding.

## Review findings (review 3, 2026-09-15)

No finding of its own. Reopened so the sweep runs once more after the
review 3 fixes to Phases 1 and 4. The review also observed that the
readiness checklist verifies only that test names are unique across phases,
not that each named test exists and asserts what its table claims; this
re-run must add that check: every `Test…` name cited in an acceptance,
invariant, or error-coverage table of phases 1 through 4 resolves to a
`func Test…` in the tree, and every reason, file-reason, and action token
in the README tables has at least one test assertion.

## Review findings (review 5, 2026-09-15)

Reviewer: independent session (Fable 5.1). Gates re-run independently:
`make qa` exit 0 (coverage 84.0 % against the 70 % floor, npm 35/35);
`go run github.com/mibk/dupl@v1.0.0 -t 80 .` reports the one accepted
clone group only; the cited-test check re-run mechanically resolves all 88
names. No finding of its own. Reopened so the sweep runs once more after
the review 5 fix to Phase 2 (R5-01 adds a scenario and two document
sentences), and so the coherence sweep confirms `docs/slivingdoc-v1.md` §2
"Read-only paths", `docs/running.md` "Read-only paths", and README
invariant 6 state the same qualifier.

### Re-run after the review 5 fix, 2026-09-15

Session: Fable 5.1. Re-ran the full validation policy over the tree with
the review 5 fix to Phase 2 landed (`ReadOnlySet.ReadCovered` and its use
in `commit.go`/`pull.go`, `TestReadOnlyReadCovered`,
`TestScenarioReadOnlyPullInvalidContentUnderEntry`, the §2 and running.md
sentences, the README invariant 6, 8, and 9 wording): `make qa` exit 0
(gofumpt, go vet, staticcheck, go fix -diff, `go test -race -count=3
-timeout=30s -coverpkg=./...`, npm 35/35); coverage 84.0 % against the
70 % floor; `go run github.com/mibk/dupl@v1.0.0 -t 80 .` reports only the
accepted `TestFileReasonWords`/`TestActionWording` clone group, unchanged.

Evidence check re-run mechanically: 90 `Test…` names are now cited in the
acceptance, invariant, limit, and error-coverage tables of phases 1
through 4 (the two new ones included), and every one resolves to a `func
Test…` in the tree; zero unresolved. Coherence sweep: `docs/slivingdoc-v1.md`
§2 "Read-only paths", `docs/running.md` "Read-only paths", and README
invariant 6 state the same `INVALID_CONTENT` precondition; the contract
never claimed `readOnly` was redacted, so the invariant 9 rewording needed
no document change. Readiness checklist greps show the same previously
cleared hits only (cross-phase name mentions in notes and review sections;
rune/byte counts in evidence text).

Phase 5 returns to Complete; every phase is Complete and the README
top-level `**Status:**` is set to `Complete`.
