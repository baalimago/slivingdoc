# Phase 4 — CLI report

**Status:** Complete

**README:** [README.md](README.md)

## Goal

The `pull` and `commit` subcommands render the README's CLI report
skeleton: code and reason on the status line, per-file reasons in an
aligned column, the next step, and the read-only trailer, plain and
coloured.

## Specification

### Rendering (`internal/app/command.go`, `internal/app/colour.go`)

- `Report` gains a `readOnly []string` argument; `cmd/pull` and
  `cmd/commit` pass `Runtime.ReadOnlyPaths()`. `Report` sets the
  envelope's `ReadOnly` before rendering so the CLI and MCP paths agree.
- `writeSuccess` is unchanged except for the trailer line `read-only:
<entries>` after the totals line when the set is non-empty, entries joined
  by the README's read-only list separator.
- `writeError` renders, in order:
  - the status line: the code, the README's CLI status separator, the
    reason token;
  - the message on its own line;
  - one line per file: two-space indent, the path padded with spaces to the
    longest path in the report plus two, the file reason as the lower-case
    words the README lists, and, when ranges exist, two more spaces and
    `lines ` followed by `start-end` pairs joined by a comma and a space;
  - `next: ` and the README's CLI wording for the action;
  - `retryable: ` and the flag;
  - `recovery: ` as today, only for `RECOVERY_FAILURE`;
  - `read-only: <entries>` only when the set is non-empty.
- `painter` gains `dim` using the README's dim colour code. Colour map:
  code red, reason dim, path yellow, file reason dim, `next:` cyan,
  `read-only:` dim. With colour off every line is byte-identical to the
  plain form.
- The file reason word table and the action wording table are two package
  maps; a token missing from a map renders the raw token, never a panic.

### Contract and documents

- `docs/slivingdoc-v1.md`: the CLI report paragraph of the product-contract
  section describes the skeleton and shows the read-only refusal example
  from the README.
- `docs/running.md`: the report examples are replaced with the new plain
  output, including one read-only refusal.

### Invariant: colour is presentation-only

| Bound actor                 | Mechanism                                   | Test                                                                   |
| --------------------------- | ------------------------------------------- | ---------------------------------------------------------------------- |
| piped stdout                | `colourEnabled` false                       | `TestReport` (extended)                                                |
| `NO_COLOR` on a terminal    | `colourEnabled` false                       | `TestScenarioCLIColourOnTerminal` (extended)                           |
| terminal without `NO_COLOR` | painter on; stripped bytes equal plain form | `TestWriteErrorColoured` (extended), `TestScenarioCLIColourOnTerminal` |

## Integration contract

R starts with `docs/faq.md` = `Q: a\nA: 1\n` and `notes/a.md` = `x\n`.
Every CLI process below runs with `--read-only-paths docs` unless the row
says otherwise, against the fake backend.

| Trigger                                                                 | Collaborators or fakes | Observable result (exact plain stdout)                                                                                                                                                                              | Required side effects                                | Prohibited side effects    |
| ----------------------------------------------------------------------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | -------------------------- |
| `pull` into a fresh directory                                           | spawned process        | `OK  generation 1  <path>` then `  docs/faq.md  +2` then `  notes/a.md  +1` then `2 files changed, 3 insertions(+), 0 deletions(-)` then `read-only: docs`; exit 0                                                  | files written                                        | any error line             |
| edit `docs/faq.md` to `A: 2\n` and `notes/a.md` to `y\n`; `commit -m m` | spawned process        | `INVALID_REQUEST · READ_ONLY_PATH` then the README refusal message for `docs` then `  docs/faq.md  read-only` then `next: edit the files, then commit` then `retryable: false` then `read-only: docs`; exit nonzero | `docs/faq.md` = `Q: a\nA: 1\n`; `notes/a.md` = `y\n` | manifest change            |
| `commit -m m` again                                                     | spawned process        | `OK  generation 2  <path>` then `  notes/a.md  +1 -1` then `1 files changed, 1 insertions(+), 1 deletions(-)` then `read-only: docs`; exit 0                                                                        | manifest generation 2                                | any docs change in R       |
| `commit -m m` before any pull, no flag                                  | spawned process        | `INVALID_REQUEST · PULL_REQUIRED` then the message then `next: pull, then continue` then `retryable: false`; exit nonzero                                                                                           | none                                                 | a `read-only:` line        |
| two directories edit line 1 of `notes/a.md`; second `commit`, no flag   | spawned process        | `CONTENT_CONFLICT · MERGE_CONFLICT` then the message then `  notes/a.md  conflict  lines 1-5` then `next: edit the files, then commit` then `retryable: false`; exit nonzero                                        | markers in `notes/a.md`                              | a `read-only:` line        |
| the refusal above on a pseudo-terminal without `NO_COLOR`               | PTY helper             | the same lines with the code red, reason dim, path yellow, file reason dim, `next:` cyan, `read-only:` dim; stripping escapes yields the plain bytes                                                                | none                                                 | colour with `NO_COLOR` set |

## Acceptance criteria

| Outcome                                                      | Evidence                                                                                                                                                                                                                    |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Status line, message, aligned file rows, trailer, in order   | `TestReport` (extended fixtures), `TestWriteErrorReadOnly`, `TestWriteErrorAlignsPathColumn`                                                                                                                                |
| Read-only trailer on success and error only when non-empty   | `TestWriteSuccessReadOnlyTrailer`, `TestWriteErrorReadOnly`, `TestWriteSuccessEmptyStat` (unchanged)                                                                                                                        |
| File reason words and action wording match the README tables | `TestFileReasonWords`, `TestActionWording`                                                                                                                                                                                  |
| Coloured output strips to the plain bytes                    | `TestWriteErrorColoured` (extended), `TestWriteSuccessColoured` (extended)                                                                                                                                                  |
| Subcommands pass the runtime set and print the report        | `TestCommitReportsReadOnlyRefusal`, `TestCommitReportsMarkerConflict` (fixture updated), `TestPullPrintsReport` (fixture updated)                                                                                           |
| Black-box byte-exact reports                                 | `TestScenarioCLIReadOnlyCommit`, `TestScenarioCLIMarkerConflictReport` (updated), `TestScenarioCLICommitBeforePull` (updated), `TestScenarioCLISharedRemoteConflict` (updated), `TestScenarioCLIColourOnTerminal` (updated) |
| Contract and running docs show the new report                | reviewer reads the CLI report paragraph and `docs/running.md` against `TestScenarioCLIReadOnlyCommit`                                                                                                                       |

## Error coverage

| Failure                         | Expected outcome                                    | Test                           |
| ------------------------------- | --------------------------------------------------- | ------------------------------ |
| unknown file reason token       | raw token rendered, no panic                        | `TestFileReasonWords`          |
| unknown action token            | raw token rendered, no panic                        | `TestActionWording`            |
| error with no files             | no file lines; trailer follows the message directly | `TestReport`                   |
| non-domain error (cancellation) | returned unchanged, nothing printed                 | `TestReport` (existing branch) |
| `RECOVERY_FAILURE`              | `recovery:` line before `read-only:`                | `TestReport`                   |

## Implementation notes

Session: worklog-work executor, 2026-09-15.

Implemented exactly the specification. `Report` (`internal/app/command.go`)
gained the `readOnly []string` parameter; `cmd/pull` and `cmd/commit` pass
`Runtime.ReadOnlyPaths()`. Report now maps through `mcp.MapSuccess` /
`mcp.MapError` and sets `.ReadOnly` on the returned envelope before
rendering, exactly mirroring `internal/mcp/server.go`'s own
`info.ReadOnly = h.svc.ReadOnlyPaths()` / `te.ReadOnly = h.svc.ReadOnlyPaths()`
pattern, so `writeSuccess` takes `*mcp.SuccessInfo` and `writeError` takes
`*mcp.ToolError` instead of the raw `notebook.Result`. Added `statusSeparator`
(" · "), the `fileReasonWords`/`actionWordings` package maps and their
`fileReasonWord`/`actionWording` lookups (raw-token passthrough for an
unmapped token, never a panic), and `painter.dim` (`colourDim = "\x1b[2m"`)
in `internal/app/colour.go`. `writeError` renders the file column padded to
`longestErrorFilePath(te.Files) + 2`.

Deviation (byte fixture correction, not a spec change): the README's own
"CLI report skeleton" illustration is not byte-exact — its two-file
example padding (field width 18) does not match the phase's own
padding rule (longest path length + 2, which computes to 17 for
`docs/faq.md`/`docs/pricing.md` and 16 for `notes/today.md`/`notes/plan.md`).
The phase file's own integration-contract rows (single-file cases) are
internally consistent with the longest+2 rule and were used as ground
truth; all byte fixtures here (unit and integration) and the
`docs/slivingdoc-v1.md` §2 example were built and verified against the
longest+2 rule, not against the README illustration's literal spacing.
This is exactly the case the README anticipates ("Phase 4 owns the exact
byte fixtures"), so no README gap is filed for it — flagging here only so
a reviewer isn't surprised the visual column width differs slightly from
the README illustration.

Deviation (test-name discovery, not a spec change): `TestScenarioCLIMarkerConflictReport`
and `TestCommitReportsMarkerConflict` write a conflict-marker block before
any pull, which is `Commit`'s own pre-merge rejection — `UNRESOLVED_MARKERS`,
not `MERGE_CONFLICT` — per the Phase 1 finding already recorded in the
README's Phase 1 session-journal entry. Their fixtures were updated to the
correct reason token and file-reason word ("unresolved markers") rather
than the `MERGE_CONFLICT`/"conflict" wording a naive read of the skeleton
might suggest. `TestScenarioCLISharedRemoteConflict` (a genuine two-writer
divergent-edit conflict) legitimately renders `MERGE_CONFLICT`/"conflict"
and was updated accordingly.

Interpretation call on scope, recorded for transparency: the parent task
handed down Phase 3's note that `docs/running.md` contains "a refusal
example" showing pre-Phase-4 bytes and said to update "that one example...
and change nothing else." Two examples in `docs/running.md` in fact carried
pre-Phase-4 bytes: the generic status/detail/trailer skeleton example under
"Prints the unified result report" (a `CONTENT_CONFLICT` example, not
read-only-specific, predating this worklog) and the read-only-specific
refusal example Phase 3 added under "Read-only paths" (matching Phase 3's
own note that it "deliberately shows today's actual CLI report bytes...
since that phase [4] has not built it yet"). The phase's own "Contract and
documents" section requires "the report examples [plural] are replaced
with the new plain output, including one read-only refusal," so both were
updated to the new byte-exact shape (verified against `writeError` output);
nothing else in `docs/running.md` was touched. No README gap: this is
squarely inside the phase's documented contract obligation, just broader
than the parent's terse paraphrase of Phase 3's note.

Test-organization note: the phase's error-coverage table names `TestReport`
for "error with no files," "non-domain error," and `RECOVERY_FAILURE`
trailer ordering; these are `t.Run` subtests of `TestReport` (not separate
top-level functions), matching the existing convention in this file and the
readiness-checklist rule that a test name belongs to exactly one file/phase.

Dupl finding (reviewed, no action): `dupl -t 80` flags `TestFileReasonWords`
and `TestActionWording` as one clone group. Both are independent
table-driven tests over two different pure functions (`fileReasonWord`,
`actionWording`); per AGENTS.md's Duplication policy this is the
"table-driven test loops... is the idiom, not a clone" case, not the
"bodies differ only in parameterised values" case (they exercise different
functions), so no refactor was made.

`cmd/pull`/`cmd/commit` help text (`helpText` constants) was left unchanged:
it was not named in the phase's "Contract and documents" section (only
`docs/slivingdoc-v1.md` §2 and `docs/running.md` were), and its existing
wording ("the category and message... the retryable verdict") remains true,
just not exhaustive of the new reason/next/read-only detail. Flagging for
Phase 5's documentation-coherence pass in case the maintainer wants it
tightened; not a gap against this phase's own contract.

`TestPullPrintsReport`'s assertions (`HasPrefix`/`Contains`, not an exact
match) are unaffected by the additive-only success trailer and needed no
edit despite the acceptance-criteria table listing it as "(fixture
updated)" — the anticipated update was a no-op once written, because the
test never pins the whole byte string. No README gap.

Verification commands run, all green:

- `go build ./...` — no output.
- `go vet ./...` — no output.
- `go run mvdan.cc/gofumpt@v0.11.0 -l .` — no output (nothing unformatted).
- `go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...` — no output.
- `go fix -diff ./...` — no output.
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group
  (`TestFileReasonWords`/`TestActionWording`), reviewed above, accepted.
- `go test ./internal/app/... -run 'TestReport|TestWriteSuccess|TestWriteError|TestFileReasonWords|TestActionWording' -v` — all pass.
- `go test ./cmd/... -run 'TestCommit|TestPull' -v` — all pass.
- `go test ./internal/integrationtest/... -run 'TestScenarioCLI' -v -timeout 300s` — all pass, including the new
  `TestScenarioCLIReadOnlyCommit` and the extended `TestScenarioCLIColourOnTerminal`
  (both subtests) against the real SeaweedFS-backed test container (Docker
  available in this environment).
- `make qa` (gofumpt, go vet, staticcheck, go fix -diff, `go test -race
  -count=3 -timeout=30s -coverpkg=./...`, `npm test --prefix npm/slivingdoc`)
  — all green; coverage 83.9% against the 70% floor.

Files changed this session: `internal/app/colour.go`, `internal/app/command.go`,
`internal/app/command_test.go`, `cmd/pull/pull.go`, `cmd/commit/commit.go`,
`cmd/commit/commit_test.go`, `internal/integrationtest/scenario_cli_test.go`,
`internal/integrationtest/scenario_colour_linux_test.go`,
`docs/slivingdoc-v1.md` (§2), `docs/running.md`, plus this phase file and
the README status board and session journal.

Every acceptance-criterion test and every invariant-table row test exists
and passed as cited above and in the table rows themselves. Phase 4 is
Complete; nothing outstanding for Phase 5 beyond the two documentation
observations noted above (help text wording, not a contract violation).

## Review findings (review 1, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). Gates re-run: `make qa` exit
0. Code read: `internal/app/command.go`, `colour.go`, `cmd/pull/pull.go`,
`cmd/commit/commit.go`, `TestScenarioCLIReadOnlyCommit`, and the PTY
colour scenario.

- [x] **R1-05 (minor).** `writeError` pads the path column with
  `width-len(f.Path)`, a byte count. A notebook path is UTF-8, so a file
  such as `docs/résumé.md` is padded short and its reason column drifts
  left of the other rows. Fix: measure paths with
  `utf8.RuneCountInString` in both `longestErrorFilePath` and the padding,
  and add a unit fixture with a multi-byte path to
  `TestWriteErrorAlignsPathColumn`. Resolved (fix 1): see implementation
  notes below.
- [x] **R1-06 (minor).** `docs/running.md` says the `read-only:` trailer
  appears "when the refusal touched one". The trailer appears on every
  success and error report whenever the set is non-empty, as the README
  skeleton and `TestScenarioCLIReadOnlyCommit` show. Fix the sentence.
  Resolved (fix 1): see implementation notes below.

Verified good:

- The plain report matches the README skeleton line for line at the real
  boundary (`TestScenarioCLIReadOnlyCommit` against a spawned process), and
  the coloured report strips to the same bytes; colour is gated on a real
  terminal and `NO_COLOR` (invariant 10).
- The CLI attaches the runtime's set to the mapped envelope exactly as the
  MCP handler does, so the CLI and MCP surfaces cannot drift (invariant 7).
- R1-07 (note, resolved in the README by this review): the README's
  skeleton illustration padded to a fixed column; it now follows the
  shipped longest-path-plus-two rule, which the phase specified and the
  fixtures prove.

### Implementation notes (Phase 4 fix, review 1)

Session: worklog-work executor, 2026-09-15.

Fixed R1-05 (minor): `internal/app/command.go`'s `longestErrorFilePath`
and `writeError`'s per-file padding both measured a path's length with
`len()` (bytes). `longestErrorFilePath` now returns
`utf8.RuneCountInString(f.Path)`'s maximum, and the padding call uses
`width-utf8.RuneCountInString(f.Path)` instead of `width-len(f.Path)`,
so a multi-byte path is padded to its rune count, matching every other
row in the report. `TestWriteErrorAlignsPathColumn`
(`internal/app/command_test.go`) gained a third file,
`docs/résumé.md` (14 runes, 16 bytes — same rune count as
`notes/today.md`, a longer byte count than any path in the fixture), and
the fixture's `want` string pins its `read-only`/reason column at the
same offset as the ASCII rows. Confirmed the fixture is discriminating:
reverting the two `command.go` lines to byte-based `len()` changes the
computed column width (18 instead of 16) and the test fails.

Fixed R1-06 (minor): `docs/running.md`'s domain-error paragraph said the
`read-only:` trailer appears "when the refusal touched one." That
implied the trailer is conditioned on the specific error touching a
read-only path, but per the README's "CLI report skeleton" section and
`internal/app/command.go`'s `writeError`/`writeSuccess`, the trailer is
unconditional on error kind and appears on every success and error
report whenever the configured set is non-empty. Reworded the sentence
in place (and only that sentence) to "...and a `read-only:` trailer
naming the configured read-only set whenever one is configured, on every
success and error report alike." No other wording in the paragraph or
document changed.

Verification commands run, all green:

- `go test ./internal/app/... -run 'TestWriteErrorAlignsPathColumn' -v` — pass.
- `go test ./internal/app/... -run 'TestWriteError|TestWriteSuccess|TestReport|TestFileReasonWords|TestActionWording' -v` — all pass (16 top-level/subtests).
- `make lint` (gofumpt, go vet, staticcheck, go fix) — clean, no output.
- `make test` (race, count=3, timeout=30s, `-coverpkg=./...`, real
  SeaweedFS-backed containers via Docker) — all packages pass; coverage
  84.0% against the 70% floor.
- `make npm-test` — 35/35 pass.
- `make qa` — exit 0 (lint + test + npm-test together).
- `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — one clone group
  (`internal/app/command_test.go:432,452` and `:457,477`, i.e.
  `TestFileReasonWords`/`TestActionWording`), the same pre-existing group
  already reviewed and accepted in the Phase 4 and Phase 5 sessions under
  the AGENTS.md table-driven-test idiom; the new `docs/résumé.md` fixture
  row did not create a new clone group.

Files changed this session: `internal/app/command.go`,
`internal/app/command_test.go`, `docs/running.md`, plus this phase file,
the README status board, feedback index, and session journal.

Phase 4 returns to Complete; both review-1 findings resolved and
verified.

## Review findings (review 2, 2026-09-15)

Reviewer: orchestrating session (Fable 5.1). R1-05 and R1-06 resolved and re-verified: `writeError` and `longestErrorFilePath` count runes, with a multi-byte fixture in `TestWriteErrorAlignsPathColumn`; the running.md sentence now states the trailer appears whenever a set is configured. No new finding.

## Review findings (review 3, 2026-09-15)

Reviewer: independent Opus agent (holistic review), verified and relayed by
the orchestrating session.

- [x] **R3-05 (minor).** `docs/running.md` no longer shows a generic domain
  error report: the paragraph introducing the report skeleton now shows the
  read-only refusal, and the identical block appears again in the
  "Read-only paths" section, while `CONTENT_CONFLICT` appears only in prose.
  Fix: restore a generic conflict example (the `CONTENT_CONFLICT ·
  MERGE_CONFLICT` shape from the README skeleton, byte-verified against
  `writeError`) in the report paragraph, and keep the read-only refusal
  example only in the "Read-only paths" section. Resolved (fix 3): see
  implementation notes below.

### Implementation notes (Phase 4 fix, review 3)

Session: worklog-work executor, 2026-09-15.

Fixed R3-05 (minor): `docs/running.md`'s report-skeleton paragraph (the
"Prints the unified result report" section) showed the read-only refusal
example, identical to the block already present in the "Read-only paths"
section below it, so the generic `CONTENT_CONFLICT` shape from the
README's own skeleton had disappeared from the doc entirely. Replaced the
paragraph's example with a single-file `CONTENT_CONFLICT · MERGE_CONFLICT`
block (message, one `conflict` file row with `lines` ranges, `next: edit
the files, then commit`, `retryable: false`, no `read-only:` trailer since
no read-only paths are configured in this example) and reworded the
sentence that introduced the old example ("A commit that touches a
read-only path is refused and the touched files are reset to the
published state:") to "A commit that conflicts with the remote reports
each file's reason and the line ranges to resolve:", since the old
sentence only made sense for the read-only example it used to introduce.
The read-only refusal block stays exactly where it already was, solely in
the "Read-only paths" section further down; nothing else in
`docs/running.md` changed.

The exact bytes (message, path padding, and line-range formatting) were
verified two ways rather than guessed from the README illustration alone:
(1) `TestWriteErrorAlignsPathColumn` (`internal/app/command_test.go`)
already pins `"  notes/today.md  conflict  lines 12-18, 40-42\n"` for
this same path/reason/range combination against real `writeError` output;
(2) a throwaway test (`internal/app/zzthrowaway_test.go`, written, run,
and deleted before finishing this session — never committed) called
`writeError` directly with `{Code: "CONTENT_CONFLICT", Reason:
"MERGE_CONFLICT", Action: "EDIT_FILES", Message: "Resolve the conflict
blocks before notes_commit.", Files: [{Path: "notes/today.md", Reason:
"TEXT_CONFLICT", Ranges: [{12,18},{40,42}]}]}` (the single-file case,
matching what the doc now shows) and printed the resulting bytes via
`t.Logf`, confirming the single-file column width (17-14=... width =
longest(14)+2=16, padding 2 spaces) matches the multi-file fixture's
padding for the same path, and that the trailer stops after `retryable:
false` with no `read-only:` line when `ReadOnly` is empty. Output:
`"CONTENT_CONFLICT · MERGE_CONFLICT\nResolve the conflict blocks before
notes_commit.\n  notes/today.md  conflict  lines 12-18, 40-42\nnext: edit
the files, then commit\nretryable: false\n"` — byte-identical to what was
written into `docs/running.md`.

Verification commands run, all green:

- `go test ./internal/app/... -run TestZZThrowawayGenericConflict -v`
  (throwaway, deleted after) — pass; logged bytes matched the doc edit
  exactly, then the file was removed (`git status --short internal/app/`
  confirmed no trace remained).
- `go test ./internal/app/... -run 'TestWriteError|TestWriteSuccess|TestReport|TestFileReasonWords|TestActionWording' -v` — all pass (existing fixtures unaffected by a docs-only change).
- `make lint` — clean, no output.
- `make npm-test` — 35/35 pass.
- `make qa` — exit 0; coverage held against the 70% floor (docs-only
  change, no coverage-relevant code touched).

Files changed this session: `docs/running.md`, plus this phase file and
the README status board, feedback index, and session journal.

Phase 4 returns to Complete; R3-05 resolved and verified. No code changed;
this was a docs-only fix.
