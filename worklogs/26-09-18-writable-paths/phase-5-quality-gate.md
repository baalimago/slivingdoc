# Phase 5 — Quality gate

**Status:** Complete

[← README](README.md)

## Goal

Prove the whole effort against the repository's own gates, unedited, and prove
that every claim the earlier phases made is backed by a test that exists and
asserts something.

## Specification

### The gates

The gates are the repository's, listed in the QA validation table of the agent
guide, and this phase runs them as written. No target, build tag, environment
variable, or flag runs a subset. No skip is added, and the timeout, count, and
race settings of the test command are not modified. The tool versions are the
pinned baseline recorded in that table, never a floating latest.

| Gate | Source of truth |
| --- | --- |
| Format, static analysis, vet, and the fix diff | The QA validation table in `AGENTS.md` |
| The single Go test command, including race, repeat count, timeout, and the coverage floor | The same table |
| The single npm test command | The same table |
| Duplication signal | The same table, which lists `dupl` as its own command; the `lint` target does not run it |
| The whole gate in one command | `make qa` |

Coverage is reported against the repository floor named in the README validation
policy. A figure below it fails the run; the achieved figure is recorded in
Implementation notes.

**The run of record is taken on an idle machine.** `make test` runs packages
concurrently and the timeout it sets is per package, so the budget is shared by
whatever runs beside a package rather than reserved for it. The root package's
`TestReleaseBinary` spends most of that budget on an in-suite `go build`; the
`Makefile` says so at its `$(BIN)` target, which exists to warm exactly that
build's compile cache. Under competing load the build outlasts the budget and the
root package panics on the timeout while every other package passes. This effort
lengthens the window it competes in, so a result is only recorded from a run with
nothing else of consequence on the machine. A timeout there is a scheduling
artefact, not a finding, and it is never answered by touching the timeout, the
count, or the race flag.

**An artefact is confirmed, never assumed.** The discriminator is the same
package alone under the same command, from the repository root:

```sh
go test . -race -count=3 -timeout=30s
```

Alone the package has the whole per-package budget instead of a share of it. A
root-package failure in the whole-suite run that passes here, while every other
package passed beside it, is the scheduling artefact this section describes, and
the whole-suite run is then repeated on an idle machine for the result of
record. A failure that reproduces here, or that reproduces on an idle machine,
is a real root-package regression: it is recorded as a finding against the phase
that owns the code, and the gate settings still do not move.

### Duplication verdicts

The duplication tool is a signal, not a verdict. Every clone it reports that
touches this effort's files is recorded in Implementation notes with a verdict
and the clause of the duplication policy that justifies it. One clone is expected
and acceptable in advance:

| Expected clone | Policy clause |
| --- | --- |
| The writable advertisement helpers mirroring the read-only ones in the same file | Thin wrappers over a shared helper, where both delegate to the one joining routine |

No clone is expected in `internal/git`: Phase 1 extends the `fakeRepository` that
`internal/git/fake_test.go` already declares rather than adding a second one. A
second repository fake in that package is a finding against Phase 1, not a clone
to excuse here — the fake-mirroring clause is about independent packages, and two
fakes in one package are not that.

A clone that is not one of these is assessed on its merits and either fixed or
recorded with the clause that excuses it. A clone recorded without a clause is a
defect.

### Evidence check

Every test name cited in a phase file must resolve to a declared test, and every
acceptance row must name one. The checks are repeatable from the worklog
directory and each one's passing condition is genuinely empty output.

A phase file has two halves and they are read differently. Everything above
`## Implementation notes` is **specification**: it claims evidence, and a
backticked `Test…` token there is a citation. Everything from that heading down —
Implementation notes and Review findings — is **record**: it quotes `-run`
patterns, names glob families, and adjudicates tokens, so a backticked `Test…`
token there is not necessarily a name. A check that cannot tell the two apart
reports its own record as a failure, which is what happened in review 1. The
split is therefore part of the check, not a concession in it.

The acceptance gate runs over the specification halves. It carries no exclusion
list and no tolerance:

```sh
for f in phase-*.md; do awk '/^## Implementation notes/{s=1} !s' "$f"; done \
  | grep -ohE '`Test[A-Za-z0-9_]+`' | tr -d '`' | sort -u | while read -r name; do
      grep -rqE "func ${name}\(" ../../internal ../../cmd ../../*.go || echo "MISSING ${name}"
    done
```

The completeness sweep runs over the record halves, so that a name cited in an
Implementation-notes table is checked too. It is prefix-tolerant, because a
`-run` argument and a glob family are prefixes of declared names rather than
names: a token passes when some declared test begins with it. Exactly one token
is excluded, and it is stated rather than discovered — the bare word Tests, the
English plural in the heading "Tests beyond the named ones", which no declared
name begins with. It is written unbackticked wherever this worklog discusses it,
so that discussing the exclusion does not create one:

```sh
for f in phase-*.md; do awk '/^## Implementation notes/{s=1} s' "$f"; done \
  | grep -ohE '`Test[A-Za-z0-9_]+\*?`' | tr -d '`*' | sort -u | grep -vx Tests \
  | while read -r name; do
      grep -rqE "func ${name}[A-Za-z0-9]*\(" ../../internal ../../cmd ../../*.go \
        || echo "MISSING ${name}"
    done
```

The distinct-name count of each is recorded in Implementation notes so a later
reader can tell the check ran against the whole set rather than a fragment.

The acceptance sweep proves the other direction — that no acceptance row rests on
a reviewer's word. It prints every acceptance row that names neither a test nor a
command. Header and separator rows are dropped by what they are rather than by
their column count, so a phase whose acceptance table carries an extra column
does not print:

```sh
awk '/^## Acceptance criteria/{f=1;next} /^## Error coverage/{f=0} f' phase-*.md \
  | grep '^| ' | grep -vE '\| *Proven by *\|' | grep -vE '^\|[ :|-]+$' \
  | grep -vE '`Test[A-Za-z0-9_]+`|`(grep|awk|make|git|go|dupl)|_test\.go'
```

The skip scan is the last of these checks. It proves no phase bought its green by
narrowing what runs. The `.` is escaped: unescaped it matches the `thSkip` inside
`TestRestoreProtectedUnknownPathSkipped` and the scan reports a test name as a
skip:

```sh
grep -rn -e 't\.Skip' -e 'testing\.Short()' \
  ../../internal/git/policy_test.go \
  ../../internal/integrationtest/scenario_writable_test.go \
  ../../internal/notebook/commit_test.go ../../internal/notebook/pull_test.go \
  ../../internal/app/config_test.go ../../internal/mcp/server_test.go \
  ../../internal/app/command_test.go
```

The scan is then widened to the whole tree, where a non-empty result is expected:
the pre-existing platform-capability skips. Every hit is attributed to a file
this effort does not touch, or it is a finding.

```sh
grep -rn -e 't\.Skip' -e 'testing\.Short()' ../../internal ../../cmd ../../*.go
```

### Maintenance contracts

The repository requires that a change touching an accepted invariant updates the
contract document in the same commit. Phases 3 and 4 each carry their own
document edits, so this phase verifies rather than performs them.

| Contract | Verified by |
| --- | --- |
| The accepted contract carries the writable subsection and the extended unchanged-process sentence | `grep -nE '^#+ .*[Ww]ritable' ../../docs/slivingdoc-v1.md`, then reading that subsection against Phase 4's specification |
| The accepted contract's configuration table carries the setting | `grep -n 'writable-paths' ../../docs/slivingdoc-v1.md` |
| The operator guide carries the setting | `grep -n 'writable-paths' ../../docs/running.md` |
| The agent guide carries the flag note and the composed invariant | `grep -n 'writable-paths' ../../AGENTS.md` |
| The help text carries the flag line, and it is the authoritative copy | Phase 3's acceptance row for the help text line |

A grep proves the text is present, not that it is right. Each row that greps is
read afterwards against the phase that owns it; the command exists so an absent
edit fails loudly rather than being missed by a tired reader.

### Readiness checklist

The README's readiness checklist is run here as well as by the author before
validation, and its outcome is recorded in Implementation notes. A line that
fails is a finding against the phase that owns the text, not a defect of this
phase.

## Integration contract

`unit-test-only`. This phase adds no behavior and therefore has no boundary of
its own. It runs the suites the earlier phases wrote, over the code they built.

## Acceptance criteria

| Outcome | Proven by |
| --- | --- |
| The whole gate passes unedited | `make qa` output recorded in Implementation notes |
| Coverage is at or above the repository floor | The `make test` coverage line, recorded in Implementation notes |
| The test command's race, repeat count, and timeout are unmodified | `git diff --stat ../../Makefile` empty for the test target, with the command line recorded in Implementation notes |
| No skip, short guard, or second gate command was introduced | An empty result from the `grep -rn -e 't\.Skip'` scan above over this effort's files, plus the widened tree scan whose every hit is attributed to a file this effort does not touch, both recorded in Implementation notes |
| Every test name cited in a phase file resolves to a declared test | The acceptance gate above: the `awk` slice of the specification halves piped into the `grep -rqE "func …"` loop, which carries no exclusion and prints nothing, with its distinct-name count recorded in Implementation notes |
| Every test name cited in an Implementation-notes or Review-findings table names or prefixes a declared test | The completeness sweep above: the `awk` slice of the record halves under the prefix-tolerant `grep -rqE`, one stated exclusion, printing nothing |
| Every acceptance row in every phase names a test or a repeatable command | An empty result from the acceptance sweep above, whose `awk` slice of the acceptance tables prints no unbacked row |
| Every duplication report touching this effort carries a verdict and a policy clause | `go run github.com/mibk/dupl@v1.0.0 -t 80 .` — the agent guide's QA-table command, not a `make lint` stage — with one verdict line per clone recorded in Implementation notes |
| Every maintenance contract above is satisfied | Each `grep -n` in the maintenance contract table, then a read of what it found |
| The readiness checklist passes | Its seven lines run from this directory, starting with its `grep -nE` numeral scan, recorded line by line in Implementation notes |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| A gate fails | The phase stays open; the failure is recorded as a finding against the owning phase rather than worked around | The gate's own output |
| Coverage falls below the floor | The test command fails on its own; missing coverage is added to the owning phase's tests, never to a test written only to raise the figure | The test command |
| A cited test name does not resolve | The evidence check names it; the owning phase either declares the test or drops the citation | The evidence-check command |
| A duplication report has no justifying clause | The clone is refactored rather than recorded | The duplication tool's output |
| A document required by a maintenance contract was not updated | A finding against the phase that owns the document, which updates it in its own commit | The contract table |
| The readiness checklist fails a line | A finding against the phase that owns the offending text | The checklist |

## Implementation notes

### 2026-09-18 — Phase 5 executed — Claude Opus 5 (1M context), [[worklog-work]]

One production-code change was made by this phase: none. One test-code change
was made, the duplication fix recorded under "Duplication verdicts" below.

#### The gate

`make qa` (lint, test, npm-test) run unedited from the repository root, twice:
once over the tree as Phases 1–4 left it, and once after the duplication fix.
Both exited zero. `git diff --stat Makefile` prints nothing, so the test
target's race, count, and timeout are the committed ones.

| Command | Result |
| --- | --- |
| `make qa` (before the duplication fix) | exit 0 |
| `make qa` (after the duplication fix, the run of record) | exit 0 |
| `make lint` → `gofumpt -l .` (`mvdan.cc/gofumpt@v0.11.0`) | no unformatted files |
| `make lint` → `go vet ./...` | clean |
| `make lint` → `staticcheck ./...` (`honnef.co/go/tools/cmd/staticcheck@v0.7.0`) | clean |
| `make lint` → `go fix -diff ./...` | printed nothing |
| `make test` → `go test -race -count=3 -timeout=30s -coverpkg=./... ./...` against the pinned libgit2 and one SeaweedFS container | 18 `ok` packages, 0 `FAIL` |
| `make test` coverage gate | `== coverage: 84.5% (floor 70%) ==` |
| `make npm-test` → `npm test --prefix npm/slivingdoc` | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | 3 clone groups before the fix, 2 after |

`make lint` does not run `dupl`; the agent guide's QA table lists it as its own
command, so it was run separately. The acceptance row that says "`make lint`
duplication output" is satisfied by that command's output, not by the `lint`
target. Recorded as P5-N2.

Coverage is 84.5 %, above the seventy percent floor and unchanged by the
duplication fix (84.5 % in both runs). The slowest package, `internal/integrationtest`,
finished in 16.6 s of the per-package budget.

#### Duplication verdicts

Three clone groups before the fix, each assessed against the duplication policy
in `AGENTS.md`:

| Clone | Introduced by | Verdict | Clause |
| --- | --- | --- | --- |
| `internal/integrationtest/scenario_writable_test.go:134,154` ↔ `:158,178` — `TestScenarioWritableCommitRefusesRootFile` and `TestScenarioWritableCommitRefusesNewDirectory` | Phase 2 | **Fixed**, not excused | Actionable: "two or more tests whose bodies differ only in parameterised values… extract a parameterised helper". The bodies differed only in the written path, its content, and the commit message |
| `internal/integrationtest/harness.go:470,482` ↔ `internal/mcp/server.go:209,221` — `pathSetText` and `successText` | Phase 4 | **Accepted** | Acceptable: "cross-package contract assertions". The black-box harness re-derives the expected success text independently; merging them would make the suite depend on a helper of the package it observes from outside, which is the clause's stated reason |
| `internal/app/command_test.go:424,444` ↔ `:447,467` — `TestFileReasonWords` and `TestActionWording` | Neither — pre-existing | **Accepted** | Acceptable: "table-driven test loops". Both functions sit at the same line numbers in `git show HEAD:internal/app/command_test.go`, so no phase of this effort touched them |

The fix keeps both cited test names, so Phase 2's acceptance evidence and this
phase's evidence check both still resolve. Both tests now call one
`assertAddedPathRefused(t, relPath, content, message)` helper local to the file.

The clone this phase expected in advance — "the writable advertisement helpers
mirroring the read-only ones in the same file" — did not appear. Phase 4 widened
the existing `successText` and `instructions` rather than adding parallel
writable helpers beside them, so there is nothing in that file to excuse. The
clone that did appear is the cross-package one above, which grew past the
threshold because Phase 4 took both copies from five lines to thirteen.

`internal/git` reported no clone, as the phase required.

#### Evidence check

`grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md | sort -u` yields **130** distinct
tokens. The specified loop printed eight lines rather than none:

```text
MISSING TestFlagsWritablePaths
MISSING TestHelpText
MISSING Tests
MISSING TestScenarioReadOnly
MISSING TestScenarioWritable
MISSING TestScenarioWritableFlag
MISSING TestScenarioWritableOverlap
MISSING TestSetup
```

None of the eight is a citation of a test name. Each was located and read:

| Token | Where | What it is |
| --- | --- | --- |
| `TestFlagsWritablePaths`, `TestSetup`, `TestHelpText` | `phase-3-flag-and-config.md:228` | Prefixes inside one `go test -run 'A\|B\|C'` alternation recorded in Phase 3's verification table |
| `TestScenarioWritableFlag`, `TestScenarioWritableOverlap` | `phase-3-flag-and-config.md:230` | The same, in the second recorded `-run` pattern |
| `TestScenarioWritable`, `TestScenarioReadOnly` | `phase-2-enforcement.md:240,264,273` | A glob-family reference (`` `TestScenarioWritable*` ``) and a third `-run` pattern |
| `Tests` | `phase-2-enforcement.md:211`, `phase-3-flag-and-config.md:164` | The English word, in the heading "**Tests beyond the named ones.**" |

Every token the phases write as a cited name is delimited by backticks. Reading
only those gives **122** distinct names, and every one resolves:

```sh
grep -ohE '`Test[A-Za-z0-9_]+`' phase-*.md | tr -d '`' | sort -u | while read -r name; do
  grep -rqE "func ${name}\(" ../../internal ../../cmd ../../*.go || echo "MISSING ${name}"
done
```

This printed nothing. 122 + 8 = 130, so the eight adjudicated non-citations are
exactly the difference between the two readings and no name was dropped
silently. The outcome the acceptance row names — every cited test name resolves
to a declared test — holds. The specified command's literal empty-output
condition does not, because the regex cannot tell a `-run` prefix in an
Implementation-notes table from a citation. Recorded as P5-N1.

The acceptance sweep printed one line:

```text
| Done | Outcome | Proven by |
```

That is `phase-1-path-policy.md:169`, the header row of Phase 1's acceptance
table, which carries a third `Done` column the sweep's `^\| (Outcome|-)` filter
does not anticipate. It is a header, not a row. No acceptance row in any phase
printed, so every one names a test or a repeatable command.

The skip scan printed two lines, both from
`internal/git/policy_test.go:763,765` — the comment and signature of
`TestRestoreProtectedUnknownPathSkipped`, matched because the pattern's
unescaped `.` makes `t.Skip` match the `thSkip` inside `…PathSkipped`. Escaping
it and widening the scan to the whole tree,
`grep -rn -e 't\.Skip' -e 'testing\.Short()' ../../internal ../../cmd ../../*.go`,
returns twenty hits, none in any file this effort touched: they are the
pre-existing platform-capability skips of `internal/workspace/scan_test.go`,
`materialize_test.go`, `native_test.go`, `internal/app/colour_test.go`,
`internal/integrationtest/scenario_colour_linux_test.go`, and
`scenario_validation_test.go`, each naming the capability the host cannot
provide, which the agent guide permits. No phase of this effort introduced a
skip, a short guard, or a second gate command.

#### Maintenance contracts

| Contract | Command | Result |
| --- | --- | --- |
| Writable subsection in the accepted contract | `grep -nE '^#+ .*[Ww]ritable' ../../docs/slivingdoc-v1.md` | `336:### Writable paths`. Read against Phase 4: it carries the composition rule, the unmatched default, exact overlap as a startup refusal, the enforcement restatement, and the full advertisement paragraph including the `writable`-first ordering and the `readOnly` meaning clause |
| The extended unchanged-process sentence | Same subsection's preceding paragraph, line 333 | "…byte-for-byte unchanged on every surface except the always-present empty `readOnly` and `writable` arrays" — extended as Phase 4 specifies |
| Configuration table in the accepted contract | `grep -n 'writable-paths' ../../docs/slivingdoc-v1.md` | `1467` (the settings table row), `1540` (the section 17 resolution paragraph), `1843` (invariant 38) |
| Operator guide | `grep -n 'writable-paths' ../../docs/running.md` | `108` (the settings table row), `237`, `246`, `253`, `270` (the prose, two worked examples, and the explicitly-empty idiom) |
| Agent guide | `grep -n 'writable-paths' ../../AGENTS.md` | `263` (the key-flags note on startup refusal and exact overlap), `365` (the composed invariant under "Invariants that a change must not break") |
| Help text, the authoritative copy | Phase 3's acceptance row | `internal/app/config.go:556` carries the `--writable-paths` line with its environment variable and default, beside the flag registration at `:102` |

#### Readiness checklist

| Line | Outcome |
| --- | --- |
| 1. No numerals with units outside oracle rows | Pass. One hit, `phase-3-flag-and-config.md:227` — "coverage 84.4 % (floor 70 %); `internal/integrationtest` 14.9 s of the 30 s budget". Every figure is a recorded gate result in Implementation notes, which is the oracle row the line exempts, not a tunable in specification prose |
| 2. Every test name declared in exactly one phase and one file list | Pass across the 122 cited names. Five tokens appear in two phase files: `Tests` (the English word) and four `TestScenarioWritable*` names that Phase 4 cites at `phase-4-advertisement.md:186–189`, inside its Implementation notes, recording the scenarios it updated per P4-N3. Phases 2 and 3 remain the sole declaring phases |
| 3. Every config field, flag, and injectable field has one owner row | Pass. Each field the Parameters table names exists under that exact name: `app.ServiceConfig.WritablePaths` (`internal/app/service.go:36`), `app.config.writablePaths` (`internal/app/config.go:45`), `notebook.Config.WritablePaths` (`internal/notebook/notebook.go:55`), `integrationtest.HarnessConfig.WritablePaths` (`internal/integrationtest/harness.go:65`). No field was introduced without a row |
| 4. Every invariant and limit is a table with a test per row | Pass, unchanged. Execution appended Implementation-notes sections and rewrote no specification section, so the structure validation round 1 passed is the structure that shipped |
| 5. Every phase mentioning listening, manual, or paid carries `Human required` | Pass. `grep -niE '\b(listening\|manual\|paid)\b' phase-*.md` prints nothing, and no phase carries the subsection, as expected |
| 6. No phase references text scheduled for deletion | Pass. Phase 1's disposition table is the only retention decision, and it retains rather than deletes |
| 7. New conventions do not contradict existing code conventions | Pass. `grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md \| grep '_'` prints nothing, and `grep -rhoE '^func Test[A-Za-z0-9_]+' ../../internal ../../cmd --include='*_test.go' \| grep -c '_'` prints `0`, so the underscore-free test-naming convention still holds on both sides |

#### Notes for the maintainer

| ID | Severity | Where | Note |
| --- | --- | --- | --- |
| P5-N1 | note | Phase 5 "Evidence check", skip scan, acceptance sweep | All three checks were authored against specification prose and now read the Implementation notes the phases appended, where they over-match: a `-run` alternation is not a citation, `t.Skip` with an unescaped dot matches `…PathSkipped`, and the sweep's header filter does not anticipate Phase 1's third `Done` column. Each was adjudicated above and the underlying outcome holds in every case. Either tighten the three commands (backtick-delimited names, `t\.Skip`, a filter that drops any header row), or record that their output is read rather than counted |
| P5-N2 | note | Phase 5 "The gates", the duplication acceptance row | The row says "`make lint` duplication output", but the `lint` target runs gofumpt, vet, staticcheck, and `go fix` only. The agent guide's QA table lists `dupl` as its own command, which is how it was run. Either add `dupl` to the `lint` target, or correct the row to name the command |
| P5-N3 | note | `phase-3-flag-and-config.md:228` | The recorded command is written with ellipses (`-run 'TestFlagsWritablePaths\|TestSetup...\|TestHelpText...'`), so the exact command is not recoverable from the record. Implementation notes are meant to be repeatable; either write the alternation out or state the prefixes as prefixes |

No finding against Phases 1–4's behavior surfaced. V1-13 (the downstream
consumer's repository and revision) is still open and touches nothing this phase
ran.

### 2026-09-19 — Phase 5 review-1 fixes — Claude Opus 5 (1M context), [[worklog-work]]

R1-03 and R1-02 closed; P5-N2 and P5-N3 closed with them. No production code
changed and no test changed. The whole gate was re-run from scratch rather than
carried over, because Phases 1 and 3 changed under it after the first sweep:
`internal/git/readonly.go` and `policy.go` now validate before they collapse,
`TestNewPolicyRejectsCollapsedOverlap` is new, `TestSetupRefusesBeforeEngineAndProbe`
gained rows, and both `docs/running.md` and `docs/slivingdoc-v1.md` were edited.
Every figure below is from this session's runs.

#### R1-03 — the evidence check made reproducible

The diagnosis in the finding is right and the fix follows it rather than working
around it. The three checks could not distinguish a phase's **specification**,
where a backticked `Test…` token is a citation, from its **record** — the
Implementation notes and Review findings — where the same token may be a `-run`
prefix, a glob stem, or a word under adjudication. Each check therefore read the
notes that the previous sweep had written and reported them as failures. That is
why the recorded arithmetic could not survive its own session: the count was
taken before the adjudication table was written and quoted after.

The Specification section now splits the two halves explicitly and the checks
read the half they were written for. Four commands, every one of them empty:

| Check | Slice | Tolerance | Output | Distinct names |
| --- | --- | --- | --- | --- |
| Acceptance gate | Specification halves, above `## Implementation notes` | None — exact `func NAME(` match, no exclusion list | empty | **109** |
| Completeness sweep | Record halves, from that heading down | Prefix-tolerant (`func NAME…(`), glob `*` stripped, one stated exclusion | empty | **130** |
| Acceptance sweep | The `awk` slice of every `## Acceptance criteria` table | Header dropped by matching `Proven by`, separator by its dashes | empty | 54 rows scanned |
| Skip scan | This effort's seven test files, `t\.Skip` escaped | None | empty | — |

The union of the two name sets is **134** distinct tokens, of which 133 resolve
and one is excluded: the bare word Tests, the English plural in the heading
"Tests beyond the named ones", which no declared test name begins with. The
exclusion is one token, stated in the Specification rather than discovered by
running the command, and this worklog writes that word unbackticked wherever it
discusses it so that discussing it does not create another instance — the trap
the last sweep fell into.

Two points are worth a later reader's attention.

The prefix tolerance is not a weakening of the acceptance gate. The gate is the
first row and it has no tolerance at all; the prefix rule belongs to the second
row, whose job is only to prove that a name written in a notes table is a real
name. It replaced the eight-token exclusion list the first draft of this fix
carried: seven of those eight — `TestSetup`, `TestHelpText`,
`TestFlagsWritablePaths`, and the four `TestScenario…` stems — are proper
prefixes of declared tests, so the rule absorbs them by what they are rather than
by naming them.

The checks caught this session's own edits. A first pass at the fix backticked
the excluded word in the Specification and wrote an acceptance row naming no
command; re-running the four checks printed `MISSING Tests` and the offending
row. Both were fixed and the checks re-run to empty. The mechanism the finding
describes is therefore live, not merely documented.

Counts that superseded review-1's figures: the review reported 132 distinct
backticked tokens over whole files. That reading is gone — nothing now scans a
whole file — and the figures of record are the 109, 130 and 134 above, taken
against the tree as it stands after Phases 1 and 3's review-1 fixes and after
this session's edits to this file and to `phase-3-flag-and-config.md`.

The widened skip scan returns **20** hits, none in a file this effort touches:
`internal/workspace/scan_test.go` (9), `internal/integrationtest/scenario_colour_linux_test.go` (6),
`internal/workspace/materialize_test.go` (2), and one each in
`internal/workspace/native_test.go`, `internal/app/colour_test.go`, and
`internal/integrationtest/scenario_validation_test.go` — the pre-existing
platform-capability skips the agent guide permits. No phase of this effort
introduced a skip, a short guard, or a second gate command.

#### R1-02 — the gate's load sensitivity, recorded

Recorded in the Specification under "The gates", where a later runner sees it
before running rather than after failing, and phrased against the `Makefile`'s
own `$(BIN)` comment rather than restating its numbers. Nothing in the gate
moved: `git diff --stat ../../Makefile` is empty, and the test command is the
committed `go test -race -count=3 -timeout=30s -coverpkg=./... ./...`.

This session's runs were taken with nothing else of consequence on the machine
and none of them hit the timeout. The precondition is recorded as a precondition,
not as an excuse: a timeout in the root package under load stays a scheduling
artefact only while every other package passes and the package passes alone.

#### The gate, re-run from scratch

| Command | Result |
| --- | --- |
| `make qa` | exit 0 |
| `make lint`, standalone | exit 0; gofumpt listed no file, `go vet ./...` clean, `staticcheck ./...` (v0.7.0) clean, `go fix -diff ./...` printed nothing |
| `make test`, standalone | exit 0; 18 `ok` packages, 0 `FAIL` |
| `make test` coverage gate | `== coverage: 84.5% (floor 70%) ==` |
| `make npm-test`, standalone | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` |
| `git diff --stat ../../Makefile` | empty |
| `go tool cover -func=.build/cover.out` | total **84.5 %**; every exported function of `internal/git/policy.go` at 100 %, the file's lowest branch `collectTree` at 85.7 % |

Coverage is 84.5 %, unchanged by Phases 1 and 3's review-1 fixes. Package times
on the idle run: `internal/integrationtest` 15.5 s of the per-package budget,
`internal/git` 13.6 s, the root package 5.4 s.

#### Duplication verdicts, re-derived

`dupl -t 80` reports the same two groups the first sweep left, and no new one:

| Clone | Verdict | Clause |
| --- | --- | --- |
| `internal/integrationtest/harness.go:470,482` ↔ `internal/mcp/server.go:209,221` — `pathSetText` and `successText` | **Accepted**, unchanged | Acceptable: "cross-package contract assertions". The black-box harness re-derives the expected success text independently; merging them would make the suite depend on a helper of the package it observes from outside |
| `internal/app/command_test.go:424,444` ↔ `:447,467` — `TestFileReasonWords` and `TestActionWording` | **Accepted**, unchanged | Acceptable: "table-driven test loops", and pre-existing: both sit at the same line numbers in `git show HEAD:internal/app/command_test.go` |

The Phase 2 clone the first sweep fixed has not returned, and `internal/git`
reports none — including after Phase 1's review-1 fix split `validateEntries`
and `collapseEntries` out of `NewPolicy`, which is the one place a new clone in
that package could plausibly have appeared.

#### Maintenance contracts, re-verified

Every grep re-run after Phases 1 and 3 edited both documents:

| Contract | Result |
| --- | --- |
| Writable subsection in the accepted contract | `336:### Writable paths`, read against Phase 4: composition rule, unmatched default, exact overlap as a startup refusal, the enforcement restatement, and the advertisement paragraph |
| Configuration table in the accepted contract | `writable-paths` at `1469` (settings table), `1542` (section 17 resolution), `1845` (invariant 38) — the review-1 fix added the written-entry rule and shifted these two lines from their earlier positions |
| Operator guide | `108`, `237`, `246`, `253`, `277` — the last one is the explicitly-empty idiom; `253` is the composed example whose inversion paragraph R1-04 added |
| Agent guide | `263` (key-flags note), `365` (the composed invariant) |
| Help text, the authoritative copy | `internal/app/config.go:556`, beside the flag registration at `:102` and the overlap message at `:291` |

#### Readiness checklist, re-run

| Line | Outcome |
| --- | --- |
| 1. No numerals with units outside oracle rows | Pass. Twelve hits, every one below its file's `## Implementation notes` heading — four in Phase 3's verification and review tables, eight in this phase's notes and review section. No specification prose in any phase carries a numeral with a unit; the paragraph added for R1-02 was written without one deliberately |
| 2. Every test name declared in exactly one phase and one file list | Pass. Of the 109 names the acceptance gate reads, exactly one appears in two phase specifications: `TestRestoreProtectedUnknownPathSkipped`, declared and cited by Phase 1 and named by this phase only as the string an unescaped `.` over-matched. Phase 1 stays its sole declaring phase. Every name across both slices resolves in exactly one file |
| 3. Every config field, flag, and injectable field has one owner row | Pass, re-checked: `app.ServiceConfig.WritablePaths`, `app.config.writablePaths`, `notebook.Config.WritablePaths`, `integrationtest.HarnessConfig.WritablePaths`. Phase 1's review-1 fix added no field — `validateEntries` and `collapseEntries` are unexported functions, not configuration |
| 4. Every invariant and limit is a table with a test per row | Pass. This session appended notes and rewrote one Specification subsection of this phase only; no invariant or limit table in any phase changed shape |
| 5. Every phase mentioning listening, manual, or paid carries `Human required` | Pass. The only hit is this phase's own record of the checklist line quoting the words. No phase carries the subsection, as expected |
| 6. No phase references text scheduled for deletion | Pass, unchanged. Phase 1's disposition table retains rather than deletes |
| 7. New conventions do not contradict existing code conventions | Pass. `grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md \| grep '_'` prints nothing, and the repository-side count prints `0` |

#### Notes for the maintainer, updated

| ID | Status |
| --- | --- |
| P5-N1 | **Closed.** All three checks tightened and a fourth added; every one empty against the tree as it stands. The note's second option — "record that their output is read rather than counted" — was not taken: the output is now genuinely empty and counted |
| P5-N2 | **Closed** by correcting the row, not by changing the `Makefile`. The duplication acceptance row and the gates table now name `go run github.com/mibk/dupl@v1.0.0 -t 80 .` and state that `lint` does not run it. Adding `dupl` to the `lint` target is a repository-wide change this worklog has no mandate to make; it is left to the maintainer |
| P5-N3 | **Closed.** `phase-3-flag-and-config.md` now records the alternation written out, states that Go's `-run` matches unanchored so each alternative is a prefix, names the eleven tests it selects, and carries a fresh result from a re-run |

V1-13 (the downstream consumer's repository and revision) is still open and
touches nothing this phase ran. R1-05, R1-06 and R1-08 remain open notes against
Phases 2 and 4 and do not block this phase.

### 2026-09-19 — Phase 5 review-2 gate sweep — Claude Opus 5 (1M context), [[worklog-work]]

R2-04 closed. No production code and no test changed. The whole gate was re-run
from scratch rather than carried over, because the tree moved under the last
sweep: the review-2 fix passes to Phases 1, 2 and 4 landed thirteen tests, a
resolution change in `internal/git`, a rewritten composed advertisement in
`internal/mcp` and `internal/app`, and edits to both documents. `dupl` had not
been run against any of it. Every figure below is from this session's runs, and
the figures of the two earlier sweeps are superseded rather than amended.

#### R2-04 — the discriminator, recorded

The note is right that the section carried the excuse and not the test. "The
gates" now prescribes the single-package re-run — `go test . -race -count=3
-timeout=30s` from the repository root — as the thing that turns a root-package
timeout into either an artefact or a finding, and says which way each outcome
falls. The command moves no gate setting: it is the committed race, count and
timeout applied to one package instead of to the whole tree, which is the only
difference that matters, since the budget is per package and alone the package
has all of it.

This session exercised the discriminator rather than only writing it down, and
the run of record is the closest observation yet to the fragility R1-02 names.
In `make qa` the root package took **29.172 s** of its 30 s budget — it passed,
but with the thinnest margin any sweep has recorded. The discriminator run then
returned `ok github.com/baalimago/slivingdoc 3.071s`, and the standalone
`make test` put the same package at 7.020 s. The ratio is the whole argument:
the package is not slow, it is starved, and this effort's thirteen new tests
lengthen the window it is starved in. A later runner who meets the timeout now
has the procedure in the section rather than an incidental number in a table.

#### The gate, re-run from scratch

| Command | Result |
| --- | --- |
| `make qa` | exit 0 on the first attempt |
| `make lint`, standalone | exit 0; gofumpt listed no file, `go vet ./...` clean, `staticcheck ./...` (v0.7.0) clean, `go fix -diff ./...` printed nothing |
| `make test`, standalone | exit 0; 18 `ok` packages, 0 `FAIL` |
| `make test` coverage gate | `== coverage: 84.6% (floor 70%) ==` |
| `make npm-test`, standalone | `tests 35, pass 35, fail 0, skipped 0`, todo 0 |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 1 clone groups` — one fewer than either earlier sweep; see below |
| `go test . -race -count=3 -timeout=30s` (the R2-04 discriminator) | `ok github.com/baalimago/slivingdoc 3.071s` |
| `git diff --stat ../../Makefile` | empty |
| `go tool cover -func=.build/cover.out` | total **84.6 %**; every exported function of `internal/git/policy.go` at 100 %, the file's lowest branch `collectTree` at 85.7 % |

Coverage is **84.6 %**, up from the 84.5 % of both earlier sweeps as the
thirteen tests of the review-2 fix passes landed. It is above the seventy
percent floor. Package times on the standalone run: `internal/integrationtest`
20.3 s of the per-package budget, `internal/git` 18.2 s, `internal/notebook`
15.6 s, the root package 7.0 s.

#### Duplication verdicts, re-derived

`dupl -t 80` reports **one** clone group, where both earlier sweeps reported
two. Nothing was refactored to achieve that; a clone stopped matching. All
three candidates are adjudicated here, including the one the tool no longer
reports and the one Phase 4 predicted, because a verdict that rests on absence
must say why the absence is honest.

| Clone | Introduced by | Verdict | Clause |
| --- | --- | --- | --- |
| `internal/app/command_test.go:424,444` ↔ `:447,467` — `TestFileReasonWords` and `TestActionWording` | Neither — pre-existing | **Accepted**, unchanged | Acceptable: "table-driven test loops". Both functions still sit at the same line numbers in `git show HEAD:internal/app/command_test.go`, byte-for-byte, so no phase of this effort touched them |
| `internal/integrationtest/harness.go` `pathSetText` ↔ `internal/mcp/server.go` `successText` | Phase 4 | **No longer reported**; the duplication it excused is still deliberate | Acceptable: "cross-package contract assertions". Phase 4's review-2 fix appended the nest rule to both, and the two took it from different places — the server from `notebook.PathSetsNestRule`, the harness from a literal written out so the black-box oracle stays an independent expectation. That divergence sits in the middle of the shared token run and splits it into two fragments, each below the threshold. The clause did not stop applying; the tool stopped seeing it, and for the same reason the clause gives |
| `writePathSets` (`internal/app/command.go:216`) ↔ the trailer block of `errorText` (`internal/mcp/server.go:297`) | Phase 4 | **Accepted** — not reported, and correct on the merits | Acceptable: "thin wrappers over a shared helper". Phase 4 predicted this one; it does not reach the threshold, and it should not be merged if it ever does. The two render the same three-part contract on two surfaces, but what must never drift between them is already one shared thing — `notebook.ReadOnlyListSeparator` and `notebook.PathSetsNestRule` — and what remains is each surface's own formatting: a `strings.Builder` taking dim-painted, newline-terminated report trailers through a `painter`, against `fmt.Fprintf` appending newline-prefixed plain-text trailers to an error body. Merging them would make the CLI renderer of `internal/app` depend on the MCP text builder or the reverse, which is the coupling the clause exists to prevent |

`internal/git` reports no clone, as the phase requires — including after Phase
1's review-2 fix rewrote `collapseEntries` to take the other set and `covering`
to resolve the longest entry, which is the one place in that package where a new
clone could plausibly have appeared.

#### Evidence check, re-derived

All four checks were re-run from this directory against the tree as it stands,
and all four print nothing.

| Check | Slice | Tolerance | Output | Distinct names |
| --- | --- | --- | --- | --- |
| Acceptance gate | Specification halves, above `## Implementation notes` | None — exact `func NAME(` match, no exclusion list | empty | **124** |
| Completeness sweep | Record halves, from that heading down | Prefix-tolerant (`func NAME…(`), glob `*` stripped, one stated exclusion | empty | **147** checked, **148** before the exclusion |
| Acceptance sweep | The `awk` slice of every `## Acceptance criteria` table | Header dropped by matching `Proven by`, separator by its dashes | empty | 74 rows scanned |
| Skip scan | This effort's seven test files, `t\.Skip` escaped | None | empty | — |

The union of the two name sets is **152** distinct tokens, of which 151 are
checked and one is excluded: the bare word Tests, the English plural in the
heading "Tests beyond the named ones", which no declared test name begins with.
Each count is stated twice where the exclusion could move it, because the
earlier sweeps recorded a single number without saying which side of the
exclusion it fell on, and a later reader could not reproduce the arithmetic
without guessing. The rule for this record: the acceptance gate's number is the
tokens it checks, and the completeness sweep's two numbers are the tokens it
checks and the tokens it read.

Superseded figures: 109 and 130 (the review-1 fix sweep), and 111 and 133 (the
Phase 1 review-2 pass, recorded in the README session journal). The acceptance
gate moved 111 → 124 and the completeness sweep 133 → 148 as the thirteen new
tests were cited in the phases that own them and as three fix passes appended
Implementation-notes and Review-findings tables. All thirteen were located in
exactly one file each: the five `TestScenarioWritable…` names in
`internal/integrationtest/scenario_writable_test.go`,
`TestCommitInvalidContentRefusedBeforeTreeBuild`,
`TestCommitTreeBuildWriteFailureIsInvalidContent` and
`TestReadOnlyRefusalSetMatchesPolicyEntries` in
`internal/notebook/commit_test.go`, `TestServiceNestedEntriesSurviveRebuild` in
`internal/app/readonly_test.go`, `TestAdvertisementNestedSetsStateRule` and
`TestErrorTextCarriesBothSets` in `internal/mcp/server_test.go`, and
`TestReportNestedSetsStateRule` in `internal/app/command_test.go`.

The split between a phase's specification half and its record half continues to
do the work R1-03 asked of it. Three fix passes appended notes citing test names
since the last sweep, the acceptance gate's tolerance is still nil, and it still
prints nothing.

The widened skip scan returns **20** hits, the same twenty as the last sweep and
none in a file this effort touches: `internal/workspace/scan_test.go` (9),
`internal/integrationtest/scenario_colour_linux_test.go` (6),
`internal/workspace/materialize_test.go` (2), and one each in
`internal/workspace/native_test.go`, `internal/app/colour_test.go`, and
`internal/integrationtest/scenario_validation_test.go` — the pre-existing
platform-capability skips the agent guide permits. Cross-checked against
`git status`: not one of those six files is in the modified or untracked set, so
no phase of this effort introduced a skip, a short guard, or a second gate
command.

#### Maintenance contracts, re-verified

Every line number of record moved, because Phase 1's and Phase 4's review-2
passes both added prose to `docs/slivingdoc-v1.md` above the citations. Every
grep was re-run and every subsection re-read rather than re-cited.

| Contract | Result |
| --- | --- |
| Writable subsection in the accepted contract | `346:### Writable paths`, up from 336. Read against Phase 4: the composition rule, the unmatched default, exact overlap as a startup refusal over the written entries, the written-entry *resolution* rule and the rebuild fixed point that Phase 1's review-2 pass added, the enforcement restatement, and the advertisement paragraph with the `writable`-first ordering, the `readOnly` meaning clause, and the composed both-sets form Phase 4's review-2 pass added — "the longest matching entry decides" replacing "write elsewhere", the composed success text item, the error text item's writable trailer above the read-only one, and the report's `path-rule:` trailer |
| The extended unchanged-process sentence | Line 343, up from 333: "…byte-for-byte unchanged on every surface except the always-present empty `readOnly` and `writable` arrays" — as Phase 4 specifies, and the invariant-4 promise it carries is untouched by the review-2 work |
| Configuration table in the accepted contract | `writable-paths` at `1501` (settings table), `1574` (the section 17 resolution paragraph), `1877` (invariant 38) — up from 1469, 1542 and 1845 |
| Operator guide | `110`, `239`, `248`, `255`, `277`, `302` — up from 108, 237, 246, 253 and 277. Two citations are new since the last sweep: `277` is now the three-level worked example R2-01 motivated (`--read-only-paths notes,notes/agent-a/locked --writable-paths notes/agent-a`), and the explicitly-empty idiom moved down to `302` |
| Agent guide | `263` (key-flags note on startup refusal and exact overlap), `365` (the composed invariant, now phrased "over `--read-only-paths` and `--writable-paths` as the operator wrote") — both unmoved |
| Help text, the authoritative copy | `internal/app/config.go:560`, beside the flag registration at `:102` and the overlap message at `:295` |

#### Readiness checklist, re-run

| Line | Outcome |
| --- | --- |
| 1. No numerals with units outside oracle rows | Pass. Fifteen hits, up from twelve — four in Phase 3's tables and eleven in this phase's notes and review sections. Every one was checked against its file's `## Implementation notes` line and every one falls below it, so no specification prose in any phase carries a numeral with a unit. The R2-04 paragraph added this session was written without one deliberately, and its `-timeout=30s` is exempt by the line's own `[^=]` guard |
| 2. Every test name declared in exactly one phase and one file list | Pass across the 124 names the acceptance gate reads. Exactly one appears in two phase specifications, unchanged from the last sweep: `TestRestoreProtectedUnknownPathSkipped`, declared and cited by Phase 1 and named by this phase only as the string an unescaped `.` over-matched. Each of the 124 resolves in exactly one file, checked by counting declaring files per name rather than by reading |
| 3. Every config field, flag, and injectable field has one owner row | Pass, re-checked at their current lines: `app.ServiceConfig.WritablePaths` (`internal/app/service.go:36`), `app.config.writablePaths` (`internal/app/config.go:45`), `notebook.Config.WritablePaths` (`internal/notebook/notebook.go:55`), `integrationtest.HarnessConfig.WritablePaths` (`internal/integrationtest/harness.go:65`). The three review-2 fix passes added no configuration field — Phase 1 changed two unexported function signatures, Phase 2 added tests, Phase 4 added wording |
| 4. Every invariant and limit is a table with a test per row | Pass. The review-2 passes added rows to existing tables (Phase 2's invariant, integration-contract and acceptance tables; Phase 4's set-state table, split into a disjoint and a nested case) and turned no table into prose. This session appended notes and added one Specification paragraph to this phase only |
| 5. Every phase mentioning listening, manual, or paid carries `Human required` | Pass. The only hits are this phase's own records of the checklist line quoting the words. No phase carries the subsection, as expected |
| 6. No phase references text scheduled for deletion | Pass, unchanged. Phase 1's disposition table retains rather than deletes |
| 7. New conventions do not contradict existing code conventions | Pass. `grep -ohE '\bTest[A-Za-z0-9_]+' phase-*.md \| grep '_'` prints nothing, and the repository-side count prints `0`, so the underscore-free convention still holds on both sides across the thirteen new names |

#### Notes for the maintainer, updated

| ID | Status |
| --- | --- |
| P5-N1, P5-N2, P5-N3 | **Closed**, unchanged. Nothing in this sweep reopened them; all four checks are still literally empty |
| R2-04 | **Closed.** The discriminator is in "The gates" and was exercised rather than only written |

One thing the maintainer may want to know, recorded rather than filed as a
finding because no gate failed and no contract is unmet: the accepted
cross-package clone between the harness and the server is no longer reported by
`dupl`, so from this sweep onward the tool's own output no longer corroborates
that verdict. The duplication is still there and still deliberate; only its
token run is now broken in the middle. If the harness is ever changed to take
the rule from `notebook.PathSetsNestRule` rather than from its literal, the
clone returns and the clause above applies to it again.

V1-13 (the downstream consumer's repository and revision) is still open and
touches nothing this phase ran. Every finding against Phases 1–4 is closed, and
no new one surfaced in this sweep.

## Review findings

### Review 1 — 2026-09-19 — status `Reopened (review 1)`

**R1-03 — defect — this phase's "Evidence check" and its acceptance row "Every
test name cited in a phase file resolves to a declared test".**
The recorded evidence is self-invalidating and its numbers no longer hold. Phase 5
adjudicated the eight over-matching tokens by writing them into its own
Implementation notes **backticked** (`phase-5-quality-gate.md`, the token table),
so the refined command it recommends now collects them too. Re-run from the
worklog directory during this review:

```sh
grep -ohE '`Test[A-Za-z0-9_]+`' phase-*.md | tr -d '`' | sort -u | while read -r name; do
  grep -rqE "func ${name}\(" ../../internal ../../cmd ../../*.go || echo "MISSING ${name}"
done
```

prints eight `MISSING` lines — `TestFlagsWritablePaths`, `TestHelpText`, `Tests`,
`TestScenarioReadOnly`, `TestScenarioWritable`, `TestScenarioWritableFlag`,
`TestScenarioWritableOverlap`, `TestSetup` — and the distinct-name count is
**132**, not the recorded 122, so "122 + 8 = 130, and no name was dropped
silently" is arithmetic against a file state that no longer exists. The
underlying outcome still holds — every token a phase writes as a citation
resolves to exactly one declared test — but the phase's acceptance row is proven
by a command that does not pass and by counts that cannot be reproduced, which is
the failure mode the evidence check was written to prevent. P5-N1 names the
over-matching but not that the notes themselves now feed it.

- [x] Make the check immune to its own record — filter tokens that are followed
      by `*`, exclude the Implementation-notes sections, or keep a checked-in
      list of citations — then re-run it and record the true count.
- [x] Re-run the acceptance sweep and the skip scan and record their current
      output. Both were reproduced unchanged during this review: the sweep prints
      only Phase 1's three-column header row, and the skip scan prints only the
      two `…PathSkipped` lines at `internal/git/policy_test.go:763,765`.

**R1-02 — note — this phase's "The gate" table.**
`make qa` is load-sensitive and the record does not say so. Two consecutive runs
during this review exited 2, both with
`FAIL github.com/baalimago/slivingdoc 30.2s / 30.5s — panic: test timed out after
30s` inside `TestReleaseBinary`'s in-suite `go build` (`release_test.go:427`), a
file this effort does not touch; `internal/integrationtest` ran 30.4 s and 32.1 s
in those runs against the 14.9 s it takes alone. A third run on an idle machine
passed clean: 18 `ok` packages, `== coverage: 84.5% (floor 70%) ==`,
`internal/integrationtest` 14.9 s. The `Makefile` already documents the cause —
the root package's in-suite release build shares the 30 s per-package budget —
and this effort's new tests lengthen the concurrency window it competes with. The
gate is green; the record should say under what conditions.

- [x] Record the idle-machine precondition beside the gate result, or cite the
      `Makefile` comment that explains it.

### Verified good — every gate re-run independently

| Command | Result |
| --- | --- |
| `make lint` (inside `make qa`, twice) | clean: gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` (third run, idle machine) | 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==` |
| `make npm-test` | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — exactly the two the phase records as accepted: `internal/integrationtest/harness.go:470,482` ↔ `internal/mcp/server.go:209,221`, and the pre-existing `internal/app/command_test.go:424,444` ↔ `:447,467`. The Phase 2 clone the phase fixed is gone, and `internal/git` reports none |
| `git diff --stat Makefile` | empty — no gate setting moved |
| `go tool cover -func=.build/cover.out` | total 84.5 %; `internal/git/policy.go` exported functions all 100 % |
| Acceptance sweep | prints Phase 1's header row only, as recorded |
| Skip scan | prints `internal/git/policy_test.go:763,765` only, as recorded |
| Readiness checklist line 7 | The phase-side token scan piped into `grep '_'` prints nothing; the repository-side count prints `0` |

The duplication verdicts are sound. The harness/server pair is a genuine
cross-package contract assertion — merging it would make the black-box suite
depend on a helper of the package it observes from outside — and the
`internal/app` pair sits at the same line numbers in `git show
HEAD:internal/app/command_test.go`, so no phase of this effort touched it. The
Phase 2 fix kept both test names, so the evidence citations still resolve.

**P5-N2 and P5-N3 are both real and both trivially closable**: `make lint` does
not run `dupl` (the agent guide lists it separately, which is how it was run and
how this review ran it), and the ellipsised `-run` alternation recorded at
`phase-3-flag-and-config.md:228` is not a repeatable command.

### Review 2 — 2026-09-19 — status `Complete`

**R1-03 is closed and the closure reproduces.** All four checks were re-run from
this directory against the tree as it now stands, unedited, and all four print
nothing:

| Check | Result |
| --- | --- |
| Acceptance gate over the specification halves | empty; 109 distinct names, as recorded |
| Completeness sweep over the record halves | empty; 130 distinct names, as recorded |
| Acceptance sweep | empty — the header row that leaked in review 1 is gone, dropped by the `Proven by` filter |
| Skip scan over this effort's files | empty — the escaped `.` no longer matches `…PathSkipped` |
| Widened skip scan over `internal`, `cmd` and the root | 20 hits, every one in a file this effort does not touch: `internal/app/colour_test.go`, `internal/integrationtest/scenario_validation_test.go`, `internal/integrationtest/scenario_colour_linux_test.go`, `internal/workspace/{scan,materialize,native}_test.go` — all platform-capability skips, none in the modified set of `git status` |

The split between a phase's specification half and its record half is the right
fix and it survives its own notes: this review's findings added backticked
`Test…` tokens to four Review-findings sections and the acceptance gate did not
move, because those sections sit below `## Implementation notes`.

**R1-02's record is adequate, with one gap (R2-04).** The precondition is stated
where the gate is specified rather than buried in a result, it is justified
against the `Makefile`'s own `$(BIN)` comment rather than by restating a number,
and it forbids the wrong fix explicitly ("never answered by touching the
timeout, the count, or the race flag"). `git diff --stat Makefile` is empty, so
nothing moved. This review's own `make qa` exited 0 on the first attempt, which
is a fourth observation consistent with the record.

**R2-04 — note — this phase's "The gates" section.**
The record states the precondition but names no discriminator, so a later reader
who meets the failure has the excuse and not the test. The notes already contain
the right one — the root package alone under the same flags is `ok` in about
three seconds — but it appears only as an incidental result, not as the
procedure the section prescribes.

- [x] State in "The gates" that a root-package timeout is confirmed as a
      scheduling artefact by re-running that package alone under the same
      command, and that a failure which reproduces on an idle machine is a
      finding rather than an artefact.

**P5-N1, P5-N2 and P5-N3 stay closed**; nothing in this review reopened them.

### Verified good (review 2)

Every gate re-run from the repository root, independently of the notes:

| Command | Result |
| --- | --- |
| `make qa` | exit 0 on the first attempt |
| `make lint` (inside `make qa`) | gofumpt listed no file, `go vet` clean, staticcheck v0.7.0 clean, `go fix -diff` silent |
| `make test` (inside `make qa`) | 18 `ok` packages, 0 `FAIL`, `== coverage: 84.5% (floor 70%) ==`; slowest `internal/integrationtest` 18.2 s |
| `make npm-test` (inside `make qa`) | `tests 35, pass 35, fail 0, skipped 0` |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | `Found total 2 clone groups` — `internal/integrationtest/harness.go:470,482` ↔ `internal/mcp/server.go:209,221` and the pre-existing `internal/app/command_test.go:424,444` ↔ `:447,467`; both carry their clause, neither is in `internal/git` |
| `git diff --stat Makefile` | empty |
| Readiness checklist line 1 | hits only inside Implementation-notes oracle rows, as the line exempts |
| Readiness checklist line 7, both halves | phase-side token scan prints nothing; repository-side count prints `0` |

The gates are green and they are green honestly. They are not the verdict:
R2-01 is a configuration the whole suite accepts and an operator would not.
